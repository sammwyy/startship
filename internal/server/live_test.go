package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestLive(t *testing.T) {
	a := testServer(t)
	a.liveSessionInterval = 50 * time.Millisecond
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	sid := randomID()
	if _, err = a.store.DB.Exec("INSERT INTO sessions VALUES(?,?,?)", sid, uid, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	p := Record{ID: randomID(), Kind: "projects", Owner: uid, Name: "project"}
	if err = a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.api())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dial := func(headers http.Header) (*websocket.Conn, *http.Response, error) {
		return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: headers})
	}
	if c, r, e := dial(nil); e == nil || r.StatusCode != 401 {
		if c != nil {
			c.CloseNow()
		}
		t.Fatalf("unauthenticated: %v %v", r, e)
	}
	headers := http.Header{"Cookie": []string{"startship_session=" + sid}, "Origin": []string{"https://evil.test"}}
	if c, r, e := dial(headers); e == nil || r.StatusCode != 403 {
		if c != nil {
			c.CloseNow()
		}
		t.Fatalf("origin: %v %v", r, e)
	}
	headers.Del("Origin")
	c, _, err := dial(headers)
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	read := func() map[string]any {
		t.Helper()
		_, b, e := c.Read(ctx)
		if e != nil {
			t.Fatal(e)
		}
		var m map[string]any
		if e = json.Unmarshal(b, &m); e != nil {
			t.Fatal(e)
		}
		return m
	}
	send := func(v any) {
		t.Helper()
		b, _ := json.Marshal(v)
		if e := c.Write(ctx, websocket.MessageText, b); e != nil {
			t.Fatal(e)
		}
	}
	if m := read(); m["type"] != "ready" || m["user_id"] != uid {
		t.Fatal(m)
	}
	send(map[string]any{"type": "subscribe", "id": "p", "topic": "project", "project_id": p.ID})
	if m := read(); m["type"] != "snapshot" {
		t.Fatal(m)
	}
	release := Record{ID: randomID(), Kind: "releases", Owner: uid, ProjectID: p.ID, Status: "building"}
	if err = a.store.Save(release); err != nil {
		t.Fatal(err)
	}
	if m := read(); m["type"] != "upsert" || m["kind"] != "release" {
		t.Fatal(m)
	}
	route := Record{ID: randomID(), Kind: "routes", Owner: uid, ProjectID: p.ID, Host: "test.local"}
	if err = a.store.Save(route); err != nil {
		t.Fatal(err)
	}
	if m := read(); m["type"] != "upsert" || m["kind"] != "route" {
		t.Fatal(m)
	}
	req, err := http.NewRequestWithContext(ctx, "DELETE", srv.URL+"/api/routes/"+route.ID, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Cookie", "startship_session="+sid)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatal(resp.Status)
	}
	if m := read(); m["type"] != "delete" || m["kind"] != "route" || m["record_id"] != route.ID {
		t.Fatal(m)
	}
	for range 200 {
		release.Log += "text"
		if err = a.store.Save(release); err != nil {
			t.Fatal(err)
		}
	}
	// A barrier on the same socket proves log-only saves did not enqueue upserts.
	time.Sleep(200 * time.Millisecond)
	send(map[string]any{"type": "subscribe", "id": "l", "topic": "release_log", "release_id": release.ID})
	if m := read(); m["type"] != "snapshot" {
		t.Fatal(m)
	}
	logger := logWriter{a: a, id: release.ID, secret: "secret"}
	if _, err = logger.Write([]byte("secret message")); err != nil {
		t.Fatal(err)
	}
	if m := read(); m["type"] != "log" || m["data"] != "[REDACTED] message" {
		t.Fatal(m)
	}
	send(map[string]any{"type": "subscribe", "id": "l", "topic": "release_log", "release_id": release.ID})
	m := read()
	if m["type"] != "snapshot" {
		t.Fatal(m)
	}
	log := m["data"].(map[string]any)["log"].(string)
	if !strings.HasSuffix(log, "[REDACTED] message") {
		t.Fatal(log)
	}
	// Model a persisted chunk whose publication lost the race to the snapshot.
	a.live.publish(liveEvent{id: release.ID, text: "[REDACTED] message", end: len(log)})
	if _, err = logger.Write([]byte("next")); err != nil {
		t.Fatal(err)
	}
	if m := read(); m["type"] != "log" || m["data"] != "next" {
		t.Fatalf("duplicated snapshot log: %v", m)
	}
	send(map[string]any{"type": "unsubscribe", "id": "l"})
	send(map[string]any{"type": "unsubscribe", "id": "p"})
	release.Status = "running"
	if err = a.store.Save(release); err != nil {
		t.Fatal(err)
	}
	time.Sleep(200 * time.Millisecond)
	send(map[string]any{"type": "subscribe", "id": "bad", "topic": "unknown"})
	if m := read(); m["type"] != "error" || m["error"] != "invalid subscription" {
		t.Fatal(m)
	}
	if _, err = a.store.DB.Exec("DELETE FROM sessions WHERE id=?", sid); err != nil {
		t.Fatal(err)
	}
	_, _, err = c.Read(ctx)
	if websocket.CloseStatus(err) != 4401 {
		t.Fatalf("close: %v", err)
	}
}

func TestLiveForbidden(t *testing.T) {
	a := testServer(t)
	owner, _ := a.createUser("owner", "password123")
	uid, _ := a.createUser("other", "password123")
	p := Record{ID: randomID(), Kind: "projects", Owner: owner}
	a.store.Save(p)
	sid := randomID()
	a.store.DB.Exec("INSERT INTO sessions VALUES(?,?,?)", sid, uid, time.Now().Add(time.Hour).Unix())
	srv := httptest.NewServer(a.api())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"startship_session=" + sid}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	c.Read(ctx)
	b, _ := json.Marshal(map[string]string{"type": "subscribe", "id": "p", "topic": "project", "project_id": p.ID})
	c.Write(ctx, websocket.MessageText, b)
	_, b, err = c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	json.Unmarshal(b, &m)
	if m["error"] != "forbidden" {
		t.Fatal(string(b))
	}
}

func TestLiveData(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	workspace := a.store.List("workspaces", uid)[0]
	project := Record{ID: randomID(), Kind: "projects", Owner: uid, WorkspaceID: workspace.ID}
	container := Record{ID: randomID(), Kind: "apps", Owner: uid, ProjectID: project.ID}
	for _, r := range []Record{project, container} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	ctx := context.Background()
	for _, s := range []liveSub{
		{Topic: "workspace", WorkspaceID: workspace.ID},
		{Topic: "project", ProjectID: project.ID},
		{Topic: "traffic", ProjectID: project.ID, Window: 900},
		{Topic: "container_log", ContainerID: container.ID},
	} {
		data, _, _, why := a.liveData(ctx, uid, &s)
		if why != "" || data == nil {
			t.Fatalf("%s: %v %s", s.Topic, data, why)
		}
	}
	for _, s := range []liveSub{
		{Topic: "project"},
		{Topic: "traffic", ProjectID: project.ID, Window: 1},
		{Topic: "container_log", ContainerID: project.ID},
		{Topic: "release_log", ReleaseID: "missing"},
	} {
		_, _, _, why := a.liveData(ctx, uid, &s)
		if why == "" {
			t.Fatalf("accepted invalid subscription: %+v", s)
		}
	}
	s := liveSub{Topic: "container_log", ContainerID: container.ID}
	data, _, _, why := a.liveData(ctx, uid, &s)
	if why != "" || data.(map[string]string)["log"] != "Container is not deployed yet." {
		t.Fatal(data, why)
	}
	deployment := Record{ID: randomID(), Kind: "deployments", Owner: uid, AppID: container.ID, ProjectID: project.ID, StaticDir: "/static"}
	if err := a.store.Save(deployment); err != nil {
		t.Fatal(err)
	}
	container.Active = deployment.ID
	if err := a.store.Save(container); err != nil {
		t.Fatal(err)
	}
	data, _, _, why = a.liveData(ctx, uid, &s)
	if why != "" || data.(map[string]string)["log"] != "Static container: files are served directly; no runtime process logs." {
		t.Fatal(data, why)
	}
}

func TestLiveRelevant(t *testing.T) {
	for _, tt := range []struct {
		s    liveSub
		r    Record
		want bool
	}{
		{liveSub{Topic: "project", ProjectID: "p"}, Record{ID: "p"}, true},
		{liveSub{Topic: "project", ProjectID: "p"}, Record{ProjectID: "p", Kind: "releases"}, true},
		{liveSub{Topic: "project", ProjectID: "p"}, Record{ProjectID: "other"}, false},
		{liveSub{Topic: "workspace", WorkspaceID: "w"}, Record{ID: "w"}, true},
		{liveSub{Topic: "workspace", WorkspaceID: "w"}, Record{Kind: "projects", WorkspaceID: "w"}, true},
		{liveSub{Topic: "workspace", WorkspaceID: "w"}, Record{Kind: "releases", WorkspaceID: "w"}, false},
		{liveSub{Topic: "release_log", ReleaseID: "r"}, Record{ID: "r"}, true},
		{liveSub{Topic: "release_log", ReleaseID: "r"}, Record{ID: "other"}, false},
		{liveSub{Topic: "container_log", ContainerID: "c"}, Record{ID: "c"}, true},
		{liveSub{Topic: "container_log", ContainerID: "c"}, Record{ID: "r", ContainerID: "c"}, false},
		{liveSub{Topic: "project", ProjectID: "p"}, Record{}, false},
	} {
		if got := tt.s.relevant(tt.r); got != tt.want {
			t.Fatalf("%+v %+v: got %v", tt.s, tt.r, got)
		}
	}
}

func TestLiveLogOffsets(t *testing.T) {
	a := testServer(t)
	for _, id := range []string{"r", "group"} {
		if err := a.store.Save(Record{ID: id, Kind: "releases"}); err != nil {
			t.Fatal(err)
		}
	}
	c := &liveClient{events: make(chan liveEvent, 4), pending: map[string]liveEvent{}, cancel: func() { t.Error("overflow") }}
	a.live.clients[c] = true
	l := logWriter{a: a, id: "r", group: "group", secret: "token"}
	for _, text := range []string{"token", "next"} {
		if _, err := l.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []liveEvent{{id: "r", text: "[REDACTED]", end: 10}, {id: "group", text: "[REDACTED]", end: 10}, {id: "r", text: "next", end: 14}, {id: "group", text: "next", end: 14}} {
		got := <-c.events
		if got.id != want.id || got.text != want.text || got.end != want.end {
			t.Fatalf("got %+v want %+v", got, want)
		}
	}
	l.id = "missing"
	if _, err := l.Write([]byte("not saved")); err == nil {
		t.Fatal("expected missing record error")
	}
	if len(c.events) != 0 {
		t.Fatal("published failed append")
	}
}

func TestLiveIdleAccessRevoked(t *testing.T) {
	a := testServer(t)
	a.liveSessionInterval = 50 * time.Millisecond
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	p := Record{ID: randomID(), Kind: "projects", Owner: uid}
	if err = a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	sid := randomID()
	if _, err = a.store.DB.Exec("INSERT INTO sessions VALUES(?,?,?)", sid, uid, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.api())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"startship_session=" + sid}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	if _, _, err = c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]string{"type": "subscribe", "id": "p", "topic": "project", "project_id": p.ID})
	if err = c.Write(ctx, websocket.MessageText, b); err != nil {
		t.Fatal(err)
	}
	if _, _, err = c.Read(ctx); err != nil {
		t.Fatal(err)
	}
	// Revoke legacy ownership without a store event to exercise idle validation.
	if _, err = a.store.DB.Exec("UPDATE records SET owner=? WHERE id=?", "other", p.ID); err != nil {
		t.Fatal(err)
	}
	_, b, err = c.Read(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m["type"] != "error" || m["error"] != "forbidden" || m["id"] != "p" {
		t.Fatal(string(b))
	}
}

func TestLiveProjectActivation(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	project := Record{ID: "project", Kind: "projects", Owner: uid, Active: "old-release"}
	container := Record{ID: "container", Kind: "apps", Owner: uid, ProjectID: project.ID, Active: "old-deployment"}
	for _, r := range []Record{
		project, container,
		{ID: "old-release", Kind: "releases", Owner: uid, ProjectID: project.ID, Status: "running"},
		{ID: "new-release", Kind: "releases", Owner: uid, ProjectID: project.ID, Status: "building"},
		{ID: "old-deployment", Kind: "deployments", Owner: uid, ProjectID: project.ID, AppID: container.ID, Status: "running"},
		{ID: "new-deployment", Kind: "deployments", Owner: uid, ProjectID: project.ID, AppID: container.ID, ReleaseID: "new-release", Status: "ready", StaticDir: "/static"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	sid := randomID()
	if _, err := a.store.DB.Exec("INSERT INTO sessions VALUES(?,?,?)", sid, uid, time.Now().Add(time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(a.api())
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: http.Header{"Cookie": []string{"startship_session=" + sid}}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	read := func() map[string]any {
		t.Helper()
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	if m := read(); m["type"] != "ready" {
		t.Fatal(m)
	}
	for _, s := range []liveSub{{Type: "subscribe", ID: "logs", Topic: "container_log", ContainerID: container.ID}, {Type: "subscribe", ID: "project", Topic: "project", ProjectID: project.ID}} {
		b, _ := json.Marshal(s)
		if err := c.Write(ctx, websocket.MessageText, b); err != nil {
			t.Fatal(err)
		}
		m := read()
		if m["type"] != "snapshot" || m["id"] != s.ID {
			t.Fatal(m)
		}
		if s.Topic == "container_log" && m["data"].(map[string]any)["log"] != "No runtime output available." {
			t.Fatal(m)
		}
	}
	if err := a.activateProject(project.ID, "new-release", []Record{container}, []string{"new-deployment"}); err != nil {
		t.Fatal(err)
	}
	reset, containerUpsert, releaseUpsert := false, false, false
	for !reset || !containerUpsert || !releaseUpsert {
		m := read()
		if m["id"] == "logs" {
			if m["type"] != "log" || m["reset"] != true || m["data"] != "Static container: files are served directly; no runtime process logs." {
				t.Fatal(m)
			}
			reset = true
		} else if m["type"] == "upsert" {
			r := m["record"].(map[string]any)
			if m["kind"] == "container" && r["id"] == container.ID {
				if r["active_deployment"] != "new-deployment" {
					t.Fatal(m)
				}
				containerUpsert = true
			}
			if m["kind"] == "release" && r["id"] == "new-release" {
				if r["status"] != "running" {
					t.Fatal(m)
				}
				releaseUpsert = true
			}
		}
	}
}

func TestRuntimeTransactionNotifications(t *testing.T) {
	for _, name := range []string{"legacy activation", "project source", "personal workspace"} {
		t.Run(name, func(t *testing.T) {
			a := testServer(t)
			var run func() error
			var expected []string
			switch name {
			case "legacy activation":
				for _, r := range []Record{{ID: "app", Kind: "apps", Active: "old"}, {ID: "old", Kind: "deployments", Status: "running"}, {ID: "new", Kind: "deployments"}} {
					if err := a.store.Save(r); err != nil {
						t.Fatal(err)
					}
				}
				run = func() error { return a.activate(Record{ID: "app"}, "new") }
				expected = []string{"app", "new", "old"}
			case "project source":
				for _, r := range []Record{{ID: "project", Kind: "projects", Owner: "owner"}, {ID: "app", Kind: "apps", Owner: "owner", ProjectID: "project", GitURL: "https://example.com/repo"}} {
					if err := a.store.Save(r); err != nil {
						t.Fatal(err)
					}
				}
				run = func() error { return migrateProjectSources(a.store) }
				expected = []string{"project", "app"}
			case "personal workspace":
				run = func() error { _, err := a.createUser("alice", "password123"); return err }
			}
			seen := map[string]Record{}
			a.store.OnChange = func(r Record, deleted bool) {
				if deleted {
					t.Fatal("unexpected delete")
				}
				stored, err := a.store.Get(r.ID)
				if err != nil {
					t.Fatal("notification before committed record", err)
				}
				want, _ := json.Marshal(r)
				got, _ := json.Marshal(stored)
				if string(want) != string(got) {
					t.Fatalf("notification differs from committed state: %s != %s", want, got)
				}
				seen[r.ID] = r
			}
			if err := run(); err != nil {
				t.Fatal(err)
			}
			if name == "personal workspace" {
				if len(seen) != 1 {
					t.Fatalf("workspace notifications: %v", seen)
				}
				for _, r := range seen {
					if r.Kind != "workspaces" {
						t.Fatal(r)
					}
				}
				clear(seen)
				if err := run(); err == nil {
					t.Fatal("expected duplicate username error")
				}
				if len(seen) != 0 {
					t.Fatal("failed transaction emitted notifications")
				}
			} else {
				for _, id := range expected {
					if _, ok := seen[id]; !ok {
						t.Fatalf("missing notification for %s: %v", id, seen)
					}
				}
			}
		})
	}
}
