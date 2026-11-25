package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPreviewCommands(t *testing.T) {
	empty := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/projects":
			w.Write([]byte(`[{"id":"p","name":"demo"}]`))
		case "/api/projects/p":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			if r.Method != "PATCH" || body["previews"] != true {
				t.Error(r.Method, body)
			}
			w.Write([]byte(`{"id":"p","previews":true}`))
		case "/api/projects/p/previews":
			if empty {
				w.Write([]byte(`[]`))
				return
			}
			w.Write([]byte(`[{"id":"preview","branch":"feature","status":"running","addresses":["https://feature--demo.localhost/"],"commit":"abc123"}]`))
		case "/api/projects/p/previews/preview":
			if r.Method != "DELETE" {
				t.Error(r.Method)
			}
			w.Write([]byte(`{"deleted":true}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &out}
	for _, args := range [][]string{{"projects", "update", "demo", "--previews", "on"}, {"previews", "demo"}, {"previews", "remove", "demo", "preview"}} {
		if err := c.run(args); err != nil {
			t.Fatal(err)
		}
	}
	for _, want := range []string{"feature", "running", "https://feature--demo.localhost/", "abc123"} {
		if !strings.Contains(out.String(), want) {
			t.Fatal(out.String())
		}
	}
	empty = true
	if err := c.run([]string{"previews", "demo"}); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"previews"}, {"previews", "missing"}, {"projects", "update", "demo", "--previews", "maybe"}} {
		if err := c.run(args); err == nil {
			t.Fatal(args)
		}
	}
}
