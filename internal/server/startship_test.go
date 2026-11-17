package server

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestDeploymentResultLog(t *testing.T) {
	a := testServer(t)
	app := Record{ID: "demo", Name: "Demo app"}
	var out bytes.Buffer
	a.logDeploymentResult(&out, app, nil)
	if out.String() != "Deployment healthy; now serving Demo app\n" {
		t.Fatal(out.String())
	}
	if err := a.store.Save(Record{ID: "route", Kind: "routes", AppID: "demo", Host: "demo.localtest.me", PathPrefix: "/"}); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	a.logDeploymentResult(&out, app, nil)
	if out.String() != "Deployment healthy; now serving demo.localtest.me/\n" {
		t.Fatal(out.String())
	}
	out.Reset()
	a.logDeploymentResult(&out, app, errors.New("health check timed out"))
	if out.String() != "Deployment failed: health check timed out\n" {
		t.Fatal(out.String())
	}
}

func TestBuildFailureLog(t *testing.T) {
	a := testServer(t)
	project := Record{ID: "project", Kind: "projects", GitURL: "file:///nonexistent-startship-test-repo", Branch: "main"}
	if err := a.store.Save(project); err != nil {
		t.Fatal(err)
	}
	app := Record{ID: "missing", Kind: "apps", ProjectID: project.ID, Name: "Missing", RootDir: ".", BuildType: "auto"}
	if err := a.store.Save(app); err != nil {
		t.Fatal(err)
	}
	if err := a.store.Save(Record{ID: "failed", Kind: "deployments", AppID: app.ID}); err != nil {
		t.Fatal(err)
	}
	a.build(app.ID, "failed")
	d, err := a.store.Get("failed")
	if err != nil || d.Status != "failed" {
		t.Fatal(d, err)
	}
	last := strings.TrimSpace(d.Log)
	last = last[strings.LastIndex(last, "\n")+1:]
	if !strings.HasPrefix(last, "Deployment failed:") || !strings.Contains(last, "does not appear to be a git repository") {
		t.Fatal(d.Log)
	}
}

func TestCheckoutRoot(t *testing.T) {
	base := t.TempDir()
	checkout := filepath.Join(base, "_data", "builds", "checkout")
	app := filepath.Join(checkout, "app")
	outside := filepath.Join(base, "outside")
	for _, dir := range []string{app, outside} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for link, target := range map[string]string{
		filepath.Join(base, "data"):          filepath.Join(base, "_data"),
		filepath.Join(base, "checkout-link"): checkout,
		filepath.Join(checkout, "internal"):  app,
		filepath.Join(checkout, "external"):  outside,
		filepath.Join(checkout, "sibling"):   filepath.Dir(checkout),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{checkout, filepath.Join(base, "data", "builds", "checkout"), filepath.Join(base, "checkout-link")} {
		for _, tc := range []struct {
			root, want string
			escape     bool
		}{
			{"", checkout, false},
			{".", checkout, false},
			{"app", app, false},
			{"internal", app, false},
			{"external", "", true},
			{"sibling", "", true},
			{"..", "", true},
		} {
			t.Run(dir+"/"+tc.root, func(t *testing.T) {
				got, err := checkoutRoot(dir, tc.root)
				if tc.escape {
					if err == nil || err.Error() != "root directory escapes repository" {
						t.Fatalf("got %q, %v; want containment error", got, err)
					}
					return
				}
				if err != nil || got != tc.want {
					t.Fatalf("got %q, %v; want %q", got, err, tc.want)
				}
			})
		}
	}
	for _, tc := range []struct{ dir, root string }{{filepath.Join(base, "missing"), "."}, {checkout, "missing"}} {
		if _, err := checkoutRoot(tc.dir, tc.root); !os.IsNotExist(err) {
			t.Fatalf("got %v; want missing path error", err)
		}
	}
}

func TestDashboardEmbedded(t *testing.T) {
	a := testServer(t)
	index := httptest.NewRecorder()
	a.api().ServeHTTP(index, httptest.NewRequest("GET", "/", nil))
	if index.Code != http.StatusOK {
		t.Fatal("dashboard entrypoint", index.Code)
	}
	assets := regexp.MustCompile(`(?:src|href)="(/assets/[^"]+)"`).FindAllStringSubmatch(index.Body.String(), -1)
	if len(assets) < 2 {
		t.Fatal("production scripts and styles missing from entrypoint")
	}
	for _, asset := range assets {
		path := asset[1]
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(path, w.Code)
		}
	}
	for _, path := range []string{"/app.js", "/src/main.tsx", "/package.json"} {
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusNotFound {
			t.Fatal("development source exposed", path, w.Code)
		}
	}
}
func TestListenerErrors(t *testing.T) {
	l, err := listen("test", "127.0.0.1:0", "TEST_ADDR", ":8081")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	for _, name := range []string{"dashboard", "proxy"} {
		_, err := listen(name, l.Addr().String(), "TEST_ADDR", ":8081")
		if err == nil || !strings.Contains(err.Error(), name+": cannot listen on "+l.Addr().String()) || !strings.Contains(err.Error(), "set TEST_ADDR=:8081") {
			t.Fatal(err)
		}
	}
	first, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := first.Addr().String()
	first.Close()
	if err := serve([]*http.Server{{Addr: addr}, {Addr: l.Addr().String()}}); err == nil || !strings.Contains(err.Error(), "STARTSHIP_HTTP_ADDR=:8081") {
		t.Fatal(err)
	}
	reopened, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatal("listener leaked after startup failure:", err)
	}
	reopened.Close()
}
func TestRouteNormalizationAPI(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	project := Record{ID: randomID(), Kind: "projects", Owner: uid}
	app := Record{ID: randomID(), Kind: "apps", Owner: uid, ProjectID: project.ID}
	for _, record := range []Record{project, app} {
		if err := a.store.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	request := func(method, path, prefix string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"project_id": project.ID, "app_id": app.ID, "host": "example.com", "path_prefix": prefix})
		r := httptest.NewRequest(method, path, strings.NewReader(string(body)))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	w = request("POST", "/api/routes", "api/*")
	var route Record
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil || w.Code != 200 || route.PathPrefix != "/api/" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	if duplicate := request("POST", "/api/routes", "/api/"); duplicate.Code != 409 {
		t.Fatal(duplicate.Code, duplicate.Body.String())
	}
	w = request("PATCH", "/api/routes/"+route.ID, "api")
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil || w.Code != 200 || route.PathPrefix != "/api" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
	w = request("PATCH", "/api/routes/"+route.ID, "")
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil || w.Code != 200 || route.PathPrefix != "/" {
		t.Fatal(w.Code, w.Body.String(), err)
	}
}
func TestSignature(t *testing.T) {
	body := []byte("payload")
	m := hmac.New(sha256.New, []byte("secret"))
	m.Write(body)
	sig := "sha256=" + hex.EncodeToString(m.Sum(nil))
	if !verifySignature("secret", body, sig) {
		t.Fatal("valid signature rejected")
	}
	for _, s := range []string{"", sig + "ff", "sha256=xx"} {
		if verifySignature("secret", body, s) {
			t.Fatal("invalid accepted")
		}
	}
	if verifySignature("other", body, sig) {
		t.Fatal("wrong key")
	}
}
func testServer(t *testing.T) *Server {
	t.Helper()
	s, e := openStore(filepath.Join(t.TempDir(), "test.db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.DB.Close() })
	return &Server{store: s, data: t.TempDir(), queues: make(map[string]chan string)}
}
func TestSetupAndOwnership(t *testing.T) {
	a := testServer(t)
	h := a.api()
	req := httptest.NewRequest("POST", "/api/setup", strings.NewReader("{\"username\":\"alice\",\"password\":\"password123\"}"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/setup", strings.NewReader("{}")))
	if w.Code != 409 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader("{\"username\":\"alice\",\"password\":\"password123\"}")))
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cookie := w.Result().Cookies()[0]
	if !cookie.HttpOnly {
		t.Fatal("cookie")
	}
	r := httptest.NewRequest("GET", "/api/workspaces", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if !strings.Contains(w.Body.String(), "alice's Workspace") {
		t.Fatal(w.Body.String())
	}
	var aliceWorkspace []Record
	json.Unmarshal(w.Body.Bytes(), &aliceWorkspace)
	a.createUser("bob", "password123")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"bob","password":"password123"}`)))
	r = httptest.NewRequest("GET", "/api/workspaces/"+aliceWorkspace[0].ID, nil)
	r.AddCookie(w.Result().Cookies()[0])
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatal(w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/api/projects", nil))
	if w.Code != http.StatusUnauthorized {
		t.Fatal(w.Code)
	}
}
func TestStaticProxy(t *testing.T) {
	a := testServer(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "index.html"), []byte("static-ok"), 0600)
	a.targets = []Target{{Route: Record{Host: "test.local", PathPrefix: "/static/", StripPrefix: true}, Deployment: Record{Status: "running", StaticDir: dir}}}
	for _, p := range []string{"/static/", "/static/deep/link"} {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://test.local"+p, nil)
		a.ServeHTTP(w, r)
		if w.Code != 200 || !strings.Contains(w.Body.String(), "static-ok") {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}
func setupAndLogin(t *testing.T, h http.Handler, header map[string]string) *http.Response {
	t.Helper()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/setup", strings.NewReader(`{"username":" carol ","password":"password123"}`)))
	if w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	req := httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"carol","password":"password123"}`))
	for k, v := range header {
		req.Header.Set(k, v)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	return w.Result()
}
func TestNamesRequiredAndTrimmed(t *testing.T) {
	a := testServer(t)
	h := a.api()
	cookie := setupAndLogin(t, h, nil).Cookies()[0]
	post := func(path, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", path, strings.NewReader(body))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	for _, body := range []string{`{}`, `{"name":"   "}`} {
		if w := post("/api/workspaces", body); w.Code != 400 || !strings.Contains(w.Body.String(), "name is required") {
			t.Fatal(body, w.Code, w.Body.String())
		}
	}
	w := post("/api/workspaces", `{"name":"  Client Sites "}`)
	var ws Record
	if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &ws) != nil || ws.Name != "Client Sites" || ws.Created.IsZero() {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post("/api/projects", `{"workspace_id":"`+ws.ID+`","name":" "}`); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if w := post("/api/projects", `{"workspace_id":"`+ws.ID+`","name":"Acme"}`); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"carol","password":"wrong-password"}`)))
	if w.Code != 401 || !strings.Contains(w.Body.String(), "invalid username or password") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func TestSecureCookiesBehindTLSProxy(t *testing.T) {
	a := testServer(t)
	h := a.api()
	res := setupAndLogin(t, h, map[string]string{"X-Forwarded-Proto": "https"})
	cookie := res.Cookies()[0]
	if !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatal(cookie)
	}
	r := httptest.NewRequest("POST", "/api/logout", nil)
	r.Header.Set("X-Forwarded-Proto", "https")
	r.AddCookie(cookie)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	cleared := w.Result().Cookies()[0]
	if cleared.MaxAge >= 0 || !cleared.Secure || cleared.SameSite != http.SameSiteLaxMode {
		t.Fatal(cleared)
	}
	r = httptest.NewRequest("GET", "/api/me", nil)
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatal("session survived logout", w.Code)
	}
}
func TestDashboardDomainHTTPS(t *testing.T) {
	a := testServer(t)
	a.dashboardDomain = "Dash.Example.com"
	a.store.Save(Record{ID: "r1", Kind: "routes", Owner: "u", Host: "app.example.com", PathPrefix: "/"})
	for host, want := range map[string]bool{"dash.example.com": true, "app.example.com": true, "other.example.com": false, "": false} {
		if a.certAllowed(host) != want {
			t.Fatal(host, want)
		}
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "http://dash.example.com/api/setup", nil))
	if w.Code != 200 {
		t.Fatal("plain HTTP dashboard without ACME", w.Code)
	}
	a.https = true
	w = httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "http://dash.example.com/api/setup?x=1", nil))
	if w.Code != http.StatusPermanentRedirect || w.Header().Get("Location") != "https://dash.example.com/api/setup?x=1" {
		t.Fatal(w.Code, w.Header().Get("Location"))
	}
	w = httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "https://dash.example.com/api/setup", nil))
	if w.Code != 200 || !strings.Contains(w.Body.String(), "required") {
		t.Fatal(w.Code, w.Body.String())
	}
}
