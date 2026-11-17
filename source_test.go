package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectSourceAPIAndWebhook(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("source-owner", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"source-owner","password":"password123"}`)))
	cookie := login.Result().Cookies()[0]
	request := func(method, path string, body any, status int) []byte {
		t.Helper()
		data, _ := json.Marshal(body)
		r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(string(data)))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	workspace := a.store.list("workspaces", uid)[0]
	var project Record
	body := request("POST", "projects", map[string]any{"name": "Monorepo", "workspace_id": workspace.ID, "git_url": "https://example.com/shared.git", "git_token": "test-project-token"}, 200)
	json.Unmarshal(body, &project)
	a.queues["project:"+project.ID] = make(chan string, 10)
	if project.Branch != "main" || project.WebhookSecret == "" || strings.Contains(string(body), "test-project-token") {
		t.Fatal("project source/default branch/webhook or token redaction is incorrect")
	}
	apps := []Record{}
	for _, root := range []string{"frontend", "backend"} {
		var app Record
		json.Unmarshal(request("POST", "apps", map[string]any{"name": root, "project_id": project.ID, "root_dir": root, "auto_deploy": true}, 200), &app)
		if app.GitURL != "" || app.Branch != "" || app.WebhookSecret != "" {
			t.Fatal("new app has an independent source or webhook")
		}
		apps = append(apps, app)
		a.queues[app.ID] = make(chan string, 10) // Inspect queued work without starting Docker builds.
	}
	for key, value := range map[string]string{"git_url": "https://example.com/other.git", "branch": "other", "git_token": "other-token"} {
		request("POST", "apps", map[string]any{"name": "Other", "project_id": project.ID, key: value}, 400)
		request("PATCH", "apps/"+apps[0].ID, map[string]any{key: value}, 400)
	}
	request("POST", "detect", map[string]string{"project_id": project.ID, "git_url": "file:///other"}, 400)
	request("POST", "detect", map[string]string{"project_id": "missing"}, 400)
	foreign := Record{ID: randomID(), Kind: "projects", Owner: "other-owner", GitURL: "file:///private", Branch: "main"}
	a.store.save(foreign)
	request("POST", "apps", map[string]any{"name": "Forbidden", "project_id": foreign.ID}, 400)
	request("POST", "detect", map[string]string{"project_id": foreign.ID}, 400)
	request("PATCH", "projects/"+project.ID, map[string]string{"branch": "release"}, 200)
	for _, app := range apps {
		stored, _ := a.store.get(app.ID)
		source, err := a.projectSource(stored)
		if err != nil || source.Branch != "release" || source.GitURL != project.GitURL || source.GitToken != "test-project-token" {
			t.Fatal("all apps must inherit the latest project source and preserved credential")
		}
	}
	deliver := func(id, secret, branch string, status int) Record {
		t.Helper()
		data := []byte(`{"ref":"refs/heads/` + branch + `"}`)
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(data)
		r := httptest.NewRequest("POST", "/api/webhooks/github/"+id, strings.NewReader(string(data)))
		r.Header.Set("X-GitHub-Event", "push")
		r.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(w.Code, w.Body.String())
		}
		var out Record
		json.Unmarshal(w.Body.Bytes(), &out)
		return out
	}
	deliver(project.ID, "wrong-secret", "release", 401)
	deliver(project.ID, project.WebhookSecret, "main", 200)
	deliver(project.ID, project.WebhookSecret, "release", 200)
	if len(a.queues[apps[0].ID]) != 0 || len(a.queues[apps[1].ID]) != 0 {
		t.Fatal("wrong-branch push queued work")
	}
	request("PATCH", "projects/"+project.ID, map[string]bool{"auto_deploy": true}, 200)
	queued := deliver(project.ID, project.WebhookSecret, "release", 202)
	if queued.ProjectID != project.ID || len(a.queues["project:"+project.ID]) != 1 || len(a.queues[apps[0].ID]) != 0 || len(a.queues[apps[1].ID]) != 0 {
		t.Fatal("one project push must enqueue one project deployment, never separate container deployments")
	}
	request("PATCH", "apps/"+apps[1].ID, map[string]bool{"auto_deploy": false}, 200)
	if queued = deliver(project.ID, project.WebhookSecret, "release", 202); queued.ProjectID != project.ID || len(a.queues["project:"+project.ID]) != 2 {
		t.Fatal("legacy app options must not split project deployments")
	}
	// Old signed webhook URLs also queue the complete project.
	a.store.update(apps[0].ID, func(app *Record) { app.WebhookSecret = "legacy-hook" })
	deliver(apps[0].ID, "legacy-hook", "main", 200)
	deliver(apps[0].ID, "legacy-hook", "release", 202)
}

func TestProjectSourceMigration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "migration.db")
	store, err := openStore(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range []Record{
		{ID: "shared", Kind: "projects", Owner: "owner"},
		{ID: "front", Kind: "apps", Owner: "owner", ProjectID: "shared", GitURL: "https://example.com/repo", GitToken: "test-token", RootDir: "frontend", Active: "active-release", WebhookSecret: "old-hook"},
		{ID: "back", Kind: "apps", Owner: "owner", ProjectID: "shared", GitURL: "https://example.com/repo", GitToken: "test-token", Branch: "main", RootDir: "backend"},
		{ID: "conflict", Kind: "projects", Owner: "owner"},
		{ID: "first", Kind: "apps", Owner: "owner", ProjectID: "conflict", GitURL: "file:///first", Branch: "main"},
		{ID: "second", Kind: "apps", Owner: "owner", ProjectID: "conflict", GitURL: "file:///second", Branch: "main"},
	} {
		if err := store.save(r); err != nil {
			t.Fatal(err)
		}
	}
	store.db.Close()
	store, err = openStore(path) // Exercise the actual startup migration.
	if err != nil {
		t.Fatal(err)
	}
	defer store.db.Close()
	project, _ := store.get("shared")
	app, _ := store.get("front")
	if project.GitURL != "https://example.com/repo" || project.Branch != "main" || project.GitToken != "test-token" || project.WebhookSecret == "" {
		t.Fatal("consistent legacy source was not promoted")
	}
	if app.GitURL != "" || app.Branch != "" || app.GitToken != "" || app.RootDir != "frontend" || app.Active != "active-release" || app.WebhookSecret != "old-hook" {
		t.Fatal("migration changed app runtime/active state or retained an independent source")
	}
	conflict, _ := store.get("conflict")
	first, _ := store.get("first")
	if !conflict.SourceConflict || conflict.GitURL != "" || first.GitURL != "file:///first" {
		t.Fatal("migration must not pick an arbitrary conflicting source")
	}
	a := &Server{store: store}
	if _, err := a.projectSource(first); err == nil {
		t.Fatal("conflicting legacy source must block builds")
	}
	store.update("conflict", func(p *Record) { p.GitURL = "file:///chosen"; p.Branch = "release" })
	if err := store.migrateProjectSources(); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"first", "second"} {
		app, _ := store.get(id)
		source, err := a.projectSource(app)
		if err != nil || source.GitURL != "file:///chosen" || source.Branch != "release" || app.GitURL != "" {
			t.Fatal("explicit project source did not resolve every app")
		}
	}
}

func TestBuildAndDetectionUseProjectSource(t *testing.T) {
	a := testServer(t)
	repo := t.TempDir()
	for _, root := range []string{"frontend", "backend"} {
		os.MkdirAll(filepath.Join(repo, root), 0700)
		os.WriteFile(filepath.Join(repo, root, "index.html"), []byte("from shared project "+root), 0600)
	}
	for _, args := range [][]string{{"init", "-b", "main"}, {"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "-m", "shared project fixture"}} {
		if output, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	uid, err := a.createUser("builder", "password123")
	if err != nil {
		t.Fatal(err)
	}
	source := Record{ID: randomID(), Kind: "projects", Owner: uid, GitURL: "file://" + repo, Branch: "main"}
	a.store.save(source)
	h := a.api()
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"builder","password":"password123"}`)))
	request := httptest.NewRequest("POST", "/api/detect", strings.NewReader(`{"project_id":"`+source.ID+`"}`))
	request.AddCookie(login.Result().Cookies()[0])
	result := httptest.NewRecorder()
	h.ServeHTTP(result, request)
	var found []Candidate
	json.Unmarshal(result.Body.Bytes(), &found)
	if result.Code != http.StatusOK || len(found) != 2 {
		t.Fatal(result.Code, result.Body.String())
	}
	for _, root := range []string{"frontend", "backend"} {
		app := Record{ID: randomID(), Kind: "apps", Owner: uid, ProjectID: source.ID, RootDir: root, BuildType: "static", GitURL: "file:///must-not-be-used", Branch: "must-not-be-used"}
		deployment := Record{ID: randomID(), Kind: "deployments", Owner: uid, AppID: app.ID}
		a.store.save(app)
		a.store.save(deployment)
		a.build(app.ID, deployment.ID)
		built, _ := a.store.get(deployment.ID)
		content, err := os.ReadFile(filepath.Join(built.StaticDir, "index.html"))
		if built.Status != "running" || err != nil || string(content) != "from shared project "+root {
			t.Fatal("build did not use the project repository and app root:", built.Status, built.Log, err)
		}
	}
}
