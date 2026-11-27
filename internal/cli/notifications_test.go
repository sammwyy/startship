package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestNotificationsCommands(t *testing.T) {
	const base = "/api/workspaces/ws/notifications"
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/workspaces":
			w.Write([]byte(`[{"id":"ws","name":"Team"}]`))
		case "POST " + base + "/channels", "PATCH " + base + "/channels/c":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			want := map[string]any{"enabled": false, "events": []any{}}
			if r.Method == "POST" {
				want = map[string]any{"name": "Alerts", "kind": "webhook", "url": "https://hooks.example/private", "events": []any{"deploy.failed", "uptime.down"}, "enabled": true}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s got %#v; want %#v", r.Method, got, want)
			}
			w.Write([]byte(`{"id":"c","host":"https://hooks.example"}`))
		case "GET " + base + "/channels":
			w.Write([]byte(`[{"id":"c","name":"Alerts","kind":"webhook","host":"https://hooks.example","enabled":true,"events":["deploy.failed"]}]`))
		case "GET " + base + "/deliveries":
			w.Write([]byte(`[{"channel_id":"c","event":"deploy.failed","status":"ok","http_status":204,"created_at":"today"}]`))
		case "POST " + base + "/channels/c/test":
			w.WriteHeader(202)
			w.Write([]byte(`{"queued":true}`))
		case "DELETE " + base + "/channels/c":
			w.Write([]byte(`{"deleted":true}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
	for _, args := range [][]string{
		{"list", "Team"},
		{"add", "Team", "--name", "Alerts", "--kind", "webhook", "--url", "https://hooks.example/private", "--events", "deploy.failed,uptime.down", "--enable"},
		{"update", "Team", "c", "--disable", "--events", ""},
		{"test", "Team", "c"},
		{"log", "Team"},
		{"remove", "Team", "c"},
	} {
		if err := c.run(append([]string{"notifications"}, args...)); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"https://hooks.example", "deploy.failed", "204", "queued: true", "deleted: true"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
	if strings.Contains(out.String(), "/private") {
		t.Errorf("output exposed the webhook path: %s", out.String())
	}
}
