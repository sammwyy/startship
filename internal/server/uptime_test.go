package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/sammwyy/startship/internal/pipeline"
)

func TestUptimeIncidentTransitionsAndWindows(t *testing.T) {
	a := testServer(t)
	p := Record{ID: "project", Active: "release", UptimeMonitoring: true}
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	check := func(at time.Time, cause string) {
		t.Helper()
		if err := a.recordUptime(p.ID, at, 23, cause); err != nil {
			t.Fatal(err)
		}
	}
	summary := func() uptimeSummary {
		t.Helper()
		out, err := a.summarizeUptime(p, now, 30, true)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	if got := summary(); got.Uptime24h != nil || got.State != "pending" {
		t.Fatalf("empty summary: %+v", got)
	}
	check(now.Add(-10*uptimeDay), "")
	check(now.Add(-2*uptimeDay), "")
	firstFailure := now.Add(-3 * time.Minute)
	check(firstFailure, "first")
	if got := summary(); got.State != "degraded" || len(got.Incidents) != 0 {
		t.Fatalf("single failure = %s, %+v", got.State, got.Incidents)
	}
	check(now.Add(-2*time.Minute), "second")
	check(now.Add(-time.Minute), "third")
	if got := summary(); got.State != "down" || len(got.Incidents) != 1 || !got.Incidents[0].Start.Equal(firstFailure) || got.Incidents[0].Cause != "first" || got.Incidents[0].End != nil {
		t.Fatalf("open incident = %s, %+v", got.State, got.Incidents)
	}
	check(now, "")
	got := summary()
	if got.State != "operational" || len(got.Incidents) != 1 || got.Incidents[0].End == nil || !got.Incidents[0].End.Equal(now) {
		t.Fatalf("closed incident = %s, %+v", got.State, got.Incidents)
	}
	if *got.Uptime24h != 25 || *got.Uptime7d != 40 || *got.Uptime30d != 50 || got.History[29].Checks != 4 || got.History[29].Uptime != 25 || got.Checks[0].LatencyMS != 23 {
		t.Fatalf("unexpected uptime figures: %v %v %v %+v", *got.Uptime24h, *got.Uptime7d, *got.Uptime30d, got.History[29])
	}
}

func TestPublicStatusOnlyExposesSelectedProjects(t *testing.T) {
	a := testServer(t)
	for _, rec := range []Record{
		{ID: "ws", Kind: "workspaces"},
		{ID: "another-ws", Kind: "workspaces"},
		{ID: "selected-id", Kind: "projects", Name: "Public API", WorkspaceID: "ws", Active: "release", UptimeMonitoring: true, IP: "192.0.2.3"},
		{ID: "hidden-id", Kind: "projects", Name: "Secret name", WorkspaceID: "ws"},
		{ID: "foreign-id", Kind: "projects", Name: "Foreign project", WorkspaceID: "another-ws"},
	} {
		if err := a.store.Save(rec); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.store.DB.Exec("INSERT INTO status_pages VALUES(?,?,?,?,?)", "ws", "test", true, "Status", `["selected-id","foreign-id"]`); err != nil {
		t.Fatal(err)
	}
	for _, minutes := range []int{-2, -1} {
		if err := a.recordUptime("selected-id", time.Now().Add(time.Duration(minutes)*time.Minute), 13, "private error 192.0.2.3"); err != nil {
			t.Fatal(err)
		}
	}
	call := func(method, path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, httptest.NewRequest(method, path, nil))
		return w
	}
	w := call("GET", "/api/status/test")
	body := w.Body.String()
	if w.Code != 200 || !strings.Contains(body, "Public API") || !strings.Contains(body, "Health check failed") || !strings.Contains(body, `"state":"down"`) {
		t.Fatalf("public summary: %d %s", w.Code, body)
	}
	for _, private := range []string{"selected-id", "hidden-id", "foreign-id", "Secret name", "Foreign project", "192.0.2.3", "private error", "latency_ms"} {
		if strings.Contains(body, private) {
			t.Errorf("public response contains %q", private)
		}
	}
	for _, tc := range []struct {
		method, path string
		want         int
	}{
		{"PATCH", "/api/status/test", 405},
		{"GET", "/api/status/unpublished", 404},
		{"GET", "/api/workspaces/ws/status-page", 401},
		{"GET", "/api/projects/selected-id/uptime", 401},
	} {
		if got := call(tc.method, tc.path); got.Code != tc.want {
			t.Errorf("%s %s: got %d, want %d", tc.method, tc.path, got.Code, tc.want)
		}
	}
}

func TestActiveReleaseProbePushesUptimeSnapshots(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("uptime-owner", "password123")
	if err != nil {
		t.Fatal(err)
	}
	var failing atomic.Bool
	var probes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		if r.URL.Path != "/ready" || failing.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer upstream.Close()
	address, _ := url.Parse(upstream.URL)
	port, _ := strconv.Atoi(address.Port())
	health := &pipeline.Pipeline{}
	health.Deploy.HealthPath, health.Deploy.ExpectStatus = "/ready", http.StatusNoContent
	p := Record{ID: "p", Kind: "projects", Owner: uid, Name: "Example", Active: "active", UptimeMonitoring: true}
	for _, r := range []Record{
		p,
		{ID: "active", Kind: "releases", ProjectID: p.ID, ContainerReleases: []string{"running", "static"}},
		{ID: "preview", Kind: "releases", ProjectID: p.ID, ContainerReleases: []string{"preview-revision"}},
		{ID: "running", Kind: "deployments", ProjectID: p.ID, Status: "running", IP: address.Hostname(), Port: port, Pipeline: health},
		{ID: "static", Kind: "deployments", ProjectID: p.ID, StaticDir: "/static"},
		{ID: "preview-revision", Kind: "deployments", ProjectID: p.ID, Status: "running", IP: address.Hostname(), Port: port},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	_, token := liveTestSession(t, a, uid)
	srv := httptest.NewServer(a.api())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Origin": {srv.URL}, "Cookie": {"startship_session=" + token}}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var ready map[string]any
	if err := wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("live ready: %v %v", ready, err)
	}
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "subscribe", "id": "uptime", "topic": "uptime", "project_id": p.ID}); err != nil {
		t.Fatal(err)
	}
	assertSnapshot := func(state string) {
		t.Helper()
		var msg struct {
			Type string
			Data uptimeSummary
		}
		if err := wsjson.Read(ctx, conn, &msg); err != nil || msg.Type != "snapshot" || msg.Data.State != state {
			t.Fatalf("wanted %s snapshot, got %s %s: %v", state, msg.Type, msg.Data.State, err)
		}
	}
	assertSnapshot("pending")
	a.sampleUptime(p)
	assertSnapshot("operational")
	failing.Store(true)
	a.sampleUptime(p)
	assertSnapshot("degraded")
	a.sampleUptime(p)
	assertSnapshot("down")
	failing.Store(false)
	a.sampleUptime(p)
	assertSnapshot("operational")
	sleeping := p
	sleeping.Sleeping = true
	a.sampleUptime(sleeping)
	if got := probes.Load(); got != 4 {
		t.Fatalf("got %d readiness attempts; want one per active runtime check", got)
	}
}
