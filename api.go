package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"golang.org/x/crypto/bcrypt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Server struct {
	store   *Store
	data    string
	mu      sync.Mutex
	queues  map[string]chan string
	routeMu sync.RWMutex
	targets []Target
}

// Response-only fields must not be persisted or accepted as app configuration.
type apiRecord struct {
	Record
	URL string `json:"url,omitempty"`
}

func publicRecord(v Record) apiRecord {
	v.GitToken = ""
	out := apiRecord{Record: v}
	if v.Kind == "routes" {
		out.URL = routeURL(v)
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
func (a *Server) createUser(user, pass string) (string, error) {
	if len(user) < 1 || len(user) > 100 || len(pass) < 8 {
		return "", fmt.Errorf("username required; password must be at least 8 characters")
	}
	hash, e := bcrypt.GenerateFromPassword([]byte(pass), bcrypt.DefaultCost)
	if e != nil {
		return "", e
	}
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	tx, e := a.store.db.Begin()
	if e != nil {
		return "", e
	}
	defer tx.Rollback()
	id := randomID()
	if _, e = tx.Exec("INSERT INTO users VALUES(?,?,?)", id, user, string(hash)); e != nil {
		return "", e
	}
	ws := Record{ID: randomID(), Kind: "workspaces", Owner: id, Name: user + "'s Workspace"}
	b, _ := json.Marshal(ws)
	if _, e = tx.Exec("INSERT INTO records VALUES(?,?,?,?,?)", ws.ID, ws.Kind, id, "", string(b)); e != nil {
		return "", e
	}
	return id, tx.Commit()
}
func (a *Server) user(r *http.Request) string {
	c, e := r.Cookie("startship_session")
	if e != nil {
		return ""
	}
	var id string
	a.store.db.QueryRow("SELECT user_id FROM sessions WHERE id=? AND expires>?", c.Value, time.Now().Unix()).Scan(&id)
	return id
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
				a.store.db.QueryRow("SELECT count(*) FROM users").Scan(&n)
				reply(w, 200, map[string]bool{"required": n == 0})
				return
			}
			if r.Method != "POST" {
				fail(w, 405, "method not allowed")
				return
			}
			a.mu.Lock()
			defer a.mu.Unlock()
			var n int
			a.store.db.QueryRow("SELECT count(*) FROM users").Scan(&n)
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
			reply(w, 201, map[string]string{"id": id})
			return
		}
		if p[1] == "login" && r.Method == "POST" {
			var v struct{ Username, Password string }
			if decode(w, r, &v) != nil {
				fail(w, 400, "invalid JSON")
				return
			}
			var id, hash string
			e := a.store.db.QueryRow("SELECT id,password FROM users WHERE username=?", v.Username).Scan(&id, &hash)
			if e != nil || bcrypt.CompareHashAndPassword([]byte(hash), []byte(v.Password)) != nil {
				fail(w, 401, "invalid credentials")
				return
			}
			token := randomID()
			_, e = a.store.db.Exec("INSERT INTO sessions VALUES(?,?,?)", token, id, time.Now().Add(7*24*time.Hour).Unix())
			if e != nil {
				fail(w, 500, "session failed")
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Value: token, Path: "/", HttpOnly: true, Secure: r.TLS != nil, SameSite: http.SameSiteLaxMode, MaxAge: 604800})
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
			a.store.db.Exec("DELETE FROM sessions WHERE id=?", c.Value)
			http.SetCookie(w, &http.Cookie{Name: "startship_session", Path: "/", MaxAge: -1, HttpOnly: true})
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if p[1] == "me" {
			var name string
			a.store.db.QueryRow("SELECT username FROM users WHERE id=?", uid).Scan(&name)
			reply(w, 200, map[string]string{"id": uid, "username": name})
			return
		}
		if p[1] == "detect" && r.Method == "POST" {
			a.detectAPI(w, r)
			return
		}
		kind := p[1]
		if kind != "workspaces" && kind != "projects" && kind != "apps" && kind != "routes" && kind != "deployments" {
			fail(w, 404, "not found")
			return
		}
		var old Record
		if len(p) > 2 {
			var e error
			old, e = a.store.get(p[2])
			if e != nil || old.Owner != uid || old.Kind != kind {
				fail(w, 404, "not found")
				return
			}
		}
		if len(p) == 4 && kind == "apps" && p[3] == "deploy" && r.Method == "POST" {
			d, e := a.enqueue(old)
			if e != nil {
				fail(w, 500, e.Error())
				return
			}
			reply(w, 202, d)
			return
		}
		if r.Method == "GET" {
			if len(p) > 2 {
				reply(w, 200, publicRecord(old))
				return
			}
			out := []apiRecord{}
			for _, v := range a.store.list(kind, uid) {
				if parent := r.URL.Query().Get("project_id"); parent != "" && v.ProjectID != parent {
					continue
				}
				if parent := r.URL.Query().Get("workspace_id"); parent != "" && v.WorkspaceID != parent {
					continue
				}
				if parent := r.URL.Query().Get("app_id"); parent != "" && v.AppID != parent {
					continue
				}
				out = append(out, publicRecord(v))
			}
			reply(w, 200, out)
			return
		}
		if kind == "deployments" {
			fail(w, 405, "read only")
			return
		}
		if r.Method == "DELETE" && len(p) == 3 {
			for _, k := range []string{"projects", "apps", "routes"} {
				for _, v := range a.store.list(k, uid) {
					if v.Parent == old.ID || (k == "routes" && v.AppID == old.ID) {
						fail(w, 409, "delete children first")
						return
					}
				}
			}
			if old.Kind == "apps" {
				for _, d := range a.store.list("deployments", uid) {
					if d.AppID == old.ID && (d.Status == "queued" || d.Status == "building" || d.Status == "testing" || d.Status == "starting") {
						fail(w, 409, "wait for deployments to finish")
						return
					}
				}
				a.removeActive(old)
				a.store.db.Exec("DELETE FROM records WHERE kind='deployments' AND parent=?", old.ID)
			}
			a.store.db.Exec("DELETE FROM records WHERE id=?", old.ID)
			a.refresh()
			reply(w, 200, map[string]bool{"ok": true})
			return
		}
		if r.Method != "POST" && r.Method != "PUT" && r.Method != "PATCH" {
			fail(w, 405, "method not allowed")
			return
		}
		v := old
		if decode(w, r, &v) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		v.Kind = kind
		v.Owner = uid
		if old.ID == "" {
			v.ID = randomID()
			v.Active = ""
			v.WebhookSecret = ""
		} else {
			v.ID = old.ID
			v.Active = old.Active
			v.WebhookSecret = old.WebhookSecret
		}
		switch kind {
		case "projects":
			v.Parent = v.WorkspaceID
		case "apps":
			v.Parent = v.ProjectID
			if v.Branch == "" {
				v.Branch = "main"
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
			if v.WebhookSecret == "" {
				v.WebhookSecret = randomID()
			}
			if !safeRoot(v.RootDir) || v.Port < 1 || v.Port > 65535 || !validBuildType(v.BuildType) {
				fail(w, 400, "invalid app configuration")
				return
			}
			if e := validateGit(v.GitURL); e != nil {
				fail(w, 400, e.Error())
				return
			}
		case "routes":
			v.Parent = v.ProjectID
			v.Host = normalizeHost(v.Host)
			if v.PathPrefix == "" {
				v.PathPrefix = "/"
			}
			app, e := a.store.get(v.AppID)
			if e != nil || app.Kind != "apps" || app.ProjectID != v.ProjectID || app.Owner != uid || v.Host == "" || !strings.HasPrefix(v.PathPrefix, "/") {
				fail(w, 400, "invalid route")
				return
			}
			for _, other := range a.store.list("routes", "") {
				if other.ID != v.ID && other.Host == v.Host && other.PathPrefix == v.PathPrefix {
					fail(w, 409, "route already exists")
					return
				}
			}
		}
		if kind != "workspaces" {
			parent, e := a.store.get(v.Parent)
			expected := "workspaces"
			if kind == "apps" || kind == "routes" {
				expected = "projects"
			}
			if e != nil || parent.Owner != uid || parent.Kind != expected {
				fail(w, 400, "invalid parent")
				return
			}
		}
		if e := a.store.save(v); e != nil {
			fail(w, 500, "save failed")
			return
		}
		a.refresh()
		reply(w, 200, publicRecord(v))
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
	app, e := a.store.get(id)
	if e != nil || app.Kind != "apps" {
		fail(w, 404, "not found")
		return
	}
	body, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if e != nil || !verifySignature(app.WebhookSecret, body, r.Header.Get("X-Hub-Signature-256")) {
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
	if r.Header.Get("X-GitHub-Event") == "push" && event.Ref == "refs/heads/"+app.Branch && app.AutoDeploy {
		d, e := a.enqueue(app)
		if e != nil {
			fail(w, 500, e.Error())
			return
		}
		reply(w, 202, d)
		return
	}
	reply(w, 200, map[string]bool{"ignored": true})
}
func safeRoot(root string) bool {
	return !filepath.IsAbs(root) && root != ".." && !strings.HasPrefix(filepath.Clean(root), ".."+string(filepath.Separator))
}
