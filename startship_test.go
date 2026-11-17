package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetection(t *testing.T) {
	for _, v := range []struct{ file, kind string }{{"Dockerfile", "dockerfile"}, {"package.json", "node"}, {"requirements.txt", "python"}, {"pyproject.toml", "python"}, {"go.mod", "go"}, {"Cargo.toml", "rust"}, {"index.html", "static"}} {
		t.Run(v.kind+v.file, func(t *testing.T) {
			dir := t.TempDir()
			if got := detect(dir); got != "" {
				t.Fatal(got)
			}
			os.WriteFile(filepath.Join(dir, v.file), []byte("{}"), 0600)
			if got := detect(dir); got != v.kind {
				t.Fatal(got)
			}
		})
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "api"), 0700)
	os.WriteFile(filepath.Join(dir, "api", "go.mod"), nil, 0600)
	if c := candidates(dir); len(c) != 1 || c[0].RootDir != "api" {
		t.Fatal(c)
	}
}
func TestDockerfiles(t *testing.T) {
	for _, kind := range []string{"node", "python", "go", "rust"} {
		dir := t.TempDir()
		if kind == "python" {
			os.WriteFile(filepath.Join(dir, "main.py"), nil, 0600)
		}
		f, e := dockerfile(kind, 8123, dir)
		if e != nil || !strings.Contains(f, "ENV PORT=8123") || !strings.Contains(f, "CMD ") {
			t.Fatal(kind, f, e)
		}
	}
	if _, e := dockerfile("", 0, ""); e == nil {
		t.Fatal("expected unsupported type")
	}
}
func TestEntrypoints(t *testing.T) {
	for _, file := range []string{"main.py", "app.py", "manage.py"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, file), nil, 0600)
		if detect(dir) != "python" {
			t.Fatal(file)
		}
		got, err := dockerfile("python", 8080, dir)
		if err != nil || !strings.Contains(got, file) {
			t.Fatal(got, err)
		}
		if file == "manage.py" && !strings.Contains(got, "0.0.0.0:$PORT") {
			t.Fatal(got)
		}
	}
	for _, kind := range []string{"node", "python"} {
		dir := t.TempDir()
		cmd := "node server.js"
		if kind == "python" {
			cmd = "gunicorn main:app --bind 0.0.0.0:$PORT"
		}
		os.WriteFile(filepath.Join(dir, "Procfile"), []byte("worker: ignore\nweb: "+cmd+"\n"), 0600)
		got, err := dockerfile(kind, 9000, dir)
		if err != nil || !strings.Contains(got, cmd) || detect(dir) != kind {
			t.Fatal(got, err)
		}
	}
	if _, err := dockerfile("python", 8080, t.TempDir()); err == nil {
		t.Fatal("missing entrypoint accepted")
	}
}
func TestDashboardEmbedded(t *testing.T) {
	a := testServer(t)
	for _, path := range []string{"/", "/app.js", "/style.css"} {
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != 200 || w.Body.Len() == 0 {
			t.Fatal(path, w.Code)
		}
	}
}
func TestRoutes(t *testing.T) {
	ts := []Target{{Route: Record{Host: "example.com", PathPrefix: "/"}}, {Route: Record{Host: "example.com", PathPrefix: "/api/"}}}
	if got := matchRoute(ts, "EXAMPLE.COM:80", "/api/a"); got == nil || got.Route.PathPrefix != "/api/" {
		t.Fatal(got)
	}
	if matchRoute(ts, "other.com", "/") != nil {
		t.Fatal("host mismatch")
	}
	if matchRoute(nil, "", "") != nil {
		t.Fatal("empty")
	}
	if stripped("/api/hello", "/api/") != "/hello" {
		t.Fatal("strip")
	}
}
func TestRoutePrefixes(t *testing.T) {
	for _, v := range []struct{ input, want string }{{"", "/"}, {"*", "/"}, {"api", "/api"}, {"api/*", "/api/"}, {"/api/*", "/api/"}, {"/api*", "/api"}, {"/api/", "/api/"}, {" /api ", "/api"}} {
		if got := normalizePrefix(v.input); got != v.want {
			t.Errorf("normalizePrefix(%q) = %q, want %q", v.input, got, v.want)
		}
	}
	for _, v := range []struct {
		prefix, path string
		want         bool
	}{{"/", "/apiary", true}, {"/api", "/api", true}, {"/api", "/api/", true}, {"/api", "/api/health", true}, {"/api", "/apiary", false}, {"/api", "/apiculture", false}, {"/api/", "/api", false}, {"/api/", "/api/health", true}} {
		if got := matchesPrefix(v.path, v.prefix); got != v.want {
			t.Errorf("matchesPrefix(%q, %q) = %v", v.path, v.prefix, got)
		}
	}
	ts := []Target{{Route: Record{Host: "example.com", PathPrefix: "/"}}, {Route: Record{Host: "example.com", PathPrefix: "/api"}}}
	for _, p := range []string{"/api", "/api/health", "/apiary"} {
		want := "/api"
		if p == "/apiary" {
			want = "/"
		}
		if got := matchRoute(ts, "example.com", p); got == nil || got.Route.PathPrefix != want {
			t.Fatalf("route for %s: %+v", p, got)
		}
	}
	for _, v := range []struct{ path, prefix, want string }{{"/api", "/api", "/"}, {"/api/", "/api", "/"}, {"/api/health", "/api", "/health"}, {"/api/health", "/api/", "/health"}} {
		if got := stripped(v.path, v.prefix); got != v.want {
			t.Errorf("stripped(%q, %q) = %q", v.path, v.prefix, got)
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
		if err := a.store.save(record); err != nil {
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
	t.Cleanup(func() { s.db.Close() })
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
	id, _ := a.createUser("bob", "password123")
	private := Record{ID: randomID(), Kind: "workspaces", Owner: id}
	a.store.save(private)
	r = httptest.NewRequest("GET", "/api/workspaces/"+private.ID, nil)
	r.AddCookie(cookie)
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
