package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sammwyy/startship/internal/migrate"
)

type fakeMigrateDirInfo struct{ name string }

func (f fakeMigrateDirInfo) Name() string       { return f.name }
func (f fakeMigrateDirInfo) Size() int64        { return 0 }
func (f fakeMigrateDirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0755 }
func (f fakeMigrateDirInfo) ModTime() time.Time { return time.Time{} }
func (f fakeMigrateDirInfo) IsDir() bool        { return true }
func (f fakeMigrateDirInfo) Sys() any           { return nil }

type testMigrateHost struct {
	files    map[string]string
	dirs     map[string]bool
	onDocker func(args []string) (string, error)
}

func (m *testMigrateHost) RunDocker(_ context.Context, args ...string) (string, error) {
	if m.onDocker != nil {
		return m.onDocker(args)
	}
	return "", nil
}

func (m *testMigrateHost) ReadFile(path string) ([]byte, error) {
	if s, ok := m.files[path]; ok {
		return []byte(s), nil
	}
	return nil, os.ErrNotExist
}

func (m *testMigrateHost) Stat(path string) (fs.FileInfo, error) {
	if m.dirs[path] {
		return fakeMigrateDirInfo{name: path}, nil
	}
	if _, ok := m.files[path]; ok {
		return fakeMigrateDirInfo{name: path}, nil
	}
	return nil, os.ErrNotExist
}

func (m *testMigrateHost) HomeDir() string { return "/home/test" }

func newDokployTestHost() *testMigrateHost {
	return &testMigrateHost{
		dirs: map[string]bool{"/etc/dokploy": true},
		files: map[string]string{
			"/etc/dokploy/logs/app1/dep1.log": "Build finished ok",
		},
		onDocker: func(args []string) (string, error) {
			switch args[0] {
			case "ps":
				return `{"ID":"1","Names":"dokploy-postgres.1.abc","Image":"postgres:16","State":"running","Labels":"com.docker.swarm.service.name=dokploy-postgres"}`, nil
			case "inspect":
				if len(args) >= 3 && strings.Contains(args[2], ".Config.Env") {
					return `["POSTGRES_USER=dokploy","POSTGRES_DB=dokploy"]`, nil
				}
				return `[]`, nil
			case "exec":
				resp := map[string]any{
					"orgs":         []map[string]any{{"id": "o1", "name": "Acme"}},
					"projects":     []map[string]any{{"projectId": "p1", "name": "Store", "organizationId": "o1", "env": ""}},
					"environments": []map[string]any{{"environmentId": "e1", "name": "production", "projectId": "p1", "env": ""}},
					"applications": []map[string]any{
						{
							"applicationId": "a1", "name": "store-api", "appName": "store-api-xyz",
							"env": "API_SECRET=topsecret\nNODE_ENV=production", "sourceType": "github", "buildType": "dockerfile",
							"owner": "acme", "repository": "store", "branch": "main", "buildPath": "/",
							"dockerfile": "Dockerfile", "autoDeploy": true, "environmentId": "e1",
						},
						{
							"applicationId": "a2", "name": "store-web", "appName": "store-web-xyz",
							"env": "PUBLIC_URL=https://store.test", "sourceType": "github", "buildType": "nixpacks",
							"owner": "acme", "repository": "store-web", "branch": "main", "buildPath": "/",
							"autoDeploy": false, "environmentId": "e1",
						},
					},
					"composes": []any{},
					"postgres": []map[string]any{{
						"id": "pg1", "name": "store-db", "appName": "store-db-123",
						"databaseName": "store", "databaseUser": "storeuser", "databasePassword": "secretpassword123",
						"dockerImage": "postgres:16", "externalPort": 0, "environmentId": "e1",
					}},
					"mysql": []any{}, "mariadb": []any{}, "mongo": []any{}, "redis": []any{},
					"domains": []map[string]any{{
						"host": "api.store.test", "https": true, "port": 8080, "path": "/", "applicationId": "a1",
					}},
					"mounts": []map[string]any{{
						"type": "volume", "volumeName": "store_uploads", "mountPath": "/uploads", "applicationId": "a1",
					}},
					"deployments": []map[string]any{{
						"deploymentId": "d1", "title": "Deploy v1", "status": "done",
						"logPath": "/etc/dokploy/logs/app1/dep1.log", "applicationId": "a1",
						"createdAt": "2026-11-21T10:00:00Z", "finishedAt": "2026-11-21T10:01:00Z",
					}},
				}
				b, _ := json.Marshal(resp)
				return string(b), nil
			case "volume", "run":
				return "ok", nil
			}
			return "", nil
		},
	}
}

func doAPIJSON(t *testing.T, h http.Handler, method, path, cookie string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&buf).Encode(body); err != nil {
			t.Fatal(err)
		}
	}
	req := httptest.NewRequest(method, path, &buf)
	if cookie != "" {
		req.Header.Set("Cookie", cookie)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func loginCookie(t *testing.T, h http.Handler, username, password string) string {
	t.Helper()
	rec := doAPIJSON(t, h, "POST", "/api/login", "", map[string]string{
		"username": username,
		"password": password,
	})
	if rec.Code != 200 {
		t.Fatalf("login failed (%d): %s", rec.Code, rec.Body.String())
	}
	for _, c := range rec.Result().Cookies() {
		if c.Value != "" {
			return c.Name + "=" + c.Value
		}
	}
	t.Fatal("missing session cookie")
	return ""
}

func TestMigrateAPIAdminOnlyAndImportAllAndCherryPick(t *testing.T) {
	a := testServer(t)
	a.migrateHost = newDokployTestHost()
	a.dockerService = func(args ...string) (string, error) { return "ok", nil }
	h := a.api()

	if _, err := a.createUser("admin", "password123"); err != nil {
		t.Fatal(err)
	}
	adminSess := loginCookie(t, h, "admin", "password123")

	if _, err := a.createUser("member", "password123"); err != nil {
		t.Fatal(err)
	}
	userSess := loginCookie(t, h, "member", "password123")

	// Non-admin must be rejected with 403.
	if rec := doAPIJSON(t, h, "GET", "/api/system/migrations/detect", userSess, nil); rec.Code != 403 {
		t.Fatalf("expected 403 for non-admin detect, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec := doAPIJSON(t, h, "GET", "/api/system/migrations/jobs", userSess, nil); rec.Code != 403 {
		t.Fatalf("expected 403 for non-admin jobs, got %d", rec.Code)
	}

	// Admin detect returns redacted Dokploy projects and services.
	rec := doAPIJSON(t, h, "GET", "/api/system/migrations/detect?platform=dokploy", adminSess, nil)
	if rec.Code != 200 {
		t.Fatalf("detect failed (%d): %s", rec.Code, rec.Body.String())
	}
	var detectResp struct {
		Platforms []migrate.DetectedPlatform `json:"platforms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &detectResp); err != nil {
		t.Fatal(err)
	}
	if len(detectResp.Platforms) != 1 || !detectResp.Platforms[0].Detected {
		t.Fatalf("expected detected dokploy, got %+v", detectResp.Platforms)
	}
	if len(detectResp.Platforms[0].Projects) != 2 || len(detectResp.Platforms[0].Services) != 1 {
		t.Fatalf("expected 2 projects and 1 service, got %+v", detectResp.Platforms[0])
	}
	if strings.Contains(rec.Body.String(), "topsecret") || strings.Contains(rec.Body.String(), "secretpassword123") {
		t.Fatalf("detect response leaked secret values: %s", rec.Body.String())
	}

	// Import ALL into a brand-new workspace.
	f := false
	rec = doAPIJSON(t, h, "POST", "/api/system/migrations/import?sync=true", adminSess, migrate.ImportRequest{
		Platform:            migrate.PlatformDokploy,
		Mode:                "all",
		CreateWorkspaceName: "Migrated Dokploy",
		StartServices:       &f,
		DeployProjects:      &f,
	})
	if rec.Code != 200 {
		t.Fatalf("import all failed (%d): %s", rec.Code, rec.Body.String())
	}
	var job migrate.MigrationJob
	if err := json.Unmarshal(rec.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	if job.Status != "completed" || job.TargetWorkspaceID == "" || len(job.Projects) != 2 || len(job.Services) != 1 {
		t.Fatalf("unexpected import all job: %+v", job)
	}

	// Verify project 1 has decrypted env, domain, volume, historical log, and linked service.
	var p1ID string
	for _, pr := range job.Projects {
		if pr.SourceID == "a1" {
			p1ID = pr.ProjectID
			if pr.EnvCount != 2 || pr.VolumesCopied != 1 || pr.DomainsImported != 1 || pr.LogsImported != 1 {
				t.Fatalf("unexpected project 1 migration result: %+v", pr)
			}
		}
	}
	if len(job.Services) != 1 || job.Services[0].AttachedProjects != 2 {
		t.Fatalf("expected service attached to 2 projects in same environment, got %+v", job.Services)
	}
	p1, err := a.store.Get(p1ID)
	if err != nil || p1.Env["API_SECRET"] != "topsecret" {
		t.Fatalf("expected decrypted env on migrated project, got %+v (%v)", p1, err)
	}
	// Verify historical log stored as release.
	var relCount int
	for _, r := range a.store.List("releases", "") {
		if r.ProjectID == p1ID && strings.Contains(r.Log, "Build finished ok") {
			relCount++
		}
	}
	if relCount != 1 {
		t.Fatalf("expected 1 migrated release log, got %d", relCount)
	}

	// Cherry-pick a single project with a custom name into the same workspace.
	rec = doAPIJSON(t, h, "POST", "/api/system/migrations/import?sync=true", adminSess, migrate.ImportRequest{
		Platform:          migrate.PlatformDokploy,
		Mode:              "cherry_pick",
		TargetWorkspaceID: job.TargetWorkspaceID,
		StartServices:     &f,
		DeployProjects:    &f,
		Projects: []migrate.ProjectSelection{{
			SourceID:   "a2",
			TargetName: "Renamed Web Frontend",
		}},
	})
	if rec.Code != 200 {
		t.Fatalf("cherry-pick import failed (%d): %s", rec.Code, rec.Body.String())
	}
	var cpJob migrate.MigrationJob
	if err := json.Unmarshal(rec.Body.Bytes(), &cpJob); err != nil {
		t.Fatal(err)
	}
	if len(cpJob.Projects) != 1 || cpJob.Projects[0].ProjectName != "Renamed Web Frontend" {
		t.Fatalf("unexpected cherry-pick result: %+v", cpJob)
	}

	// Jobs history lists both jobs.
	rec = doAPIJSON(t, h, "GET", "/api/system/migrations/jobs", adminSess, nil)
	if rec.Code != 200 {
		t.Fatalf("jobs list failed: %d", rec.Code)
	}
	var jobsResp struct {
		Jobs []migrate.MigrationJob `json:"jobs"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &jobsResp); err != nil {
		t.Fatal(err)
	}
	if len(jobsResp.Jobs) != 2 {
		t.Fatalf("expected 2 jobs in history, got %d", len(jobsResp.Jobs))
	}
}
