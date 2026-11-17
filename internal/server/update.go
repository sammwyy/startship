package server

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/sammwyy/startship/internal/update"
	"github.com/sammwyy/startship/internal/version"
)

const updateCheckInterval = 12 * time.Hour

// updates caches the latest release check and hands install requests to the
// privileged startship-update.service, since the daemon cannot replace its own binaries.
type updates struct {
	mu      sync.Mutex
	client  *update.Client
	latest  update.Release
	checked time.Time
	err     string
	// request is the file startship-update.path watches; empty disables self-update.
	request string
	status  string
}

type updateInfo struct {
	Current         string          `json:"current"`
	Commit          string          `json:"commit"`
	Development     bool            `json:"development"`
	Latest          string          `json:"latest,omitempty"`
	UpdateAvailable bool            `json:"update_available"`
	ReleaseURL      string          `json:"release_url,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	Published       time.Time       `json:"published,omitzero"`
	CheckedAt       time.Time       `json:"checked_at,omitzero"`
	CheckError      string          `json:"check_error,omitempty"`
	SelfUpdate      bool            `json:"self_update"`
	Update          *update.Status  `json:"update,omitempty"`
	Pending         *update.Request `json:"pending,omitempty"`
}

func (a *Server) updater() *updates {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.updates == nil {
		a.updates = &updates{client: update.FromEnv(), request: os.Getenv("STARTSHIP_UPDATE_REQUEST"), status: os.Getenv("STARTSHIP_UPDATE_STATUS")}
		if a.updates.request != "" && a.updates.status == "" {
			a.updates.status = filepath.Join(filepath.Dir(a.updates.request), "status.json")
		}
	}
	return a.updates
}

func (u *updates) check(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	r, err := u.client.Latest(ctx)
	u.mu.Lock()
	defer u.mu.Unlock()
	u.checked = time.Now().UTC()
	if err != nil {
		u.err = err.Error()
		return
	}
	u.latest, u.err = r, ""
}

func (u *updates) info(ctx context.Context, refresh bool) updateInfo {
	u.mu.Lock()
	stale := refresh || u.checked.IsZero()
	u.mu.Unlock()
	if stale {
		u.check(ctx)
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	v := updateInfo{Current: version.Version, Commit: version.Commit, Development: !update.ValidVersion(version.Version), Latest: u.latest.Version, ReleaseURL: u.latest.URL, Notes: u.latest.Notes, Published: u.latest.Published, CheckedAt: u.checked, CheckError: u.err, SelfUpdate: u.request != ""}
	v.UpdateAvailable = !v.Development && v.Latest != "" && update.Compare(v.Latest, v.Current) > 0
	if u.request != "" {
		var p update.Request
		if update.ReadJSON(u.request, &p) == nil {
			v.Pending = &p
		}
		var s update.Status
		if update.ReadJSON(u.status, &s) == nil {
			v.Update = &s
		}
	}
	return v
}

func (a *Server) watchUpdates(ctx context.Context) {
	switch strings.ToLower(os.Getenv("STARTSHIP_UPDATE_CHECK")) {
	case "0", "false", "off", "no":
		return
	}
	u := a.updater()
	for {
		u.check(ctx)
		if v := u.info(ctx, false); v.UpdateAvailable {
			log.Printf("Startship %s is available (running %s): %s", v.Latest, v.Current, v.ReleaseURL)
		} else if v.CheckError != "" {
			log.Printf("update check failed: %s", v.CheckError)
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(updateCheckInterval):
		}
	}
}

// systemAPI serves /api/system/version and /api/system/update.
func (a *Server) systemAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) != 3 || p[1] != "system" {
		return false
	}
	switch p[2] {
	case "version":
		if r.Method != "GET" {
			fail(w, 405, "method not allowed")
			return true
		}
		reply(w, 200, map[string]string{"version": version.Version, "commit": version.Commit})
	case "update":
		u := a.updater()
		switch r.Method {
		case "GET":
			reply(w, 200, u.info(r.Context(), r.URL.Query().Get("refresh") != ""))
		case "POST":
			a.requestUpdate(w, r, u, uid, x)
		default:
			w.Header().Set("Allow", "GET, POST")
			fail(w, 405, "method not allowed")
		}
	default:
		fail(w, 404, "not found")
	}
	return true
}

func (a *Server) requestUpdate(w http.ResponseWriter, r *http.Request, u *updates, uid string, x *access) {
	if !x.admin {
		fail(w, 403, "only Platform Admins can update Startship")
		return
	}
	var v struct {
		Version string `json:"version"`
	}
	if r.ContentLength != 0 {
		if err := decode(w, r, &v); err != nil {
			fail(w, 400, "invalid JSON")
			return
		}
	}
	if u.request == "" {
		fail(w, 409, "self-update is not configured for this daemon; run sudo startship update on the server (the installer sets it up)")
		return
	}
	info := u.info(r.Context(), v.Version == "")
	if v.Version == "" {
		if info.Latest == "" {
			fail(w, 502, "cannot check for updates: "+info.CheckError)
			return
		}
		v.Version = info.Latest
	}
	if !update.ValidVersion(v.Version) {
		fail(w, 400, "version must be a release tag like v1.2.3")
		return
	}
	if v.Version == version.Version {
		fail(w, 409, "already running "+v.Version)
		return
	}
	if info.Update != nil && info.Update.State == "running" && time.Since(info.Update.Started) < 15*time.Minute {
		fail(w, 409, "an update to "+info.Update.Version+" is already running")
		return
	}
	req := update.Request{Version: v.Version, Requested: time.Now().UTC(), By: a.username(uid)}
	if err := os.MkdirAll(filepath.Dir(u.request), 0o750); err != nil && !errors.Is(err, os.ErrExist) {
		fail(w, 500, "cannot queue update")
		return
	}
	if err := update.WriteJSON(u.request, req, 0o640); err != nil {
		log.Printf("queue update: %v", err)
		fail(w, 500, "cannot queue update")
		return
	}
	a.audit(uid, "system.update", v.Version, scopePlatform, "", map[string]string{"from": version.Version, "to": v.Version})
	reply(w, 202, req)
}
