package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/config"
)

func routerTestListener(t *testing.T) net.Listener {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	return l
}

func TestSystemRouterSettings(t *testing.T) {
	for _, key := range []string{"STARTSHIP_ROUTER_DISABLED", "STARTSHIP_HTTP_ADDR", "STARTSHIP_HTTPS_ADDR"} {
		t.Setenv(key, "")
	}
	t.Setenv("STARTSHIP_HTTP_ADDR", "127.0.0.1:0")
	a := testServer(t)
	if _, err := a.createUserPending("admin", "password123", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "startship.toml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load([]string{"--config", path, "--https-listen=127.0.0.1:0"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.routerListeners.httpHandler = a
	certificate := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer certificate.Close()
	a.legacyCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return &certificate.TLS.Certificates[0], nil
	}
	initial, err := a.prepareRouterListeners(settings)
	if err != nil {
		t.Fatal(err)
	}
	a.commitRouterListeners(settings, initial.next)
	t.Cleanup(a.stopRouterListeners)
	oldHTTP := initial.next[listenerKey{1, "127.0.0.1:0"}].listener.Addr().String()
	oldHTTPS := initial.next[listenerKey{2, "127.0.0.1:0"}].listener.Addr().String()
	h := a.api()
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"bob","password":"password123"}`)))
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	bob := login.Result().Cookies()[0]
	call := func(local bool, method, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, "/api/system/router", strings.NewReader(body))
		if local {
			r = r.WithContext(context.WithValue(r.Context(), socketContextKey{}, true))
		} else {
			r.AddCookie(bob)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: got %d want %d: %s", method, body, w.Code, status, w.Body.String())
		}
		return w.Body.String()
	}
	put := func(enabled bool, httpAddr, httpsAddr string) string {
		t.Helper()
		body, err := json.Marshal(routerConfig{Enabled: enabled, HTTPListen: []string{httpAddr}, HTTPSListen: []string{httpsAddr}})
		if err != nil {
			t.Fatal(err)
		}
		return string(body)
	}
	client := certificate.Client()
	client.Timeout = 2 * time.Second
	checkProxy := func(url string, running bool) {
		t.Helper()
		client.CloseIdleConnections()
		response, err := client.Get(url)
		if response != nil {
			response.Body.Close()
		}
		if running && (err != nil || response.StatusCode != http.StatusNotFound) || !running && err == nil {
			t.Fatalf("proxy %s, expected running=%t, got response=%v error=%v", url, running, response, err)
		}
	}
	oldURL := "http://" + oldHTTP
	httpsURL := "https://" + oldHTTPS
	checkProxy(oldURL, true)
	checkProxy(httpsURL, true)
	var got routerConfig
	if err := json.Unmarshal([]byte(call(false, "GET", "", 200)), &got); err != nil || !got.Enabled || got.ConfigPath != settings.ConfigPath || len(got.Overridden) != 0 {
		t.Fatal(got, err)
	}
	call(false, "PUT", put(false, "127.0.0.1:0", "127.0.0.1:0"), 403)
	call(true, "PUT", `{"http_listen":[],"https_listen":[]}`, 400)
	call(true, "PUT", `{"enabled":true,"http_listen":[],"https_listen":[]}`, 400)
	call(true, "PUT", put(true, "not an address", "127.0.0.1:0"), 400)
	if msg := call(true, "PUT", put(true, "127.0.0.1:9000", "127.0.0.1:0"), 409); !strings.Contains(msg, "dashboard_listen") {
		t.Fatal(msg)
	}
	if err := a.saveService(Service{ID: "router-service", Name: "cache", ExternalPort: 16379}); err != nil {
		t.Fatal(err)
	}
	if msg := call(true, "PUT", put(true, "127.0.0.1:16379", "127.0.0.1:0"), 409); !strings.Contains(msg, "external_port") {
		t.Fatal(msg)
	}
	occupied := routerTestListener(t)
	disabled := settings
	disabled.RouterDisabled = true
	disabled.HTTPListen = []string{occupied.Addr().String()}
	if prepared, err := a.prepareRouterListeners(disabled); err != nil || len(prepared.next) != 0 {
		t.Fatalf("disabled router attempted to bind: %v, %v", prepared.next, err)
	}
	candidate := routerTestListener(t)
	newHTTP := candidate.Addr().String()
	candidate.Close()
	bindError := call(true, "PUT", `{"enabled":true,"http_listen":["`+newHTTP+`","`+occupied.Addr().String()+`"],"https_listen":["127.0.0.1:0"]}`, 409)
	if !strings.Contains(bindError, "pick a free port or stop the process using it") || strings.Contains(bindError, "CAP_NET_BIND_SERVICE") {
		t.Fatal(bindError)
	}
	checkProxy(oldURL, true)
	checkProxy(httpsURL, true)
	checkProxy("http://"+newHTTP, false)
	if err := os.Mkdir(settings.ConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", put(true, newHTTP, "127.0.0.1:0"), 500)
	checkProxy(oldURL, true)
	checkProxy("http://"+newHTTP, false)
	if err := os.Remove(settings.ConfigPath); err != nil {
		t.Fatal(err)
	}

	call(true, "PUT", put(true, newHTTP, "127.0.0.1:0"), 200)
	checkProxy(oldURL, false)
	checkProxy("http://"+newHTTP, true)
	checkProxy(httpsURL, true)
	port := a.routerListeners.active[listenerKey{1, newHTTP}].listener.Addr().(*net.TCPAddr).Port
	if os.Getenv("STARTSHIP_HTTP_ADDR") != newHTTP || !a.serviceReservedPorts[port] {
		t.Fatal("new port not exported and reserved")
	}
	app := recipeApp{ID: "router-app", Name: "game", Recipe: builtinRecipes["minecraft"], Ports: map[string]int{"game": port}}
	if err := a.validateRecipePorts(app); err == nil {
		t.Fatal("app allowed to use the active router port")
	}
	call(true, "PUT", put(false, newHTTP, "127.0.0.1:0"), 200)
	checkProxy("http://"+newHTTP, false)
	checkProxy(httpsURL, false)
	if a.serviceReservedPorts[port] || os.Getenv("STARTSHIP_ROUTER_DISABLED") != "true" {
		t.Fatal("router not reported disabled or port still reserved")
	}
	b, err := os.ReadFile(settings.ConfigPath)
	saved := config.Defaults()
	if err != nil || config.Parse(b, &saved) != nil || !saved.RouterDisabled || saved.HTTPListen[0] != newHTTP {
		t.Fatal(string(b), err)
	}
	if err := a.validateRecipePorts(app); err != nil {
		t.Fatal("app cannot use the disabled router port:", err)
	}
	if err := a.saveRecipeApp(app); err != nil {
		t.Fatal(err)
	}
	if err := a.validateService(Service{Name: "app-port-test", ExternalPort: port}); err == nil || !strings.Contains(err.Error(), "port already used by an app") {
		t.Fatal("expected service to reject app port:", err)
	}
	if msg := call(true, "PUT", put(true, newHTTP, "127.0.0.1:0"), 409); !strings.Contains(msg, "is used by an app") {
		t.Fatal(msg)
	}
	if _, err := a.store.DB.Exec("DELETE FROM recipe_apps WHERE id=?", app.ID); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", put(true, newHTTP, "127.0.0.1:0"), 200)
	checkProxy("http://"+newHTTP, true)
	reboundHTTPS := a.routerListeners.active[listenerKey{2, "127.0.0.1:0"}].listener.Addr().String()
	checkProxy("https://"+reboundHTTPS, true)
	candidateHTTPS := routerTestListener(t)
	newHTTPS := candidateHTTPS.Addr().String()
	candidateHTTPS.Close()
	call(true, "PUT", put(true, newHTTP, newHTTPS), 200)
	checkProxy("https://"+newHTTPS, true)
	a.routerListeners.mu.Lock()
	a.routerListeners.settings.RouterDisabledOverridden = true
	a.routerListeners.mu.Unlock()
	if msg := call(true, "PUT", put(false, newHTTP, newHTTPS), 409); !strings.Contains(msg, "STARTSHIP_ROUTER_DISABLED") {
		t.Fatal(msg)
	}
}

func TestSystemDashboardAndInterfaces(t *testing.T) {
	t.Setenv("STARTSHIP_DASHBOARD_ADDR", "127.0.0.1:0")
	a := testServer(t)
	if _, err := a.createUserPending("admin", "password123", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "startship.toml")
	if err := os.WriteFile(path, nil, 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := config.Load([]string{"--config", path, "--http-listen=127.0.0.1:18081", "--https-listen=127.0.0.1:18443", "--router-disabled=false"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(path); err != nil {
		t.Fatal(err)
	}
	a.routerListeners.dashboardHandler = a.api()
	a.listInterfaces = func() ([]config.NetworkInterface, error) {
		return []config.NetworkInterface{
			{Name: "tailscale0", Addresses: []string{"100.64.0.5"}, Up: true},
			{Name: "lo", Addresses: []string{"127.0.0.1", "::1"}, Up: true, Loopback: true},
		}, nil
	}
	initial, err := a.prepareDashboardListeners(settings)
	if err != nil {
		t.Fatal(err)
	}
	a.commitDashboardListeners(settings, initial.next)
	t.Cleanup(a.stopRouterListeners)
	oldAddr := initial.next[listenerKey{0, "127.0.0.1:0"}].listener.Addr().String()
	h := a.api()
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"bob","password":"password123"}`)))
	if login.Code != 200 {
		t.Fatal(login.Code, login.Body.String())
	}
	bob := login.Result().Cookies()[0]
	call := func(local bool, method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if local {
			r = r.WithContext(context.WithValue(r.Context(), socketContextKey{}, true))
		} else {
			r.AddCookie(bob)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s %s: got %d want %d: %s", method, path, body, w.Code, status, w.Body.String())
		}
		return w.Body.String()
	}
	var ifacesResp struct {
		Interfaces []config.NetworkInterface `json:"interfaces"`
	}
	if err := json.Unmarshal([]byte(call(false, "GET", "/api/system/interfaces", "", 200)), &ifacesResp); err != nil || len(ifacesResp.Interfaces) != 2 || ifacesResp.Interfaces[0].Name != "tailscale0" {
		t.Fatal(ifacesResp, err)
	}
	var got struct {
		dashboardConfig
		Overridden json.RawMessage `json:"overridden"`
	}
	if err := json.Unmarshal([]byte(call(false, "GET", "/api/system/dashboard", "", 200)), &got); err != nil || len(got.DashboardListen) != 1 || got.DashboardListen[0] != "127.0.0.1:0" || got.Overridden != nil {
		t.Fatal(got, err)
	}
	call(false, "PUT", "/api/system/dashboard", `{"dashboard_listen":["127.0.0.1:0"]}`, 403)
	call(true, "PUT", "/api/system/dashboard", `{}`, 400)
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":[]}`, 400)
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["bad"]}`, 400)
	if msg := call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["127.0.0.1:18081"]}`, 409); !strings.Contains(msg, "public router") {
		t.Fatal(msg)
	}
	if err := a.saveService(Service{ID: "dash-service", Name: "cache", ExternalPort: 16380}); err != nil {
		t.Fatal(err)
	}
	if msg := call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["127.0.0.1:16380"]}`, 409); !strings.Contains(msg, "external_port") {
		t.Fatal(msg)
	}
	occupied := routerTestListener(t)
	if msg := call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["`+occupied.Addr().String()+`"]}`, 409); !strings.Contains(msg, "pick a free port") {
		t.Fatal(msg)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	checkDashboard := func(addr string, running bool) {
		t.Helper()
		client.CloseIdleConnections()
		resp, err := client.Get("http://" + addr + "/api/system/version")
		if resp != nil {
			resp.Body.Close()
		}
		if running && err != nil || !running && err == nil {
			t.Fatalf("dashboard %s, expected running=%t, got resp=%v err=%v", addr, running, resp, err)
		}
	}
	checkDashboard(oldAddr, true)
	candidate := routerTestListener(t)
	newAddr := candidate.Addr().String()
	candidate.Close()
	if err := os.Mkdir(settings.ConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["`+newAddr+`"]}`, 500)
	checkDashboard(oldAddr, true)
	checkDashboard(newAddr, false)
	if err := os.Remove(settings.ConfigPath); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["`+newAddr+`"]}`, 200)
	checkDashboard(oldAddr, false)
	checkDashboard(newAddr, true)
	_, port, err := net.SplitHostPort(newAddr)
	if err != nil {
		t.Fatal(err)
	}
	// Switching between specific interface and wildcard on the same port must succeed in both directions.
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":[":`+port+`"]}`, 200)
	checkDashboard("127.0.0.1:"+port, true)
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["lo:`+port+`"]}`, 200)
	checkDashboard("127.0.0.1:"+port, true)
	// Config write failure during a same-port switch must roll back to the previous listener.
	if err := os.Remove(settings.ConfigPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(settings.ConfigPath, 0700); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":[":`+port+`"]}`, 500)
	checkDashboard("127.0.0.1:"+port, true)
	if err := os.Remove(settings.ConfigPath); err != nil {
		t.Fatal(err)
	}
	call(true, "PUT", "/api/system/dashboard", `{"dashboard_listen":["`+newAddr+`"]}`, 200)
	checkDashboard(newAddr, true)
	if os.Getenv("STARTSHIP_DASHBOARD_ADDR") != newAddr {
		t.Fatalf("STARTSHIP_DASHBOARD_ADDR=%q, want %q", os.Getenv("STARTSHIP_DASHBOARD_ADDR"), newAddr)
	}
	b, err := os.ReadFile(settings.ConfigPath)
	saved := config.Defaults()
	if err != nil || config.Parse(b, &saved) != nil || len(saved.DashboardListen) != 1 || saved.DashboardListen[0] != newAddr {
		t.Fatal(string(b), err)
	}
	t.Setenv("STARTSHIP_DASHBOARD_ADDR", "127.0.0.1:0")
	restarted, err := config.Load([]string{"--config", settings.ConfigPath}, io.Discard)
	if err != nil || len(restarted.DashboardListen) != 1 || restarted.DashboardListen[0] != newAddr {
		t.Fatal(restarted.DashboardListen, err)
	}
}
