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

func TestUptimeAndStatusPageCommands(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.Method + " " + r.URL.Path {
		case "GET /api/projects":
			w.Write([]byte(`[{"id":"p","name":"API"}]`))
		case "GET /api/workspaces":
			w.Write([]byte(`[{"id":"ws","name":"Team"}]`))
		case "GET /api/projects/p/uptime":
			w.Write([]byte(`{"state":"down","uptime_24h":50,"incidents":[{"start":"2026-04-20T10:00:00Z","end":null,"cause":"unavailable"}]}`))
		case "PATCH /api/projects/p", "PATCH /api/workspaces/ws/status-page":
			var got map[string]any
			if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
				t.Error(err)
			}
			want := map[string]any{"uptime_monitoring": true}
			if r.URL.Path != "/api/projects/p" {
				want = map[string]any{"slug": "team-status", "title": "Team status", "projects": []any{"p"}, "enabled": true}
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("PATCH %s got %#v; want %#v", r.URL.Path, got, want)
			}
			json.NewEncoder(w).Encode(got)
		case "GET /api/workspaces/ws/status-page":
			w.Write([]byte(`{"slug":"team-status","enabled":true}`))
		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL)
			http.NotFound(w, r)
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
	for _, args := range [][]string{
		{"uptime", "API"}, {"uptime", "API", "enable"},
		{"status-page", "set", "Team", "--slug", "team-status", "--title", "Team status", "--projects", "API", "--enable"},
		{"status-page", "show", "Team"},
	} {
		if err := c.run(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"24h uptime: 50.00%", "ongoing: unavailable", "slug: team-status"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output missing %q: %s", want, out.String())
		}
	}
}
