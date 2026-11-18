package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionAndCommands(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/login" {
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: "session"})
			w.Write([]byte(`{"id":"user"}`))
			return
		}
		cookie, e := r.Cookie("startship_session")
		if e != nil || cookie.Value != "session" {
			t.Error("missing session cookie")
		}
		switch r.URL.Path {
		case "/api/me":
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: "rotated"})
			w.Write([]byte(`{"username":"admin"}`))
		case "/api/workspaces":
			w.Write([]byte(`[{"id":"ws","name":"Team"}]`))
		case "/api/projects":
			if r.URL.RawQuery != "" && r.URL.Query().Get("workspace_id") != "ws" {
				t.Error("incorrect workspace filter", r.URL.RawQuery)
			}
			if r.Method == "POST" {
				w.Write([]byte(`{"id":"project","name":"Website"}`))
			} else {
				w.Write([]byte(`[]`))
			}
		default:
			w.WriteHeader(403)
			w.Write([]byte(`{"error":"access denied"}`))
		}
	}))
	defer s.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: s.URL}, path: filepath.Join(t.TempDir(), "config.json"), in: bufio.NewReader(strings.NewReader("admin\npassword\n")), out: &out, client: s.Client()}
	for _, a := range [][]string{{"login"}, {"workspaces", "list"}, {"projects", "create", "Website", "Team"}, {"projects", "list", "Team", "--json"}, {"whoami"}} {
		if e := c.run(a); e != nil {
			t.Fatal(e)
		}
	}
	if !strings.Contains(out.String(), "Website") || !strings.Contains(out.String(), "[]") {
		t.Fatal(out.String())
	}
	if c.Token != "rotated" {
		t.Fatal("session not rotated")
	}
	info, e := os.Stat(c.path)
	if e != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config must be private", e)
	}
	c.Token = "session"
	if e := c.run([]string{"routes", "list"}); e == nil || !strings.Contains(e.Error(), "access denied") {
		t.Fatal(e)
	}
}

func TestInvalidCommands(t *testing.T) {
	for _, a := range [][]string{{"missing"}, {"projects", "create"}, {"projects", "update", "x", "null"}, {"deploy"}, {"account"}, {"routes", "add"}} {
		t.Run(strings.Join(a, "_"), func(t *testing.T) {
			c := &cli{out: &bytes.Buffer{}, client: http.DefaultClient}
			if e := c.run(a); e == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestSessionRefreshAndScopedActivity(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/refresh":
			cookie, err := r.Cookie("startship_refresh")
			if err != nil || cookie.Value != "refresh" || r.Method != http.MethodPost {
				t.Error("refresh request missing the saved refresh cookie")
			}
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: "renewed"})
			w.Write([]byte(`{"ok":true}`))
			return
		}
		if _, err := r.Cookie("startship_refresh"); err == nil {
			t.Error("refresh cookie sent on an ordinary request")
		}
		if token, err := r.Cookie("startship_session"); err != nil || token.Value != "renewed" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"expired"}`))
			return
		}
		switch r.URL.Path {
		case "/api/projects":
			w.Write([]byte(`[{"id":"p","name":"Site"}]`))
		case "/api/projects/p/activity":
			w.Write([]byte(`[{"action":"project.create","target":"Site"}]`))
		default:
			t.Error("unexpected endpoint", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: server.URL, Token: "expired", Refresh: "refresh"}, client: server.Client(),
		out: &out, path: filepath.Join(t.TempDir(), "config.json")}
	if err := c.run([]string{"activity", "project", "Site"}); err != nil || !strings.Contains(out.String(), "project.create") {
		t.Fatal(err, out.String())
	}
	saved, err := os.ReadFile(c.path)
	if err != nil {
		t.Fatal(err)
	}
	var credentials config
	if err := json.Unmarshal(saved, &credentials); err != nil || credentials.Token != "renewed" || credentials.Refresh != "refresh" {
		t.Fatal("renewed access and refresh were not persisted", err)
	}
}

func TestEmptyListsAndAPIError(t *testing.T) {
	for _, body := range []string{`[]`, `{"error":"broken"}`, `not json`} {
		t.Run(body, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.Contains(body, "error") {
					w.WriteHeader(500)
				}
				w.Write([]byte(body))
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
			e := c.run([]string{"workspaces", "list"})
			if body == `[]` {
				if e != nil || !strings.Contains(out.String(), "ID") {
					t.Fatal(e, out.String())
				}
				if _, e = c.resolve("workspaces", "absent"); e == nil {
					t.Fatal("missing name accepted")
				}
			} else if e == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestLatestBuildLogs(t *testing.T) {
	for _, releases := range []string{`[]`, `[{"id":"old","created":"2026-01-01"},{"id":"new","created":"2026-02-01"}]`} {
		t.Run(releases, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/projects":
					w.Write([]byte(`[{"id":"p","name":"site"}]`))
				case "/api/releases":
					w.Write([]byte(releases))
				case "/api/releases/new":
					w.Write([]byte(`{"log":"latest failed build"}`))
				default:
					t.Error("unexpected path", r.URL.Path)
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
			if e := c.run([]string{"logs", "site"}); e != nil {
				t.Fatal(e)
			}
			if releases != "[]" && !strings.Contains(out.String(), "latest failed build") {
				t.Fatal(out.String())
			}
		})
	}
}
