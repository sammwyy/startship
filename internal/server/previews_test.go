package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPreviewHost(t *testing.T) {
	for _, tc := range []struct{ branch, host, want string }{
		{"Feature/X", "demo.example.com", "feature-x--demo.example.com"},
		{"Fix", "localhost", "fix.localhost"},
		{"!!!", "demo.localhost", "branch--demo.localhost"},
		{strings.Repeat("x", 60), "a.b", strings.Repeat("x", 40) + "--a.b"},
	} {
		if got := previewHost(tc.branch, tc.host); got != tc.want {
			t.Fatalf("%q != %q", got, tc.want)
		}
	}
}

func TestPreviewWebhook(t *testing.T) {
	for _, tc := range []struct {
		event, body      string
		code             int
		previews, queued int
	}{
		{"push", `{"ref":"refs/heads/feature"}`, 202, 1, 1},
		{"push", `{"ref":"refs/heads/main"}`, 202, 1, 1},
		{"push", `{"ref":"refs/heads/feature","deleted":true}`, 200, 0, 0},
		{"pull_request", `{"action":"closed","pull_request":{"head":{"ref":"feature"}}}`, 200, 0, 0},
		{"pull_request", `{"action":"opened","pull_request":{"head":{"ref":"feature"}}}`, 200, 1, 0},
		{"push", `{"ref":"refs/tags/feature"}`, 200, 1, 0},
		{"push", `{`, 400, 1, 0},
	} {
		t.Run(tc.body, func(t *testing.T) {
			a := testServer(t)
			p := Record{ID: "p", Kind: "projects", Owner: "owner", Name: "project", GitURL: "file:///fixture", Branch: "main", AutoDeploy: true, Previews: true, WebhookSecret: "secret", Active: "live"}
			a.store.Save(p)
			a.store.Save(Record{ID: "preview", Kind: "previews", ProjectID: p.ID, Parent: p.ID, Owner: p.Owner, Branch: "feature"})
			q := make(chan string, 10)
			a.queues["project:p"] = q
			req := httptest.NewRequest("POST", "/", strings.NewReader(tc.body))
			mac := hmac.New(sha256.New, []byte("secret"))
			mac.Write([]byte(tc.body))
			req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			req.Header.Set("X-GitHub-Event", tc.event)
			w := httptest.NewRecorder()
			a.webhook(w, req, p.ID)
			if w.Code != tc.code || len(a.previews(p.ID)) != tc.previews || len(q) != tc.queued {
				t.Fatal(w.Code, w.Body.String(), len(a.previews(p.ID)), len(q))
			}
			if len(q) > 0 {
				job, _ := a.store.Get(<-q)
				if strings.Contains(tc.body, "feature") && (job.PreviewID != "preview" || job.Branch != "feature") {
					t.Fatal(job)
				}
				if strings.Contains(tc.body, "main") && job.PreviewID != "" {
					t.Fatal(job)
				}
			}
			current, _ := a.store.Get(p.ID)
			if current.Active != "live" {
				t.Fatal(current)
			}
		})
	}
}

func TestPreviewRoutingKeepsLive(t *testing.T) {
	a := testServer(t)
	// Static releases exercise the same target selection without Docker.
	for _, r := range []Record{
		{ID: "p", Kind: "projects", Parent: "ws", WorkspaceID: "ws", Active: "live-release"},
		{ID: "app", Kind: "apps", ProjectID: "p", Active: "live-deploy"},
		{ID: "route", Kind: "routes", ProjectID: "p", AppID: "app", Host: "demo.localhost", PathPrefix: "/"},
		{ID: "preview", Kind: "previews", ProjectID: "p", Branch: "feature", Active: "preview-release"},
		{ID: "preview-release", Kind: "releases", Status: "running", ContainerReleases: []string{"preview-deploy"}},
		{ID: "live-deploy", Kind: "deployments", Status: "running", StaticDir: writePreviewStatic(t, "live")},
		{ID: "preview-deploy", Kind: "deployments", AppID: "app", Status: "running", StaticDir: writePreviewStatic(t, "preview")},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	_, err := a.store.DB.Exec("INSERT INTO workspace_domains VALUES(?,?,?,?,?,?,?)", "domain", "ws", "localhost", false, true, "now", "owner")
	if err != nil {
		t.Fatal(err)
	}
	a.refresh()
	for host, want := range map[string]string{"demo.localhost": "live", "feature--demo.localhost": "preview"} {
		req := httptest.NewRequest("GET", "http://"+host+"/", nil)
		w := httptest.NewRecorder()
		a.ServeHTTP(w, req)
		if w.Code != 200 || strings.TrimSpace(w.Body.String()) != want {
			t.Fatal(host, w.Code, w.Body.String())
		}
	}
}

func writePreviewStatic(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "index.html"), []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestPreviewActivationIsolation(t *testing.T) {
	a := testServer(t)
	for _, r := range []Record{
		{ID: "p", Kind: "projects", Owner: "owner", Active: "live", Sleeping: true},
		{ID: "app", Kind: "apps", ProjectID: "p", Active: "live-container"},
		{ID: "preview", Kind: "previews", ProjectID: "p", Active: "old", Sleeping: true},
		{ID: "old", Kind: "releases", PreviewID: "preview", Status: "running", ContainerReleases: []string{"old-container"}},
		{ID: "old-container", Kind: "deployments", PreviewID: "preview", Status: "running"},
		{ID: "new", Kind: "releases", PreviewID: "preview", ProjectID: "p", Commit: "abc", Status: "building", ContainerReleases: []string{"new-container"}},
		{ID: "new-container", Kind: "deployments", PreviewID: "preview", AppID: "app", Status: "ready"},
	} {
		if err := a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	job, _ := a.store.Get("new")
	if err := a.activatePreview(job, []string{"missing"}); err == nil {
		t.Fatal("expected unavailable container error")
	}
	p, _ := a.store.Get("preview")
	if p.Active != "old" || !p.Sleeping {
		t.Fatal(p)
	}
	if err := a.activatePreview(job, job.ContainerReleases); err != nil {
		t.Fatal(err)
	}
	p, _ = a.store.Get("preview")
	if p.Active != "new" || p.Sleeping || p.Commit != "abc" || p.Status != "running" {
		t.Fatal(p)
	}
	project, _ := a.store.Get("p")
	app, _ := a.store.Get("app")
	if project.Active != "live" || !project.Sleeping || app.Active != "live-container" {
		t.Fatal(project, app)
	}
	old, _ := a.store.Get("old")
	if old.Status != "superseded" {
		t.Fatal(old)
	}
	if err := a.removePreview(p); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"preview", "old", "new", "old-container", "new-container"} {
		if _, err := a.store.Get(id); err == nil {
			t.Fatal("record remains", id)
		}
	}
	if err := a.activatePreview(job, nil); err == nil {
		t.Fatal("deleted preview activated")
	}
	if len(a.previews("p")) != 0 {
		t.Fatal("deleted preview remains")
	}
}

func TestPreviewOptInAndSleepTimeout(t *testing.T) {
	a := testServer(t)
	p := Record{ID: "p", Kind: "projects", Branch: "main", AutoDeploy: true, WebhookSecret: "secret"}
	a.store.Save(p)
	for _, enabled := range []bool{false, true} {
		p.Previews = enabled
		p.AutoDeploy = false
		a.store.Save(p)
		body := `{"ref":"refs/heads/feature"}`
		req := httptest.NewRequest("POST", "/", strings.NewReader(body))
		req.Header.Set("X-GitHub-Event", "push")
		mac := hmac.New(sha256.New, []byte("secret"))
		mac.Write([]byte(body))
		req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
		w := httptest.NewRecorder()
		a.webhook(w, req, p.ID)
		if w.Code != 200 || len(a.previews(p.ID)) != 0 {
			t.Fatal(w.Code, w.Body.String())
		}
		req.Header.Set("X-Hub-Signature-256", "invalid")
		w = httptest.NewRecorder()
		a.webhook(w, req, p.ID)
		if w.Code != 401 {
			t.Fatal(w.Code)
		}
	}
	preview := Record{ProjectID: p.ID}
	if got := a.previewSleepAfter(preview); got != 5 {
		t.Fatal(got)
	}
	a.store.Update(p.ID, func(r *Record) { r.SleepAfter = 12 })
	if got := a.previewSleepAfter(preview); got != 12 {
		t.Fatal(got)
	}
}
