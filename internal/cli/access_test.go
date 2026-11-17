package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestAccessCommands(t *testing.T) {
	fixtures := map[string]string{
		"/api/users":      `[{"id":"u","username":"bob"}]`,
		"/api/roles":      `[{"id":"member","name":"Member"},{"id":"r","name":"My Role"}]`,
		"/api/workspaces": `[{"id":"w","name":"Team"}]`,
		"/api/projects":   `[{"id":"p","name":"Site"}]`,
	}
	tests := []struct{ args, method, path, query, body, response, output string }{
		{args: "users list", method: "GET", path: "/api/users", response: fixtures["/api/users"], output: "bob"},
		{args: "users create bob", method: "POST", path: "/api/users", body: `{"username":"bob","password":"temporary-password"}`},
		{args: "users rename bob robert", method: "PATCH", path: "/api/users/u", body: `{"username":"robert"}`},
		{args: "users reset-password u", method: "PATCH", path: "/api/users/u", body: `{"password":"temporary-password"}`},
		{args: "users delete bob", method: "DELETE", path: "/api/users/u"},
		{args: "roles list", method: "GET", path: "/api/roles", response: fixtures["/api/roles"], output: "Member"},
		{args: "roles get Member", method: "GET", path: "/api/roles/member"},
		{args: "roles create Support --clone-of member --color #123456 --icon star --perm project.view,logs.view --perm traffic.view", method: "POST", path: "/api/roles", body: `{"name":"Support","clone_of":"member","color":"#123456","icon":"star","permissions":["project.view","logs.view","traffic.view"]}`},
		{args: "roles edit r --name Support --perm=", method: "PATCH", path: "/api/roles/r", body: `{"name":"Support","permissions":[]}`},
		{args: "roles delete r", method: "DELETE", path: "/api/roles/r"},
		{args: "permissions", method: "GET", path: "/api/permissions", response: `{"permissions":[{"key":"logs.view","label":"Read logs"}],"icons":["star"]}`, output: "Read logs"},
		{args: "grant bob Member", method: "POST", path: "/api/assignments", body: `{"user_id":"u","role_id":"member","scope":"platform","scope_id":""}`},
		{args: "grant bob Member --workspace Team", method: "POST", path: "/api/assignments", body: `{"user_id":"u","role_id":"member","scope":"workspace","scope_id":"w"}`},
		{args: "grant u member --project Site", method: "POST", path: "/api/assignments", body: `{"user_id":"u","role_id":"member","scope":"project","scope_id":"p"}`},
		{args: "revoke bob Member --workspace Team", method: "DELETE", path: "/api/assignments/a"},
		{args: "assignments list --user bob --project Site", method: "GET", path: "/api/assignments", query: "scope=project&scope_id=p&user_id=u", response: `[]`, output: "ROLE_ID"},
		{args: "assignments list", method: "GET", path: "/api/assignments", response: `[]`, output: "USER_ID"},
		{args: "members Team", method: "GET", path: "/api/workspaces/w/members", response: `{"members":[{"user_id":"u","username":"bob","roles":[{"role_id":"r","scope":"workspace","scope_id":"w"},{"role_id":"member","scope":"project","scope_id":"p","project_name":"web"}]}],"can_manage":true}`, output: "My Role, Member (project web)"},
		{args: "workspaces members w", method: "GET", path: "/api/workspaces/w/members", response: `{"members":[],"can_manage":false}`, output: "USER_ID"},
		{args: "overrides list workspace Team", method: "GET", path: "/api/overrides", query: "scope=workspace&scope_id=w", response: `[]`, output: "SUBJECT_ID"},
		{args: "overrides set workspace Team --user bob --allow logs.view --deny deploy", method: "PUT", path: "/api/overrides", body: `{"scope":"workspace","scope_id":"w","subject_type":"user","subject_id":"u","allow":["logs.view"],"deny":["deploy"]}`},
		{args: "overrides set project Site --role member", method: "PUT", path: "/api/overrides", body: `{"scope":"project","scope_id":"p","subject_type":"role","subject_id":"member","allow":[],"deny":[]}`},
		{args: "effective bob --workspace Team", method: "GET", path: "/api/effective", query: "user_id=u&workspace_id=w"},
		{args: "effective --project p", method: "GET", path: "/api/effective", query: "project_id=p"},
		{args: "effective", method: "GET", path: "/api/effective"},
		{args: "audit --workspace Team", method: "GET", path: "/api/audit", query: "workspace_id=w", response: `[{"id":1,"action":"user.create","actor":"admin"}]`, output: "user.create"},
	}
	for _, tt := range tests {
		t.Run(tt.args, func(t *testing.T) {
			called := false
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == tt.method && r.URL.Path == tt.path {
					called = true
					if r.URL.RawQuery != tt.query {
						t.Errorf("query = %q, want %q", r.URL.RawQuery, tt.query)
					}
					if tt.body != "" {
						var got, want any
						if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
							t.Error(e)
						}
						json.Unmarshal([]byte(tt.body), &want)
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body = %#v, want %#v", got, want)
						}
					}
					response := tt.response
					if response == "" {
						response = `{"ok":true}`
					}
					w.Write([]byte(response))
					return
				}
				if r.Method == "GET" {
					if response, ok := fixtures[r.URL.Path]; ok {
						w.Write([]byte(response))
						return
					}
					if r.URL.Path == "/api/assignments" {
						if r.URL.Query().Get("user_id") != "u" || r.URL.Query().Get("scope") != "workspace" || r.URL.Query().Get("scope_id") != "w" {
							t.Error("incorrect revoke filter", r.URL.RawQuery)
						}
						w.Write([]byte(`[{"id":"a","user_id":"u","role_id":"member","scope":"workspace","scope_id":"w"}]`))
						return
					}
				}
				t.Errorf("unexpected request %s %s", r.Method, r.URL)
				w.WriteHeader(404)
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, in: bufio.NewReader(strings.NewReader("temporary-password\n")), out: &out, client: s.Client()}
			if e := c.run(strings.Fields(tt.args)); e != nil {
				t.Fatal(e)
			}
			if !called {
				t.Fatal("expected request not sent")
			}
			if !strings.Contains(out.String(), tt.output) {
				t.Fatalf("output %q missing %q", out.String(), tt.output)
			}
		})
	}
}

func TestAccessErrorsAndEmpty(t *testing.T) {
	for _, command := range []string{"users list", "roles list", "permissions", "members Team", "grant bob Member", "revoke bob Member", "assignments list", "overrides list workspace Team", "overrides set workspace Team --user bob", "effective bob", "audit"} {
		t.Run(command, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(403)
				w.Write([]byte(`{"error":"permission denied"}`))
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if e := c.run(strings.Fields(command)); e == nil || !strings.Contains(e.Error(), "permission denied") {
				t.Fatal(e)
			}
		})
	}
	for _, command := range []string{"users list", "roles list", "assignments list", "audit"} {
		t.Run("empty "+command, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
			if e := c.run(append(strings.Fields(command), "--json")); e != nil {
				t.Fatal(e)
			}
			if strings.TrimSpace(out.String()) != "[]" {
				t.Fatal(out.String())
			}
		})
	}
	for _, command := range []string{"users", "roles", "roles create", "users create", "members", "overrides list platform x", "grant bob Member --workspace w --project p", "effective --unknown x", "audit --workspace", "roles list --icon star", "overrides list workspace w --user bob"} {
		t.Run("invalid "+command, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(`[]`)) }))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if e := c.run(strings.Fields(command)); e == nil {
				t.Fatal("expected error")
			}
		})
	}
}

func TestAccessResolutionFailures(t *testing.T) {
	for _, fixture := range []string{`[]`, `[{"id":"u1","username":"bob"},{"id":"u2","username":"bob"}]`} {
		t.Run(fixture, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(fixture)) }))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if _, e := c.resolve("users", "bob"); e == nil {
				t.Fatal("expected unresolved/ambiguous error")
			}
		})
	}
}

func TestMemberRolesJSONAndLookupError(t *testing.T) {
	const response = `{"members":[{"user_id":"u","username":"bob","roles":[{"role_id":"member","scope":"project","scope_id":"p","project_name":"web"}]}],"can_manage":true}`
	for _, raw := range []bool{false, true} {
		t.Run(fmt.Sprint(raw), func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/workspaces":
					w.Write([]byte(`[{"id":"w","name":"Team"}]`))
				case "/api/workspaces/w/members":
					w.Write([]byte(response))
				case "/api/roles":
					if raw {
						t.Error("JSON must not resolve roles")
					}
					w.WriteHeader(403)
					w.Write([]byte(`{"error":"roles unavailable"}`))
				default:
					t.Error("unexpected request", r.URL.Path)
				}
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client(), raw: raw}
			err := c.run([]string{"members", "Team"})
			if !raw {
				if err == nil || !strings.Contains(err.Error(), "roles unavailable") {
					t.Fatal(err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			var got, want any
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			json.Unmarshal([]byte(response), &want)
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("JSON changed: %s", out.String())
			}
		})
	}
}
