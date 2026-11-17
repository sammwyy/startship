package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPublicRouteURL(t *testing.T) {
	for _, tt := range []struct{ name, httpAddr, httpsAddr, email, host, prefix, want string }{
		{"default", "", "", "", "EXAMPLE.COM", "/", "http://example.com/"},
		{"native quick start", ":8081", "", "", "localhost", "/site/", "http://localhost:8081/site/"},
		{"bound interface is not public host", "127.0.0.1:18081", "", "", "test.local", "/python/", "http://test.local:18081/python/"},
		{"https", ":80", ":443", "admin@example.com", "example.com", "/", "https://example.com/"},
		{"custom https", ":8081", ":8443", "admin@example.com", "example.com", "/docs/", "https://example.com:8443/docs/"},
		{"escaped prefix", "", "", "", "example.com", "/a b?#/", "http://example.com/a%20b%3F%23/"},
		{"ipv6", ":8081", "", "", "[::1]", "/", "http://[::1]:8081/"},
		{"ipv6 default port", "", "", "", "[::1]", "/", "http://[::1]/"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("STARTSHIP_HTTP_ADDR", tt.httpAddr)
			t.Setenv("STARTSHIP_HTTPS_ADDR", tt.httpsAddr)
			t.Setenv("STARTSHIP_ACME_EMAIL", tt.email)
			if got := routeURL(Record{Host: tt.host, PathPrefix: tt.prefix}); got != tt.want {
				t.Fatalf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func TestRouteAPIIncludesPublicURL(t *testing.T) {
	t.Setenv("STARTSHIP_HTTP_ADDR", ":8081")
	t.Setenv("STARTSHIP_ACME_EMAIL", "")
	a := testServer(t)
	owner, err := a.createUser("router-user", "password123")
	if err != nil {
		t.Fatal(err)
	}
	workspace := a.store.list("workspaces", owner)[0]
	project := Record{ID: randomID(), Kind: "projects", Owner: owner, Parent: workspace.ID, WorkspaceID: workspace.ID, Name: "site"}
	app := Record{ID: randomID(), Kind: "apps", Owner: owner, Parent: project.ID, ProjectID: project.ID, Name: "static"}
	for _, r := range []Record{project, app} {
		if err := a.store.save(r); err != nil {
			t.Fatal(err)
		}
	}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"router-user","password":"password123"}`)))
	cookie := w.Result().Cookies()[0]
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != 200 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w
	}
	w = request("POST", "/api/routes", `{"project_id":"`+project.ID+`","app_id":"`+app.ID+`","host":"localhost","path_prefix":"/site/"}`)
	var route apiRecord
	if err := json.Unmarshal(w.Body.Bytes(), &route); err != nil {
		t.Fatal(err)
	}
	for _, response := range []*httptest.ResponseRecorder{w, request("GET", "/api/routes/"+route.ID, ""), request("GET", "/api/routes?project_id="+project.ID, "")} {
		if !strings.Contains(response.Body.String(), `"url":"http://localhost:8081/site/"`) {
			t.Fatal(response.Body.String())
		}
	}
	// URLs are recomputed on read after configuration changes, never persisted.
	t.Setenv("STARTSHIP_HTTP_ADDR", ":80")
	w = request("GET", "/api/routes/"+route.ID, "")
	if !strings.Contains(w.Body.String(), `"url":"http://localhost/site/"`) {
		t.Fatal(w.Body.String())
	}
	if publicRecord(Record{Kind: "apps", GitToken: "private-token"}).GitToken != "" {
		t.Fatal("token exposed")
	}
}
