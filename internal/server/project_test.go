package server

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestProjectContainerDetection(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		roots []string
	}{
		{"single", map[string]string{"index.html": "root", "assets/index.html": "nested"}, []string{"."}},
		{"monorepo", map[string]string{"package.json": `{"private":true,"workspaces":["packages/*"]}`, "packages/web/package.json": `{"scripts":{"start":"node server.js"}}`, "packages/api/Dockerfile": "FROM scratch\nEXPOSE 7070", "node_modules/other/index.html": "ignored"}, []string{"packages/api", "packages/web"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				file := filepath.Join(dir, name)
				os.MkdirAll(filepath.Dir(file), 0700)
				os.WriteFile(file, []byte(body), 0600)
			}
			found, err := projectCandidates(dir, Record{RootDir: ".", BuildType: "auto"})
			if err != nil || len(found) != len(tc.roots) {
				t.Fatal(found, err)
			}
			for i, root := range tc.roots {
				if found[i].RootDir != root {
					t.Fatal(found)
				}
				if root == "packages/api" && found[i].Port != 7070 {
					t.Fatal(found)
				}
			}
		})
	}
	dir := t.TempDir()
	if _, err := projectCandidates(dir, Record{RootDir: "."}); err == nil {
		t.Fatal("empty repository must not deploy successfully")
	}
	os.Symlink(t.TempDir(), filepath.Join(dir, "outside"))
	if _, err := projectCandidates(dir, Record{RootDir: "outside"}); err == nil {
		t.Fatal("root escapes checkout")
	}
}

func TestProjectConfigMigration(t *testing.T) {
	a := testServer(t)
	for _, r := range []Record{
		{ID: "p", Kind: "projects", Owner: "owner"},
		{ID: "a", Kind: "apps", Owner: "owner", ProjectID: "p", RootDir: "frontend", Env: map[string]string{"MODE": "web"}, AutoDeploy: true, Active: "old-a"},
		{ID: "b", Kind: "apps", Owner: "owner", ProjectID: "p", RootDir: "backend", Env: map[string]string{"MODE": "api"}, Active: "old-b"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	if err := migrateProjectConfig(a.store); err != nil {
		t.Fatal(err)
	}
	p, _ := a.store.Get("p")
	before, _ := a.store.Get("a")
	if !p.ConfigConflict || !p.AutoDeploy || p.ConfigVersion != 1 || p.RootDir != "." || before.Active != "old-a" {
		t.Fatal(p, before)
	}
	if _, err := a.enqueueProject(p, Record{}); err == nil {
		t.Fatal("conflicting configuration must require confirmation")
	}
	a.store.Update("p", func(r *Record) { r.Env = map[string]string{"CHOSEN": "yes"}; r.ConfigConflict = false })
	if err := migrateProjectConfig(a.store); err != nil {
		t.Fatal(err)
	}
	p, _ = a.store.Get("p")
	if p.Env["CHOSEN"] != "yes" || p.ConfigConflict {
		t.Fatal("migration overwrote an explicit choice")
	}
}

func commitProjectFixture(t *testing.T, repo string) {
	t.Helper()
	for _, args := range [][]string{{"add", "."}, {"-c", "user.name=Test", "-c", "user.email=test@localhost", "commit", "-m", "fixture"}} {
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatal(string(out), err)
		}
	}
}

func TestProjectDeploymentSwitchesContainersTogether(t *testing.T) {
	a := testServer(t)
	repo := t.TempDir()
	exec.Command("git", "-C", repo, "init", "-b", "main").Run()
	for _, root := range []string{"a-web", "b-api"} {
		os.MkdirAll(filepath.Join(repo, root), 0700)
		os.WriteFile(filepath.Join(repo, root, "index.html"), []byte("old "+root), 0600)
	}
	commitProjectFixture(t, repo)
	p := Record{ID: randomID(), Kind: "projects", Owner: "owner", GitURL: "file://" + repo, Branch: "main", RootDir: ".", BuildType: "auto", Env: map[string]string{"MODE": "shared"}}
	a.store.Save(p)
	a.queues["project:"+p.ID] = make(chan string, 10)
	t.Cleanup(func() { command("", nil, "docker", "network", "rm", "startship-"+p.ID) })
	deploy := func(options Record) Record {
		t.Helper()
		current, _ := a.store.Get(p.ID)
		d, err := a.enqueueProject(current, options)
		if err != nil {
			t.Fatal(err)
		}
		a.buildProject(d.ID)
		d, _ = a.store.Get(d.ID)
		return d
	}
	first := deploy(Record{})
	if first.Status != "running" || len(first.ContainerReleases) != 2 || strings.Count(first.Log, "Cloning into") != 1 {
		t.Fatal(first.Status, first.Log)
	}
	ids := map[string]string{}
	for _, id := range first.ContainerReleases {
		d, _ := a.store.Get(id)
		c, _ := a.store.Get(d.AppID)
		ids[c.RootDir] = c.ID
		if d.ReleaseID != first.ID || d.Status != "running" || c.Env["MODE"] != "shared" {
			t.Fatal(d, c)
		}
	}
	a.store.Save(Record{ID: "route", Kind: "routes", Owner: p.Owner, ProjectID: p.ID, AppID: ids["a-web"], Host: "project.local", PathPrefix: "/"})
	a.refresh()
	response := func() string {
		w := httptest.NewRecorder()
		r := httptest.NewRequest("GET", "http://project.local/", nil)
		a.ServeHTTP(w, r)
		return w.Body.String()
	}
	if response() != "old a-web" {
		t.Fatal(response())
	}
	os.WriteFile(filepath.Join(repo, "a-web", "index.html"), []byte("new a-web"), 0600)
	os.Symlink("/etc/passwd", filepath.Join(repo, "b-api", "unsafe"))
	commitProjectFixture(t, repo)
	failed := deploy(Record{})
	current, _ := a.store.Get(p.ID)
	if failed.Status != "failed" || current.Active != first.ID || response() != "old a-web" {
		t.Fatal("partially built project replaced healthy containers", failed.Log)
	}
	for _, id := range failed.ContainerReleases {
		d, _ := a.store.Get(id)
		if d.Status != "failed" {
			t.Fatal(d)
		}
	}
	os.Remove(filepath.Join(repo, "b-api", "unsafe"))
	commitProjectFixture(t, repo)
	second := deploy(Record{})
	if second.Status != "running" || response() != "new a-web" {
		t.Fatal(second.Log, response())
	}
	previous, _ := a.store.Get(first.ID)
	if previous.Status != "superseded" {
		t.Fatal(previous.Status)
	}
	for _, id := range second.ContainerReleases {
		d, _ := a.store.Get(id)
		if ids[d.RootDir] != d.AppID {
			t.Fatal("routing target changed", d)
		}
	}
	a.store.Update(p.ID, func(r *Record) { r.Env = map[string]string{"MODE": "current"}; r.TestCommand = "exit 1" })
	rollback := deploy(Record{RedeployOf: first.ID})
	if rollback.Status != "running" || response() != "old a-web" || strings.Contains(rollback.Log, "Cloning into") {
		t.Fatal(rollback.Log, response())
	}
	for _, id := range rollback.ContainerReleases {
		d, _ := a.store.Get(id)
		c, _ := a.store.Get(d.AppID)
		if c.Env["MODE"] != "current" || d.RedeployOf == "" {
			t.Fatal(c, d)
		}
	}
}

func TestProjectDeploymentAPI(t *testing.T) {
	a := testServer(t)
	uid, _ := a.createUser("project-owner", "password123")
	p := Record{ID: "project", Kind: "projects", Owner: uid, WorkspaceID: a.store.List("workspaces", uid)[0].ID, Name: "Project", GitURL: "file:///repo", GitToken: "private-token", Branch: "main", Env: map[string]string{"MODE": "snapshot"}, ConfigVersion: 1}
	a.store.Save(p)
	a.queues["project:"+p.ID] = make(chan string, 10)
	a.store.Save(Record{ID: "container", Kind: "apps", Owner: uid, ProjectID: p.ID, ReleaseID: "release", Name: "web"})
	a.store.Save(Record{ID: "foreign", Kind: "projects", Owner: "other", Name: "Other"})
	a.store.Save(Record{ID: "other-container", Kind: "apps", Owner: uid, ProjectID: "another", Name: "elsewhere"})
	h := a.api()
	login := httptest.NewRecorder()
	h.ServeHTTP(login, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"project-owner","password":"password123"}`)))
	request := func(method, path, body string, status int) []byte {
		t.Helper()
		r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
		r.AddCookie(login.Result().Cookies()[0])
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.Bytes()
	}
	var release Record
	body := request("POST", "projects/project/deploy", `{"no_cache":true}`, 202)
	json.Unmarshal(body, &release)
	if release.ProjectID != p.ID || release.AppID != "" || !release.NoCache || strings.Contains(string(body), "private-token") {
		t.Fatal(string(body))
	}
	a.store.Update(p.ID, func(r *Record) { r.Env["MODE"] = "changed" })
	snapshot, _ := a.store.Get(release.ID)
	if snapshot.Env["MODE"] != "snapshot" {
		t.Fatal("queued configuration changed")
	}
	request("POST", "projects/foreign/deploy", "{}", 404)
	request("POST", "containers", "{}", 405)
	request("PATCH", "containers/container", "{}", 405)
	request("PATCH", "apps/container", `{"port":9000}`, 405)
	request("POST", "apps/container/deploy", `{}`, 405)
	request("GET", "containers/other-container/logs", "", 200)
	request("POST", "routes", `{"project_id":"project","container_id":"other-container","host":"bad.local"}`, 400)
	body = request("POST", "routes", `{"project_id":"project","container_id":"container","host":"good.local"}`, 200)
	if !strings.Contains(string(body), `"container_id":"container"`) {
		t.Fatal(string(body))
	}
	request("GET", "releases?project_id=project&logs=false", "", 200)
	request("POST", "projects/project/redeploy", "{}", 404)
	a.store.Update(p.ID, func(r *Record) { r.ConfigConflict = true })
	request("POST", "projects/project/deploy", "{}", 409)
	request("PATCH", "projects/project", `{"config_confirmed":true}`, 200)
	request("POST", "projects/project/deploy", "{}", 202)
}
