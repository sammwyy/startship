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
	"testing"
	"time"
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
