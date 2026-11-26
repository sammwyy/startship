package server

import (
	"database/sql"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

var statusSlug = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?$`)

type statusPageConfig struct {
	Enabled  bool     `json:"enabled"`
	Slug     string   `json:"slug"`
	Title    string   `json:"title"`
	Projects []string `json:"projects"`
}

type statusPageProject struct {
	Name      string           `json:"name"`
	State     string           `json:"state"`
	Uptime    *float64         `json:"uptime"`
	History   []uptimeBucket   `json:"history"`
	Incidents []uptimeIncident `json:"incidents"`
}

type publicStatusPage struct {
	Title    string              `json:"title"`
	Projects []statusPageProject `json:"projects"`
}

func scanStatusPage(row *sql.Row) (statusPageConfig, error) {
	out := statusPageConfig{Projects: []string{}}
	var projects string
	if err := row.Scan(&out.Enabled, &out.Slug, &out.Title, &projects); err != nil {
		return out, err
	}
	return out, json.Unmarshal([]byte(projects), &out.Projects)
}

func (a *Server) statusConfig(workspace string) (statusPageConfig, error) {
	out, err := scanStatusPage(a.store.DB.QueryRow("SELECT enabled,COALESCE(slug,''),title,projects FROM status_pages WHERE workspace_id=?", workspace))
	if errors.Is(err, sql.ErrNoRows) {
		err = nil
	}
	return out, err
}

func (a *Server) workspaceStatusPage(w http.ResponseWriter, r *http.Request, ws Record, x *access, uid string) {
	if !x.can(ws, "workspace.manage") {
		forbidden(w)
		return
	}
	if r.Method == http.MethodGet {
		config, err := a.statusConfig(ws.ID)
		if err != nil {
			fail(w, 500, "status page unavailable")
			return
		}
		reply(w, 200, config)
		return
	}
	if r.Method != http.MethodPatch {
		fail(w, 405, "method not allowed")
		return
	}
	a.store.Lock()
	defer a.store.Unlock()
	config, err := a.statusConfig(ws.ID)
	if err != nil {
		fail(w, 500, "status page unavailable")
		return
	}
	previous := slices.Clone(config.Projects)
	if err := decode(w, r, &config); err != nil {
		fail(w, 400, "invalid status page settings")
		return
	}
	config.Title = strings.TrimSpace(config.Title)
	config.Slug = strings.TrimSpace(config.Slug)
	if config.Slug != "" && !statusSlug.MatchString(config.Slug) {
		fail(w, 400, "slug must be 1–63 ASCII letters, digits, or hyphens, and start and end with a letter or digit")
		return
	}
	config.Slug = strings.ToLower(config.Slug)
	if len(config.Title) > 120 || len(config.Projects) > 25 {
		fail(w, 400, "use a title of at most 120 bytes and select at most 25 projects")
		return
	}
	seen := make(map[string]bool)
	selected := []string{}
	for _, id := range config.Projects {
		if seen[id] {
			fail(w, 400, "projects must be unique")
			return
		}
		seen[id] = true
		p, err := a.store.Get(id)
		if errors.Is(err, sql.ErrNoRows) && slices.Contains(previous, id) {
			continue
		}
		if err != nil || p.Kind != "projects" || p.WorkspaceID != ws.ID || !x.visible(p) {
			fail(w, 400, "select projects from this workspace")
			return
		}
		selected = append(selected, id)
	}
	config.Projects = selected
	if config.Enabled && (config.Slug == "" || config.Title == "" || len(config.Projects) == 0) {
		fail(w, 400, "a published page needs a slug, title, and at least one project")
		return
	}
	projects, _ := json.Marshal(config.Projects)
	var slug any
	if config.Slug != "" {
		slug = config.Slug
		var owner string
		err = a.store.DB.QueryRow("SELECT workspace_id FROM status_pages WHERE slug=? AND workspace_id<>?", slug, ws.ID).Scan(&owner)
		if err == nil {
			fail(w, 409, "status page address is already in use; choose a different slug")
			return
		}
		if !errors.Is(err, sql.ErrNoRows) {
			fail(w, 500, "status page unavailable")
			return
		}
	}
	if _, err = a.store.DB.Exec(`INSERT INTO status_pages VALUES(?,?,?,?,?) ON CONFLICT(workspace_id)
		DO UPDATE SET slug=excluded.slug, enabled=excluded.enabled, title=excluded.title, projects=excluded.projects`,
		ws.ID, slug, config.Enabled, config.Title, string(projects)); err != nil {
		fail(w, 500, "could not save status page")
		return
	}
	a.live.publish(liveEvent{changeKey: "status_page:" + ws.ID})
	a.audit(r.Context(), uid, "workspace.status_page", ws.Name, scopeWorkspace, ws.ID, nil)
	reply(w, 200, config)
}

func (a *Server) publicStatusData(slug string) (publicStatusPage, error) {
	page := publicStatusPage{Projects: []statusPageProject{}}
	var workspace string
	var projects string
	err := a.store.DB.QueryRow(`SELECT workspace_id,title,projects FROM status_pages JOIN records ON records.id=status_pages.workspace_id
		WHERE slug=? AND enabled=1 AND kind='workspaces'`, slug).Scan(&workspace, &page.Title, &projects)
	if err != nil {
		return page, err
	}
	var selected []string
	if err := json.Unmarshal([]byte(projects), &selected); err != nil {
		return page, err
	}
	now := time.Now()
	for _, id := range selected {
		p, err := a.store.Get(id)
		if err != nil || p.Kind != "projects" || p.WorkspaceID != workspace {
			continue
		}
		data, err := a.summarizeUptime(p, now, 90, false)
		if err != nil {
			return page, err
		}
		for i := range data.Incidents {
			data.Incidents[i].Cause = "Health check failed"
		}
		page.Projects = append(page.Projects, statusPageProject{p.Name, data.State, data.Uptime30d, data.History, data.Incidents})
	}
	return page, nil
}

func (a *Server) servePublicStatus(w http.ResponseWriter, r *http.Request, slug string) {
	if r.Method != http.MethodGet {
		fail(w, 405, "method not allowed")
		return
	}
	if !statusSlug.MatchString(slug) {
		fail(w, 404, "status page not found")
		return
	}
	page, err := a.publicStatusData(strings.ToLower(slug))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		fail(w, 404, "status page not found")
	case err != nil:
		log.Printf("public status summary: %v", err)
		fail(w, 500, "status page unavailable")
	default:
		reply(w, 200, page)
	}
}
