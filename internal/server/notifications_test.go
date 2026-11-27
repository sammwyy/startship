package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func notificationReceiver(t *testing.T) (string, <-chan notificationPayload) {
	t.Helper()
	incoming := make(chan notificationPayload, 16)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload notificationPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected webhook request: %s %v", r.Method, err)
		}
		incoming <- payload
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(receiver.Close)
	t.Setenv("STARTSHIP_NOTIFICATION_LOCAL_HOST", strings.TrimPrefix(receiver.URL, "http://"))
	return receiver.URL, incoming
}

func TestNotificationSlackPayload(t *testing.T) {
	a := testServer(t)
	incoming := make(chan map[string]string, 1)
	receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("unexpected Slack request: %s %v", r.Method, err)
		}
		incoming <- body
		w.WriteHeader(http.StatusOK)
	}))
	defer receiver.Close()
	t.Setenv("STARTSHIP_NOTIFICATION_LOCAL_HOST", strings.TrimPrefix(receiver.URL, "http://"))
	ws := Record{ID: "ws", Kind: "workspaces", Name: "Team"}
	if err := a.store.Save(ws); err != nil {
		t.Fatal(err)
	}
	name, kind, enabled := "Slack", "slack", true
	events := []string{deployFailed}
	_, _, err := a.mutateNotificationChannel(ws.ID, nil, notificationPatch{Name: &name, Kind: &kind, URL: &receiver.URL, Events: &events, Enabled: &enabled}, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	a.dispatchNotification(Record{WorkspaceID: ws.ID, Name: "API & <!channel>"}, deployFailed, time.Now())
	select {
	case body := <-incoming:
		if len(body) != 1 || body["text"] != "API &amp; &lt;!channel&gt;: deployment failed" {
			t.Fatalf("unexpected incoming webhook payload: %v", body)
		}
	default:
		t.Fatal("receiver did not get a Slack notification")
	}
	rows, err := a.notificationDeliveries(ws.ID)
	if err != nil || len(rows) != 1 || rows[0].Status != "ok" || rows[0].HTTPStatus != 200 {
		t.Fatalf("Slack delivery: %+v, %v", rows, err)
	}
}

func TestNotificationDeliveryErrors(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		error  string
	}{
		{"timeout", 0, "webhook request timed out"},
		{"server error", 500, "webhook returned a non-success status"},
		{"refused", 0, "could not reach webhook"},
		{"redirect", 307, "webhook returned a non-success status"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testServer(t)
			var followed atomic.Bool
			receiver := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/followup" {
					followed.Store(true)
					w.WriteHeader(http.StatusNoContent)
					return
				}
				switch tc.name {
				case "timeout":
					if _, err := io.Copy(io.Discard, r.Body); err != nil {
						t.Error(err)
					}
					// Let the client timeout before the receiver sends headers.
					select {
					case <-r.Context().Done():
					case <-time.After(15 * time.Second):
						t.Error("client did not cancel the request")
					}
				case "redirect":
					w.Header().Set("Location", "/followup?redirect-secret=1")
					w.WriteHeader(http.StatusTemporaryRedirect)
				default:
					w.WriteHeader(http.StatusInternalServerError)
					w.Write([]byte("private-path secret-query"))
				}
			}))
			defer receiver.Close()
			if tc.name == "refused" {
				receiver.Close()
			}
			t.Setenv("STARTSHIP_NOTIFICATION_LOCAL_HOST", strings.TrimPrefix(receiver.URL, "http://"))
			ws := Record{ID: "ws", Kind: "workspaces"}
			if err := a.store.Save(ws); err != nil {
				t.Fatal(err)
			}
			name, kind := "Receiver", "webhook"
			target := receiver.URL + "/private-path?token=secret-query"
			c, _, err := a.mutateNotificationChannel(ws.ID, nil, notificationPatch{Name: &name, Kind: &kind, URL: &target}, true, false, false)
			if err != nil {
				t.Fatal(err)
			}
			start := time.Now()
			a.deliverNotification(ws.ID, c, notificationPayload{Event: "test", At: time.Now()})
			if tc.name == "timeout" && time.Since(start) < 9*time.Second {
				t.Fatalf("unexpected timeout duration: %s", time.Since(start))
			}
			data, err := a.notificationsData(ws.ID)
			if err != nil || len(data.Deliveries) != 1 {
				t.Fatalf("delivery results: %+v, %v", data, err)
			}
			d := data.Deliveries[0]
			if d.Status != "failed" || d.HTTPStatus != tc.status || d.Error != tc.error || followed.Load() {
				t.Fatalf("unexpected delivery: %+v (redirect followed: %t)", d, followed.Load())
			}
			encoded, err := json.Marshal(data)
			if err != nil || strings.Contains(string(encoded), "private-path") || strings.Contains(string(encoded), "secret-query") || strings.Contains(string(encoded), "redirect-secret") {
				t.Fatal("snapshot exposes destination secrets")
			}
		})
	}
}

func TestNotificationRestrictedAccess(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	workspace := "workspaces/" + f.workspace.ID
	accountRequest(t, f.h, "GET", workspace, "", f.bob, 200)
	base := workspace + "/notifications/"
	channelBody := `{"name":"private channel","kind":"webhook","url":"https://hooks.example/private-path?token=private-query"}`
	channel := decodeBody[notificationChannel](t, accountRequest(t, f.h, "POST", base+"channels", channelBody, f.alice, 201).Body.Bytes())
	if _, err := f.a.store.DB.Exec("INSERT INTO notification_deliveries(channel_id,workspace_id,event,status,http_status,error,created_at) VALUES(?,?,'test','ok',204,'','now')", channel.ID, f.workspace.ID); err != nil {
		t.Fatal(err)
	}
	for _, req := range []struct{ method, path, body string }{
		{"GET", "channels", ""},
		{"POST", "channels", channelBody},
		{"PATCH", "channels/" + channel.ID, `{"enabled":true}`},
		{"DELETE", "channels/" + channel.ID, ""},
		{"POST", "channels/" + channel.ID + "/test", ""},
		{"GET", "deliveries", ""},
	} {
		body := accountRequest(t, f.h, req.method, base+req.path, req.body, f.bob, 403).Body.String()
		if strings.Contains(body, "private") || strings.Contains(body, channel.ID) {
			t.Fatal("restricted response disclosed channel data")
		}
	}
	srv := httptest.NewServer(f.h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	headers := http.Header{"Origin": {srv.URL}, "Cookie": {f.bob.Name + "=" + f.bob.Value}}
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/api/live", &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	var ready map[string]any
	if err = wsjson.Read(ctx, conn, &ready); err != nil || ready["type"] != "ready" {
		t.Fatalf("live ready: %v %v", ready, err)
	}
	if err := wsjson.Write(ctx, conn, map[string]string{"type": "subscribe", "id": "notify", "topic": "notifications", "workspace_id": f.workspace.ID}); err != nil {
		t.Fatal(err)
	}
	var msg map[string]any
	if err = wsjson.Read(ctx, conn, &msg); err != nil || msg["type"] != "error" || msg["error"] != "forbidden" || msg["data"] != nil {
		t.Fatalf("expected forbidden without a snapshot, got %v: %v", msg, err)
	}
}

func awaitNotification(t *testing.T, incoming <-chan notificationPayload, expected string) {
	t.Helper()
	select {
	case payload := <-incoming:
		if payload.Event != expected || payload.Workspace != "Team" || payload.Message == "" || payload.At.IsZero() ||
			(expected != "test" && payload.Project != "Site") {
			t.Fatalf("unexpected notification: %+v, want %s", payload, expected)
		}
	case <-time.After(3 * time.Second):
		t.Fatalf("did not receive %s", expected)
	}
}

func TestNotificationTransitions(t *testing.T) {
	a := testServer(t)
	target, incoming := notificationReceiver(t)
	ws := Record{ID: "ws", Kind: "workspaces", Name: "Team"}
	p := Record{ID: "project", Kind: "projects", WorkspaceID: ws.ID, Name: "Site", UptimeMonitoring: true, Active: "release"}
	if err := a.store.Save(ws); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	name, kind, enabled := "receiver", "webhook", true
	events := []string{uptimeDown, uptimeRecovered, deployFailed, deploySucceeded}
	_, _, err := a.mutateNotificationChannel(ws.ID, nil, notificationPatch{Name: &name, Kind: &kind, URL: &target, Events: &events, Enabled: &enabled}, true, false, false)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	for i, sample := range []struct{ cause, event string }{
		{}, {"probe failed", ""}, {"probe failed", uptimeDown}, {"probe failed", ""}, {"", uptimeRecovered}, {},
	} {
		if err := a.commitUptime(p, now.Add(time.Duration(i)*time.Second), 1, sample.cause); err != nil {
			t.Fatal(err)
		}
		if sample.event != "" {
			awaitNotification(t, incoming, sample.event)
		}
	}
	repo := t.TempDir()
	if err := command("", nil, "git", "-C", repo, "init", "-b", "main"); err != nil {
		t.Fatal(err)
	}
	p.GitURL, p.BuildType = "file://"+repo, "auto"
	if err := a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	a.queues["project:"+p.ID] = make(chan string, 10)
	deploy := func(want string) {
		t.Helper()
		release, err := a.enqueueProject(p, Record{})
		if err != nil {
			t.Fatal(err)
		}
		a.buildProject(release.ID)
		awaitNotification(t, incoming, want)
	}
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("no runnable source"), 0600); err != nil {
		t.Fatal(err)
	}
	commitProjectFixture(t, repo)
	deploy(deployFailed)
	if err := os.WriteFile(filepath.Join(repo, "index.html"), []byte("running"), 0600); err != nil {
		t.Fatal(err)
	}
	commitProjectFixture(t, repo)
	deploy(deploySucceeded)
	t.Cleanup(func() { command("", nil, "docker", "network", "rm", "startship-"+p.ID) })
	waitNotificationDeliveries(t, a, ws.ID, 4)
}

func waitNotificationDeliveries(t *testing.T, a *Server, ws string, want int) {
	t.Helper()
	for deadline := time.Now().Add(3 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		rows, err := a.notificationDeliveries(ws)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == want {
			return
		}
	}
	t.Fatalf("did not record %d deliveries", want)
}

func TestNotificationAPIMaskingLiveAndRetention(t *testing.T) {
	a := testServer(t)
	if err := a.bootstrapAdmin("manager", "password123"); err != nil {
		t.Fatal(err)
	}
	var uid string
	if err := a.store.DB.QueryRow("SELECT id FROM users WHERE username='manager'").Scan(&uid); err != nil {
		t.Fatal(err)
	}
	_, token := liveTestSession(t, a, uid)
	ws := Record{ID: "ws", Kind: "workspaces", Owner: uid, Name: "Team"}
	if err := a.store.Save(ws); err != nil {
		t.Fatal(err)
	}
	target, incoming := notificationReceiver(t)
	api := a.api()
	srv := httptest.NewServer(api)
	defer srv.Close()
	call := func(method, path, body string, status int) string {
		t.Helper()
		req := httptest.NewRequest(method, "/api/workspaces/ws/notifications/"+path, strings.NewReader(body))
		req.AddCookie(&http.Cookie{Name: "startship_session", Value: token})
		w := httptest.NewRecorder()
		api.ServeHTTP(w, req)
		if w.Code != status || strings.Contains(w.Body.String(), "private-secret") {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
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
	if err := wsjson.Write(ctx, conn, map[string]any{"type": "subscribe", "id": "notify", "topic": "notifications", "workspace_id": ws.ID}); err != nil {
		t.Fatal(err)
	}
	snapshot := func() notificationSnapshot {
		t.Helper()
		var msg struct {
			Type string
			Data json.RawMessage
		}
		if err := wsjson.Read(ctx, conn, &msg); err != nil || msg.Type != "snapshot" || strings.Contains(string(msg.Data), "private-secret") {
			t.Fatalf("unexpected notifications snapshot: %s %s %v", msg.Type, msg.Data, err)
		}
		var data notificationSnapshot
		if err := json.Unmarshal(msg.Data, &data); err != nil {
			t.Fatal(err)
		}
		return data
	}
	if data := snapshot(); len(data.Channels) != 0 || len(data.Deliveries) != 0 {
		t.Fatal(data)
	}
	channel := call("POST", "channels", `{"name":"Local","kind":"webhook","url":"`+target+`/private-secret?k=private-secret","events":["uptime.down"]}`, 201)
	call("POST", "channels", `{"name":"Bad","kind":"webhook","url":"file:///private-secret"}`, 400)
	var c notificationChannel
	if err := json.Unmarshal([]byte(channel), &c); err != nil || c.ID == "" || c.Host != target || c.Enabled {
		t.Fatalf("invalid masked channel: %s %v", channel, err)
	}
	if data := snapshot(); len(data.Channels) != 1 || data.Channels[0].Host != target {
		t.Fatal(data)
	}
	call("POST", "channels/"+c.ID+"/test", "", 202)
	awaitNotification(t, incoming, "test")
	if data := snapshot(); len(data.Deliveries) != 1 || data.Deliveries[0].Status != "ok" || data.Deliveries[0].HTTPStatus != 204 {
		t.Fatal(data)
	}
	call("GET", "channels", "", 200)
	call("GET", "deliveries", "", 200)
	call("PATCH", "channels/"+c.ID, `{"enabled":true}`, 200)
	for range 65 {
		if _, err := a.store.DB.Exec("INSERT INTO notification_deliveries(channel_id,workspace_id,event,status,http_status,error,created_at) VALUES(?,?,'test','ok',204,'','now')", c.ID, ws.ID); err != nil {
			t.Fatal(err)
		}
	}
	var count int
	if err := a.store.DB.QueryRow("SELECT count(*) FROM notification_deliveries WHERE workspace_id=?", ws.ID).Scan(&count); err != nil || count != 50 {
		t.Fatalf("retention count = %d, %v", count, err)
	}
	call("DELETE", "channels/"+c.ID, "", 200)
	waitNotificationDeliveries(t, a, ws.ID, 0)
	call("POST", "channels", `{"name":"Local","kind":"webhook","url":"`+target+`/private-secret"}`, 201)
	if err := a.store.Delete(ws); err != nil {
		t.Fatal(err)
	}
	if rows, err := a.notificationChannels(ws.ID); err != nil || len(rows) != 0 {
		t.Fatalf("workspace delete left channels: %v %v", rows, err)
	}
}
