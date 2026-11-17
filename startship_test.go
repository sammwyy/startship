package main

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDataDirectory(t *testing.T) {
	t.Chdir(t.TempDir())
	t.Setenv("STARTSHIP_DATA", "")
	if got := dataDirectory(); got != "./_data" {
		t.Fatalf("fresh install: got %q", got)
	}
	if err := os.Mkdir("data", 0700); err != nil {
		t.Fatal(err)
	}
	if got := dataDirectory(); got != "./data" {
		t.Fatalf("existing install: got %q", got)
	}
	if err := os.Mkdir("_data", 0700); err != nil {
		t.Fatal(err)
	}
	if got := dataDirectory(); got != "./data" {
		t.Fatalf("both directories exist: got %q", got)
	}
	t.Setenv("STARTSHIP_DATA", "custom-data")
	if got := dataDirectory(); got != "custom-data" {
		t.Fatalf("explicit override: got %q", got)
	}
}

func TestBindServers(t *testing.T) {
	occupied, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer occupied.Close()
	listeners, err := bindServers([]*http.Server{{Addr: "127.0.0.1:0"}, {Addr: occupied.Addr().String()}})
	if err == nil || len(listeners) != 0 || !strings.Contains(err.Error(), occupied.Addr().String()) || !strings.Contains(err.Error(), "cap_net_bind_service") {
		t.Fatalf("%v %v", listeners, err)
	}
	listeners, err = bindServers([]*http.Server{{Addr: "127.0.0.1:0"}, {Addr: "127.0.0.1:0"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, ln := range listeners {
		ln.Close()
	}
	listeners, err = bindServers(nil)
	if err != nil || len(listeners) != 0 {
		t.Fatal(listeners, err)
	}
}

func TestNodeEngines(t *testing.T) {
	for _, tc := range []struct {
		engine string
		want   int
	}{
		{"", 22}, {"22.x || 24.x", 24}, {">=20", 26}, {"^18", 18}, {"20", 20}, {"~20.0.0", 20}, {">=20 <24", 22}, {">= 20", 26}, {"*", 26}, {"invalid", 22}, {">=99", 22}, {"22.x || 24.x || 26.x", 26},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			if got := nodeMajor(tc.engine); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"engines":{"node":"24.x"},"scripts":{"start":"node server.js"}}`), 0600)
	got, err := dockerfile("node", 8080, dir)
	if err != nil || !strings.HasPrefix(got, "FROM node:24-alpine\n") || !strings.Contains(got, "RUN npm install\n") {
		t.Fatal(got, err)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{invalid`), 0600)
	got, err = dockerfile("node", 8080, dir)
	if err != nil || !strings.HasPrefix(got, "FROM node:22-alpine\n") {
		t.Fatal(got, err)
	}
}

func TestDeploymentResultLog(t *testing.T) {
	a := testServer(t)
	app := Record{ID: "demo", Name: "Demo app"}
	var out bytes.Buffer
	a.logDeploymentResult(&out, app, nil)
	if out.String() != "Deployment healthy; now serving Demo app\n" {
		t.Fatal(out.String())
	}
	if err := a.store.save(Record{ID: "route", Kind: "routes", AppID: "demo", Host: "demo.localtest.me", PathPrefix: "/"}); err != nil {
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
	app := Record{ID: "missing", Kind: "apps", Name: "Missing", GitURL: "file:///nonexistent-startship-test-repo", Branch: "main", RootDir: ".", BuildType: "auto"}
	if err := a.store.save(app); err != nil {
		t.Fatal(err)
	}
	if err := a.store.save(Record{ID: "failed", Kind: "deployments", AppID: app.ID}); err != nil {
		t.Fatal(err)
	}
	a.build(app.ID, "failed")
	d, err := a.store.get("failed")
	if err != nil || d.Status != "failed" {
		t.Fatal(d, err)
	}
	last := strings.TrimSpace(d.Log)
	last = last[strings.LastIndex(last, "\n")+1:]
	if !strings.HasPrefix(last, "Deployment failed:") || !strings.Contains(last, "does not appear to be a git repository") {
		t.Fatal(d.Log)
	}
}

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
