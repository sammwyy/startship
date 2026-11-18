package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/sammwyy/startship/internal/firewall"
)

func cliFirewallPolicy() firewall.Policy {
	p := firewall.DefaultPolicy()
	p.Mode = "monitor"
	p.Limits.MaxHeaderBytes = 4096
	p.Rules = []firewall.Rule{{ID: "r1", Name: "First", Enabled: true, Match: "all", Conditions: []firewall.Condition{{Field: "path", Op: "eq", Value: "/private"}}, Action: firewall.Block}, {ID: "r2", Name: "Second", Enabled: false, Match: "any", Conditions: []firewall.Condition{{Field: "method", Op: "in", Value: []string{"POST", "PUT"}}}, Action: firewall.Log}}
	return p
}
func cliFirewallJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
func TestFirewallCommands(t *testing.T) {
	file := filepath.Join(t.TempDir(), "policy.json")
	if err := os.WriteFile(file, []byte(cliFirewallJSON(cliFirewallPolicy())), 0600); err != nil {
		t.Fatal(err)
	}
	modified := func(change func(*firewall.Policy)) any { p := cliFirewallPolicy(); change(&p); return p }
	enabled := cliFirewallPolicy().Rules[1]
	enabled.Enabled = true
	disabled := cliFirewallPolicy().Rules[0]
	disabled.Enabled = false
	rule := firewall.Rule{Name: "Login", Enabled: true, Match: "any", Conditions: []firewall.Condition{{Field: "method", Op: "in", Value: []string{"POST", "PUT"}}, {Field: "path", Op: "prefix", Value: "/login"}}, Action: firewall.RateLimit, Rate: &firewall.Rate{Requests: 10, WindowSeconds: 60, By: "ip"}, BanSeconds: 900}
	for _, tc := range []struct {
		args           []string
		method, path   string
		body           any
		response, text string
	}{
		{[]string{"show", "Team"}, "GET", "", nil, cliFirewallJSON(cliFirewallPolicy()), "mode:"},
		{[]string{"export", "w"}, "GET", "", nil, cliFirewallJSON(cliFirewallPolicy()), `"mode"`},
		{[]string{"limits", "w"}, "GET", "", nil, cliFirewallJSON(cliFirewallPolicy()), `"max_header_bytes"`},
		{[]string{"apply", "Team", file}, "PUT", "", cliFirewallPolicy(), `{"ok":true}`, "ok:"},
		{[]string{"mode", "Team", "enforce"}, "PUT", "", modified(func(p *firewall.Policy) { p.Mode = "enforce" }), `{"ok":true}`, "ok:"},
		{[]string{"managed", "w", "sqli", "off", "--action=log"}, "PUT", "", modified(func(p *firewall.Policy) {
			p.Managed["sqli"] = firewall.ManagedRule{Enabled: false, Action: firewall.Log}
		}), `{"ok":true}`, "ok:"},
		{[]string{"managed", "w", "crawlers", "on"}, "PUT", "", modified(func(p *firewall.Policy) {
			p.Managed["crawlers"] = firewall.ManagedRule{Enabled: true, Action: firewall.Block}
		}), `{"ok":true}`, "ok:"},
		{[]string{"limits", "Team", "--rate=100/30s", "--rate-action", "ban", "--ban", "7d", "--concurrent=4", "--max-body", "10MB", "--max-url=512KB"}, "PUT", "", modified(func(p *firewall.Policy) {
			p.Limits.Rate = &firewall.WorkspaceRate{Requests: 100, WindowSeconds: 30, Action: firewall.Ban, BanSeconds: 604800}
			p.Limits.ConcurrentPerIP = 4
			p.Limits.MaxBodyBytes = 10000000
			p.Limits.MaxURLBytes = 512000
		}), `{"ok":true}`, "ok:"},
		{[]string{"limits", "w", "--rate", "2/1h"}, "PUT", "", modified(func(p *firewall.Policy) {
			p.Limits.Rate = &firewall.WorkspaceRate{Requests: 2, WindowSeconds: 3600, Action: firewall.Block}
		}), `{"ok":true}`, "ok:"},
		{[]string{"limits", "w", "--max-body=0", "--max-url=0", "--concurrent=0"}, "PUT", "", modified(func(p *firewall.Policy) {
			p.Limits.MaxBodyBytes = 0
			p.Limits.MaxURLBytes = 0
			p.Limits.ConcurrentPerIP = 0
		}), `{"ok":true}`, "ok:"},
		{[]string{"rules", "list", "Team"}, "GET", "", nil, cliFirewallJSON(cliFirewallPolicy()), "CONDITIONS"},
		{[]string{"rules", "add", "w", "--name", "Login", "--when", "method in POST, PUT", "--when", "path prefix /login", "--any", "--action=rate_limit", "--rate", "10/1m", "--ban=15m"}, "POST", "/rules", rule, `{"ok":true}`, "ok:"},
		{[]string{"rules", "add", "w", "--name", "Deny", "--when", "header.X-Test exists", "--action=block"}, "POST", "/rules", firewall.Rule{Name: "Deny", Enabled: true, Match: "all", Conditions: []firewall.Condition{{Field: "header.X-Test", Op: "exists", Value: ""}}, Action: firewall.Block}, `{"ok":true}`, "ok:"},
		{[]string{"rules", "rm", "w", "r/1"}, "DELETE", "/rules/r%2F1", nil, `{"ok":true}`, "ok:"},
		{[]string{"rules", "enable", "w", "r2"}, "PUT", "/rules/r2", enabled, `{"ok":true}`, "ok:"},
		{[]string{"rules", "disable", "w", "r1"}, "PUT", "/rules/r1", disabled, `{"ok":true}`, "ok:"},
		{[]string{"rules", "move", "w", "r1", "2"}, "POST", "/rules/order", map[string]any{"ids": []string{"r2", "r1"}}, `{"ok":true}`, "ok:"},
		{[]string{"rules", "move", "w", "r2", "1"}, "POST", "/rules/order", map[string]any{"ids": []string{"r2", "r1"}}, `{"ok":true}`, "ok:"},
		{[]string{"lists", "list", "Team"}, "GET", "/lists", nil, `[{"id":"l","name":"Office","kind":"allow","entries":[]}]`, "ENTRIES"},
		{[]string{"lists", "add", "w", "Office", "--kind=allow"}, "POST", "/lists", firewall.List{Name: "Office", Kind: "allow", Entries: []firewall.Entry{}}, `{"ok":true}`, "ok:"},
		{[]string{"lists", "add", "w", "Denied"}, "POST", "/lists", firewall.List{Name: "Denied", Kind: "block", Entries: []firewall.Entry{}}, `{"ok":true}`, "ok:"},
		{[]string{"lists", "rm", "w", "l"}, "DELETE", "/lists/l", nil, `{"ok":true}`, "ok:"},
		{[]string{"lists", "entry", "add", "w", "l", "203.0.113.0/24", "--for", "1h"}, "POST", "/lists/l/entries", map[string]any{"value": "203.0.113.0/24", "ttl_seconds": 3600}, `{"ok":true}`, "ok:"},
		{[]string{"lists", "entry", "add", "w", "l", "2001:db8::1"}, "POST", "/lists/l/entries", map[string]any{"value": "2001:db8::1", "ttl_seconds": 0}, `{"ok":true}`, "ok:"},
		{[]string{"lists", "entry", "rm", "w", "l", "203.0.113.0/24"}, "DELETE", "/lists/l/entries/203.0.113.0%2F24", nil, `{"ok":true}`, "ok:"},
		{[]string{"ban", "Team", "192.0.2.1", "--for=7d"}, "POST", "/ban", map[string]any{"ip": "192.0.2.1", "ttl_seconds": 604800}, `{"ok":true}`, "ok:"},
		{[]string{"ban", "w", "2001:db8::1"}, "POST", "/ban", map[string]any{"ip": "2001:db8::1", "ttl_seconds": 0}, `{"ok":true}`, "ok:"},
		{[]string{"unban", "Team", "192.0.2.1"}, "DELETE", "/ban/192.0.2.1", nil, `{"ok":true}`, "ok:"},
		{[]string{"events", "Team"}, "GET", "/events", nil, `[{"time":"now","ip":"192.0.2.1","action":"block","status":403,"monitored":false}]`, "MONITORED"},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(strings.Join(tc.args, " ")+fmt.Sprint(raw), func(t *testing.T) {
				called := false
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == tc.method && r.URL.RequestURI() == "/api/workspaces/w/firewall"+tc.path {
						called = true
						var got any
						if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
							t.Error(err)
						}
						var want any
						json.Unmarshal([]byte(cliFirewallJSON(tc.body)), &want)
						if !reflect.DeepEqual(got, want) {
							t.Errorf("body = %s; want %s", cliFirewallJSON(got), cliFirewallJSON(want))
						}
						fmt.Fprint(w, tc.response)
						return
					}
					if r.Method == "GET" && r.URL.Path == "/api/workspaces" {
						fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
						return
					}
					if r.Method == "GET" && r.URL.Path == "/api/workspaces/w/firewall" {
						fmt.Fprint(w, cliFirewallJSON(cliFirewallPolicy()))
						return
					}
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					http.Error(w, "unexpected", 404)
				}))
				defer server.Close()
				var out bytes.Buffer
				c := &cli{config: config{Server: server.URL}, out: &out, client: server.Client(), raw: raw}
				if err := c.run(append([]string{"firewall"}, tc.args...)); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("expected command request")
				}
				if raw || tc.args[0] == "export" {
					if !json.Valid(out.Bytes()) {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), tc.text) {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func TestFirewallArgumentErrors(t *testing.T) {
	for _, args := range [][]string{
		{}, {"unknown", "w"}, {"show"}, {"show", "w", "extra"}, {"mode", "w", "invalid"}, {"managed", "w", "sqli", "maybe"}, {"managed", "w", "missing", "on"}, {"managed", "w", "sqli", "on", "--action=rate_limit"},
		{"limits", "w", "--rate=0/1m"}, {"limits", "w", "--rate=1/0s"}, {"limits", "w", "--rate=1"}, {"limits", "w", "--rate-action=ban"}, {"limits", "w", "--rate=2/1m", "--rate-action=log"}, {"limits", "w", "--ban=366d"}, {"limits", "w", "--concurrent=-1"}, {"limits", "w", "--concurrent=x"}, {"limits", "w", "--max-body=-1"}, {"limits", "w", "--max-url=9223372036854775807GB"},
		{"rules"}, {"rules", "bogus", "w"}, {"rules", "add", "w"}, {"rules", "add", "w", "--name=x", "--action=block", "--when=foo eq x"}, {"rules", "add", "w", "--name=x", "--action=block", "--when=path"}, {"rules", "add", "w", "--name=x", "--action=block", "--when=path eq"}, {"rules", "add", "w", "--name=x", "--action=block", "--when=method in GET,"}, {"rules", "add", "w", "--name=x", "--action=rate_limit", "--when=path eq /"}, {"rules", "add", "w", "--name=x", "--action=block", "--when=path eq /", "--ban=1h"},
		{"rules", "enable", "w", "missing"}, {"rules", "move", "w", "r1", "0"}, {"rules", "move", "w", "r1", "3"}, {"rules", "move", "w", "r1", "x"},
		{"lists"}, {"lists", "entry"}, {"lists", "add", "w", "Office", "--kind=no"}, {"lists", "entry", "add", "w", "l", "example.com"}, {"lists", "entry", "rm", "w", "l", "192.0.2.1", "--for=1h"},
		{"ban", "w", "192.0.2.0/24"}, {"ban", "w", "192.0.2.1", "--for=-1s"}, {"ban", "w", "192.0.2.1", "--for=500ms"}, {"ban", "w", "192.0.2.1", "--for"}, {"ban", "w", "192.0.2.1", "--for=1h", "--for=2h"}, {"show", "w", "--unknown"}, {"events", "w", "--follow=false"}, {"events", "w", "--follow", "--follow"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method != "GET" {
					t.Errorf("mutation for invalid arguments: %s", r.Method)
				}
				if r.URL.Path == "/api/workspaces" {
					fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
				} else {
					fmt.Fprint(w, cliFirewallJSON(cliFirewallPolicy()))
				}
			}))
			defer server.Close()
			c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
			if err := c.run(append([]string{"firewall"}, args...)); err == nil {
				t.Fatal("accepted invalid arguments")
			}
		})
	}
}

func TestFirewallUnits(t *testing.T) {
	for value, want := range map[string]int{"0": 0, "30s": 30, "15m": 900, "1h": 3600, "7d": 604800, "0.5d": 43200} {
		got, err := firewallDuration(value)
		if err != nil || got != want {
			t.Fatal(value, got, err)
		}
	}
	for _, value := range []string{"", "NaNd", "Infd", "-1d", "1000000000000000d", "1.5s", "366d"} {
		if _, err := firewallDuration(value); err == nil {
			t.Fatal(value)
		}
	}
	for value, want := range map[string]int64{"0": 0, "123": 123, "10MB": 10000000, "512KB": 512000, "2gb": 2000000000, "10B": 10} {
		got, err := firewallSize(value)
		if err != nil || got != want {
			t.Fatal(value, got, err)
		}
	}
	for _, value := range []string{"", "1.5MB", "-1", "NaN", "99999999999999999999", "10MiB"} {
		if _, err := firewallSize(value); err == nil {
			t.Fatal(value)
		}
	}
	cond, err := firewallCondition(`path matches ^/a  b$`)
	if err != nil || cond.Value != `^/a  b$` {
		t.Fatal(cond, err)
	}
}

func TestFirewallEmptyOutputAndErrors(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		response string
	}{{[]string{"rules", "list", "w"}, `{"rules":[]}`}, {[]string{"rules", "list", "w"}, `{"rules":null}`}, {[]string{"lists", "list", "w"}, `[]`}, {[]string{"events", "w"}, `[]`}} {
		var out bytes.Buffer
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/workspaces" {
				fmt.Fprint(w, `[{"id":"w"}]`)
			} else {
				fmt.Fprint(w, tc.response)
			}
		}))
		c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &out}
		if err := c.run(append([]string{"firewall"}, tc.args...)); err != nil || out.Len() == 0 {
			t.Fatal(out.String(), err)
		}
		server.Close()
	}
	for _, response := range []string{`{}`, `{"mode":"bad"}`, `null`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/api/workspaces" {
				fmt.Fprint(w, `[{"id":"w"}]`)
			} else {
				fmt.Fprint(w, response)
			}
		}))
		c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
		if err := c.run([]string{"firewall", "mode", "w", "off"}); err == nil {
			t.Fatal(response)
		}
		server.Close()
	}
	for _, fileBody := range []string{`{`, `{"mode":"invalid"}`} {
		file := filepath.Join(t.TempDir(), "invalid.json")
		if err := os.WriteFile(file, []byte(fileBody), 0600); err != nil {
			t.Fatal(err)
		}
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != "GET" {
				t.Error("invalid apply mutated policy")
			}
			fmt.Fprint(w, `[{"id":"w"}]`)
		}))
		c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
		if err := c.run([]string{"firewall", "apply", "w", file}); err == nil {
			t.Fatal("invalid apply accepted")
		}
		if err := c.run([]string{"firewall", "apply", "w", file + "missing"}); err == nil {
			t.Fatal("missing file accepted")
		}
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		fmt.Fprint(w, `{"error":"forbidden"}`)
	}))
	defer server.Close()
	c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
	if err := c.run([]string{"firewall", "show", "w"}); err == nil || !strings.Contains(err.Error(), "forbidden") {
		t.Fatal(err)
	}
}

func TestFirewallFollow(t *testing.T) {
	for _, socket := range []bool{false, true} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprint(socket, raw), func(t *testing.T) {
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/api/workspaces" {
						fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
						return
					}
					if r.URL.Path != "/api/live" {
						t.Error(r.URL)
						w.WriteHeader(404)
						return
					}
					if !socket {
						cookie, err := r.Cookie("startship_session")
						if err != nil || cookie.Value != "token" {
							t.Error("missing live auth")
						}
					}
					conn, err := websocket.Accept(w, r, nil)
					if err != nil {
						t.Error(err)
						return
					}
					defer conn.CloseNow()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					_, b, err := conn.Read(ctx)
					if err != nil {
						t.Error(err)
						return
					}
					var sub map[string]string
					if err = json.Unmarshal(b, &sub); err != nil || sub["type"] != "subscribe" || sub["topic"] != "firewall" || sub["workspace_id"] != "w" {
						t.Error(string(b), err)
					}
					for _, message := range []string{`{"type":"ready"}`, `{"type":"snapshot","id":"firewall","data":{"events":[]}}`, `{"type":"snapshot","id":"firewall","data":{"events":[{"path":"/second","status":403},{"path":"/first","status":403}]}}`, `{"type":"snapshot","id":"firewall","data":{"events":[{"path":"/third","status":403},{"path":"/second","status":403},{"path":"/first","status":403}]}}`, `{"type":"snapshot","id":"firewall","data":{"events":[{"status":403,"path":"/third"},{"path":"/second","status":403},{"path":"/first","status":403}]}}`} {
						if err := conn.Write(ctx, websocket.MessageText, []byte(message)); err != nil {
							t.Error(err)
							return
						}
					}
					conn.Close(websocket.StatusNormalClosure, "")
				})
				var server string
				client := http.DefaultClient
				if socket {
					path := filepath.Join(t.TempDir(), "live.sock")
					l, err := net.Listen("unix", path)
					if err != nil {
						t.Fatal(err)
					}
					srv := &http.Server{Handler: handler}
					go srv.Serve(l)
					defer srv.Close()
					server = "unix://" + path
				} else {
					srv := httptest.NewServer(handler)
					defer srv.Close()
					server = srv.URL
					client = srv.Client()
				}
				var out bytes.Buffer
				c := &cli{config: config{Server: server, Token: "token"}, client: client, out: &out, raw: raw}
				if err := c.run([]string{"firewall", "events", "Team", "--follow"}); err != nil {
					t.Fatal(err)
				}
				for _, path := range []string{"/first", "/second", "/third"} {
					if strings.Count(out.String(), path) != 1 {
						t.Fatal(out.String())
					}
				}
				if !(strings.Index(out.String(), "/first") < strings.Index(out.String(), "/second") && strings.Index(out.String(), "/second") < strings.Index(out.String(), "/third")) {
					t.Fatal(out.String())
				}
				if raw {
					lines := strings.Split(strings.TrimSpace(out.String()), "\n")
					if len(lines) != 3 {
						t.Fatal(out.String())
					}
					for _, line := range lines {
						if !json.Valid([]byte(line)) {
							t.Fatal(line)
						}
					}
				}
			})
		}
	}
}

func TestFirewallFollowErrors(t *testing.T) {
	for _, message := range []string{`{"type":"error","id":"firewall","error":"forbidden"}`, `not JSON`} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			conn, err := websocket.Accept(w, r, nil)
			if err != nil {
				t.Error(err)
				return
			}
			defer conn.CloseNow()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			conn.Read(ctx)
			conn.Write(ctx, websocket.MessageText, []byte(message))
			conn.Close(websocket.StatusNormalClosure, "")
		}))
		c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		if err := c.followFirewall(ctx, "w"); err == nil {
			t.Fatal("live error ignored")
		}
		cancel()
		server.Close()
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(403) }))
	defer server.Close()
	c := &cli{config: config{Server: server.URL}, client: server.Client(), out: &bytes.Buffer{}}
	if err := c.followFirewall(context.Background(), "w"); err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatal(err)
	}
}
