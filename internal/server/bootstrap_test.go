package server

import (
	"bytes"
	"encoding/json"
	"log"
	"regexp"
	"testing"
)

func bootstrapPassword(t *testing.T, a *Server) string {
	t.Helper()
	var out bytes.Buffer
	old := log.Writer()
	log.SetOutput(&out)
	defer log.SetOutput(old)
	if err := a.bootstrapAdmin("", ""); err != nil {
		t.Fatal(err)
	}
	m := regexp.MustCompile(`Password: (\S+)`).FindStringSubmatch(out.String())
	if len(m) != 2 {
		t.Fatal("missing credentials banner")
	}
	return m[1]
}

func TestBootstrapLifecycle(t *testing.T) {
	a := testServer(t)
	password := bootstrapPassword(t, a)
	if len(password) != 20 || a.setupComplete() {
		t.Fatal("invalid bootstrap state")
	}
	h := a.api()
	cookie := accountLogin(t, h, "admin", password)
	w := accountRequest(t, h, "GET", "me", "", cookie, 200)
	var me struct {
		Pending  bool `json:"must_change_password"`
		Complete bool `json:"setup_completed"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil || !me.Pending || me.Complete {
		t.Fatal(w.Body.String(), err)
	}
	for _, path := range []string{"workspaces", "projects", "detect", "unknown"} {
		accountRequest(t, h, "GET", path, "", cookie, 403)
	}
	accountRequest(t, h, "PATCH", "me", `{}`, cookie, 400)
	accountRequest(t, h, "PATCH", "me", `{"current_password":"wrong","new_password":"replacement"}`, cookie, 403)
	next := bootstrapPassword(t, a)
	if next == password {
		t.Fatal("temporary password did not rotate")
	}
	accountRequest(t, h, "GET", "me", "", cookie, 401)
	accountRequest(t, h, "POST", "login", `{"username":"admin","password":"`+password+`"}`, nil, 401)
	cookie = accountLogin(t, h, "admin", next)
	body, _ := json.Marshal(map[string]string{"current_password": next, "new_password": "replacement-password"})
	w = accountRequest(t, h, "PATCH", "me", string(body), cookie, 200)
	cookie = w.Result().Cookies()[0]
	accountRequest(t, h, "GET", "workspaces", "", cookie, 200)
	if !a.setupComplete() {
		t.Fatal("setup not completed")
	}
	if err := a.bootstrapAdmin("", ""); err != nil {
		t.Fatal(err)
	}
	accountLogin(t, h, "admin", "replacement-password")
}

func TestBootstrapEnvironment(t *testing.T) {
	a := testServer(t)
	if err := a.bootstrapAdmin("owner", "seeded-password"); err != nil {
		t.Fatal(err)
	}
	if !a.setupComplete() {
		t.Fatal("setup incomplete")
	}
	c := accountLogin(t, a.api(), "owner", "seeded-password")
	accountRequest(t, a.api(), "GET", "workspaces", "", c, 200)
	if err := a.bootstrapAdmin("other", "different-password"); err != nil {
		t.Fatal(err)
	}
	accountLogin(t, a.api(), "owner", "seeded-password")
}

func TestBootstrapKeepsInvitedUsers(t *testing.T) {
	a := testServer(t)
	if err := a.bootstrapAdmin("owner", "seeded-password"); err != nil {
		t.Fatal(err)
	}
	h := a.api()
	owner := accountLogin(t, h, "owner", "seeded-password")
	accountRequest(t, h, "POST", "users", `{"username":"invited","password":"temporary-pass"}`, owner, 201)
	if err := a.bootstrapAdmin("", ""); err != nil {
		t.Fatal(err)
	}
	accountLogin(t, h, "invited", "temporary-pass")
}
