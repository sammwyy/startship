package server

import (
	"errors"
	"fmt"
	"maps"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/recipes"
	"github.com/sammwyy/startship/internal/routing"
)

func publicRecipeApp(app recipeApp, manage bool) recipeApp {
	if !manage {
		app.Config, app.Error = nil, ""
	}
	for id, status := range app.Containers {
		status.IP = ""
		app.Containers[id] = status
	}
	return app
}

func (a *Server) recipeApps(ws string, manage bool) map[string]any {
	apps := listResources[recipeApp](a, "recipe_apps", ws)
	for i, app := range apps {
		apps[i] = publicRecipeApp(app, manage)
	}
	return map[string]any{"apps": apps, "can_manage": manage}
}

func (a *Server) publicRouteRecords() []Record {
	out := a.store.List("routes", "")
	for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
		for _, d := range app.Domains {
			out = append(out, Record{ID: d.ID, Host: d.Host, PathPrefix: "/", WorkspaceID: app.WorkspaceID, CertificateID: d.CertificateID, ForceHTTPS: d.ForceHTTPS})
		}
	}
	return out
}

func recipePortUsed(app recipeApp, n int, proto string) bool {
	return slices.ContainsFunc(app.Recipe.Ports, func(p recipes.Port) bool {
		return p.Protocol == proto && app.Ports[p.ID] == n
	})
}

func (a *Server) validateRecipePorts(app recipeApp) error {
	if len(app.Ports) != len(app.Recipe.Ports) {
		return fmt.Errorf("unknown published port")
	}
	others := listResources[recipeApp](a, "recipe_apps", "")
	services := listResources[Service](a, "services", "")
	used := map[string]bool{}
	for _, p := range app.Recipe.Ports {
		n, ok := app.Ports[p.ID]
		if !ok {
			return fmt.Errorf("unknown published port")
		}
		if n < 0 || n > 65535 {
			return fmt.Errorf("invalid host port for %s", p.ID)
		}
		if n == 0 {
			continue
		}
		key := fmt.Sprintf("%d/%s", n, p.Protocol)
		if used[key] || a.daemonPortReserved(n) ||
			slices.ContainsFunc(others, func(other recipeApp) bool { return other.ID != app.ID && recipePortUsed(other, n, p.Protocol) }) ||
			(p.Protocol == "tcp" && slices.ContainsFunc(services, func(s Service) bool { return s.ExternalPort == n })) {
			return fmt.Errorf("host port %s is already in use", key)
		}
		used[key] = true
	}
	return nil
}

func (a *Server) recipeAppsAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) < 4 || p[1] != "workspaces" || p[3] != "apps" {
		return false
	}
	ws, err := a.store.Get(p[2])
	if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
		fail(w, 404, "not found")
		return true
	}
	manage := x.at(ws.ID, "")["apps.manage"]
	if len(p) == 4 && r.Method == "GET" {
		reply(w, 200, a.recipeApps(ws.ID, manage))
		return true
	}
	if !manage && (r.Method != "GET" || len(p) > 5) {
		forbidden(w)
		return true
	}
	if len(p) == 4 {
		if r.Method != "POST" {
			fail(w, 405, "method not allowed")
			return true
		}
		a.installRecipeApp(w, r, ws.ID, uid)
		return true
	}
	id := p[4]
	if r.Method == "GET" && len(p) == 5 {
		app, err := getResource[recipeApp](a, "recipe_apps", id)
		if err != nil || app.WorkspaceID != ws.ID {
			fail(w, 404, "not found")
		} else {
			reply(w, 200, publicRecipeApp(app, manage))
		}
		return true
	}
	lock := serviceLock("recipe:" + id)
	if !lock.TryLock() {
		fail(w, 409, "app is busy")
		return true
	}
	defer lock.Unlock()
	app, err := getResource[recipeApp](a, "recipe_apps", id)
	if err != nil || app.WorkspaceID != ws.ID {
		fail(w, 404, "not found")
		return true
	}
	if app.Action != "" {
		fail(w, 409, "app is busy")
		return true
	}
	action := ""
	switch {
	case len(p) == 5 && r.Method == "DELETE":
		if err := a.removeRecipeApp(app, r.URL.Query().Get("purge") == "true"); err != nil {
			fail(w, 502, err.Error())
			return true
		}
		a.certMu.Lock()
		_, err := a.store.DB.Exec("DELETE FROM recipe_apps WHERE id=?", id)
		a.certMu.Unlock()
		if err != nil {
			fail(w, 500, "delete failed")
			return true
		}
		a.store.Notify(Record{ID: id, Kind: "recipe_apps", WorkspaceID: ws.ID})
		a.refresh()
		a.audit(r.Context(), uid, "app.delete", id, scopeWorkspace, ws.ID, nil)
		reply(w, 200, map[string]bool{"ok": true})
		return true
	case len(p) == 6 && r.Method == "POST" && slices.Contains([]string{"start", "stop", "restart", "update"}, p[5]):
		action = p[5]
		if action != "update" {
			app.Running = action != "stop"
		}
	case len(p) == 6 && p[5] == "config" && r.Method == "GET":
		reply(w, 200, app.Config)
		return true
	case len(p) == 6 && p[5] == "config" && r.Method == "PATCH":
		var in struct {
			Config map[string]any `json:"config"`
			Ports  map[string]int `json:"ports"`
		}
		if decode(w, r, &in) != nil || (in.Config == nil && in.Ports == nil) {
			fail(w, 400, "invalid app config")
			return true
		}
		if in.Config != nil {
			app.Config, err = app.Recipe.Config.Validate(in.Config, true)
			if err != nil {
				fail(w, 400, err.Error())
				return true
			}
		}
		if in.Ports != nil {
			a.resourcesMu.Lock()
			defer a.resourcesMu.Unlock()
			maps.Copy(app.Ports, in.Ports)
			if err = a.validateRecipePorts(app); err != nil {
				fail(w, 409, err.Error())
				return true
			}
		}
		action = "configure"
	case len(p) == 7 && p[5] == "files":
		restart := a.recipeAppFileAPI(w, r, app, p[6], uid)
		if !restart {
			return true
		}
		action = "restart"
	case len(p) >= 6 && p[5] == "domains":
		a.recipeAppDomainsAPI(w, r, p, app, uid, x.admin)
		return true
	default:
		fail(w, 405, "method not allowed")
		return true
	}
	app.Action, app.Status, app.Error = action, "working", ""
	if err := a.saveRecipeApp(app); err != nil {
		fail(w, 500, "save failed")
		return true
	}
	a.launchRecipeApp(id)
	a.audit(r.Context(), uid, "app."+action, id, scopeWorkspace, ws.ID, nil)
	reply(w, 202, publicRecipeApp(app, true))
	return true
}

func (a *Server) installRecipeApp(w http.ResponseWriter, r *http.Request, ws, uid string) {
	var input struct {
		Name   string         `json:"name"`
		Recipe string         `json:"recipe"`
		Config map[string]any `json:"config"`
		Ports  map[string]int `json:"ports"`
	}
	if decode(w, r, &input) != nil || !serviceName.MatchString(input.Name) {
		fail(w, 400, "invalid app name")
		return
	}
	index, _ := a.recipeRegistry()
	recipe, ok := index[input.Recipe]
	if !ok {
		fail(w, 400, "unknown recipe")
		return
	}
	config, err := recipe.Config.Validate(input.Config, true)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	for _, other := range listResources[recipeApp](a, "recipe_apps", ws) {
		if other.Name == input.Name {
			fail(w, 409, "app name already in use")
			return
		}
	}
	ports := map[string]int{}
	for _, p := range recipe.Ports {
		ports[p.ID] = p.HostPort
	}
	maps.Copy(ports, input.Ports)
	app := recipeApp{ID: randomID(), WorkspaceID: ws, Name: input.Name, Recipe: recipe, Config: config, Ports: ports,
		Running: true, Action: "install", Status: "pending", Created: time.Now().UTC(), Domains: []recipeDomain{}, Containers: map[string]appStatus{}}
	if err := a.validateRecipePorts(app); err != nil {
		fail(w, 409, err.Error())
		return
	}
	if err := a.saveRecipeApp(app); err != nil {
		fail(w, 500, "save failed")
		return
	}
	a.launchRecipeApp(app.ID)
	a.audit(r.Context(), uid, "app.install", app.ID, scopeWorkspace, ws, map[string]string{"recipe": recipe.ID})
	reply(w, 201, app)
}

func (a *Server) recipeAppFileAPI(w http.ResponseWriter, r *http.Request, app recipeApp, id, uid string) bool {
	index := slices.IndexFunc(app.Recipe.Files, func(f recipes.File) bool { return f.ID == id })
	if index < 0 {
		fail(w, 404, "undeclared config file")
		return false
	}
	f := app.Recipe.Files[index]
	if r.Method != "GET" && r.Method != "PUT" {
		fail(w, 405, "method not allowed")
		return false
	}
	existing, err := a.appFile(app, f, "read", "")
	if err != nil && !errors.Is(err, errRecipeFileMissing) {
		fail(w, 502, err.Error())
		return false
	}
	if r.Method == "GET" {
		var values map[string]any
		if errors.Is(err, errRecipeFileMissing) {
			values, err = f.Schema.Validate(f.Defaults, true)
		} else {
			values, err = f.Read(existing)
		}
		if err != nil {
			fail(w, 422, "config file does not match its schema")
		} else {
			reply(w, 200, values)
		}
		return false
	}
	var input struct {
		Values  map[string]any `json:"values"`
		Restart bool           `json:"restart"`
	}
	if decode(w, r, &input) != nil || input.Values == nil {
		fail(w, 400, "invalid config")
		return false
	}
	content, err := f.Write(existing, input.Values)
	if err != nil {
		fail(w, 400, err.Error())
		return false
	}
	if _, err := a.appFile(app, f, "write", content); err != nil {
		fail(w, 502, err.Error())
		return false
	}
	a.audit(r.Context(), uid, "app.file.write", app.ID, scopeWorkspace, app.WorkspaceID, map[string]string{"file": id})
	if input.Restart && app.Running {
		return true
	}
	reply(w, 200, map[string]bool{"ok": true})
	return false
}

func (a *Server) recipeAppDomainsAPI(w http.ResponseWriter, r *http.Request, p []string, app recipeApp, uid string, admin bool) {
	a.certMu.Lock()
	defer a.certMu.Unlock()
	action := "add"
	var domain recipeDomain
	switch {
	case len(p) == 6 && r.Method == "GET":
		reply(w, 200, app.Domains)
		return
	case len(p) == 6 && r.Method == "POST":
		if decode(w, r, &domain) != nil || !slices.ContainsFunc(app.Recipe.HTTP, func(e recipes.Endpoint) bool { return e.ID == domain.Endpoint }) {
			fail(w, 400, "invalid HTTP endpoint")
			return
		}
		domain.Host = routing.NormalizeHost(strings.TrimSpace(domain.Host))
		if len(domain.Host) > 253 || !domainPattern.MatchString(domain.Host) {
			fail(w, 400, "enter a hostname like app.example.com")
			return
		}
		if !admin && !a.routeAllowed(app.WorkspaceID, domain.Host) {
			fail(w, 403, domain.Host+" is not one of this workspace's domains")
			return
		}
		if domain.CertificateID != "" {
			c, err := a.certificate(domain.CertificateID)
			if err != nil || c.WorkspaceID != app.WorkspaceID || !c.covers(domain.Host) {
				fail(w, 400, "certificate must belong to the workspace and cover the route host")
				return
			}
		}
		if domain.Host == routing.NormalizeHost(a.dashboardDomain) || slices.ContainsFunc(a.publicRouteRecords(), func(route Record) bool { return route.Host == domain.Host && route.PathPrefix == "/" }) {
			fail(w, 409, "route already exists")
			return
		}
		domain.ID = randomID()
		app.Domains = append(app.Domains, domain)
	case len(p) == 7 && r.Method == "DELETE":
		idx := slices.IndexFunc(app.Domains, func(d recipeDomain) bool { return d.ID == p[6] })
		if idx < 0 {
			fail(w, 404, "domain not found")
			return
		}
		domain, action = app.Domains[idx], "remove"
		app.Domains = slices.Delete(app.Domains, idx, idx+1)
	default:
		fail(w, 405, "method not allowed")
		return
	}
	if err := a.saveRecipeApp(app); err != nil {
		fail(w, 500, "save failed")
		return
	}
	a.refresh()
	a.audit(r.Context(), uid, "app.domain."+action, app.ID, scopeWorkspace, app.WorkspaceID, map[string]string{"host": domain.Host})
	if action == "add" {
		reply(w, 201, domain)
	} else {
		reply(w, 200, map[string]bool{"ok": true})
	}
}
