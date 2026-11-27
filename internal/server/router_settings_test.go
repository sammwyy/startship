package server

import (
	"context"
	"crypto/tls"
	"encoding/json"
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
	a := testServer(t)
	if _, err := a.createUserPending("admin", "password123", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	settings := config.Defaults()
	settings.ConfigPath = filepath.Join(t.TempDir(), "startship.toml")
	settings.HTTPListen = []string{"127.0.0.1:0"}
	settings.HTTPSListen = []string{"127.0.0.1:0"}
	settings.Overridden = map[string]bool{"https_listen": true}
	a.routerListeners.httpHandler = a
	certificate := httptest.NewTLSServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	defer certificate.Close()
	a.legacyCertificate = func(*tls.ClientHelloInfo) (*tls.Certificate, error) {
		return &certificate.TLS.Certificates[0], nil
	}
	initial, _, err := a.prepareRouterListeners(settings)
	if err != nil {
		t.Fatal(err)
	}
	a.commitRouterListeners(settings, initial)
	t.Cleanup(a.stopRouterListeners)
	oldHTTP := initial[listenerKey{1, "127.0.0.1:0"}].listener.Addr().String()
	oldHTTPS := initial[listenerKey{2, "127.0.0.1:0"}].listener.Addr().String()
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
	if err := json.Unmarshal([]byte(call(false, "GET", "", 200)), &got); err != nil || !got.Enabled || got.ConfigPath != settings.ConfigPath || len(got.Overridden) != 1 || got.Overridden[0] != "https_listen" {
		t.Fatal(got, err)
	}
	call(false, "PUT", put(false, "127.0.0.1:0", "127.0.0.1:0"), 403)
	call(true, "PUT", `{"http_listen":[],"https_listen":[]}`, 400)
	call(true, "PUT", `{"enabled":true,"http_listen":[],"https_listen":[]}`, 400)
	call(true, "PUT", put(true, "not an address", "127.0.0.1:0"), 400)
	if msg := call(true, "PUT", put(true, "127.0.0.1:0", ":18443"), 409); !strings.Contains(msg, "STARTSHIP_HTTPS_ADDR") {
		t.Fatal(msg)
	}
	if msg := call(true, "PUT", put(true, "127.0.0.1:8080", "127.0.0.1:0"), 409); !strings.Contains(msg, "dashboard_listen") {
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
	if listeners, _, err := a.prepareRouterListeners(disabled); err != nil || len(listeners) != 0 {
		t.Fatalf("disabled router attempted to bind: %v, %v", listeners, err)
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
}
