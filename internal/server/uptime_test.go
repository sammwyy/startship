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
		if _, err := a.recordUptime(p.ID, at, 23, cause); err != nil {
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
		if _, err := a.recordUptime("selected-id", time.Now().Add(time.Duration(minutes)*time.Minute), 13, "private error 192.0.2.3"); err != nil {
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
	if got := probes.Load(); got != 4 {
		t.Fatalf("got %d readiness attempts; want one per active runtime check", got)
	}
	sample := func() {
		t.Helper()
		a.sampleUptime(p)
		a.sampleUptime(p)
	}
	summary := func(state string, checks int) {
		t.Helper()
		current, err := a.store.Get(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		got, err := a.summarizeUptime(current, time.Now(), 30, true)
		if err != nil || got.State != state || len(got.Checks) != checks || len(got.Incidents) != 1 || got.Incidents[0].End == nil {
			t.Fatalf("want %s with %d checks and one resolved incident; got %s with %d checks and %+v: %v", state, checks, got.State, len(got.Checks), got.Incidents, err)
		}
		if want := 100 * float64(checks-2) / float64(checks); got.Uptime24h == nil || *got.Uptime24h != want {
			t.Fatalf("want %g%%, got %v", want, got.Uptime24h)
		}
	}
	save := func(r Record) {
		t.Helper()
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	current := p
	current.Sleeping = true
	save(current)
	failing.Store(true)
	sample() // An awake version of the project was previously read by the worker.
	summary("sleeping", 4)
	state := a.projectSleepState(p.ID)
	state.mu.Lock()
	current.Sleeping = false
	save(current)
	sample() // A wake or activation still owns the lifecycle lock.
	state.mu.Unlock()
	summary("operational", 4)
	failing.Store(false)
	a.sampleUptime(p)
	summary("operational", 5)

	save(Record{ID: "static-only", Kind: "releases", ProjectID: p.ID, ContainerReleases: []string{"static"}})
	current.Active = "static-only"
	save(current)
	sample()
	summary("static", 5)

	preview, err := a.store.Get("preview")
	if err != nil {
		t.Fatal(err)
	}
	preview.PreviewID = "branch"
	save(preview)
	a.sampleUptime(Record{ID: p.ID, Kind: "previews", Active: preview.ID, UptimeMonitoring: true})
	current.Active = preview.ID
	save(current)
	sample()
	summary("operational", 5)
	if got := probes.Load(); got != 5 {
		t.Fatalf("preview, static, and sleeping targets must not be probed, got %d requests", got)
	}
	current.Active = "replacement"
	save(Record{ID: current.Active, Kind: "releases", ProjectID: p.ID, ContainerReleases: []string{"running"}})
	save(current)
	a.sampleUptime(p)
	summary("operational", 6)
}

func TestUptimeRetentionKeepsBoundaryAndOngoingIncidents(t *testing.T) {
	a := testServer(t)
	now := time.Date(2026, 4, 20, 12, 0, 0, 0, time.UTC)
	cutoff := now.Add(-uptimeRetention)
	p := Record{ID: "kept", Kind: "projects", Active: "r", UptimeMonitoring: true}
	if err := a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	for _, row := range []struct {
		at    time.Time
		cause string
	}{
		{cutoff.Add(-5 * time.Hour), "expired"},
		{cutoff.Add(-4 * time.Hour), "expired"},
		{cutoff.Add(-3 * time.Hour), ""},
		{cutoff.Add(-2 * time.Hour), "ongoing"},
		{cutoff.Add(-time.Hour), "ongoing"},
		{cutoff, "inside retention"},
	} {
		if _, err := a.recordUptime(p.ID, row.at, 10, row.cause); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.recordUptime("deleted", now, 10, ""); err != nil {
		t.Fatal(err)
	}
	if err := a.pruneUptime(now); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := a.store.DB.QueryRow("SELECT COUNT(*) FROM uptime_checks").Scan(&count); err != nil || count != 1 {
		t.Fatalf("wanted only the at-cutoff check; got %d: %v", count, err)
	}
	if err := a.store.DB.QueryRow("SELECT COUNT(*) FROM uptime_incidents").Scan(&count); err != nil || count != 1 {
		t.Fatalf("wanted only the ongoing incident; got %d: %v", count, err)
	}
	if _, err := a.recordUptime(p.ID, now, 10, ""); err != nil {
		t.Fatal(err)
	}
	got, err := a.summarizeUptime(p, now, 90, true)
	if err != nil || got.State != "operational" || got.Uptime30d == nil || *got.Uptime30d != 100 || len(got.Incidents) != 1 || got.Incidents[0].End == nil || !got.Incidents[0].End.Equal(now) {
		t.Fatalf("unexpected retained summary: %s, %v, %+v: %v", got.State, got.Uptime30d, got.Incidents, err)
	}
}

func TestUptimeRestrictedAccess(t *testing.T) {
	f := newRBACFixture(t)
	p := "projects/" + f.project.ID
	ws := "workspaces/" + f.workspace.ID + "/status-page"
	toggle := `{"uptime_monitoring":true}`
	for _, path := range []string{p + "/uptime", ws} {
		accountRequest(t, f.h, "GET", path, "", f.bob, 404)
	}
	accountRequest(t, f.h, "PATCH", p, toggle, f.bob, 404)
	accountRequest(t, f.h, "PATCH", ws, `{"enabled":false}`, f.bob, 404)
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	subscribe := func(topic, key, id, want string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		headers := http.Header{"Origin": {srv.URL}, "Cookie": {f.bob.Name + "=" + f.bob.Value}}
		conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: headers})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseNow()
		var msg map[string]any
		if err := wsjson.Read(ctx, conn, &msg); err != nil || msg["type"] != "ready" {
			t.Fatalf("ready: %v %v", msg, err)
		}
		if err := wsjson.Write(ctx, conn, map[string]string{"type": "subscribe", "id": topic, "topic": topic, key: id}); err != nil {
			t.Fatal(err)
		}
		msg = nil
		if err := wsjson.Read(ctx, conn, &msg); err != nil || msg["type"] != want || want == "error" && msg["data"] != nil {
			t.Fatalf("wanted %s, got %v: %v", want, msg, err)
		}
	}
	subscribe("uptime", "project_id", f.project.ID, "error")
	subscribe("status_page", "workspace_id", f.workspace.ID, "error")
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	accountRequest(t, f.h, "GET", p+"/uptime", "", f.bob, 200)
	subscribe("uptime", "project_id", f.project.ID, "snapshot")
	accountRequest(t, f.h, "PATCH", p, toggle, f.bob, 403)
	accountRequest(t, f.h, "GET", ws, "", f.bob, 403)
	accountRequest(t, f.h, "PATCH", ws, `{"enabled":false}`, f.bob, 403)
	subscribe("status_page", "workspace_id", f.workspace.ID, "error")
	accountRequest(t, f.h, "PATCH", p, toggle, f.alice, 200)
}

func TestStatusPageSlugCollisionAndRecordDeletion(t *testing.T) {
	f := newRBACFixture(t)
	other := f.a.store.List("workspaces", f.bobID)[0]
	first := "workspaces/" + f.workspace.ID + "/status-page"
	second := "workspaces/" + other.ID + "/status-page"
	patch := func(path, slug string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
		t.Helper()
		return accountRequest(t, f.h, "PATCH", path, `{"slug":"`+slug+`"}`, cookie, status)
	}
	patch(first, "Bad/Slug", f.alice, 400)
	patch(first, "Team-Status", f.alice, 200)
	if got := patch(second, "TEAM-STATUS", f.bob, 409).Body.String(); !strings.Contains(got, "choose a different slug") {
		t.Fatalf("unhelpful collision error: %s", got)
	}
	config := f.project.ID + `","` + f.second.ID
	accountRequest(t, f.h, "PATCH", first, `{"enabled":true,"title":"Team","projects":["`+config+`"]}`, f.alice, 200)
	call := func(path string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		f.h.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		return w
	}
	if w := call("/status/TEAM-STATUS"); w.Code != 200 || !strings.Contains(w.Body.String(), `id="root"`) {
		t.Fatalf("mixed-case page did not serve the app shell: %d", w.Code)
	}
	public := func(want int) publicStatusPage {
		t.Helper()
		w := call("/api/status/TEAM-STATUS")
		if w.Code != want {
			t.Fatalf("expected public status %d, got %d: %s", want, w.Code, w.Body.String())
		}
		if want == 200 {
			return decodeBody[publicStatusPage](t, w.Body.Bytes())
		}
		return publicStatusPage{}
	}
	if got := public(200); len(got.Projects) != 2 {
		t.Fatalf("selected projects missing: %+v", got.Projects)
	}
	for _, p := range []Record{f.project, f.second} {
		if err := f.a.store.Delete(p); err != nil {
			t.Fatal(err)
		}
		config := decodeBody[statusPageConfig](t, accountRequest(t, f.h, "GET", first, "", f.alice, 200).Body.Bytes())
		if p.ID == f.project.ID {
			if got := public(200); len(got.Projects) != 1 || got.Projects[0].Name != f.second.Name {
				t.Fatalf("deleted project still published: %+v", got.Projects)
			}
			if len(config.Projects) != 1 || config.Projects[0] != f.second.ID || !config.Enabled {
				t.Fatalf("unexpected retained config: %+v", config)
			}
		} else if len(config.Projects) != 0 || config.Enabled || config.Slug != "team-status" {
			t.Fatalf("last project deletion must unpublish and retain the slug: %+v", config)
		}
	}
	public(404)
	patch(second, "team-status", f.bob, 409)
	if err := f.a.store.Delete(f.workspace); err != nil {
		t.Fatal(err)
	}
	patch(second, "team-status", f.bob, 200)
	patch(second, "api", f.bob, 200) // This slug is in /status/, not the API namespace.
}
