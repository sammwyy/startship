package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestAppsCommands(t *testing.T) {
	const base = "/api/workspaces/w/apps"
	for _, tc := range []struct {
		args               []string
		method, path, body string
	}{
		{[]string{"recipes", "show", "gitea"}, "GET", "/api/recipes/gitea", ""},
		{[]string{"recipes", "refresh", "Team"}, "POST", "/api/recipes/refresh?workspace=w", ""},
		{[]string{"install", "Team", "gitea", "git", "--config", `{"root_url":"https://git.example.com"}`}, "POST", base, `{"name":"git","recipe":"gitea","config":{"root_url":"https://git.example.com"}}`},
		{[]string{"update", "w", "git"}, "POST", base + "/a/update", ""},
		{[]string{"config", "set", "w", "git", "--file", "config", "--config", `{"enabled":true}`, "--restart"}, "PUT", base + "/a/files/config", `{"values":{"enabled":true},"restart":true}`},
		{[]string{"domains", "rm", "w", "git", "git.example.com"}, "DELETE", base + "/a/domains/d", ""},
		{[]string{"delete", "w", "git", "--purge"}, "DELETE", base + "/a?purge=true", ""},
	} {
		t.Run(tc.args[0], func(t *testing.T) {
			called := make(chan bool, 1)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == tc.method && r.URL.RequestURI() == tc.path:
					called <- true
					var got, want any
					json.NewDecoder(r.Body).Decode(&got)
					if tc.body != "" {
						json.Unmarshal([]byte(tc.body), &want)
					}
					if !reflect.DeepEqual(got, want) {
						t.Errorf("body = %#v, want %#v", got, want)
					}
					fmt.Fprint(w, `{"ok":true}`)
				case r.URL.Path == "/api/workspaces":
					fmt.Fprint(w, `[{"id":"w","name":"Team"}]`)
				case r.URL.Path == base && r.Method == "GET":
					fmt.Fprint(w, `{"apps":[{"id":"a","name":"git","domains":[{"id":"d","host":"git.example.com"}]}]}`)
				default:
					t.Errorf("unexpected request %s %s", r.Method, r.URL)
					w.WriteHeader(404)
				}
			}))
			defer s.Close()
			var out bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
			if err := c.run(append([]string{"apps"}, tc.args...)); err != nil {
				t.Fatal(err)
			}
			select {
			case <-called:
			default:
				t.Fatal("missing apps request")
			}
		})
	}
}
