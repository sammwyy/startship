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
