package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/update"
	"github.com/sammwyy/startship/internal/version"
)

func TestSystemUpdateAPI(t *testing.T) {
	releases := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"tag_name":"v1.5.0","html_url":"https://example.test/v1.5.0","body":"notes"}`))
	}))
	defer releases.Close()
	old := version.Version
	version.Version = "v1.4.0"
	defer func() { version.Version = old }()

	a := testServer(t)
	if _, err := a.createUserPending("admin", "password123", false); err != nil {
		t.Fatal(err)
	}
	if _, err := a.createUser("bob", "password123"); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(t.TempDir(), "update")
	a.updates = &updates{client: &update.Client{HTTP: releases.Client(), ReleasesURL: releases.URL}, request: filepath.Join(dir, "request.json"), status: filepath.Join(dir, "status.json")}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"bob","password":"password123"}`)))
	bob := w.Result().Cookies()[0]
	call := func(local bool, method, path, body string, status int) map[string]any {
		t.Helper()
		r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
		if local {
			r = r.WithContext(context.WithValue(r.Context(), socketContextKey{}, true))
		} else {
			r.AddCookie(bob)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		var v map[string]any
		json.Unmarshal(w.Body.Bytes(), &v)
		return v
	}
	if v := call(false, "GET", "system/version", "", 200); v["version"] != "v1.4.0" {
		t.Fatal(v)
	}
	v := call(false, "GET", "system/update", "", 200)
	if v["current"] != "v1.4.0" || v["latest"] != "v1.5.0" || v["update_available"] != true || v["self_update"] != true {
		t.Fatal(v)
	}
	call(false, "POST", "system/update", "", 403)
	call(true, "POST", "system/update", `{"version":"latest"}`, 400)
	call(true, "POST", "system/update", `{"version":"v1.4.0"}`, 409)
	if v := call(true, "POST", "system/update", "", 202); v["version"] != "v1.5.0" || v["by"] != "admin" {
		t.Fatal(v)
	}
	var req update.Request
	if err := update.ReadJSON(a.updates.request, &req); err != nil || req.Version != "v1.5.0" {
		t.Fatal(req, err)
	}
	if v := call(false, "GET", "system/update", "", 200); v["pending"] == nil {
		t.Fatal("pending request not reported", v)
	}
	os.Remove(a.updates.request)
	update.WriteJSON(a.updates.status, update.Status{State: "failed", Version: "v1.5.0", Error: "boom"}, 0o644)
	if v := call(false, "GET", "system/update", "", 200); v["update"].(map[string]any)["error"] != "boom" {
		t.Fatal(v)
	}

	a.updates.request = ""
	call(true, "POST", "system/update", "", 409)
	version.Version = "dev"
	if v := call(false, "GET", "system/update", "", 200); v["update_available"] != false || v["development"] != true {
		t.Fatal(v)
	}
}
