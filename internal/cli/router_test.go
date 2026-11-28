package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestRouterCommands(t *testing.T) {
	const initial = `{"enabled":false,"http_listen":[":80"],"https_listen":[":443"],"config_path":"/etc/startship/config/startship.toml","overridden":["router_disabled"]}`
	cases := []struct {
		name string
		args []string
		put  string
		want string
	}{
		{"default", nil, "", "enabled: false"},
		{"show", []string{"show"}, "", "http_listen: [:80]\nhttps_listen: [:443]\noverridden: [router_disabled]"},
		{"show JSON", []string{"show", "--json"}, "", `"config_path": "/etc/startship/config/startship.toml"`},
		{"set HTTP", []string{"set", "--http", " :8081, [::1]:8081 "}, `{"enabled":false,"http_listen":[":8081","[::1]:8081"],"https_listen":[":443"]}`, "enabled: true"},
		{"set HTTPS", []string{"set", "--https=:8443"}, `{"enabled":false,"http_listen":[":80"],"https_listen":[":8443"]}`, "enabled: true"},
		{"set both", []string{"set", "--http=:8081", "--https", ":8443"}, `{"enabled":false,"http_listen":[":8081"],"https_listen":[":8443"]}`, "enabled: true"},
		{"set interface", []string{"set", "--interface", "tailscale0"}, `{"enabled":false,"http_listen":["tailscale0:80"],"https_listen":["tailscale0:443"]}`, "enabled: true"},
		{"set interface any with ports", []string{"set", "--interface", "any", "--http-port", "8080", "--https-port", "8443"}, `{"enabled":false,"http_listen":[":8080"],"https_listen":[":8443"]}`, "enabled: true"},
		{"set http port only", []string{"set", "--http-port", "8088"}, `{"enabled":false,"http_listen":[":8088"],"https_listen":[":443"]}`, "enabled: true"},
		{"disable", []string{"disable"}, `{"enabled":false,"http_listen":[":80"],"https_listen":[":443"]}`, "enabled: true"},
		{"enable JSON", []string{"enable", "--json"}, `{"enabled":true,"http_listen":[":80"],"https_listen":[":443"]}`, `"enabled": true`},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			methods := make(chan string, 2)
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/system/router" {
					t.Error(r.URL.Path)
				}
				select {
				case methods <- r.Method:
				default:
					t.Error("unexpected extra request")
				}
				switch r.Method {
				case http.MethodGet:
					io.WriteString(w, initial)
				case http.MethodPut:
					var got, want map[string]any
					err := json.NewDecoder(r.Body).Decode(&got)
					if json.Unmarshal([]byte(tt.put), &want) != nil || err != nil || !reflect.DeepEqual(got, want) {
						t.Errorf("PUT: got %v, want %s (decode error: %v)", got, tt.put, err)
					}
					io.WriteString(w, strings.Replace(initial, `"enabled":false`, `"enabled":true`, 1))
				}
			}))
			defer s.Close()
			var output bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &output, client: s.Client()}
			if err := c.run(append([]string{"router"}, tt.args...)); err != nil || !strings.Contains(output.String(), tt.want) {
				t.Fatal(output.String(), err)
			}
			if len(methods) == 0 || <-methods != http.MethodGet {
				t.Fatal("expected GET first")
			}
			if tt.put != "" && (len(methods) == 0 || <-methods != http.MethodPut) {
				t.Fatal("expected PUT after GET")
			}
			if len(methods) != 0 {
				t.Fatal("unexpected requests")
			}
		})
	}
}

func TestRouterArgumentsAndAPIErrors(t *testing.T) {
	for _, args := range []string{
		"set", "set --http", "set --http=bad", "set --http=:8081,", "set --http=:8081 --http=:8082",
		"set --port=8081", "set extra --https=:8443", "enable extra", "disable --http=:8081", "show extra", "other",
	} {
		c := &cli{out: io.Discard}
		if err := c.run(append([]string{"router"}, strings.Fields(args)...)); err == nil {
			t.Errorf("expected validation error for router %s", args)
		}
	}
	for _, tc := range []struct {
		name, get, want string
		status          int
	}{
		{"GET error", `{"error":"not authorized"}`, "not authorized", http.StatusForbidden},
		{"malformed response", `{"enabled":true}`, "invalid router settings response", http.StatusOK},
		{"PUT conflict", `{"enabled":true,"http_listen":[":80"],"https_listen":[":443"]}`, "address already in use", http.StatusOK},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodGet {
					w.WriteHeader(tc.status)
					io.WriteString(w, tc.get)
					return
				}
				if tc.name != "PUT conflict" || r.Method != http.MethodPut {
					t.Errorf("unexpected %s", r.Method)
				}
				w.WriteHeader(http.StatusConflict)
				io.WriteString(w, `{"error":"proxy: cannot listen on :8081 (address already in use)"}`)
			}))
			defer s.Close()
			c := &cli{config: config{Server: s.URL}, out: io.Discard, client: s.Client()}
			if err := c.run([]string{"router", "set", "--http", ":8081"}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected %q, got %v", tc.want, err)
			}
		})
	}
}

func TestDashboardAndInterfacesCommands(t *testing.T) {
	const initial = `{"dashboard_listen":[":8080"],"config_path":"/etc/startship/config/startship.toml"}`
	cases := []struct {
		name string
		args []string
		put  string
		want string
	}{
		{"default", nil, "", "dashboard_listen: [:8080]"},
		{"show JSON", []string{"show", "--json"}, "", `"config_path": "/etc/startship/config/startship.toml"`},
		{"set listen", []string{"set", "--listen", "tailscale0:8080, 127.0.0.1:8080"}, `{"dashboard_listen":["tailscale0:8080","127.0.0.1:8080"]}`, "dashboard_listen: [tailscale0:8080]"},
		{"set interface", []string{"set", "--interface", "tailscale0"}, `{"dashboard_listen":["tailscale0:8080"]}`, "dashboard_listen: [tailscale0:8080]"},
		{"set interface any with port", []string{"set", "--interface", "any", "--port", "9090"}, `{"dashboard_listen":[":9090"]}`, "dashboard_listen: [tailscale0:8080]"},
		{"set multiple interfaces", []string{"set", "--interface", "tailscale0,lo", "--port", "8444"}, `{"dashboard_listen":["tailscale0:8444","lo:8444"]}`, "dashboard_listen: [tailscale0:8080]"},
		{"set port only", []string{"set", "--port", "9000"}, `{"dashboard_listen":[":9000"]}`, "dashboard_listen: [tailscale0:8080]"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/api/system/dashboard" {
					t.Error(r.URL.Path)
				}
				switch r.Method {
				case http.MethodGet:
					io.WriteString(w, initial)
				case http.MethodPut:
					var got, want map[string]any
					err := json.NewDecoder(r.Body).Decode(&got)
					if json.Unmarshal([]byte(tt.put), &want) != nil || err != nil || !reflect.DeepEqual(got, want) {
						t.Errorf("PUT: got %v, want %s (err: %v)", got, tt.put, err)
					}
					io.WriteString(w, strings.Replace(initial, `":8080"`, `"tailscale0:8080"`, 1))
				}
			}))
			defer s.Close()
			var output bytes.Buffer
			c := &cli{config: config{Server: s.URL}, out: &output, client: s.Client()}
			if err := c.run(append([]string{"dashboard"}, tt.args...)); err != nil || !strings.Contains(output.String(), tt.want) {
				t.Fatal(output.String(), err)
			}
		})
	}
	for _, args := range []string{
		"set", "set --listen", "set --listen=bad", "set --interface=any,lo", "set --listen=:8080 --interface=lo",
		"set --listen=:8080 --port=9000", "set --port=99999", "show extra", "other",
	} {
		c := &cli{out: io.Discard}
		if err := c.run(append([]string{"dashboard"}, strings.Fields(args)...)); err == nil {
			t.Errorf("expected validation error for dashboard %s", args)
		}
	}
	for _, args := range []string{
		"set --interface=tailscale0 --http=:80", "set --http-port=8080 --http=:80", "set --https-port=8443 --https=:443", "set --interface=any,eth0",
	} {
		c := &cli{out: io.Discard}
		if err := c.run(append([]string{"router"}, strings.Fields(args)...)); err == nil {
			t.Errorf("expected validation error for router %s", args)
		}
	}

	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/system/interfaces" || r.Method != http.MethodGet {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		io.WriteString(w, `{"interfaces":[{"name":"tailscale0","addresses":["100.64.0.5"],"up":true,"loopback":false},{"name":"lo","addresses":["127.0.0.1","::1"],"up":true,"loopback":true}]}`)
	}))
	defer s.Close()
	var out bytes.Buffer
	c := &cli{config: config{Server: s.URL}, out: &out, client: s.Client()}
	if err := c.run([]string{"interfaces"}); err != nil || !strings.Contains(out.String(), "tailscale0\tup\t100.64.0.5") || !strings.Contains(out.String(), "lo\tup,loopback\t127.0.0.1,::1") {
		t.Fatal(out.String(), err)
	}
	out.Reset()
	c.raw = false
	if err := c.run([]string{"interfaces", "--json"}); err != nil || !strings.Contains(out.String(), `"tailscale0"`) {
		t.Fatal(out.String(), err)
	}
	if err := c.run([]string{"interfaces", "extra"}); err == nil {
		t.Fatal("expected error for extra arg to interfaces")
	}
}
