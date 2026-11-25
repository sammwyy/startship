package server

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPreviewHost(t *testing.T) {
	for _, tc := range []struct{ branch, host, want string }{
		{"Feature/X", "demo.example.com", "feature-x--demo.example.com"},
		{"Fix", "localhost", "fix.localhost"},
		{"!!!", "demo.localhost", "branch--demo.localhost"},
		{strings.Repeat("x", 60), "a.b", strings.Repeat("x", 40) + "--a.b"},
	} {
		if got := previewHost(previewSlug(tc.branch), tc.host); got != tc.want {
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
		{ID: "preview", Kind: "previews", ProjectID: "p", Branch: "feature", HostSlug: "feature", Active: "preview-release"},
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

func TestPreviewSlugCollisions(t *testing.T) {
	for _, branches := range [][2]string{{"feature/x", "feature-x"}, {strings.Repeat("x", 50) + "a", strings.Repeat("x", 50) + "b"}} {
		t.Run(branches[1], func(t *testing.T) {
			a := testServer(t)
			project := Record{ID: "p", Kind: "projects", WorkspaceID: "ws"}
			a.store.Save(project)
			first, err := a.previewForBranch(project, branches[0])
			if err != nil {
				t.Fatal(err)
			}
			second, err := a.previewForBranch(project, branches[1])
			if err != nil {
				t.Fatal(err)
			}
			if first.HostSlug == second.HostSlug || second.HostSlug != previewSlug(branches[1])+"-"+shortHash(branches[1]) {
				t.Fatal(first, second)
			}
			again, _ := a.previewForBranch(project, branches[0])
			if again.ID != first.ID || again.HostSlug != first.HostSlug {
				t.Fatal(again)
			}
			a.store.Save(Record{ID: "app", Kind: "apps", ProjectID: "p"})
			host := strings.Repeat("a", 63) + ".example.com"
			a.store.Save(Record{ID: "route", Kind: "routes", ProjectID: "p", AppID: "app", Host: host, PathPrefix: "/"})
			a.store.DB.Exec("INSERT INTO workspace_domains VALUES(?,?,?,?,?,?,?)", "domain", "ws", "example.com", false, true, "now", "owner")
			hosts := map[string]bool{}
			for _, p := range []Record{first, second} {
				d := Record{ID: p.ID + "-deploy", Kind: "deployments", AppID: "app", Status: "running", StaticDir: writePreviewStatic(t, p.Branch)}
				release := Record{ID: p.ID + "-release", Kind: "releases", Status: "running", ContainerReleases: []string{d.ID}}
				p.Active = release.ID
				for _, r := range []Record{p, d, release} {
					a.store.Save(r)
				}
				address := previewHost(p.HostSlug, host)
				label, _, _ := strings.Cut(address, ".")
				if len(label) > 63 || hosts[address] {
					t.Fatal(address)
				}
				hosts[address] = true
			}
			a.refresh()
			for _, p := range []Record{first, second} {
				w := httptest.NewRecorder()
				a.ServeHTTP(w, httptest.NewRequest("GET", "http://"+previewHost(p.HostSlug, host)+"/", nil))
				if w.Code != 200 || strings.TrimSpace(w.Body.String()) != p.Branch {
					t.Fatal(w.Code, w.Body.String())
				}
			}
		})
	}
}

func TestPreviewTLS(t *testing.T) {
	t.Setenv("STARTSHIP_ACME_EMAIL", "")
	t.Setenv("STARTSHIP_HTTP_ADDR", "127.0.0.1:18081")
	t.Setenv("STARTSHIP_HTTPS_ADDR", "127.0.0.1:18443")
	a := testServer(t)
	for _, r := range []Record{
		{ID: "p", Kind: "projects", WorkspaceID: "ws"},
		{ID: "app", Kind: "apps", ProjectID: "p"},
		{ID: "route", Kind: "routes", ProjectID: "p", AppID: "app", Host: "demo.localhost", PathPrefix: "/", ForceHTTPS: true},
		{ID: "preview", Kind: "previews", ProjectID: "p", Branch: "feature", HostSlug: "feature", Active: "release"},
		{ID: "release", Kind: "releases", Status: "running", ContainerReleases: []string{"deploy"}},
		{ID: "deploy", Kind: "deployments", AppID: "app", Status: "running", StaticDir: writePreviewStatic(t, "preview")},
	} {
		a.store.Save(r)
	}
	a.store.DB.Exec("INSERT INTO workspace_domains VALUES(?,?,?,?,?,?,?)", "domain", "ws", "localhost", false, true, "now", "owner")
	assertRoute := func(scheme string, code int) {
		t.Helper()
		a.refresh()
		p, _ := a.store.Get("preview")
		port := "18081"
		if scheme == "https" {
			port = "18443"
		}
		if len(p.Addresses) != 1 || p.Addresses[0] != scheme+"://feature--demo.localhost:"+port+"/" {
			t.Fatal(p.Addresses)
		}
		w := httptest.NewRecorder()
		a.ServeHTTP(w, httptest.NewRequest("GET", "http://feature--demo.localhost/", nil))
		if w.Code != code {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	assertRoute("http", 200)
	cert, key := testPEM(t, []string{"*.localhost"}, time.Now().Add(-time.Hour), time.Now().Add(time.Hour))
	c, err := parseCertificate(cert, key)
	if err != nil {
		t.Fatal(err)
	}
	c.ID, c.WorkspaceID, c.Source = "wildcard", "ws", "upload"
	a.saveCertificate(c)
	assertRoute("https", 301)
	if pair, err := a.tlsCertificate(&tls.ClientHelloInfo{ServerName: "feature--demo.localhost"}); err != nil || len(pair.Certificate) == 0 {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	a.ServeHTTP(w, httptest.NewRequest("GET", "https://feature--demo.localhost/", nil))
	if w.Code != 200 || strings.TrimSpace(w.Body.String()) != "preview" {
		t.Fatal(w.Code, w.Body.String())
	}
	c.NotAfter = time.Now().Add(-time.Second)
	a.saveCertificate(c)
	assertRoute("http", 200)
	id := "preview:preview:feature--demo.localhost"
	a.issuing.Store(id, true)
	a.ensurePreviewCertificate("preview", "ws", "feature--demo.localhost")
	owned, err := a.certificate(id)
	if err != nil || owned.PreviewID != "preview" || owned.Status != "pending" || !owned.AutoRenew {
		t.Fatal(owned, err)
	}
	a.ensurePreviewCertificate("preview", "ws", "feature--demo.localhost")
	if len(a.certificates("ws")) != 2 {
		t.Fatal(a.certificates("ws"))
	}
	p, _ := a.store.Get("preview")
	if err := a.removePreview(p); err != nil {
		t.Fatal(err)
	}
	a.ensurePreviewCertificate("preview", "ws", "feature--demo.localhost")
	if _, err := a.certificate(id); err == nil {
		t.Fatal("preview certificate retained or recreated")
	}
	if _, err := a.certificate("wildcard"); err != nil {
		t.Fatal("shared wildcard removed", err)
	}
}

func TestCapturedPreviewWebhooks(t *testing.T) {
	for _, tc := range []struct {
		name, file, event, variant, branch string
		code, remaining, queued            int
	}{
		{"new branch", "push", "push", "", "master", 202, 1, 1},
		{"regular push", "push", "push", "regular", "master", 202, 1, 1},
		{"branch deleted", "push-tag-delete", "push", "branch", "simple-tag", 200, 0, 0},
		{"tag deleted", "push-tag-delete", "push", "", "simple-tag", 200, 1, 0},
		{"PR closed", "pull_request", "pull_request", "", "changes", 200, 0, 0},
		{"PR merged", "pull_request", "pull_request", "merged", "changes", 200, 0, 0},
		{"ping", "ping", "ping", "", "feature", 200, 1, 0},
		{"delete branch event", "delete", "delete", "branch", "simple-tag", 200, 0, 0},
		{"delete tag event", "delete", "delete", "", "simple-tag", 200, 1, 0},
		{"create event ignored until push", "create", "create", "branch", "simple-tag", 200, 1, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := testServer(t)
			p := Record{ID: "p", Kind: "projects", Branch: "main", GitURL: "file:///fixture", AutoDeploy: true, Previews: true, WebhookSecret: "secret", Active: "live"}
			a.store.Save(p)
			a.store.Save(Record{ID: "preview", Kind: "previews", ProjectID: p.ID, Branch: tc.branch, HostSlug: previewSlug(tc.branch)})
			q := make(chan string, 10)
			a.queues["project:p"] = q
			body, err := os.ReadFile(filepath.Join("testdata", "webhooks", tc.file+".json"))
			if err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err = json.Unmarshal(body, &payload); err != nil {
				t.Fatal(err)
			}
			switch tc.variant {
			case "regular":
				payload["created"] = false
				payload["before"] = "0123456789abcdef0123456789abcdef01234567"
			case "branch":
				if tc.event == "push" {
					payload["ref"] = "refs/heads/simple-tag"
				} else {
					payload["ref_type"] = "branch"
				}
			case "merged":
				payload["pull_request"].(map[string]any)["merged"] = true
			}
			body, _ = json.Marshal(payload)
			mac := hmac.New(sha256.New, []byte("secret"))
			mac.Write(body)
			req := httptest.NewRequest("POST", "/", strings.NewReader(string(body)))
			req.Header.Set("X-GitHub-Event", tc.event)
			req.Header.Set("X-Hub-Signature-256", "sha256="+hex.EncodeToString(mac.Sum(nil)))
			w := httptest.NewRecorder()
			a.webhook(w, req, p.ID)
			if w.Code != tc.code || len(a.previews(p.ID)) != tc.remaining || len(q) != tc.queued {
				t.Fatal(w.Code, w.Body.String(), len(a.previews(p.ID)), len(q))
			}
			if len(q) > 0 {
				job, _ := a.store.Get(<-q)
				if job.PreviewID != "preview" || job.Branch != tc.branch {
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

func TestPreviewIssuanceFailureAndCancellation(t *testing.T) {
	for _, cancelIssuance := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelIssuance), func(t *testing.T) {
			started := make(chan struct{}, 1)
			ca := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case started <- struct{}{}:
				default:
				}
				if cancelIssuance {
					<-r.Context().Done()
					return
				}
				http.Error(w, "CA rejected request", http.StatusBadRequest)
			}))
			defer ca.Close()
			t.Setenv("STARTSHIP_ACME_EMAIL", "demo@example.com")
			t.Setenv("STARTSHIP_ACME_DIRECTORY", ca.URL)
			a := testServer(t)
			p := Record{ID: "preview", Kind: "previews", ProjectID: "p", Active: "release"}
			a.store.Save(p)
			a.ensurePreviewCertificate(p.ID, "ws", "preview.example.com")
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("issuance did not reach CA")
			}
			id := "preview:preview:preview.example.com"
			if cancelIssuance {
				if err := a.removePreview(p); err != nil {
					t.Fatal(err)
				}
			}
			deadline := time.Now().Add(2 * time.Second)
			for {
				if _, busy := a.issuing.Load(id); !busy {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("issuance did not finish")
				}
				time.Sleep(time.Millisecond)
			}
			c, err := a.certificate(id)
			if cancelIssuance {
				if err == nil {
					t.Fatal("removed certificate recreated", c)
				}
			} else if err != nil || c.Status != "failed" || c.LastError == "" {
				t.Fatal(c, err)
			}
		})
	}
}
