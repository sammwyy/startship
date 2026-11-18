package server

import (
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
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	dockerService        func(...string) (string, error)
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
	if _, err = a.store.DB.Exec("UPDATE users SET password=?,must_change_password=1 WHERE id=?", string(hash), uid); err == nil {
		_, err = a.store.DB.Exec("DELETE FROM sessions WHERE user_id=?", uid)
	}
	return err
}
func (a *Server) user(r *http.Request) string {
	if localRequest(r) {
		// Whoever can reach the protected socket acts as the longest-serving Platform Admin.
		var id string
		a.store.DB.QueryRow("SELECT a.user_id FROM role_assignments a JOIN users u ON u.id=a.user_id WHERE a.role_id=? AND a.scope=? ORDER BY a.created,a.rowid LIMIT 1", platformAdminRole, scopePlatform).Scan(&id)
		return id
	}
	c, e := r.Cookie("startship_session")
	if e != nil {
		return ""
	}
	var id string
	a.store.DB.QueryRow("SELECT user_id FROM sessions WHERE id=? AND expires>?", c.Value, time.Now().Unix()).Scan(&id)
	return id
}
func (a *Server) updateAccount(w http.ResponseWriter, r *http.Request, uid string) {
	var v struct {
		Username        *string `json:"username"`
		CurrentPassword string  `json:"current_password"`
		NewPassword     *string `json:"new_password"`
	}
	if decode(w, r, &v) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	if v.Username == nil && v.NewPassword == nil {
		fail(w, 400, "provide a username or new password")
		return
	}
	if v.Username != nil {
		name := strings.TrimSpace(*v.Username)
		if len(name) < 1 || len(name) > 100 {
			fail(w, 400, "username must be between 1 and 100 bytes")
			return
		}
		v.Username = &name
	}
	if v.NewPassword != nil && (len(*v.NewPassword) < 8 || len(*v.NewPassword) > 72) {
		fail(w, 400, "new password must be between 8 and 72 bytes")
		return
	}
	if v.CurrentPassword == "" {
		fail(w, 400, "current password required")
		return
	}
	a.store.Lock()
	defer a.store.Unlock()
	tx, err := a.store.DB.Begin()
	if err != nil {
		fail(w, 500, "account update failed")
		return
	}
	defer tx.Rollback()
	cookie, _ := r.Cookie("startship_session")
	var name, hash string
	var pending bool
	err = tx.QueryRow("SELECT u.username,u.password,u.must_change_password FROM users u JOIN sessions s ON s.user_id=u.id WHERE u.id=? AND s.id=? AND s.expires>?", uid, cookie.Value, time.Now().Unix()).Scan(&name, &hash, &pending)
	if err != nil {
		fail(w, 401, "session expired; sign in again")
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.CurrentPassword)) != nil {
		fail(w, 403, "current password is incorrect")
		return
	}
	if v.Username != nil {
		name = *v.Username
		var count int
		if err = tx.QueryRow("SELECT count(*) FROM users WHERE username=? AND id<>?", name, uid).Scan(&count); err != nil {
			fail(w, 500, "account update failed")
			return
		}
		if count != 0 {
			fail(w, 409, "username is already in use")
			return
		}
	}
	if v.NewPassword != nil {
		var generated []byte
		generated, err = bcrypt.GenerateFromPassword([]byte(*v.NewPassword), bcrypt.DefaultCost)
		if err != nil {
			fail(w, 500, "account update failed")
			return
		}
		hash = string(generated)
	}
	if pending && v.NewPassword != nil {
		if *v.NewPassword == v.CurrentPassword {
			fail(w, 400, "choose a different password")
			return
		}
		if _, err = tx.Exec("UPDATE users SET must_change_password=0 WHERE id=?; INSERT OR IGNORE INTO settings VALUES('setup_completed',strftime('%Y-%m-%dT%H:%M:%SZ','now'))", uid); err != nil {
			fail(w, 500, "account update failed")
			return
		}
	}
	if _, err = tx.Exec("UPDATE users SET username=?,password=? WHERE id=?", name, hash, uid); err != nil {
		fail(w, 500, "account update failed")
		return
	}
	token := randomID()
	if _, err = tx.Exec("DELETE FROM sessions WHERE user_id=?", uid); err == nil {
		_, err = tx.Exec("INSERT INTO sessions VALUES(?,?,?)", token, uid, time.Now().Add(7*24*time.Hour).Unix())
	}
	if err != nil {
		fail(w, 500, "account update failed")
		return
	}
	if err = tx.Commit(); err != nil {
		fail(w, 500, "account update failed")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: token, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 604800})
	if pending && v.NewPassword != nil {
		log.Print("Setup complete: administrator password changed")
	}
	reply(w, 200, map[string]string{"id": uid, "username": name})
}
func (a *Server) api() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			dashboard.ServeHTTP(w, r)
			return
		}
		// Reject browser cross-origin mutations, including login CSRF.
		if r.Method != "GET" && r.Method != "HEAD" {
			if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
				fail(w, 403, "cross-origin request rejected")
				return
			}
		}
		p := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		if localRequest(r) && len(p) > 1 && (p[1] == "login" || p[1] == "logout" || p[1] == "setup" && r.Method != "GET" || p[1] == "me" && r.Method != "GET") {
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
		if p[1] == "login" && r.Method == "POST" {
			var v struct{ Username, Password string }
			if decode(w, r, &v) != nil {
				fail(w, 400, "invalid JSON")
				return
			}
			a.store.Lock()
			defer a.store.Unlock()
			var id, hash string
			e := a.store.DB.QueryRow("SELECT id,password FROM users WHERE username=?", strings.TrimSpace(v.Username)).Scan(&id, &hash)
			if e != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.Password)) != nil {
				fail(w, 401, "invalid username or password")
				return
			}
			token := randomID()
			_, e = a.store.DB.Exec("INSERT INTO sessions VALUES(?,?,?)", token, id, time.Now().Add(7*24*time.Hour).Unix())
			if e != nil {
				fail(w, 500, "session failed")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: token, Path: "/", HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode, MaxAge: 604800})
			reply(w, 200, map[string]string{"id": id})
			return
		}
		uid := a.user(r)
		if uid == "" {
			fail(w, 401, "authentication required")
			return
		}
		if p[1] == "logout" && r.Method == "POST" {
			c, _ := r.Cookie("startship_session")
			a.store.DB.Exec("DELETE FROM sessions WHERE id=?", c.Value)
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Path: "/", MaxAge: -1, HttpOnly: true, Secure: secureRequest(r), SameSite: http.SameSiteLaxMode})
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if p[1] == "me" {
			if r.Method == "PATCH" {
				a.updateAccount(w, r, uid)
				return
			}
			if r.Method != "GET" {
				w.Header().Set("Allow", "GET, PATCH")
				fail(w, 405, "method not allowed")
				return
			}
			var name string
			var pending bool
			a.store.DB.QueryRow("SELECT username,must_change_password FROM users WHERE id=?", uid).Scan(&name, &pending)
			x := a.access(uid)
			reply(w, 200, map[string]any{"id": uid, "username": name, "must_change_password": pending, "setup_completed": a.setupComplete(), "local_admin": localRequest(r), "platform_admin": x.admin, "permissions": x.list("", "")})
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
		x := a.access(uid)
		if len(p) == 3 && p[1] == "pipeline" && p[2] == "templates" && r.Method == "GET" {
			reply(w, 200, pipeline.Templates())
			return
		}
		if a.networkAPI(w, r, p, uid, x) || a.firewallAPI(w, r, p, uid, x) || a.servicesAPI(w, r, p, uid, x) || a.certificatesAPI(w, r, p, uid, x) {
			return
		}
		if p[1] == "detect" && r.Method == "POST" {
			a.detectAPI(w, r, x)
			return
		}
		if a.accessAPI(w, r, p, uid, x) || a.systemAPI(w, r, p, uid, x) {
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
		if len(p) == 4 && (kind == "projects" || kind == "apps") && (p[3] == "deploy" || p[3] == "redeploy") && r.Method == "POST" && !x.can(old, "deploy") {
			forbidden(w)
			return
		}
		if len(p) == 4 && kind == "projects" && (p[3] == "deploy" || p[3] == "redeploy") && r.Method == "POST" {
			a.projectDeployAPI(w, r, old, p[3])
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
			a.store.Delete(old)
			if old.Kind == "workspaces" || old.Kind == "projects" {
				a.removeScopeAccess(strings.TrimSuffix(old.Kind, "s"), old.ID)
				a.audit(uid, strings.TrimSuffix(old.Kind, "s")+".delete", old.Name, strings.TrimSuffix(old.Kind, "s"), old.ID, map[string]string{"id": old.ID})
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
			v.ConfigVersion = 1
			if v.ConfigConfirmed {
				v.ConfigConflict = false
			}
			v.ConfigConfirmed = false
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
				v.SourceConflict = false
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
			a.audit(uid, "workspace.create", v.Name, scopeWorkspace, v.ID, map[string]string{"id": v.ID})
		}
		if kind == "projects" {
			if e := migrateProjectSources(a.store); e != nil {
				fail(w, 500, "source migration failed")
				return
			}
		}
		a.refresh()
		reply(w, 200, a.access(uid).publicRecord(v))
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
	target, e := a.store.Get(id)
	if e != nil || (target.Kind != "apps" && target.Kind != "projects") {
		fail(w, 404, "not found")
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil || target.WebhookSecret == "" || !verifySignature(target.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
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
	project := target
	if target.Kind == "apps" {
		project, e = a.projectSource(target)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
	}
	if project.ConfigVersion > 0 {
		if r.Header.Get("X-GitHub-Event") != "push" || event.Ref != "refs/heads/"+project.Branch || !project.AutoDeploy {
			reply(w, 200, map[string]bool{"ignored": true})
			return
		}
		d, err := a.enqueueProject(project, Record{})
		if err != nil {
			fail(w, 409, err.Error())
			return
		}
		reply(w, 202, publicRecord(d))
		return
	}
	apps := []Record{}
	if target.Kind == "apps" {
		project, e = a.projectSource(target)
		if e != nil {
			fail(w, 400, e.Error())
			return
		}
		apps = append(apps, target)
	} else {
		for _, app := range a.store.List("apps", target.Owner) {
			if app.ProjectID == target.ID {
				apps = append(apps, app)
			}
		}
	}
	if r.Header.Get("X-GitHub-Event") != "push" || event.Ref != "refs/heads/"+project.Branch {
		reply(w, 200, map[string]bool{"ignored": true})
		return
	}
	deployments := []Record{}
	for _, app := range apps {
		if !app.AutoDeploy {
			continue
		}
		d, err := a.enqueue(app)
		if err != nil {
			fail(w, 500, err.Error())
			return
		}
		deployments = append(deployments, d)
	}
	if len(deployments) != 0 {
		if target.Kind == "apps" {
			reply(w, 202, deployments[0])
		} else {
			reply(w, 202, deployments)
		}
		return
	}
	reply(w, 200, map[string]bool{"ignored": true})
}
func safeRoot(root string) bool {
	return !filepath.IsAbs(root) && root != ".." && !strings.HasPrefix(filepath.Clean(root), ".."+string(filepath.Separator))
}
