package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

func accountRequest(t *testing.T, h http.Handler, method, path, body string, cookie *http.Cookie, status int) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
	r.Header.Set("X-Forwarded-Proto", "https")
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != status {
		t.Fatalf("%s %s: status %d, want %d: %s", method, path, w.Code, status, w.Body.String())
	}
	return w
}

func accountLogin(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	return accountRequest(t, h, "POST", "login", string(body), nil, 200).Result().Cookies()[0]
}

func TestAccountCredentialsAndSessions(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "bob-password"); err != nil {
		t.Fatal(err)
	}
	h := a.api()
	current, other := accountLogin(t, h, "alice", "password123"), accountLogin(t, h, "alice", "password123")
	bob := accountLogin(t, h, "bob", "bob-password")
	w := accountRequest(t, h, "PATCH", "me", `{"username":"  alice-renamed  ","current_password":"password123","new_password":"replacement123","id":"someone-else"}`, current, 200)
	if strings.Contains(w.Body.String(), "password") || strings.Contains(w.Body.String(), "replacement") {
		t.Fatal("account response exposed a password")
	}
	var profile map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &profile); err != nil || profile["id"] != uid || profile["username"] != "alice-renamed" {
		t.Fatal("wrong account response", err, profile)
	}
	rotated := w.Result().Cookies()[0]
	if rotated.Value == current.Value || !rotated.HttpOnly || !rotated.Secure || rotated.SameSite != http.SameSiteLaxMode || rotated.Path != "/" || rotated.MaxAge != 604800 {
		t.Fatal("session not rotated with secure attributes")
	}
	accountRequest(t, h, "GET", "me", "", rotated, 200)
	accountRequest(t, h, "GET", "me", "", current, 401)
	accountRequest(t, h, "GET", "me", "", other, 401)
	accountRequest(t, h, "GET", "me", "", bob, 200)
	accountRequest(t, h, "POST", "login", `{"username":"alice","password":"password123"}`, nil, 401)
	accountRequest(t, h, "POST", "login", `{"username":"alice-renamed","password":"password123"}`, nil, 401)
	accountLogin(t, h, "alice-renamed", "replacement123")
	var stored string
	if err := a.store.db.QueryRow("SELECT password FROM users WHERE id=?", uid).Scan(&stored); err != nil || stored == "replacement123" || bcrypt.CompareHashAndPassword([]byte(stored), []byte("replacement123")) != nil {
		t.Fatal("new password was not securely hashed", err)
	}
	if workspaces := a.store.list("workspaces", uid); len(workspaces) != 1 || workspaces[0].Owner != uid {
		t.Fatal("account update changed workspace ownership")
	}

	// Username-only and password-only updates leave the other credential intact.
	w = accountRequest(t, h, "PATCH", "me", `{"username":"alice","current_password":"replacement123"}`, rotated, 200)
	rotated = w.Result().Cookies()[0]
	accountLogin(t, h, "alice", "replacement123")
	accountRequest(t, h, "PATCH", "me", `{"current_password":"replacement123","new_password":"final-password"}`, rotated, 200)
	accountLogin(t, h, "alice", "final-password")
}

func TestAccountUpdateValidation(t *testing.T) {
	a := testServer(t)
	if _, err := a.createUser("alice", "password123"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	h := a.api()
	cookie := accountLogin(t, h, "alice", "password123")
	for _, tc := range []struct {
		name, body string
		status     int
	}{
		{"invalid JSON", `{`, 400},
		{"empty update", `{}`, 400},
		{"missing confirmation", `{"username":"new"}`, 400},
		{"wrong current password", `{"username":"new","current_password":"wrong-password"}`, 403},
		{"blank username", `{"username":"  ","current_password":"password123"}`, 400},
		{"long username", `{"username":"` + strings.Repeat("a", 101) + `","current_password":"password123"}`, 400},
		{"short password", `{"new_password":"short","current_password":"password123"}`, 400},
		{"empty password", `{"new_password":"","current_password":"password123"}`, 400},
		{"long password", `{"new_password":"` + strings.Repeat("a", 73) + `","current_password":"password123"}`, 400},
		{"unicode byte limit", `{"new_password":"` + strings.Repeat("é", 37) + `","current_password":"password123"}`, 400},
		{"duplicate username", `{"username":" bob ","new_password":"replacement123","current_password":"password123"}`, 409},
	} {
		t.Run(tc.name, func(t *testing.T) {
			accountRequest(t, h, "PATCH", "me", tc.body, cookie, tc.status)
			w := accountRequest(t, h, "GET", "me", "", cookie, 200)
			if !strings.Contains(w.Body.String(), `"username":"alice"`) {
				t.Fatal("rejected update changed account")
			}
		})
	}
	accountLogin(t, h, "alice", "password123")
	accountRequest(t, h, "PATCH", "me", `{"username":"new","current_password":"password123"}`, nil, 401)
	for _, method := range []string{"POST", "PUT", "DELETE"} {
		accountRequest(t, h, method, "me", `{"username":"new","current_password":"password123"}`, cookie, 405)
	}
	r := httptest.NewRequest("PATCH", "/api/me", strings.NewReader(`{"username":"new","current_password":"password123"}`))
	r.AddCookie(cookie)
	r.Header.Set("Origin", "https://untrusted.example")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 403 {
		t.Fatal("cross-origin credential change accepted", w.Code)
	}
	accountRequest(t, h, "GET", "me", "", cookie, 200)
}

func TestDeploymentMetadataWithoutLogs(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	cookie := accountLogin(t, h, "alice", "password123")
	if err := a.store.save(Record{ID: "release", Kind: "deployments", Owner: uid, AppID: "app", Status: "running", Log: "build log content"}); err != nil {
		t.Fatal(err)
	}
	metadata := accountRequest(t, h, "GET", "deployments?logs=false", "", cookie, 200)
	if strings.Contains(metadata.Body.String(), "build log content") || !strings.Contains(metadata.Body.String(), `"status":"running"`) {
		t.Fatal("invalid metadata response", metadata.Body.String())
	}
	for _, path := range []string{"deployments?app_id=app", "deployments/release"} {
		full := accountRequest(t, h, "GET", path, "", cookie, 200)
		if !strings.Contains(full.Body.String(), "build log content") {
			t.Fatal("log was not preserved", path)
		}
	}
}
