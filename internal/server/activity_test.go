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
	"github.com/coder/websocket/wsjson"
)

func TestActivityScopesAndLiveUpdates(t *testing.T) {
	a := testServer(t)
	alice, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	h := a.api()
	current := accountLogin(t, h, "alice", "password123")
	bob := accountLogin(t, h, "bob", "password123")
	ws := a.store.List("workspaces", alice)[0]
	srv := httptest.NewServer(h)
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, srv.URL+"/api/live", &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": []string{current.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer c.CloseNow()
	var msg struct {
		Type string
		Data []activityEntry
	}
	read := func(kind string) []activityEntry {
		t.Helper()
		if err := wsjson.Read(ctx, c, &msg); err != nil || msg.Type != kind {
			t.Fatalf("expected %s: %+v, %v", kind, msg, err)
		}
		return msg.Data
	}
	read("ready")
	if err := wsjson.Write(ctx, c, map[string]string{"type": "subscribe", "id": "w", "topic": "workspace_activity", "workspace_id": ws.ID}); err != nil {
		t.Fatal(err)
	}
	if entries := read("snapshot"); len(entries) != 0 {
		t.Fatal(entries)
	}
	create := func(name string) string {
		t.Helper()
		w := accountRequest(t, h, "POST", "projects", `{"name":"`+name+`","workspace_id":"`+ws.ID+`"}`, current, 200)
		var project Record
		if err := json.Unmarshal(w.Body.Bytes(), &project); err != nil {
			t.Fatal(err)
		}
		return project.ID
	}
	p := create("site")
	if entries := read("snapshot"); len(entries) != 1 || entries[0].ScopeID != p || entries[0].SessionID != "" {
		t.Fatal(entries)
	}
	create("api")
	w := accountRequest(t, h, "GET", "projects/"+p+"/activity", "", current, 200)
	var entries []activityEntry
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil || len(entries) != 1 || entries[0].Target != "site" {
		t.Fatal("project activity was not filtered", entries, err)
	}
	accountRequest(t, h, "GET", "workspaces/"+ws.ID+"/activity", "", bob, 404)
	accountRequest(t, h, "GET", "projects/"+p+"/activity", "", bob, 404)
	w = accountRequest(t, h, "GET", "me/activity", "", current, 200)
	if err := json.Unmarshal(w.Body.Bytes(), &entries); err != nil || len(entries) != 3 ||
		entries[0].Action != "project.create" || entries[0].SessionID == "" || entries[0].SessionID != entries[2].SessionID {
		t.Fatal("missing account session attribution", entries, err)
	}
	w = accountRequest(t, h, "GET", "me/activity", "", bob, 200)
	if strings.Contains(w.Body.String(), "project.create") {
		t.Fatal("another account's actions were included")
	}
}
