package router

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/firewall"
	"github.com/sammwyy/startship/internal/store"
)

func TestWakeProject(t *testing.T) {
	for _, wakeError := range []bool{false, true} {
		t.Run(fmt.Sprint(wakeError), func(t *testing.T) {
			r, calls, healthy := setup(t, activePolicy(), Hooks{}, nil)
			wakes := 0
			r.hooks.Wake = func(ctx context.Context, id string) error {
				wakes++
				if id != "project" {
					t.Fatalf("project = %q", id)
				}
				if wakeError {
					return errors.New("failed")
				}
				r.Update(healthy)
				return nil
			}
			asleep := healthy
			asleep.Targets = append([]Target(nil), healthy.Targets...)
			asleep.Targets[0].Deployment.IP = "192.0.2.1"
			asleep.Asleep = map[string]bool{"project": true}
			r.Update(asleep)
			w := request(r, "GET", "/", "", "")
			if wakes != 1 {
				t.Fatalf("wake calls = %d", wakes)
			}
			if wakeError {
				if w.Code != 503 || w.Body.String() != "Startship: project failed to wake.\n" || calls.Load() != 0 {
					t.Fatal(w.Code, w.Body.String(), calls.Load())
				}
			} else {
				if w.Code != 200 || calls.Load() != 1 || w.Header().Get("X-Startship-Woke") == "" {
					t.Fatal(w.Code, w.Body.String(), calls.Load(), w.Header())
				}
				request(r, "GET", "/", "", "")
				if wakes != 1 {
					t.Fatal("awake request called wake")
				}
			}
		})
	}
}

func activePolicy() firewall.Policy { p := firewall.DefaultPolicy(); p.Mode = "enforce"; return p }

func TestActivityReloadsSleepingSnapshot(t *testing.T) {
	r, _, healthy := setup(t, activePolicy(), Hooks{}, nil)
	started, finished, woke := false, false, false
	r.hooks.Activity = func(id string) func() {
		started = true
		asleep := healthy
		asleep.Asleep = map[string]bool{id: true}
		r.Update(asleep)
		return func() { finished = true }
	}
	r.hooks.Wake = func(context.Context, string) error {
		if !started || finished {
			t.Error("incorrect request lifecycle")
		}
		woke = true
		r.Update(healthy)
		return nil
	}
	if w := request(r, "GET", "/", "", ""); w.Code != 200 || !woke || !finished {
		t.Fatal(w.Code, woke, finished)
	}
}

func TestActivitySpansUpgrade(t *testing.T) {
	finished := make(chan struct{})
	r, _, _ := setup(t, activePolicy(), Hooks{Activity: func(string) func() { return func() { close(finished) } }}, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			t.Error(err)
			return
		}
		defer conn.Close()
		fmt.Fprint(rw, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: test\r\n\r\n")
		rw.Flush()
		io.Copy(conn, conn)
	}))
	proxy := httptest.NewServer(r)
	defer proxy.Close()
	req, _ := http.NewRequest("GET", proxy.URL, nil)
	req.Host = "app.test"
	req.Header.Set("Connection", "Upgrade")
	req.Header.Set("Upgrade", "test")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 101 {
		t.Fatal(resp.StatusCode)
	}
	select {
	case <-finished:
		t.Fatal("request completed while upgraded connection open")
	case <-time.After(25 * time.Millisecond):
	}
	resp.Body.Close()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("request did not finish after upgrade closed")
	}
}
func setup(t *testing.T, p firewall.Policy, h Hooks, up http.Handler) (*Router, *atomic.Int64, Snapshot) {
	t.Helper()
	calls := &atomic.Int64{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if up != nil {
			up.ServeHTTP(w, r)
		} else {
			body, _ := io.ReadAll(r.Body)
			fmt.Fprint(w, "upstream:"+string(body))
		}
	}))
	t.Cleanup(upstream.Close)
	u, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(u.Port())
	engine, err := firewall.Compile(p, nil, firewall.NewState())
	if err != nil {
		t.Fatal(err)
	}
	target := Target{Route: store.Record{ID: "route", ProjectID: "project", Host: "app.test", PathPrefix: "/"}, Deployment: store.Record{Status: "running", IP: u.Hostname()}, Port: port}
	s := Snapshot{Targets: []Target{target}, RouteWorkspace: map[string]string{"route": "workspace"}, Policies: map[string]*firewall.Engine{"workspace": engine}}
	r := New(h, Options{})
	r.Update(s)
	return r, calls, s
}
func request(r *Router, method, path, body, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, "http://app.test"+path, strings.NewReader(body))
	req.Header.Set("Content-Type", ct)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}
func TestFirewallBlockAndBodyRestoration(t *testing.T) {
	var events []firewall.Event
	var observed []firewall.Decision
	r, calls, _ := setup(t, activePolicy(), Hooks{Event: func(e firewall.Event) { events = append(events, e) }, Observe: func(ws string, req *firewall.Request, d firewall.Decision) {
		if ws != "workspace" || req.ProjectID != "project" {
			t.Errorf("bad attribution %s %+v", ws, req)
		}
		observed = append(observed, d)
	}}, nil)
	for _, path := range []string{"/?q=%27+OR+1%3D1--", "/?q=UNION+SELECT+password"} {
		w := request(r, "GET", path, "", "")
		if w.Code != 403 || !strings.Contains(w.Body.String(), "Blocked by Startship firewall") || w.Header().Get("X-Startship-Ray") == "" {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("blocked SQLi reached upstream")
	}
	for _, ct := range []string{"application/json", "application/x-www-form-urlencoded", "text/plain"} {
		body := `{"name":"O'Brien","query":"select a plan"}`
		if ct == "application/x-www-form-urlencoded" {
			body = "name=O%27Brien&q=select+a+plan"
		}
		w := request(r, "POST", "/api", body, ct)
		if w.Code != 200 || w.Body.String() != "upstream:"+body {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	w := request(r, "POST", "/api", `{"name":"<script>alert(1)</script>"}`, "application/json")
	if w.Code != 403 || calls.Load() != 3 {
		t.Fatal(w.Code, calls.Load())
	}
	if len(events) != 3 || len(observed) != 6 {
		t.Fatal(len(events), len(observed))
	}
}
func TestRateLimitBanAndRetry(t *testing.T) {
	p := activePolicy()
	p.Limits.Rate = &firewall.WorkspaceRate{Requests: 1, WindowSeconds: 30, Action: firewall.Block}
	r, calls, _ := setup(t, p, Hooks{}, nil)
	if request(r, "GET", "/", "", "").Code != 200 {
		t.Fatal("first request")
	}
	w := request(r, "GET", "/", "", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "30" || calls.Load() != 1 {
		t.Fatal(w.Code, w.Header(), calls.Load())
	}
	p.Limits.Rate.Action = firewall.Ban
	p.Limits.Rate.BanSeconds = 90
	var bans []Ban
	r, calls, _ = setup(t, p, Hooks{Ban: func(b Ban) error { bans = append(bans, b); return nil }}, nil)
	request(r, "GET", "/", "", "")
	w = request(r, "GET", "/", "", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "90" || len(bans) != 1 || bans[0].Seconds != 90 || bans[0].WorkspaceID != "workspace" {
		t.Fatal(w.Code, w.Header(), bans)
	}
	if w = request(r, "GET", "/", "", ""); w.Code != 403 || calls.Load() != 1 || len(bans) != 1 {
		t.Fatal(w.Code, calls.Load(), bans)
	}
	r, _, _ = setup(t, p, Hooks{Ban: func(Ban) error { return errors.New("storage down") }}, nil)
	request(r, "GET", "/", "", "")
	if request(r, "GET", "/", "", "").Code != 503 {
		t.Fatal("persistence error did not fail closed")
	}
}
func TestSizeLimits(t *testing.T) {
	p := activePolicy()
	p.Limits.MaxBodyBytes = 4
	r, calls, _ := setup(t, p, Hooks{}, nil)
	for _, ct := range []string{"application/json", "text/plain"} {
		w := request(r, "POST", "/", "12345", ct)
		if w.Code != 413 {
			t.Fatal(w.Code, w.Body.String())
		}
		req := httptest.NewRequest("POST", "http://app.test/", strings.NewReader("12345"))
		req.ContentLength = -1
		req.Header.Set("Content-Type", ct)
		w = httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != 413 {
			t.Fatal("chunked", w.Code, w.Body.String())
		}
	}
	if calls.Load() != 0 {
		t.Fatal("oversized body reached upstream")
	}
	p.Limits.MaxURLBytes = 3
	r, _, _ = setup(t, p, Hooks{}, nil)
	if w := request(r, "GET", "/?q=hello", "", ""); w.Code != 414 {
		t.Fatal(w.Code)
	}
	if w := request(r, "GET", "/%61%62", "", ""); w.Code != 414 {
		t.Fatal("escaped URL length", w.Code)
	}
	p.Limits.MaxURLBytes = 0
	p.Limits.MaxHeaderBytes = 3
	r, _, _ = setup(t, p, Hooks{}, nil)
	if w := request(r, "GET", "/", "", ""); w.Code != 431 {
		t.Fatal(w.Code)
	}
	p = activePolicy()
	p.Limits.MaxBodyBytes = 2
	p.Rules = []firewall.Rule{{ID: "allow", Enabled: true, Match: "all", Conditions: []firewall.Condition{{Field: "path", Op: "eq", Value: "/"}}, Action: firewall.Allow}}
	r, _, _ = setup(t, p, Hooks{}, nil)
	if w := request(r, "POST", "/", "large body", "text/plain"); w.Code != 200 || w.Body.String() != "upstream:large body" {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestStreamingBodyLimit(t *testing.T) {
	p := activePolicy()
	p.Limits.MaxBodyBytes = 65537
	var events []firewall.Event
	var observed []firewall.Decision
	r, _, _ := setup(t, p, Hooks{Event: func(e firewall.Event) { events = append(events, e) }, Observe: func(_ string, _ *firewall.Request, d firewall.Decision) { observed = append(observed, d) }}, nil)
	req := httptest.NewRequest("POST", "http://app.test/", strings.NewReader(strings.Repeat("x", 65538)))
	req.ContentLength = -1
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 413 || len(events) != 1 || events[0].RuleID != "__body" || len(observed) != 1 || observed[0].StatusCode != 413 {
		t.Fatal(w.Code, events, observed)
	}
}
func TestConcurrency(t *testing.T) {
	entered := make(chan struct{})
	finish := make(chan struct{})
	p := activePolicy()
	p.Limits.ConcurrentPerIP = 1
	r, calls, s := setup(t, p, Hooks{}, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) { entered <- struct{}{}; <-finish; fmt.Fprint(w, "done") }))
	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- request(r, "GET", "/", "", "") }()
	<-entered
	r.Update(s)
	w := request(r, "GET", "/", "", "")
	if w.Code != 429 || w.Header().Get("Retry-After") != "1" || calls.Load() != 1 {
		t.Fatal(w.Code, calls.Load())
	}
	close(finish)
	if w := <-done; w.Code != 200 {
		t.Fatal(w.Code)
	}
	go func() { done <- request(r, "GET", "/", "", "") }()
	<-entered
	if w := <-done; w.Code != 200 {
		t.Fatal("slot not released", w.Code)
	}
}

type countryGeo struct{}

func (countryGeo) Country(ip net.IP) string { return "US" }
func TestTrustedProxyAndMonitor(t *testing.T) {
	p := activePolicy()
	p.Rules = []firewall.Rule{{ID: "ip", Enabled: true, Match: "all", Conditions: []firewall.Condition{{Field: "ip", Op: "in_cidr", Value: "192.0.2.0/24"}, {Field: "country", Op: "eq", Value: "DE"}}, Action: firewall.Block}}
	r, calls, s := setup(t, p, Hooks{}, nil)
	proxies, _ := firewall.ParseTrustedProxies([]string{"10.0.0.0/8"})
	r.options = Options{TrustedProxies: proxies, GeoIP: countryGeo{}}
	req := httptest.NewRequest("GET", "http://app.test/", nil)
	req.RemoteAddr = "10.0.0.1:123"
	req.Header.Set("X-Forwarded-For", "198.51.100.1, 192.0.2.1, 10.0.0.2")
	req.Header.Set("CF-IPCountry", "DE")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 403 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
	req.RemoteAddr = "203.0.113.1:123"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("untrusted headers honored", w.Code)
	}
	p.Mode = "monitor"
	engine, err := firewall.Compile(p, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	s.Policies["workspace"] = engine
	r.Update(s)
	req.RemoteAddr = "10.0.0.1:123"
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal("monitor blocked", w.Code)
	}
}
func TestSnapshotIsolationAndBypasses(t *testing.T) {
	r, _, s := setup(t, activePolicy(), Hooks{Challenge: func(w http.ResponseWriter, r *http.Request) bool {
		if r.URL.Path == "/challenge" {
			fmt.Fprint(w, "challenge")
			return true
		}
		return false
	}, Dashboard: func(w http.ResponseWriter, r *http.Request) bool {
		if r.Host == "dashboard.test" {
			fmt.Fprint(w, "dashboard")
			return true
		}
		return false
	}}, nil)
	s.Targets[0].Deployment.Status = "failed"
	delete(s.RouteWorkspace, "route")
	delete(s.Policies, "workspace")
	if w := request(r, "GET", "/", "", ""); w.Code != 200 {
		t.Fatal("snapshot mutated", w.Code)
	}
	if w := request(r, "GET", "/challenge?q=%27+OR+1=1--", "", ""); w.Body.String() != "challenge" {
		t.Fatal(w.Body.String())
	}
	req := httptest.NewRequest("GET", "http://dashboard.test/?q=%27+OR+1=1--", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Body.String() != "dashboard" {
		t.Fatal(w.Code, w.Body.String())
	}
	req = httptest.NewRequest("GET", "http://missing.test/", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				r.Update(Snapshot{})
				request(r, "GET", "/", "", "")
			}
		}()
	}
	wg.Wait()
}

type brokenBody struct{}

func (brokenBody) Read([]byte) (int, error) { return 0, errors.New("broken") }
func (brokenBody) Close() error             { return nil }
func TestBodyReadError(t *testing.T) {
	r, calls, _ := setup(t, activePolicy(), Hooks{}, nil)
	req := httptest.NewRequest("POST", "http://app.test/", nil)
	req.Header.Set("Content-Type", "application/json")
	req.Body = brokenBody{}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != 400 || calls.Load() != 0 {
		t.Fatal(w.Code, calls.Load())
	}
}

func TestResponseWriter(t *testing.T) {
	r := httptest.NewRecorder()
	w := &responseWriter{ResponseWriter: r}
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
		observed := &responseWriter{ResponseWriter: w}
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
