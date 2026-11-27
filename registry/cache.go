package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	FreshFor   = 72 * time.Hour
	retryAfter = time.Minute
	maxSize    = 1 << 20
)

type Metadata struct {
	Source    string     `json:"source"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

type diskCache struct {
	URL       string          `json:"url"`
	UpdatedAt time.Time       `json:"updated_at"`
	Body      json.RawMessage `json:"body"`
}

type refresh struct {
	done chan struct{}
	err  error
}

type Cache[T any] struct {
	path, url string
	parse     func([]byte) (T, error)
	changed   func()
	client    *http.Client
	now       func() time.Time
	mu        sync.Mutex
	value     T
	saved     diskCache
	active    *refresh
	next      time.Time
}

func NewCache[T any](path, url string, bundled T, parse func([]byte) (T, error), changed func()) *Cache[T] {
	c := &Cache[T]{path: path, url: url, value: bundled, parse: parse, changed: changed, client: &http.Client{Timeout: 3 * time.Second}, now: time.Now}
	f, err := os.Open(path)
	if err != nil {
		return c
	}
	defer f.Close()
	var saved diskCache
	raw, err := io.ReadAll(io.LimitReader(f, 2*maxSize+1))
	if err == nil && len(raw) <= 2*maxSize && json.Unmarshal(raw, &saved) == nil &&
		len(saved.Body) <= maxSize && !saved.UpdatedAt.IsZero() && !saved.UpdatedAt.After(c.now()) {
		if value, err := parse(saved.Body); err == nil {
			c.value, c.saved = value, saved
		}
	}
	return c
}

func (c *Cache[T]) Snapshot() (T, Metadata) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saved.UpdatedAt.IsZero() {
		return c.value, Metadata{Source: "bundled"}
	}
	updated := c.saved.UpdatedAt
	return c.value, Metadata{Source: "remote", UpdatedAt: &updated}
}

func (c *Cache[T]) Ensure() {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if c.url == "" || now.Before(c.next) ||
		c.saved.URL == c.url && now.Before(c.saved.UpdatedAt.Add(FreshFor)) {
		return
	}
	c.startLocked()
}

func (c *Cache[T]) Refresh(ctx context.Context) error {
	if c.url == "" {
		return fmt.Errorf("remote registry is disabled")
	}
	c.mu.Lock()
	call := c.startLocked()
	c.mu.Unlock()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-call.done:
		return call.err
	}
}

func (c *Cache[T]) startLocked() *refresh {
	if c.active == nil {
		call := &refresh{done: make(chan struct{})}
		c.active = call
		go func() {
			updated, saved, err := c.fetch()
			c.mu.Lock()
			if err == nil {
				c.value, c.saved = updated, saved
			}
			c.next = c.now().Add(retryAfter)
			c.active = nil
			c.mu.Unlock()
			if err == nil && c.changed != nil {
				c.changed()
			}
			call.err = err
			close(call.done)
		}()
	}
	return c.active
}

func (c *Cache[T]) fetch() (value T, saved diskCache, err error) {
	res, err := c.client.Get(c.url)
	if err != nil {
		return value, saved, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return value, saved, fmt.Errorf("registry returned HTTP %d", res.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, maxSize+1))
	if err != nil || len(body) > maxSize {
		return value, saved, fmt.Errorf("could not read registry (maximum 1 MiB)")
	}
	value, err = c.parse(body)
	if err != nil {
		return value, saved, err
	}
	saved = diskCache{URL: c.url, UpdatedAt: c.now().UTC(), Body: body}
	return value, saved, save(c.path, saved)
}

func save(path string, saved diskCache) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".catalog-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	err = json.NewEncoder(f).Encode(saved)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}
