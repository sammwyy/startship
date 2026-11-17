package server

import (
	"github.com/sammwyy/startship/internal/routing"
	"net/http"
	"regexp"
	"strings"
)

// WorkspaceDomain is a hostname a workspace's projects may route. Apex allows the
// name itself and Subdomains allows any host below it, like *.example.com.
type WorkspaceDomain struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Apex        bool   `json:"apex"`
	Subdomains  bool   `json:"subdomains"`
	Routes      int    `json:"routes"`
	Created     string `json:"created"`
	CreatedBy   string `json:"created_by"`
}

var domainPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z][a-z0-9-]{0,61}[a-z0-9]$`)

func migrateDomains(s *Store) error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS workspace_domains(
		id TEXT PRIMARY KEY,
		workspace_id TEXT NOT NULL,
		name TEXT NOT NULL UNIQUE,
		apex INTEGER NOT NULL,
		subdomains INTEGER NOT NULL,
		created TEXT NOT NULL,
		created_by TEXT NOT NULL
	)`)
	return err
}

func (d WorkspaceDomain) matches(host string) bool {
	return d.Apex && host == d.Name || d.Subdomains && strings.HasSuffix(host, "."+d.Name)
}

// overlaps reports whether two domains could ever claim the same host.
func (d WorkspaceDomain) overlaps(o WorkspaceDomain) bool {
	return d.Name == o.Name || d.Subdomains && strings.HasSuffix(o.Name, "."+d.Name) || o.Subdomains && strings.HasSuffix(d.Name, "."+o.Name)
}

func (a *Server) domains(workspaceID string) []WorkspaceDomain {
	q, args := "SELECT id,workspace_id,name,apex,subdomains,created,created_by FROM workspace_domains", []any{}
	if workspaceID != "" {
		q, args = q+" WHERE workspace_id=?", append(args, workspaceID)
	}
	rows, err := a.store.DB.Query(q+" ORDER BY name", args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []WorkspaceDomain{}
	for rows.Next() {
		var d WorkspaceDomain
		if rows.Scan(&d.ID, &d.WorkspaceID, &d.Name, &d.Apex, &d.Subdomains, &d.Created, &d.CreatedBy) == nil {
			out = append(out, d)
		}
	}
	return out
}

func (a *Server) routeAllowed(workspaceID, host string) bool {
	for _, d := range a.domains(workspaceID) {
		if d.matches(host) {
			return true
		}
	}
	return false
}

// parseDomain accepts "example.com" or "*.example.com".
func parseDomain(name string, subdomains bool) (WorkspaceDomain, bool) {
	name = routing.NormalizeHost(strings.TrimSpace(name))
	d := WorkspaceDomain{Apex: true, Subdomains: subdomains}
	if rest, ok := strings.CutPrefix(name, "*."); ok {
		name, d.Apex, d.Subdomains = rest, false, true
	}
	d.Name = name
	return d, len(name) <= 253 && domainPattern.MatchString(name)
}

func (a *Server) domainsAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, ws Record, x *access) {
	manage := x.at(ws.ID, "")["workspace.domains"]
	switch {
	case len(p) == 4 && r.Method == "GET":
		list := a.domains(ws.ID)
		routes := a.store.List("routes", "")
		for i := range list {
			for _, route := range routes {
				if list[i].matches(route.Host) && projectWorkspace(x.project(route.ProjectID)) == ws.ID {
					list[i].Routes++
				}
			}
		}
		reply(w, 200, map[string]any{"domains": list, "can_manage": manage})
	case len(p) == 4 && r.Method == "POST":
		if !manage {
			forbidden(w)
			return
		}
		var v struct {
			Name       string `json:"name"`
			Subdomains bool   `json:"subdomains"`
		}
		if decode(w, r, &v) != nil {
			fail(w, 400, "invalid domain")
			return
		}
		d, ok := parseDomain(v.Name, v.Subdomains)
		if !ok {
			fail(w, 400, "enter a domain like example.com or *.example.com")
			return
		}
		if a.dashboardDomain != "" && d.matches(routing.NormalizeHost(a.dashboardDomain)) {
			fail(w, 409, "that domain serves the Startship dashboard")
			return
		}
		for _, o := range a.domains("") {
			if d.overlaps(o) && o.WorkspaceID != ws.ID {
				fail(w, 409, d.Name+" overlaps a domain another workspace owns")
				return
			}
			if o.Name == d.Name {
				fail(w, 409, d.Name+" is already in this workspace")
				return
			}
		}
		d.ID, d.WorkspaceID, d.Created, d.CreatedBy = randomID(), ws.ID, nowStamp(), uid
		if _, err := a.store.DB.Exec("INSERT INTO workspace_domains VALUES(?,?,?,?,?,?,?)", d.ID, d.WorkspaceID, d.Name, d.Apex, d.Subdomains, d.Created, d.CreatedBy); err != nil {
			fail(w, 409, d.Name+" is already claimed")
			return
		}
		a.audit(uid, "domain.create", d.Name, scopeWorkspace, ws.ID, map[string]bool{"apex": d.Apex, "subdomains": d.Subdomains})
		reply(w, 201, d)
	case len(p) == 5 && r.Method == "DELETE":
		if !manage {
			forbidden(w)
			return
		}
		var d WorkspaceDomain
		for _, o := range a.domains(ws.ID) {
			if o.ID == p[4] {
				d = o
			}
		}
		if d.ID == "" {
			fail(w, 404, "not found")
			return
		}
		for _, route := range a.store.List("routes", "") {
			if projectWorkspace(x.project(route.ProjectID)) == ws.ID && d.matches(route.Host) && !a.routeAllowedExcept(ws.ID, route.Host, d.ID) {
				fail(w, 409, route.Host+" still routes through "+d.Name+"; disconnect it first")
				return
			}
		}
		a.store.DB.Exec("DELETE FROM workspace_domains WHERE id=?", d.ID)
		a.audit(uid, "domain.delete", d.Name, scopeWorkspace, ws.ID, nil)
		reply(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 405, "method not allowed")
	}
}

func (a *Server) routeAllowedExcept(workspaceID, host, skip string) bool {
	for _, d := range a.domains(workspaceID) {
		if d.ID != skip && d.matches(host) {
			return true
		}
	}
	return false
}
