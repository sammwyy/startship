package server

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/pipeline"
)

func TestProjectSleepWake(t *testing.T) {
	a := testServer(t)
	dir := t.TempDir()
	trace := filepath.Join(dir, "calls")
	script := "#!/bin/sh\necho \"$1 $2\" >> \"$TRACE\"\ncase \"$1\" in\ninspect) echo 127.0.0.1;;\nstart) [ ! -e \"$FAIL\" ];;\nstop) exit 0;;\n*) exit 1;;\nesac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRACE", trace)
	t.Setenv("FAIL", filepath.Join(dir, "fail"))
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	defer upstream.Close()
	_, portString, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(portString)
	for _, r := range []Record{
		{ID: "project", Kind: "projects", Active: "release"},
		{ID: "release", Kind: "releases", Status: "running", ContainerReleases: []string{"one", "two", "static"}},
		{ID: "app", Kind: "apps", ProjectID: "project"},
		{ID: "one", Kind: "deployments", AppID: "app", ProjectID: "project", Container: "first", Status: "running", Port: port, IP: "192.0.2.1"},
		{ID: "two", Kind: "deployments", AppID: "app", ProjectID: "project", Container: "second", Status: "running", Port: port, IP: "192.0.2.2"},
		{ID: "static", Kind: "deployments", ProjectID: "project", StaticDir: "/static", Status: "running"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.sleepProject("project", false); err != nil {
		t.Fatal(err)
	}
	p, _ := a.store.Get("project")
	if !p.Sleeping {
		t.Fatal("project did not sleep")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := a.wakeProject(context.Background(), "project"); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	p, _ = a.store.Get("project")
	if p.Sleeping {
		t.Fatal("project did not wake")
	}
	for _, id := range []string{"one", "two"} {
		d, _ := a.store.Get(id)
		if d.Status != "running" || d.IP != "127.0.0.1" {
			t.Fatal(d)
		}
	}
	b, err := os.ReadFile(trace)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"stop first", "stop second", "start first", "start second"} {
		if strings.Count(string(b), line+"\n") != 1 {
			t.Fatal(string(b))
		}
	}
	if strings.Contains(string(b), "rm ") || strings.Contains(string(b), "static") {
		t.Fatal(string(b))
	}
	if err := a.sleepProject("project", false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "fail"), nil, 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.wakeProject(context.Background(), "project"); err == nil {
		t.Fatal("failed docker start must fail wake")
	}
	p, _ = a.store.Get("project")
	if !p.Sleeping {
		t.Fatal("failed wake cleared sleeping")
	}
}

func sleepFixture(t *testing.T, script string) (*Server, string) {
	t.Helper()
	a := testServer(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\necho \"$1 $2\" >> \"$TRACE\"\n"+script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TRACE", filepath.Join(dir, "calls"))
	t.Setenv("CONTROL", dir)
	for _, r := range []Record{
		{ID: "project", Kind: "projects", Active: "release", SleepAfter: 1},
		{ID: "release", Kind: "releases", Status: "running", ContainerReleases: []string{"one", "two"}},
		{ID: "app", Kind: "apps", ProjectID: "project", Active: "one"},
		{ID: "one", Kind: "deployments", AppID: "app", ProjectID: "project", Container: "first", Status: "running"},
		{ID: "two", Kind: "deployments", AppID: "app", ProjectID: "project", Container: "second", Status: "running"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	return a, dir
}

func TestSleepInFlight(t *testing.T) {
	a, dir := sleepFixture(t, "exit 1\n")
	s := a.projectSleepState("project")
	done := a.projectRequest("project")
	s.activityMu.Lock()
	s.last = time.Now().Add(-2 * time.Minute)
	s.activityMu.Unlock()
	if err := a.sleepProject("project", true); err != nil {
		t.Fatal(err)
	}
	done()
	if err := a.sleepProject("project", true); err != nil {
		t.Fatal(err)
	}
	s.activityMu.Lock()
	if s.inFlight != 0 || time.Since(s.last) > time.Second {
		t.Fatal("request completion did not reset activity")
	}
	s.last = time.Now().Add(-2 * time.Minute)
	s.activityMu.Unlock()
	var racedDone func()
	a.store.OnChange = func(r Record, deleted bool) {
		if r.ID == "project" && r.Sleeping {
			racedDone = a.projectRequest(r.ID)
		}
	}
	if err := a.sleepProject("project", true); err != nil {
		t.Fatal(err)
	}
	if racedDone == nil {
		t.Fatal("sleep state was not published")
	}
	racedDone()
	p, _ := a.store.Get("project")
	if p.Sleeping {
		t.Fatal("idle sleep did not revert for arriving request")
	}
	if _, err := os.Stat(filepath.Join(dir, "calls")); !os.IsNotExist(err) {
		t.Fatal("busy project invoked docker")
	}
}

func TestSleepStopRollback(t *testing.T) {
	a, dir := sleepFixture(t, "[ \"$1 $2\" != 'stop second' ]\n")
	if err := a.sleepProject("project", false); err == nil {
		t.Fatal("stop failure must fail sleep")
	}
	p, _ := a.store.Get("project")
	b, _ := os.ReadFile(filepath.Join(dir, "calls"))
	if p.Sleeping || string(b) != "stop first\nstop second\nstart first\n" {
		t.Fatal(p.Sleeping, string(b))
	}
}

func TestWakeParallelHealth(t *testing.T) {
	a, _ := sleepFixture(t, "case \"$1\" in inspect) echo 127.0.0.1;; *) exit 0;; esac\n")
	var probes atomic.Int64
	ready := make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if probes.Add(1) == 2 {
			close(ready)
		}
		select {
		case <-ready:
		case <-r.Context().Done():
		}
	}))
	defer upstream.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	for _, id := range []string{"one", "two"} {
		a.store.Update(id, func(r *Record) {
			r.Port = port
			r.Pipeline = &pipeline.Pipeline{Deploy: pipeline.Deploy{HealthPath: "/health", HealthTimeout: 1}}
		})
	}
	a.store.Update("project", func(r *Record) { r.Sleeping = true })
	if err := a.wakeProject(context.Background(), "project"); err != nil {
		t.Fatal(err)
	}
	if probes.Load() != 2 {
		t.Fatal("health probes did not overlap", probes.Load())
	}
}

func TestRequestDuringSleepWakes(t *testing.T) {
	a, dir := sleepFixture(t, "case \"$1\" in\nstop) touch \"$CONTROL/stopping\"; while [ ! -e \"$CONTROL/release\" ]; do sleep 0.01; done;;\ninspect) echo 127.0.0.1;;\n*) exit 0;;\nesac\n")
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("awake")) }))
	defer upstream.Close()
	_, portText, _ := net.SplitHostPort(strings.TrimPrefix(upstream.URL, "http://"))
	port, _ := strconv.Atoi(portText)
	for _, id := range []string{"one", "two"} {
		a.store.Update(id, func(r *Record) { r.Port = port; r.IP = "127.0.0.1" })
	}
	a.store.Save(Record{ID: "route", Kind: "routes", AppID: "app", ProjectID: "project", Host: "app.test", PathPrefix: "/", Port: port})
	a.refresh()
	slept := make(chan error, 1)
	go func() { slept <- a.sleepProject("project", false) }()
	defer os.WriteFile(filepath.Join(dir, "release"), nil, 0600)
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
waiting:
	for {
		select {
		case <-deadline.C:
			t.Fatal("docker stop not reached")
		case <-ticker.C:
			if _, err := os.Stat(filepath.Join(dir, "stopping")); err == nil {
				break waiting
			}
		}
	}
	p, _ := a.store.Get("project")
	if !p.Sleeping {
		t.Fatal("docker stop ran before publishing sleeping state")
	}
	response := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", "http://app.test/", nil))
		response <- w
	}()
	select {
	case w := <-response:
		t.Fatalf("request did not wait for sleep: %d %s", w.Code, w.Body.String())
	case <-time.After(50 * time.Millisecond):
	}
	os.WriteFile(filepath.Join(dir, "release"), nil, 0600)
	if err := <-slept; err != nil {
		t.Fatal(err)
	}
	select {
	case w := <-response:
		if w.Code != 200 || w.Body.String() != "awake" || w.Header().Get("X-Startship-Woke") == "" {
			t.Fatal(w.Code, w.Body.String(), w.Header())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("request did not wake")
	}
}

func TestSleepEligibility(t *testing.T) {
	a := testServer(t)
	p := Record{ID: "project", Kind: "projects", Active: "release", SleepAfter: 10}
	for _, r := range []Record{p, {ID: "release", Kind: "releases", Status: "running", ContainerReleases: []string{"static"}}, {ID: "static", Kind: "deployments", Status: "running", StaticDir: "/static"}} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.sleepDeployments(p); err == nil {
		t.Fatal("static-only project should not sleep")
	}
	if err := a.sleepProject(p.ID, false); err == nil {
		t.Fatal("static-only sleep should fail")
	}
	if err := a.store.Update("static", func(r *Record) { r.Container = "container"; r.StaticDir = "" }); err != nil {
		t.Fatal(err)
	}
	if err := a.sleepProject(p.ID, true); err != nil {
		t.Fatal(err)
	}
	s := a.projectSleepState(p.ID)
	s.activityMu.Lock()
	last := s.last
	s.activityMu.Unlock()
	if last.IsZero() || time.Since(last) > time.Second {
		t.Fatal("first idle scan must initialize activity")
	}
	current, _ := a.store.Get(p.ID)
	if current.Sleeping {
		t.Fatal("first idle scan slept project")
	}
	if err := a.wakeProject(context.Background(), p.ID); err != nil {
		t.Fatal("awake project should be a no-op", err)
	}
	if err := a.wakeProject(context.Background(), "missing"); err == nil {
		t.Fatal("missing project wake should fail")
	}
}
