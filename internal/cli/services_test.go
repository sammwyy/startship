package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestServiceVolumeCommands(t *testing.T) {
	for _, tt := range []struct {
		args                               []string
		method, path, body, response, text string
	}{
		{[]string{"catalog"}, "GET", "/api/catalog", "", `[{"id":"redis","name":"Redis","versions":["7.4"],"port":6379}]`, "VERSIONS"},
		{[]string{"services", "Team"}, "GET", "/api/workspaces/w/services", "", `{"services":[{"id":"s","name":"cache","engine":"redis","version":"7.4","status":"running","external_port":16380}]}`, "EXTERNAL_PORT"},
		{[]string{"services", "list", "w"}, "GET", "/api/workspaces/w/services", "", `{"services":[]}`, "ENGINE"},
		{[]string{"services", "create", "Team", "redis", "cache", "--version", "7.4", "--set", "user=test", "--set=password=a=b", "--port", "16380", "--volume", "v"}, "POST", "/api/workspaces/w/services", `{"name":"cache","engine":"redis","version":"7.4","config":{"user":"test","password":"a=b"},"external_port":16380,"volume_id":"v"}`, `{"id":"s","status":"pending"}`, "pending"},
		{[]string{"services", "create", "w", "redis", "cache"}, "POST", "/api/workspaces/w/services", `{"name":"cache","engine":"redis"}`, `{"id":"s"}`, "id:"},
		{[]string{"services", "show", "s"}, "GET", "/api/services/s", "", `{"id":"s","config":{"password":"secret"}}`, "secret"},
		{[]string{"services", "patch", "s", "--version=7.2", "--set", "password=", "--port=0"}, "PATCH", "/api/services/s", `{"version":"7.2","config":{"password":""},"external_port":0}`, `{"id":"s"}`, "id:"},
		{[]string{"services", "start", "s"}, "POST", "/api/services/s/start", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "stop", "s"}, "POST", "/api/services/s/stop", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "restart", "s"}, "POST", "/api/services/s/restart", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "logs", "s"}, "GET", "/api/services/s/logs?tail=200", "", `{"log":"ready\n"}`, "ready\n"},
		{[]string{"services", "logs", "s", "--tail", "50"}, "GET", "/api/services/s/logs?tail=50", "", `{"log":""}`, ""},
		{[]string{"services", "delete", "s"}, "DELETE", "/api/services/s", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "delete", "s", "--delete-volume"}, "DELETE", "/api/services/s?delete_volume=true", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "delete", "s", "--delete-volume=false"}, "DELETE", "/api/services/s", "", `{"ok":true}`, "ok: true"},
		{[]string{"services", "attach", "s", "API", "cache_db"}, "POST", "/api/services/s/attachments", `{"project_id":"p","alias":"cache_db"}`, `{"id":"s"}`, "Redeploy"},
		{[]string{"services", "detach", "s", "p"}, "DELETE", "/api/services/s/attachments/p", "", `{"id":"s"}`, "Redeploy"},
		{[]string{"services", "connections", "API"}, "GET", "/api/projects/p/services", "", `{"attached":[],"available":[]}`, "attached:"},
		{[]string{"volumes", "Team"}, "GET", "/api/workspaces/w/volumes", "", `{"volumes":[{"id":"v","name":"data","size_bytes":123,"project_id":"p","mount_path":"/data"}]}`, "SIZE_BYTES"},
		{[]string{"volumes", "list", "w"}, "GET", "/api/workspaces/w/volumes", "", `{"volumes":[]}`, "MOUNT_PATH"},
		{[]string{"volumes", "create", "w", "data"}, "POST", "/api/workspaces/w/volumes", `{"name":"data"}`, `{"id":"v"}`, "id:"},
		{[]string{"volumes", "create", "Team", "data", "--project=API", "--mount", "/data"}, "POST", "/api/workspaces/w/volumes", `{"name":"data","project_id":"p","mount_path":"/data"}`, `{"id":"v"}`, "Redeploy"},
		{[]string{"volumes", "attach", "v", "API", "/data"}, "PATCH", "/api/volumes/v", `{"project_id":"p","mount_path":"/data"}`, `{"id":"v"}`, "Redeploy"},
		{[]string{"volumes", "detach", "v"}, "PATCH", "/api/volumes/v", `{"project_id":"","mount_path":""}`, `{"id":"v"}`, "Redeploy"},
		{[]string{"volumes", "delete", "v"}, "DELETE", "/api/volumes/v", "", `{"ok":true}`, "ok: true"},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(strings.Join(tt.args, " ")+fmt.Sprint(raw), func(t *testing.T) {
				called := false
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == tt.method && r.URL.RequestURI() == tt.path {
						called = true
						var got, want any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Error(err)
						}
						if tt.body != "" {
							if err := json.Unmarshal([]byte(tt.body), &want); err != nil {
								t.Error(err)
							}
						}
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body = %#v, want %#v", got, want)
						}
						fmt.Fprint(w, tt.response)
						return
					}
					switch r.URL.Path {
					case "/api/workspaces":
						fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
					case "/api/projects":
						fmt.Fprint(w, `[{"id":"p","name":"API"}]`)
					default:
						t.Errorf("unexpected request %s %s", r.Method, r.URL)
						w.WriteHeader(404)
					}
				}))
				defer s.Close()
				var out bytes.Buffer
				c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
				args := append([]string{}, tt.args...)
				if raw {
					args = append(args, "--json")
				}
				if err := c.run(args); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("expected resource request")
				}
				if raw {
					var got, want any
					if err := json.Unmarshal(out.Bytes(), &got); err != nil {
						t.Fatal(err, out.String())
					}
					json.Unmarshal([]byte(tt.response), &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), tt.text) {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func TestResourceArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{"catalog", "extra"}, {"catalog", "--unknown"}, {"services"}, {"services", "list"}, {"services", "create", "w", "redis"}, {"services", "show", "s", "extra"}, {"services", "patch", "s"}, {"services", "patch", "s", "--set", "bad"}, {"services", "patch", "s", "--set", "=value"}, {"services", "patch", "s", "--version="}, {"services", "patch", "s", "--port", "-1"}, {"services", "create", "w", "redis", "cache", "--port", "65536"}, {"services", "patch", "s", "--port", "x"}, {"services", "logs", "s", "--tail", "0"}, {"services", "logs", "s", "--tail", "10001"}, {"services", "logs", "s", "--tail", "x"}, {"services", "delete", "s", "--delete-volume=bad"}, {"services", "delete", "s", "--delete-volume", "--delete-volume"}, {"services", "show", "s", "--port", "5"}, {"services", "create", "w", "redis", "cache", "--port"}, {"services", "create", "w", "redis", "cache", "--port", "1", "--port", "2"}, {"services", "attach", "s", "p"}, {"services", "detach", "s"}, {"services", "unknown", "x"}, {"services", "list", "w", "--unknown"},
		{"volumes"}, {"volumes", "list"}, {"volumes", "list", "w", "extra"}, {"volumes", "create", "w"}, {"volumes", "create", "w", "data", "--project", "p"}, {"volumes", "create", "w", "data", "--mount", "/data"}, {"volumes", "create", "w", "data", "--project=", "--mount=/data"}, {"volumes", "create", "w", "data", "--project", "p", "--mount"}, {"volumes", "create", "w", "data", "--mount=/data", "--mount=/more"}, {"volumes", "attach", "v", "p"}, {"volumes", "detach", "v", "--project", "p"}, {"volumes", "delete", "v", "--unknown"}, {"volumes", "unknown", "x"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				t.Errorf("unexpected request %s", r.URL)
				w.WriteHeader(500)
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if err := c.run(args); err == nil {
				t.Fatal("expected argument error")
			}
		})
	}
}

func TestResourceAPIErrors(t *testing.T) {
	for _, args := range [][]string{{"catalog"}, {"services", "list", "Team"}, {"services", "create", "w", "redis", "cache"}, {"services", "show", "s"}, {"services", "patch", "s", "--port", "0"}, {"services", "start", "s"}, {"services", "stop", "s"}, {"services", "restart", "s"}, {"services", "logs", "s"}, {"services", "delete", "s"}, {"services", "attach", "s", "API", "cache"}, {"services", "detach", "s", "p"}, {"services", "connections", "API"}, {"volumes", "list", "w"}, {"volumes", "create", "Team", "data"}, {"volumes", "attach", "v", "API", "/data"}, {"volumes", "detach", "v"}, {"volumes", "delete", "v"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/workspaces":
					fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
				case "/api/projects":
					fmt.Fprint(w, `[{"id":"p","name":"API"}]`)
				default:
					w.WriteHeader(409)
					fmt.Fprint(w, `{"error":"resource conflict"}`)
				}
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if err := c.run(args); err == nil || !strings.Contains(err.Error(), "resource conflict") {
				t.Fatal(err)
			}
		})
	}
}

func TestResourceListResponseErrors(t *testing.T) {
	for _, cmd := range []string{"services", "volumes"} {
		for _, response := range []string{`[]`, `{}`, `{"services":null,"volumes":null}`} {
			t.Run(cmd+response, func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/workspaces" {
						fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
					} else {
						fmt.Fprint(w, response)
					}
				}))
				defer s.Close()
				c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
				if err := c.run([]string{cmd, "list", "w"}); err == nil {
					t.Fatal("expected invalid list response")
				}
			})
		}
	}
}
