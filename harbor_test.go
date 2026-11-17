package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
		f, e := dockerfile(kind, 8123, t.TempDir())
		if e != nil || !strings.Contains(f, "ENV PORT=8123") || !strings.Contains(f, "CMD ") {
			t.Fatal(kind, f, e)
		}
	}
	if _, e := dockerfile("", 0, ""); e == nil {
		t.Fatal("expected unsupported type")
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
