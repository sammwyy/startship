package main

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

func TestDomainCommands(t *testing.T) {
	const list = `{"domains":[{"id":"d","name":"example.com","apex":false,"subdomains":true,"routes":2}],"can_manage":true}`
	for _, tt := range []struct {
		args               []string
		method, path, body string
	}{
		{[]string{"domains", "list", "Team"}, "GET", "/api/workspaces/w/domains", ""},
		{[]string{"workspaces", "domains", "list", "w"}, "GET", "/api/workspaces/w/domains", ""},
		{[]string{"domains", "add", "Team", "*.example.com"}, "POST", "/api/workspaces/w/domains", `{"name":"*.example.com","subdomains":false}`},
		{[]string{"domains", "add", "w", "test.example.com", "--subdomains"}, "POST", "/api/workspaces/w/domains", `{"name":"test.example.com","subdomains":true}`},
		{[]string{"domains", "add", "w", "test.example.com", "--subdomains=false"}, "POST", "/api/workspaces/w/domains", `{"name":"test.example.com","subdomains":false}`},
		{[]string{"domains", "rm", "Team", "*.example.com"}, "DELETE", "/api/workspaces/w/domains/d", ""},
		{[]string{"domains", "delete", "w", "d"}, "DELETE", "/api/workspaces/w/domains/d", ""},
		{[]string{"workspaces", "domains", "remove", "Team", "EXAMPLE.COM."}, "DELETE", "/api/workspaces/w/domains/d", ""},
	} {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			called := false
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == tt.method && r.URL.Path == tt.path {
					called = true
					if tt.body != "" {
						var got, want any
						if e := json.NewDecoder(r.Body).Decode(&got); e != nil {
							t.Fatal(e)
						}
						json.Unmarshal([]byte(tt.body), &want)
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body = %#v, want %#v", got, want)
						}
					}
					if tt.method == "GET" {
						w.Write([]byte(list))
					} else {
						w.Write([]byte(`{"ok":true}`))
					}
					return
				}
				if r.Method == "GET" && r.URL.Path == "/api/workspaces" {
					w.Write([]byte(`[{"id":"w","name":"Team"}]`))
					return
				}
				if r.Method == "GET" && r.URL.Path == "/api/workspaces/w/domains" {
					w.Write([]byte(list))
					return
				}
				t.Errorf("unexpected request %s %s", r.Method, r.URL)
				w.WriteHeader(404)
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
			if e := c.run(tt.args); e != nil {
				t.Fatal(e)
			}
			if !called {
				t.Fatal("expected domain request")
			}
			if tt.method == "GET" && (!strings.Contains(out.String(), "example.com") || !strings.Contains(out.String(), "SUBDOMAINS") || strings.Contains(out.String(), `"domains"`)) {
				t.Fatal(out.String())
			}
		})
	}
}

func TestDomainListEmptyAndJSON(t *testing.T) {
	for _, response := range []string{`{"domains":[],"can_manage":false}`, `{"domains":[{"id":"d","name":"example.com","apex":true,"subdomains":false,"routes":0}],"can_manage":true}`} {
		for _, raw := range []bool{false, true} {
			t.Run(response+" raw="+fmt.Sprint(raw), func(t *testing.T) {
				s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/workspaces" {
						w.Write([]byte(`[{"id":"w","name":"Team"}]`))
					} else {
						w.Write([]byte(response))
					}
				}))
				defer s.Close()
				var out bytes.Buffer
				c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client(), raw: raw}
				if e := c.run([]string{"domains", "list", "Team"}); e != nil {
					t.Fatal(e)
				}
				if raw {
					var got, want any
					if e := json.Unmarshal(out.Bytes(), &got); e != nil {
						t.Fatal(e)
					}
					json.Unmarshal([]byte(response), &want)
					if !reflect.DeepEqual(got, want) {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), "Can manage:") || !strings.Contains(out.String(), "APEX") {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func TestDomainErrors(t *testing.T) {
	for _, op := range []string{"list", "add", "rm"} {
		t.Run(op, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/workspaces" {
					w.Write([]byte(`[{"id":"w","name":"Team"}]`))
					return
				}
				if op == "rm" && r.Method == "GET" {
					w.Write([]byte(`{"domains":[{"id":"d","name":"example.com"}]}`))
					return
				}
				w.WriteHeader(409)
				w.Write([]byte(`{"error":"domain conflict"}`))
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			args := []string{"domains", op, "Team"}
			if op != "list" {
				args = append(args, "example.com")
			}
			if e := c.run(args); e == nil || !strings.Contains(e.Error(), "domain conflict") {
				t.Fatal(e)
			}
		})
	}
	for _, args := range [][]string{{"domains"}, {"domains", "list"}, {"domains", "add", "w"}, {"domains", "edit", "w", "d"}, {"domains", "list", "w", "extra"}, {"domains", "list", "w", "--subdomains"}, {"domains", "add", "w", "example.com", "--unknown"}, {"domains", "add", "w", "example.com", "--subdomains", "--subdomains"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			c := &cli{out: &bytes.Buffer{}, client: http.DefaultClient}
			if e := c.run(args); e == nil {
				t.Fatal("expected invalid arguments")
			}
		})
	}
	for _, response := range []string{`{"domains":[]}`, `[]`, `{"domains":null}`} {
		t.Run(response, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/api/workspaces" {
					w.Write([]byte(`[{"id":"w","name":"Team"}]`))
				} else {
					w.Write([]byte(response))
				}
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
			if e := c.run([]string{"domains", "rm", "w", "absent"}); e == nil {
				t.Fatal("expected lookup error")
			}
		})
	}
}
