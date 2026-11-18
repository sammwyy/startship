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

func TestNetworkCommands(t *testing.T) {
	const status = `{"enabled":true,"available":true,"rules":[{"id":"n1","kind":"cidr","value":"192.0.2.0/24","ports":"all","note":"office"},{"id":"n2","kind":"country","value":"US","ports":"router"}]}`
	for _, tt := range []struct {
		args                       []string
		method, path, body, output string
	}{
		{[]string{"status"}, "GET", "", "", `available: true`},
		{[]string{"list"}, "GET", "", "", "KIND"},
		{[]string{"block", "192.0.2.1"}, "POST", "/rules", `{"kind":"ip","value":"192.0.2.1","ports":"all","ttl_seconds":0,"note":""}`, "ok"},
		{[]string{"block", "192.0.2.9/24", "--for", "7d", "--router-only", "--note", "office"}, "POST", "/rules", `{"kind":"cidr","value":"192.0.2.0/24","ports":"router","ttl_seconds":604800,"note":"office"}`, "ok"},
		{[]string{"block", "us", "--for=30s"}, "POST", "/rules", `{"kind":"country","value":"US","ports":"all","ttl_seconds":30,"note":""}`, "ok"},
		{[]string{"unblock", "n1"}, "DELETE", "/rules/n1", "", "ok"},
		{[]string{"unblock", "192.0.2.9/24"}, "DELETE", "/rules/n1", "", "ok"},
		{[]string{"unblock", "us"}, "DELETE", "/rules/n2", "", "ok"},
	} {
		for _, raw := range []bool{false, true} {
			t.Run(fmt.Sprint(tt.args, raw), func(t *testing.T) {
				called := false
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.Method == tt.method && r.URL.Path == "/api/system/network-firewall"+tt.path {
						called = true
						if tt.body != "" {
							var got, want any
							if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
								t.Error(err)
							}
							json.Unmarshal([]byte(tt.body), &want)
							if !reflect.DeepEqual(got, want) {
								t.Errorf("body=%#v want=%#v", got, want)
							}
						}
						if r.Method == "GET" {
							fmt.Fprint(w, status)
						} else {
							fmt.Fprint(w, `{"ok":true}`)
						}
						return
					}
					if r.Method == "GET" && r.URL.Path == "/api/system/network-firewall" {
						fmt.Fprint(w, status)
						return
					}
					t.Errorf("unexpected %s %s", r.Method, r.URL)
					w.WriteHeader(404)
				}))
				defer server.Close()
				var out bytes.Buffer
				c := &cli{config: config{Server: server.URL}, out: &out, raw: raw, client: server.Client()}
				if err := c.run(append([]string{"network"}, tt.args...)); err != nil {
					t.Fatal(err)
				}
				if !called {
					t.Fatal("missing request")
				}
				if raw {
					if !json.Valid(out.Bytes()) {
						t.Fatal(out.String())
					}
				} else if !strings.Contains(out.String(), tt.output) {
					t.Fatal(out.String())
				}
			})
		}
	}
}

func TestNetworkArgumentsAndErrors(t *testing.T) {
	for _, args := range [][]string{{}, {"wat"}, {"status", "x"}, {"list", "--for", "1h"}, {"block"}, {"block", "bad-value"}, {"block", "127.0.0.2", "extra"}, {"block", "127.0.0.2", "--for", "bad"}, {"block", "127.0.0.2", "--note"}, {"block", "127.0.0.2", "--router-only=false"}, {"unblock"}, {"unblock", "x", "--for", "1h"}} {
		c := &cli{out: &bytes.Buffer{}}
		if err := c.runNetwork(args); err == nil {
			t.Fatal(args)
		}
	}
	for _, response := range []string{`{"enabled":false,"available":true,"rules":[]}`, `{"enabled":true,"available":true,"rules":[]}`} {
		s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, response) }))
		var out bytes.Buffer
		c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
		if err := c.runNetwork([]string{"list"}); err != nil || !strings.Contains(out.String(), "EXPIRES_AT") {
			t.Fatal(out.String(), err)
		}
		if err := c.runNetwork([]string{"unblock", "missing"}); err == nil {
			t.Fatal("missing rule accepted")
		}
		s.Close()
	}
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(409)
		fmt.Fprint(w, `{"error":"network firewall is disabled"}`)
	}))
	defer s.Close()
	c := &cli{config: config{Server: s.URL}, out: &bytes.Buffer{}, client: s.Client()}
	if err := c.runNetwork([]string{"block", "127.0.0.2"}); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatal(err)
	}
}
