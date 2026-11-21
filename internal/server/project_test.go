package server

import (
	"context"
	"encoding/json"
	"github.com/sammwyy/startship/internal/pipeline"
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

func TestPipelineCandidateDefaults(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"scripts":{"start":"node app.js"}}`), 0600)
	for _, strategy := range []string{"auto", "node"} {
		candidates, err := projectCandidates(dir, Record{RootDir: ".", BuildType: strategy, TestCommand: "echo ok", Port: 9000})
		if err != nil || len(candidates) != 1 {
			t.Fatal(candidates, err)
		}
		p := candidates[0].Pipeline
		if p.Deploy.Port != 9000 || len(p.Test.Commands) != 1 || p.Test.Commands[0] != "echo ok" {
			t.Fatal(p)
		}
	}
	overlay := &pipeline.Pipeline{Build: pipeline.Build{Engine: "nixpacks", Image: "ignored"}, Test: pipeline.Test{Commands: []string{"echo overlay"}, ExpectOutput: "overlay"}, Deploy: pipeline.Deploy{Port: 9090, HealthPath: "/ready", ExpectStatus: 204, HealthTimeout: 90}}
	candidates, err := projectCandidates(dir, Record{RootDir: ".", BuildType: "auto", Pipeline: overlay})
	if err != nil {
		t.Fatal(err)
	}
	p := candidates[0].Pipeline
	if p.Build.Engine != "nixpacks" || p.Build.Image != "node:22-alpine" || p.Test.ExpectOutput != "overlay" || p.Deploy.Port != 9090 || p.Deploy.ExpectStatus != 204 || p.Deploy.HealthTimeout != 90 {
		t.Fatal(p)
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

func TestExternalEngineIgnoresDockerfilePort(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "source")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "ignored"), []byte("EXPOSE 9999\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"nixpacks", "buildpacks"} {
		p := &pipeline.Pipeline{Build: pipeline.Build{Engine: engine, Dockerfile: "../ignored"}}
		candidate, err := resolvedCandidate(dir, ".", "dockerfile", Record{BuildType: "dockerfile", Pipeline: p})
		if err != nil || candidate.Port != 0 {
			t.Fatal(candidate, err)
		}
	}
}

func TestAutoEngineUsesEngineRuntime(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module engine-test\n\ngo 1.26\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, engine := range []string{"nixpacks", "buildpacks"} {
		p := &pipeline.Pipeline{Build: pipeline.Build{Engine: engine}, Test: pipeline.Test{Commands: []string{"echo test"}}}
		candidates, err := projectCandidates(dir, Record{RootDir: ".", BuildType: "auto", Pipeline: p})
		if err != nil || len(candidates) != 1 {
			t.Fatal(candidates, err)
		}
		resolved := candidates[0].Pipeline
		if resolved.Deploy.Image != "" || resolved.Deploy.Command != "" || resolved.Deploy.Artifact != "" || resolved.Deploy.Static || resolved.Deploy.Port != 8080 || len(resolved.Test.Commands) != 1 {
			t.Fatal("engine runtime replaced by Docker template defaults", resolved)
		}
		p.Deploy = pipeline.Deploy{Image: "alpine:3.23", Artifact: "dist", Command: "custom --serve", Port: 9090, HealthPath: "/health", ExpectStatus: 204, HealthTimeout: 15}
		candidates, err = projectCandidates(dir, Record{RootDir: ".", BuildType: "auto", Pipeline: p})
		if err != nil || len(candidates) != 1 {
			t.Fatal(candidates, err)
		}
		if candidates[0].Pipeline.Deploy != p.Deploy {
			t.Fatal("lost project Deploy overrides", candidates[0].Pipeline)
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
	p := Record{ID: "project", Kind: "projects", Owner: uid, WorkspaceID: a.store.List("workspaces", uid)[0].ID, Name: "Project", GitURL: "file:///repo", GitToken: "private-token", Branch: "main", Env: map[string]string{"MODE": "snapshot"}}
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
	templates := request("GET", "pipeline/templates", "", 200)
	var registry []pipeline.Template
	if err := json.Unmarshal(templates, &registry); err != nil || len(registry) != len(pipeline.Templates()) {
		t.Fatal(string(templates), err)
	}
	request("PATCH", "projects/project", `{"build_type":"custom"}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"engine":"unknown","image":"alpine:3.23"}}}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"bad image"}}}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"alpine:3.23"},"deploy":{"health_timeout":601}}}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"alpine:3.23"},"deploy":{"health_timeout":-1}}}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"alpine:3.23"},"deploy":{"artifact":"../escape"}}}`, 400)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"alpine:3.23"},"test":{"expect_output":"pipeline-actual"}}}`, 200)
	request("PATCH", "projects/project", `{"pipeline":{"build":{"image":"alpine:3.23"}}}`, 200)
	request("PATCH", "projects/project", `{"name":"Project renamed"}`, 200)
	var replaced Record
	if err := json.Unmarshal(request("GET", "projects/project", "", 200), &replaced); err != nil {
		t.Fatal(err)
	}
	if replaced.Pipeline == nil || replaced.Pipeline.Test.ExpectOutput != "" || replaced.Pipeline.Build.Image != "alpine:3.23" || replaced.Env["MODE"] != "snapshot" {
		t.Fatal("pipeline was not replaced or omitted project fields changed", replaced)
	}
	request("PATCH", "projects/project", `{"build_type":"custom","pipeline":{"build":{"engine":"nixpacks","image":"alpine:3.23"},"test":{"commands":["echo ok"],"expect_exit_code":0},"deploy":{"port":8080,"health_path":"/health","health_timeout":90}}}`, 200)
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
	if snapshot.Pipeline == nil || snapshot.Pipeline.Build.Engine != "nixpacks" || snapshot.Pipeline.Deploy.HealthPath != "/health" || snapshot.Pipeline.Deploy.HealthTimeout != 90 {
		t.Fatal(snapshot)
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
	request("POST", "projects/project/deploy", "{}", 202)
}

func TestProjectEngineAvailabilityAPI(t *testing.T) {
	old := engineHost
	t.Cleanup(func() { engineHost = old })
	engineHost = struct{ OS, Arch string }{"linux", "arm64"}
	a := testServer(t)
	uid, _ := a.createUserPending("admin", "password123", false)
	ws := a.store.List("workspaces", uid)[0].ID
	p := Record{ID: "project", Kind: "projects", Owner: uid, WorkspaceID: ws, Name: "Project"}
	a.store.Save(p)
	call := func(method, path, body string, status int) string {
		t.Helper()
		r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(body))
		r = r.WithContext(context.WithValue(r.Context(), socketContextKey{}, true))
		w := httptest.NewRecorder()
		a.api().ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body.String())
		}
		return w.Body.String()
	}
	body := `{"pipeline":{"build":{"engine":"buildpacks"}}}`
	if got := call("PATCH", "projects/project", body, 400); !strings.Contains(got, engineUnavailable("buildpacks")) {
		t.Fatal(got)
	}
	call("POST", "projects", `{"name":"New","workspace_id":"`+ws+`","pipeline":{"build":{"engine":"buildpacks"}}}`, 400)
	call("PATCH", "projects/project", `{"pipeline":{"build":{"engine":"nixpacks"}}}`, 200)
	a.store.Update(p.ID, func(r *Record) { r.Pipeline = &pipeline.Pipeline{Build: pipeline.Build{Engine: "buildpacks"}} })
	call("PATCH", "projects/project", `{"env":{"MODE":"updated"}}`, 200)
	call("PATCH", "projects/project", body, 200)
	call("PATCH", "projects/project", `{"pipeline":{"build":{"engine":"docker","image":"alpine:3.23"}}}`, 200)
	engineHost = struct{ OS, Arch string }{"linux", "amd64"}
	call("PATCH", "projects/project", body, 200)
}
