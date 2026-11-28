package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDeploymentEndpoints(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`)))
	cookie := w.Result().Cookies()[0]
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte("#!/bin/sh\n[ \"$1 $2\" = \"image inspect\" ] && [ \"$3\" = present ]\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	static := t.TempDir()
	for _, record := range []Record{
		{ID: "project", Kind: "projects", Owner: uid, GitURL: "file:///repo"},
		{ID: "app", Kind: "apps", Owner: uid, ProjectID: "project", Active: "active"},
		{ID: "empty", Kind: "apps", Owner: uid},
		{ID: "foreign-app", Kind: "apps", Owner: "someone-else"},
		{ID: "active", Kind: "deployments", Owner: uid, AppID: "app", Status: "running", Image: "present", Commit: "sha"},
		{ID: "older", Kind: "deployments", Owner: uid, AppID: "app", Status: "superseded", Image: "present", Commit: "older-sha"},
		{ID: "static", Kind: "deployments", Owner: uid, AppID: "app", Status: "superseded", StaticDir: static},
		{ID: "missing-static", Kind: "deployments", Owner: uid, AppID: "app", Status: "superseded", StaticDir: filepath.Join(static, "gone")},
		{ID: "missing-image", Kind: "deployments", Owner: uid, AppID: "app", Status: "superseded", Image: "gone"},
		{ID: "failed", Kind: "deployments", Owner: uid, AppID: "app", Status: "failed", Image: "present"},
		{ID: "other-app-release", Kind: "deployments", Owner: uid, AppID: "different", Status: "running", Image: "present"},
	} {
		if err := a.store.Save(record); err != nil {
			t.Fatal(err)
		}
	}
	a.queues["app"] = make(chan string, 20)
	for _, test := range []struct {
		name, path, body string
		status           int
		source           string
		noCache          bool
	}{
		{"default", "app/redeploy", `{}`, 202, "active", false},
		{"empty body", "app/redeploy", ``, 202, "active", false},
		{"older", "app/redeploy", `{"deployment_id":"older"}`, 202, "older", false},
		{"static", "app/redeploy", `{"deployment_id":"static"}`, 202, "static", false},
		{"missing static", "app/redeploy", `{"deployment_id":"missing-static"}`, 409, "", false},
		{"missing image", "app/redeploy", `{"deployment_id":"missing-image"}`, 409, "", false},
		{"failed", "app/redeploy", `{"deployment_id":"failed"}`, 409, "", false},
		{"wrong app", "app/redeploy", `{"deployment_id":"other-app-release"}`, 404, "", false},
		{"unknown", "app/redeploy", `{"deployment_id":"unknown"}`, 404, "", false},
		{"no active", "empty/redeploy", `{}`, 409, "", false},
		{"ownership", "foreign-app/redeploy", `{}`, 404, "", false},
		{"invalid", "app/redeploy", `{`, 400, "", false},
		{"invalid no cache", "app/deploy", `{"no_cache":"true"}`, 400, "", false},
		{"no cache", "app/deploy", `{"no_cache":true}`, 202, "", true},
		{"ordinary build", "app/deploy", `{}`, 202, "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest("POST", "/api/apps/"+test.path, strings.NewReader(test.body))
			r.AddCookie(cookie)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("%d: %s", w.Code, w.Body.String())
			}
			if w.Code != 202 {
				return
			}
			var d Record
			if err := json.Unmarshal(w.Body.Bytes(), &d); err != nil {
				t.Fatal(err)
			}
			if d.RedeployOf != test.source || d.NoCache != test.noCache || d.Status != "queued" {
				t.Fatal(d)
			}
			if got := <-a.queues["app"]; got != d.ID {
				t.Fatal("incorrect queued deployment", got)
			}
			if test.source != "" {
				source, _ := a.store.Get(test.source)
				if d.Image != source.Image || d.StaticDir != source.StaticDir || d.Commit != source.Commit {
					t.Fatal("release not copied", d)
				}
			}
		})
	}
	// Redeployment must not depend on the repository remaining configured.
	a.store.Update("project", func(r *Record) { r.GitURL = "" })
	r := httptest.NewRequest("POST", "/api/apps/app/redeploy", strings.NewReader(`{}`))
	r.AddCookie(cookie)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, id := range []string{"active", "missing-image", "static", "missing-static"} {
		r := httptest.NewRequest("GET", "/api/deployments/"+id, nil)
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var result apiRecord
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		want := id == "active" || id == "static"
		if result.Redeployable != want {
			t.Fatal(id, result)
		}
	}
}

func TestStaticActivationPreservesSharedArtifact(t *testing.T) {
	a := testServer(t)
	dir := t.TempDir()
	app := Record{ID: "app", Kind: "apps", Active: "old"}
	for _, r := range []Record{app, {ID: "old", Kind: "deployments", Status: "running", StaticDir: dir}, {ID: "new", Kind: "deployments", StaticDir: dir, RedeployOf: "old"}} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := a.activate(app, "new"); err != nil {
		t.Fatal(err)
	}
	if err := releaseAvailable(Record{StaticDir: dir}); err != nil {
		t.Fatal(err)
	}
	old, _ := a.store.Get("old")
	current, _ := a.store.Get("app")
	if old.Status != "superseded" || current.Active != "new" {
		t.Fatal(old, current)
	}
}

func TestFailedRedeployPreservesActiveRelease(t *testing.T) {
	a := testServer(t)
	dir := t.TempDir()
	log := filepath.Join(dir, "commands")
	script := "#!/bin/sh\necho \"$*\" >> \"$DOCKER_CALLS\"\ncase \"$1\" in create) echo 'start failed' >&2; exit 1;; *) exit 0;; esac\n"
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("DOCKER_CALLS", log)
	for _, r := range []Record{
		{ID: "app", Kind: "apps", Active: "old", ProjectID: "project", TestCommand: "should not run"},
		{ID: "old", Kind: "deployments", Status: "running", Image: "image"},
		{ID: "new", Kind: "deployments", Status: "queued", Image: "image", RedeployOf: "old"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	a.build("app", "new")
	app, _ := a.store.Get("app")
	next, _ := a.store.Get("new")
	if app.Active != "old" || next.Status != "failed" || !strings.Contains(next.Log, "start failed") {
		t.Fatal(app, next)
	}
	calls, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(calls), "buildx") || strings.Contains(string(calls), "should not run") {
		t.Fatal(string(calls))
	}
}
