package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTrafficProxyAndOwnership(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("metrics-owner", "password123")
	if err != nil {
		t.Fatal(err)
	}
	project := Record{ID: "metrics-project", Kind: "projects", Owner: uid, Name: "Demo"}
	if err := a.store.save(project); err != nil {
		t.Fatal(err)
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		if r.URL.Path == "/fail" {
			w.WriteHeader(503)
		} else if r.URL.Path == "/missing" {
			w.WriteHeader(404)
		}
		fmt.Fprintln(w, "data: streaming still works\n")
		w.(http.Flusher).Flush()
	}))
	defer upstream.Close()
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	a.targets = []Target{{Route: Record{ID: "r", ProjectID: project.ID, Host: "traffic.local", PathPrefix: "/"}, Deployment: Record{Status: "running", IP: u.Hostname()}, Port: port}}
	for _, path := range []string{"/ok?token=never-store-this", "/fail", "/missing"} {
		r := httptest.NewRequest("GET", "http://traffic.local"+path, nil)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, r)
		if !strings.Contains(w.Body.String(), "streaming still works") || !w.Flushed {
			t.Fatal("response streaming lost", w.Body.String())
		}
	}
	// Router-generated 502s are also errors, not just upstream failures.
	a.targets[0].Deployment.Status = "failed"
	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://traffic.local/unavailable", nil))
	// Unmatched hosts cannot be attributed to this project.
	a.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "http://other.local/", nil))
	login := func(name string) *http.Cookie {
		t.Helper()
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"`+name+`","password":"password123"}`)))
		if w.Code != 200 {
			t.Fatal(w.Code, w.Body.String())
		}
		return w.Result().Cookies()[0]
	}
	owner := login("metrics-owner")
	if _, err := a.createUser("metrics-other", "password123"); err != nil {
		t.Fatal(err)
	}
	other := login("metrics-other")
	for _, tc := range []struct {
		method, query string
		cookie        *http.Cookie
		status        int
	}{
		{"GET", "", nil, 401}, {"GET", "", other, 404}, {"GET", "", owner, 200},
		{"GET", "?window=900", owner, 200}, {"GET", "?window=3600", owner, 200},
		{"GET", "?window=1", owner, 400}, {"POST", "", owner, 405},
	} {
		r := httptest.NewRequest(tc.method, "/api/projects/"+project.ID+"/traffic"+tc.query, nil)
		if tc.cookie != nil {
			r.AddCookie(tc.cookie)
		}
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatalf("%s %s: %d %s", tc.method, tc.query, w.Code, w.Body.String())
		}
		if w.Code != 200 {
			continue
		}
		var data struct {
			Total   trafficBucket
			Buckets []trafficBucket
			Recent  []trafficRequest
		}
		if err := json.Unmarshal(w.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if data.Total.Requests != 4 || data.Total.Errors != 2 || data.Total.ClientErrors != 1 || len(data.Buckets) != 60 || len(data.Recent) != 4 {
			t.Fatalf("wrong aggregation: %s", w.Body.String())
		}
		if strings.Contains(w.Body.String(), "never-store-this") || strings.Contains(w.Body.String(), "/unavailable") {
			t.Fatal("raw request URL leaked")
		}
		if w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("metrics may be cached")
		}
	}
}

func TestTrafficStaticAndConcurrentRetention(t *testing.T) {
	a := testServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte("static"), 0600); err != nil {
		t.Fatal(err)
	}
	a.targets = []Target{{Route: Record{ProjectID: "static", Host: "static.local", PathPrefix: "/"}, Deployment: Record{ID: "static-release", Status: "running", StaticDir: dir}}}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "http://static.local/", nil))
	if w.Code != 200 || w.Body.String() != "static" {
		t.Fatal(w.Code, w.Body.String())
	}
	now := time.Now()
	a.traffic.record("static", "GET", "route", 500, time.Second, now.Add(-2*time.Hour))
	var group sync.WaitGroup
	for i := 0; i < 100; i++ {
		group.Add(1)
		go func() { defer group.Done(); a.traffic.record("static", "GET", "route", 200, 10*time.Millisecond, now) }()
	}
	group.Wait()
	p := a.traffic.projects["static"]
	if len(p.Recent) != 40 || p.bucket(now).Requests != 101 {
		t.Fatalf("unexpected recent limit/count: %d %+v", len(p.Recent), p.bucket(now))
	}
	// The old sample must not survive a full ring wrap.
	wrapped := now.Add(time.Duration(trafficSlots) * trafficStep)
	a.traffic.record("static", "GET", "route", 200, time.Millisecond, wrapped)
	if p.bucket(wrapped).Requests != 1 || p.bucket(wrapped).Errors != 0 {
		t.Fatal("old bucket survived retention")
	}
}

func TestTrafficMemorySamplesAndWriter(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  float64
	}{{"1.5MiB", 1572864}, {"512MiB", 536870912}, {"2 GB", 2e9}, {"0B", 0}} {
		got, err := memoryBytes(tc.input)
		if err != nil || got != tc.want {
			t.Fatal(tc, got, err)
		}
	}
	name, sample, err := parseContainerUsage([]byte(`{"Name":"demo","CPUPerc":"12.50%","MemUsage":"12.5MiB / 512MiB"}`))
	if err != nil || name != "demo" || sample.CPU != 12.5 || sample.Memory != 13107200 || sample.Limit != 536870912 {
		t.Fatal(name, sample, err)
	}
	for _, raw := range []string{`{}`, `{"CPUPerc":"bad"}`, `{"CPUPerc":"1%","MemUsage":"? / ?"}`} {
		if _, _, err := parseContainerUsage([]byte(raw)); err == nil {
			t.Fatal("accepted invalid sample", raw)
		}
	}
	r := httptest.NewRecorder()
	w := &trafficWriter{ResponseWriter: r}
	w.WriteHeader(103)
	w.WriteHeader(201)
	w.WriteHeader(500)
	if w.statusCode() != 201 {
		t.Fatal("informational or duplicate status corrupted final status")
	}
	if w.Unwrap() != r {
		t.Fatal("response controller cannot unwrap")
	}
	if _, _, err := w.Hijack(); err == nil {
		t.Fatal("unsupported hijack must return an error")
	}
	stream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		observed := &trafficWriter{ResponseWriter: w}
		conn, rw, err := observed.Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		rw.Flush()
	}))
	defer stream.Close()
	resp, err := http.Get(stream.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != 101 {
		t.Fatal("protocol upgrade lost", resp.StatusCode)
	}
}
