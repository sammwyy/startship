package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestTransferCommand(t *testing.T) {
	want := map[string]any{
		"resource":                 map[string]any{"kind": "projects", "id": "p1"},
		"destination_workspace_id": "ws",
		"include":                  []any{map[string]any{"kind": "services", "id": "db"}},
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/workspaces":
			w.Write([]byte(`[{"id":"ws","name":"Team"}]`))
		case "/api/transfers/move":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || r.Method != "POST" || !reflect.DeepEqual(body, want) {
				t.Errorf("unexpected transfer request: %s %+v %v", r.Method, body, err)
			}
			w.Write([]byte(`{"resources":[]}`))
		default:
			t.Errorf("unexpected request %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &out}
	if err := c.run([]string{"transfer", "move", "projects", "p1", "Team", "--include", "services:db"}); err != nil {
		t.Fatal(err)
	}
	if out.String() != "resources: []\n" {
		t.Fatal(out.String())
	}
}
