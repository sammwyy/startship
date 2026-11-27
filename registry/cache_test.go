package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func parseTest(s []byte) (string, error) {
	var value string
	err := json.Unmarshal(s, &value)
	return value, err
}

func TestCacheRefresh(t *testing.T) {
	var requests atomic.Int32
	published := make(chan struct{}, 1)
	var response atomic.Value
	response.Store(`"first"`)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		body := response.Load().(string)
		if body == "offline" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
		fmt.Fprint(w, body)
	}))
	defer upstream.Close()
	path := filepath.Join(t.TempDir(), "catalog.json")
	cache := NewCache(path, upstream.URL, "bundled", parseTest, func() { published <- struct{}{} })
	check := func(c *Cache[string], want, source string) {
		t.Helper()
		got, meta := c.Snapshot()
		if got != want || meta.Source != source || (meta.UpdatedAt == nil) != (source == "bundled") {
			t.Fatalf("got %q %+v, want %q from %q", got, meta, want, source)
		}
	}
	check(cache, "bundled", "bundled")
	if requests.Load() != 0 {
		t.Fatal("fetched on startup")
	}
	cache.Ensure()
	wait := func(c *Cache[string]) {
		t.Helper()
		c.mu.Lock()
		call := c.active
		c.mu.Unlock()
		if call == nil {
			return
		}
		select {
		case <-call.done:
			if call.err != nil {
				t.Fatal(call.err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("refresh did not finish")
		}
	}
	wait(cache)
	select {
	case <-published:
	case <-time.After(5 * time.Second):
		t.Fatal("refresh was not published")
	}
	check(cache, "first", "remote")
	reopened := NewCache(path, upstream.URL, "bundled", parseTest, nil)
	check(reopened, "first", "remote")
	reopened.Ensure()
	wait(reopened)
	if requests.Load() != 1 {
		t.Fatal("fresh catalog fetched again")
	}
	response.Store(`"second"`)
	reopened.saved.UpdatedAt = reopened.saved.UpdatedAt.Add(-FreshFor)
	reopened.Ensure()
	wait(reopened)
	check(reopened, "second", "remote")
	_, before := reopened.Snapshot()
	for _, body := range []string{"offline", "invalid JSON"} {
		response.Store(body)
		if reopened.Refresh(context.Background()) == nil {
			t.Fatal("bad response accepted")
		}
		check(reopened, "second", "remote")
		_, after := reopened.Snapshot()
		if !before.UpdatedAt.Equal(*after.UpdatedAt) {
			t.Fatal("failure extended freshness")
		}
	}
	check(NewCache(path, upstream.URL, "bundled", parseTest, nil), "second", "remote")
	empty := NewCache(filepath.Join(t.TempDir(), "catalog.json"), upstream.URL, "bundled", parseTest, nil)
	if empty.Refresh(context.Background()) == nil {
		t.Fatal("invalid JSON accepted")
	}
	check(empty, "bundled", "bundled")
}
