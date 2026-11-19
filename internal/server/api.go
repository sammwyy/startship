package server

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sammwyy/startship/internal/detect"
	"github.com/sammwyy/startship/internal/pipeline"
	"github.com/sammwyy/startship/internal/router"
	"github.com/sammwyy/startship/internal/routing"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	sleep                sleepRuntime
	dockerService        func(...string) (string, error)
	dockerStream         func(io.Writer, ...string) error
	resourcesMu          sync.Mutex
	volumeSizeMu         sync.Mutex
	volumeSizeAt         time.Time
	serviceReservedPorts map[int]bool
	store                *Store
	data                 string
	mu                   sync.Mutex
	queues               map[string]chan string
	routerOnce           sync.Once
	routerRefresh        sync.Mutex
	router               *router.Router
	routerOptions        router.Options
	firewall             firewallRuntime
	network              networkRuntime
	traffic              trafficMetrics
	// https preserves the legacy dashboard HTTP redirect when autocert is enabled.
	dashboardDomain     string
	https               bool
	updates             *updates
	live                *liveHub
	liveSessionInterval time.Duration
	certMu              sync.Mutex
	challenges          sync.Map
	issuing             sync.Map
	legacyCertificate   func(*tls.ClientHelloInfo) (*tls.Certificate, error)
}

// Response-only fields must not be persisted or accepted as app configuration.
type apiRecord struct {
	Record
	URL          string   `json:"url,omitempty"`
	Redeployable bool     `json:"redeployable,omitempty"`
	HasGitToken  bool     `json:"has_git_token,omitempty"`
	Permissions  []string `json:"permissions,omitempty"`
}

func publicRecord(v Record) apiRecord {
	hasToken := v.GitToken != ""
	v.GitToken = ""
	if v.Kind == "apps" {
		v.GitURL, v.Branch = "", ""
	}
	out := apiRecord{Record: v, HasGitToken: v.Kind == "projects" && hasToken}
	if v.Kind == "routes" {
		out.URL = routeURL(v)
		out.ContainerID = v.AppID
	}
	return out
}

func reply(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
func fail(w http.ResponseWriter, status int, msg string) {
	reply(w, status, map[string]string{"error": msg})
}
func decode(w http.ResponseWriter, r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(v)
}
func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}
func (a *Server) createUser(user, pass string) (string, error) {
	return a.createUserPending(user, pass, false)
}

var errUsernameTaken = errors.New("username is already in use")

func (a *Server) createUserPending(user, pass string, pending bool) (string, error) {
	return a.insertUser(user, pass, pending, true)
}

// insertUser creates an account; the first one becomes Platform Admin. A personal workspace is
// only created for accounts that sign themselves up (setup, first-run bootstrap).
func (a *Server) insertUser(user, pass string, pending, personal bool) (string, error) {
	user = strings.TrimSpace(user)
	if len(user) < 1 || len(user) > 100 || len(pass) < 8 {
		return "", fmt.Errorf("username required; password must be at least 8 characters")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if e != nil {
		return "", e
	}
	a.store.Lock()
	defer a.store.Unlock()
	tx, e := a.store.DB.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	id := randomID()
	var existing int
	if e = tx.QueryRow("SELECT count(*) FROM users").Scan(&existing); e != nil {
		return "", e
	}
	if _, e = tx.Exec("INSERT INTO users(id,username,password,must_change_password) VALUES(?,?,?,?)", id, user, string(hash), pending); e != nil {
		return "", e
	}
	if existing == 0 {
		if _, e = a.grant(tx, id, platformAdminRole, scopePlatform, "", "setup"); e != nil {
			return "", e
		}
	}
	var changes []Record
	if personal {
		ws := Record{ID: randomID(), Kind: "workspaces", Owner: id, Name: user + "'s Workspace", Created: time.Now().UTC()}
		b, _ := json.Marshal(ws)
		if _, e = tx.Exec("INSERT INTO records VALUES(?,?,?,?,?)", ws.ID, ws.Kind, id, "", string(b)); e != nil {
			return "", e
		}
		if _, e = a.grant(tx, id, "administrator", scopeWorkspace, ws.ID, "setup"); e != nil {
			return "", e
		}
		changes = append(changes, ws)
	}
	if e := tx.Commit(); e != nil {
		return "", e
	}
	a.store.Notify(changes...)
	return id, nil
}

// resetPassword sets a temporary password that must be replaced at the next sign-in.
func (a *Server) resetPassword(uid, pass string) error {
	if len(pass) < 8 || len(pass) > 72 {
		return fmt.Errorf("password must be between 8 and 72 bytes")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec("UPDATE users SET password=?,must_change_password=1 WHERE id=?", string(hash), uid); err != nil {
		return err
	}
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", uid); err != nil {
		return err
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.publishAccount(uid)
	return nil
}
func (a *Server) user(r *http.Request) string { return a.authenticate(r).userID }

func (a *Server) api() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			dashboard.ServeHTTP(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		// Reject browser cross-origin mutations, including login CSRF.
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				fail(w, 403, "cross-origin request rejected")
				return
			}
		}
		p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if localRequest(r) && len(p) > 1 && (p[1] == "login" || p[1] == "refresh" || p[1] == "logout" || p[1] == "setup" && r.Method != "GET" || p[1] == "me" && r.Method != "GET") {
			fail(w, 400, "this operation requires an HTTP login session, not the local admin socket")
			return
		}
		if len(p) < 2 || p[0] != "api" {
			fail(w, 404, "Startship JSON API")
			return
		}
		if len(p) == 4 && p[1] == "webhooks" && p[2] == "github" {
			a.webhook(w, r, p[3])
			return
		}
		if p[1] == "setup" {
			if r.Method == "GET" {
				var n int
				a.store.DB.QueryRow("SELECT count(*) FROM users").Scan(&n)
				reply(w, 200, map[string]bool{"required": n == 0, "completed": a.setupComplete()})
				return
			}
			if r.Method != "POST" {
				fail(w, 405, "method not allowed")
				return
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			var n int
			a.store.DB.QueryRow("SELECT count(*) FROM users").Scan(&n)
			if n != 0 {
				fail(w, 409, "already configured")
				return
			}
			var v struct{ Username, Password string }
			if decode(w, r, &v) != nil {
				fail(w, 400, "invalid JSON")
				return
			}
			id, e := a.createUser(v.Username, v.Password)
			if e != nil {
				fail(w, 400, e.Error())
				return
			}
			if e := a.markSetupComplete(); e != nil {
				fail(w, 500, "setup failed")
				return
			}
			reply(w, 201, map[string]string{"id": id})
			return
		}
		if len(p) == 2 && r.Method == "POST" {
			switch p[1] {
			case "login":
				a.login(w, r)
				return
			case "refresh":
				a.refreshSession(w, r)
				return
			case "logout":
				a.logout(w, r)
				return
			}
		}
		s := a.authenticate(r)
		uid := s.userID
		if p[1] == "mcp" && len(p) == 2 && s.token == nil {
			w.Header().Set("WWW-Authenticate", `Bearer realm="startship"`)
			fail(w, 401, "MCP requires a personal access token")
			return
		}
		if uid == "" {
			if len(p) == 2 && p[1] == "live" && s.digest != "" &&
				r.Method == http.MethodGet && strings.EqualFold(r.Header.Get("Upgrade"), "websocket") &&
				r.Header.Get("Origin") != "" {
				if refresh, err := r.Cookie(refreshCookie); err != nil || refresh.Value == "" {
					a.liveHandler(w, r, "")
					return
				}
			}
			fail(w, 401, "authentication required")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), sessionKey{}, s))
		if p[1] != "live" {
			a.touchSession(s)
		}
		if s.token != nil {
			if why := tokenDenied(r, p, s.token); why != "" {
				fail(w, 403, why)
				return
			}
		}
		if p[1] == "me" && len(p) == 2 {
			if r.Method == "PATCH" {
				a.updateAccount(w, r, s)
				return
			}
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET, PATCH")
				fail(w, 405, "method not allowed")
				return
			}
			profile := accountProfile{ID: uid}
			var pending bool
			if err := a.store.DB.QueryRow("SELECT username,display_name,email,must_change_password FROM users WHERE id=?", uid).
				Scan(&profile.Username, &profile.DisplayName, &profile.Email, &pending); err != nil {
				fail(w, 500, "account unavailable")
				return
			}
			x := a.accessFor(s)
			var token *tokenInfo
			if s.token != nil {
				token = &tokenInfo{s.token.id, s.token.name}
			}
			reply(w, 200, struct {
				accountProfile
				MustChangePassword bool       `json:"must_change_password"`
				SetupCompleted     bool       `json:"setup_completed"`
				LocalAdmin         bool       `json:"local_admin"`
				PlatformAdmin      bool       `json:"platform_admin"`
				Permissions        []string   `json:"permissions"`
				SessionID          string     `json:"session_id,omitempty"`
				Token              *tokenInfo `json:"token,omitempty"`
			}{profile, pending, a.setupComplete(), s.local, x.admin, x.list("", ""), s.sessionID, token})
			return
		}
		var pending bool
		if err := a.store.DB.QueryRow("SELECT must_change_password FROM users WHERE id=?", uid).Scan(&pending); err != nil {
			fail(w, 500, "account unavailable")
			return
		}
		if pending && !localRequest(r) {
			fail(w, 403, "change your password to complete setup before continuing")
			return
		}
		if p[1] == "live" && len(p) == 2 {
			a.liveHandler(w, r, uid)
			return
		}
		if len(p) >= 3 && p[1] == "me" && p[2] == "sessions" {
			switch {
			case len(p) == 3 && r.Method == "GET":
				items, err := a.accountSessions(uid)
				if err != nil {
					fail(w, 500, "sessions unavailable")
					return
				}
				reply(w, 200, items)
			case len(p) == 4 && r.Method == "DELETE":
				a.endSession(w, r, s, p[3], "session.revoke")
			default:
				fail(w, 405, "method not allowed")
			}
			return
		}
		x := a.accessFor(s)
		if len(p) >= 3 && p[1] == "me" && p[2] == "tokens" {
			a.tokensAPI(w, r, p, uid, x)
			return
		}
		if p[1] == "mcp" && len(p) == 2 {
			a.mcpHandler(w, r, s)
			return
		}
		if len(p) == 3 && p[1] == "pipeline" && p[2] == "templates" && r.Method == "GET" {
			reply(w, 200, pipeline.Templates())
			return
		}
		if a.networkAPI(w, r, p, uid, x) || a.firewallAPI(w, r, p, uid, x) || a.backupsAPI(w, r, p, uid, x) || a.servicesAPI(w, r, p, uid, x) || a.certificatesAPI(w, r, p, uid, x) {
			return
		}
		if p[1] == "detect" && r.Method == "POST" {
			a.detectAPI(w, r, x)
			return
		}
		if a.activityAPI(w, r, p, x) || a.accessAPI(w, r, p, uid, x) || a.systemAPI(w, r, p, uid, x) {
			return
		}
		kind := p[1]
		containers := kind == "containers"
		if containers {
			if r.Method != "GET" {
				fail(w, 405, "containers are detected and managed by their project")
				return
			}
			kind = "apps"
		}
		if kind != "workspaces" && kind != "projects" && kind != "apps" && kind != "routes" && kind != "deployments" && kind != "releases" {
			fail(w, 404, "not found")
			return
		}
		var old Record
		if len(p) > 2 {
			var e error
			old, e = a.store.Get(p[2])
			if e != nil || old.Kind != kind || !x.visible(old) {
				fail(w, 404, "not found")
				return
			}
		}
		if len(p) == 4 && kind == "projects" && p[3] == "traffic" {
			if !x.can(old, "traffic.view") {
				forbidden(w)
				return
			}
			a.projectTraffic(w, r, old)
			return
		}
		if len(p) == 4 && kind == "projects" && (p[3] == "sleep" || p[3] == "wake") && r.Method == "POST" {
			if !x.can(old, "deploy") {
				forbidden(w)
				return
			}
			var err error
			if p[3] == "sleep" {
				err = a.sleepProject(old.ID, false)
			} else {
				err = a.wakeProject(r.Context(), old.ID)
			}
			if err != nil {
				if errors.Is(err, errStaticSleep) {
					fail(w, 400, err.Error())
					return
				}
				fail(w, 503, err.Error())
				return
			}
			workspace, project := x.scope(old)
			a.auditRecord(r.Context(), uid, p[3], old, workspace, project)
			current, _ := a.store.Get(old.ID)
			reply(w, 200, x.publicRecord(current))
			return
		}
		if len(p) == 4 && (kind == "projects" || kind == "apps") && (p[3] == "deploy" || p[3] == "redeploy") && r.Method == "POST" && !x.can(old, "deploy") {
			forbidden(w)
			return
		}
		if len(p) == 4 && kind == "projects" && (p[3] == "deploy" || p[3] == "redeploy") && r.Method == "POST" {
			a.projectDeployAPI(w, r, old, uid, p[3])
			return
		}
		if containers && len(p) == 4 && p[3] == "logs" {
			if !x.can(old, "logs.view") {
				forbidden(w)
				return
			}
			a.containerLogs(w, r, old)
			return
		}
		if kind == "apps" && old.ReleaseID != "" && r.Method != "GET" && r.Method != "DELETE" {
			fail(w, 405, "detected containers are configured and deployed through their project")
			return
		}
		if len(p) == 4 && kind == "apps" && (p[3] == "deploy" || p[3] == "redeploy") && r.Method == "POST" {
			var options struct {
				NoCache      bool   `json:"no_cache"`
				DeploymentID string `json:"deployment_id"`
			}
			if e := json.NewDecoder(r.Body).Decode(&options); e != nil && e != io.EOF {
				fail(w, 400, "invalid deployment options")
				return
			}
			next := Record{NoCache: options.NoCache}
			if p[3] == "redeploy" {
				sourceID := options.DeploymentID
				if sourceID == "" {
					sourceID = old.Active
				}
				if sourceID == "" {
					fail(w, 409, "app has no active release")
					return
				}
				source, e := a.store.Get(sourceID)
				if e != nil || source.Kind != "deployments" || source.AppID != old.ID || source.Owner != old.Owner {
					fail(w, 404, "release not found")
					return
				}
				if source.Status != "running" && source.Status != "superseded" {
					fail(w, 409, "only successful releases can be redeployed")
					return
				}
				if e := releaseAvailable(source); e != nil {
					fail(w, 409, e.Error())
					return
				}
				next = Record{RedeployOf: source.ID, Image: source.Image, StaticDir: source.StaticDir, Commit: source.Commit, Pipeline: source.Pipeline}
			} else if _, e := a.projectSource(old); e != nil {
				fail(w, 400, e.Error())
				return
			}
			d, e := a.enqueueDeployment(old, next)
			if e != nil {
				fail(w, 500, e.Error())
				return
			}
			workspace, project := x.scope(old)
			a.auditRecord(r.Context(), uid, p[3], old, workspace, project)
			reply(w, 202, d)
			return
		}
		if r.Method == "GET" {
			if len(p) > 2 {
				result := x.publicRecord(old)
				if containers {
					result = a.containerRecord(old)
				}
				if old.Kind == "releases" && (old.Status == "running" || old.Status == "superseded") {
					result.Redeployable = a.projectReleaseAvailable(old) == nil
				}
				if old.Kind == "deployments" && (old.Status == "running" || old.Status == "superseded") {
					result.Redeployable = releaseAvailable(old) == nil
				}
				reply(w, 200, result)
				return
			}
			out := []apiRecord{}
			for _, v := range a.store.List(kind, "") {
				if !x.visible(v) {
					continue
				}
				if parent := r.URL.Query().Get("project_id"); parent != "" && v.ProjectID != parent {
					continue
				}
				if parent := r.URL.Query().Get("workspace_id"); parent != "" && v.WorkspaceID != parent {
					continue
				}
				if parent := r.URL.Query().Get("app_id"); parent != "" && v.AppID != parent {
					continue
				}
				if (kind == "deployments" || kind == "releases") && r.URL.Query().Get("logs") == "false" {
					v.Log = ""
				}
				result := x.publicRecord(v)
				if containers {
					result = a.containerRecord(v)
				}
				out = append(out, result)
			}
			reply(w, 200, out)
			return
		}
		if kind == "deployments" || kind == "releases" {
			fail(w, 405, "read only")
			return
		}
		if r.Method == "DELETE" && len(p) == 3 {
			if !x.can(old, map[string]string{"workspaces": "workspace.manage", "projects": "project.delete", "apps": "project.settings", "routes": "domains.manage"}[kind]) {
				forbidden(w)
				return
			}
			for _, k := range []string{"projects", "apps", "routes"} {
				for _, v := range a.store.List(k, old.Owner) {
					if v.Parent == old.ID || (k == "routes" && v.AppID == old.ID) {
						fail(w, 409, "delete children first")
						return
					}
				}
			}
			if old.Kind == "apps" {
				for _, d := range a.store.List("deployments", old.Owner) {
					if d.AppID == old.ID && (d.Status == "queued" || d.Status == "building" || d.Status == "testing" || d.Status == "starting") {
						fail(w, 409, "wait for deployments to finish")
						return
					}
				}
				a.removeActive(old)
				for _, d := range a.store.List("deployments", "") {
					if d.Parent == old.ID {
						a.store.Delete(d)
					}
				}
			}
			if old.Kind == "workspaces" && (len(listResources[Service](a, "services", old.ID)) > 0 || len(listResources[Volume](a, "volumes", old.ID)) > 0) {
				fail(w, 409, "delete workspace services and volumes first")
				return
			}
			workspace, project := x.scope(old)
			if err := a.store.Delete(old); err != nil {
				fail(w, 500, "delete failed")
				return
			}
			a.auditRecord(r.Context(), uid, "delete", old, workspace, project)
			if old.Kind == "workspaces" || old.Kind == "projects" {
				a.removeScopeAccess(strings.TrimSuffix(old.Kind, "s"), old.ID)
			}
			if old.Kind == "projects" {
				a.detachProjectServices(old.ID)
				lock := projectBuildLock(old.ID)
				lock.Lock()
				removeProjectBuilders(old.ID, "", nil)
				lock.Unlock()
				network := "startship-" + old.ID
				if self := selfContainer(); self != "" {
					command("", nil, "docker", "network", "disconnect", network, self)
				}
				command("", nil, "docker", "network", "rm", network)
			}
			a.refresh()
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if r.Method != "POST" && r.Method != "PUT" && r.Method != "PATCH" {
			fail(w, 405, "method not allowed")
			return
		}
		v := old
		body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		fields := map[string]json.RawMessage{}
		if e != nil || json.Unmarshal(body, &fields) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		if _, ok := fields["pipeline"]; ok {
			v.Pipeline = nil
		}
		if json.Unmarshal(body, &v) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		if old.ID != "" && (v.WorkspaceID != old.WorkspaceID || v.ProjectID != old.ProjectID) {
			fail(w, 400, "records cannot be moved to another workspace or project")
			return
		}
		if old.ID != "" && !x.canUpdate(old, fields) {
			forbidden(w)
			return
		}
		if old.ID == "" && kind == "workspaces" && !x.platform("workspaces.create") {
			forbidden(w)
			return
		}
		v.Kind = kind
		v.Owner = old.Owner
		if old.ID == "" {
			v.Owner = uid
		}
		v.Name = strings.TrimSpace(v.Name)
		if v.Name == "" && kind != "routes" {
			fail(w, 400, "name is required")
			return
		}
		if old.ID == "" {
			v.ID = randomID()
			v.Created = time.Now().UTC()
			v.Active = ""
			v.WebhookSecret = ""
		} else {
			v.ID = old.ID
			v.Created = old.Created
			v.Active = old.Active
			v.WebhookSecret = old.WebhookSecret
		}
		switch kind {
		case "projects":
			v.Sleeping = old.Sleeping
			if v.SleepAfter < 0 || v.SleepAfter > 10080 {
				fail(w, 400, "sleep_after must be between 0 and 10080 minutes")
				return
			}
			if v.SleepAfter > 0 && a.staticProject(old) {
				fail(w, 400, errStaticSleep.Error())
				return
			}
			if v.Pipeline != nil {
				if err := v.Pipeline.Validate(); err != nil {
					fail(w, 400, err.Error())
					return
				}
			}
			if v.BuildType == "custom" && v.Pipeline == nil {
				fail(w, 400, "custom requires a pipeline")
				return
			}
			v.Parent = v.WorkspaceID
			if v.RootDir == "" {
				v.RootDir = "."
			}
			if v.BuildType == "" {
				v.BuildType = "auto"
			}
			if !safeRoot(v.RootDir) || v.Port < 0 || v.Port > 65535 || !detect.ValidBuildType(v.BuildType) {
				fail(w, 400, "invalid project configuration")
				return
			}
			if v.GitURL != "" {
				if e := detect.ValidateGit(v.GitURL); e != nil {
					fail(w, 400, e.Error())
					return
				}
				if v.Branch == "" {
					v.Branch = "main"
				}
				if v.WebhookSecret == "" {
					v.WebhookSecret = randomID()
				}
			} else if old.GitURL != "" {
				fail(w, 400, "project source cannot be empty")
				return
			}
		case "apps":
			v.Parent = v.ProjectID
			if v.GitURL != "" || v.GitToken != "" || v.Branch != "" {
				fail(w, 400, "source is configured on the project, not on individual apps")
				return
			}
			if _, e := a.projectSource(v); e != nil {
				fail(w, 400, e.Error())
				return
			}
			if v.RootDir == "" {
				v.RootDir = "."
			}
			if v.BuildType == "" {
				v.BuildType = "auto"
			}
			if v.Port == 0 {
				v.Port = 8080
			}
			if !safeRoot(v.RootDir) || v.Port < 1 || v.Port > 65535 || !detect.ValidBuildType(v.BuildType) {
				fail(w, 400, "invalid app configuration")
				return
			}
		case "routes":
			a.certMu.Lock()
			defer a.certMu.Unlock()
			v.Parent = v.ProjectID
			if v.Port < 0 || v.Port > 65535 {
				fail(w, 400, "container port must be between 1 and 65535, or 0 for the detected port")
				return
			}
			if v.ContainerID != "" {
				v.AppID = v.ContainerID
			}
			v.ContainerID = ""
			v.Host = routing.NormalizeHost(v.Host)
			if v.CertificateID != "" {
				c, err := a.certificate(v.CertificateID)
				if err != nil || c.WorkspaceID != projectWorkspace(x.project(v.ProjectID)) || !c.covers(v.Host) {
					fail(w, 400, "certificate must belong to the workspace and cover the route host")
					return
				}
			}
			v.PathPrefix = routing.NormalizePrefix(v.PathPrefix)
			app, e := a.store.Get(v.AppID)
			if e != nil || app.Kind != "apps" || app.ProjectID != v.ProjectID || v.Host == "" || !strings.HasPrefix(v.PathPrefix, "/") {
				fail(w, 400, "invalid route")
				return
			}
			if old, e := a.store.Get(v.ID); (e != nil || old.Host != v.Host) && !x.admin && !a.routeAllowed(projectWorkspace(x.project(v.ProjectID)), v.Host) {
				fail(w, 403, v.Host+" is not one of this workspace's domains")
				return
			}
			for _, other := range a.store.List("routes", "") {
				if other.ID != v.ID && other.Host == v.Host && other.PathPrefix == v.PathPrefix {
					fail(w, 409, "route already exists")
					return
				}
			}
		}
		if kind != "workspaces" {
			parent, e := a.store.Get(v.Parent)
			expected := "workspaces"
			if kind == "apps" || kind == "routes" {
				expected = "projects"
			}
			if e != nil || parent.Kind != expected || !x.visible(parent) {
				fail(w, 400, "invalid parent")
				return
			}
			create := map[string]string{"projects": "projects.create", "apps": "project.settings", "routes": "domains.manage"}[kind]
			if old.ID == "" && !x.can(parent, create) {
				forbidden(w)
				return
			}
			v.Owner = parent.Owner
		}
		if e := a.store.Save(v); e != nil {
			fail(w, 500, "save failed")
			return
		}
		if kind == "workspaces" && old.ID == "" {
			if tx, e := a.store.DB.Begin(); e == nil {
				a.grant(tx, uid, "administrator", scopeWorkspace, v.ID, uid)
				tx.Commit()
			}
		}
		verb := "update"
		if old.ID == "" {
			verb = "create"
		}
		workspace, project := x.scope(v)
		a.auditRecord(r.Context(), uid, verb, v, workspace, project)
		a.refresh()
		reply(w, 200, a.accessFor(requestSession(r.Context())).publicRecord(v))
	})
}
func verifySignature(secret string, body []byte, signature string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}
	got, e := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if e != nil {
		return false
	}
	m := hmac.New(sha256.New, []byte(secret))
	m.Write(body)
	return hmac.Equal(got, m.Sum(nil))
}
func (a *Server) webhook(w http.ResponseWriter, r *http.Request, id string) {
	if r.Method != "POST" {
		fail(w, 405, "method not allowed")
		return
	}
	project, e := a.store.Get(id)
	if e != nil || project.Kind != "projects" {
		fail(w, 404, "not found")
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil || project.WebhookSecret == "" || !verifySignature(project.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
		fail(w, 401, "invalid signature")
		return
	}
	var event struct {
		Ref string `json:"ref"`
	}
	if json.Unmarshal(body, &event) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	if r.Header.Get("X-GitHub-Event") != "push" || event.Ref != "refs/heads/"+project.Branch || !project.AutoDeploy {
		reply(w, 200, map[string]bool{"ignored": true})
		return
	}
	d, err := a.enqueueProject(project, Record{})
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	a.audit(r.Context(), "", "project.autodeploy", project.Name, scopeProject, project.ID, map[string]string{"release_id": d.ID})
	reply(w, 202, publicRecord(d))
}
func safeRoot(root string) bool {
	return !filepath.IsAbs(root) && root != ".." && !strings.HasPrefix(filepath.Clean(root), ".."+string(filepath.Separator))
}
