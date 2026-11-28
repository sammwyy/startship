package server

import (
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Endpoint names a workload that can take part in connections and networks.
type Endpoint struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

// Connection lets From reach To by Alias over a dedicated bridge network.
type Connection struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	From        Endpoint  `json:"from"`
	To          Endpoint  `json:"to"`
	Alias       string    `json:"alias"`
	Created     time.Time `json:"created"`
	CreatedBy   string    `json:"created_by"`
}

// Network is a named workspace bridge whose members reach each other by hostname.
type Network struct {
	ID          string     `json:"id"`
	WorkspaceID string     `json:"workspace_id"`
	Name        string     `json:"name"`
	Members     []Endpoint `json:"members"`
	Created     time.Time  `json:"created"`
	CreatedBy   string     `json:"created_by"`
}

type workload struct {
	Container string
	Suffix    string
	Primary   bool
	Preview   bool
}

type netJoin struct {
	Network string
	Label   string
	Names   []string
}

var (
	networkName  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)
	networkingMu sync.Mutex
)

func connectionNetwork(id string) string { return "startship-link-" + id }
func workspaceNetwork(id string) string  { return "startship-net-" + id }
func envPrefix(alias string) string {
	return strings.ToUpper(strings.ReplaceAll(alias, "-", "_"))
}
func validEndpointKind(kind string) bool {
	return kind == "project" || kind == "app" || kind == "service"
}

func (a *Server) connections(ws string) []Connection {
	return listResources[Connection](a, "connections", ws)
}
func (a *Server) networks(ws string) []Network { return listResources[Network](a, "networks", ws) }
func (a *Server) saveConnection(c Connection) error {
	return a.saveResource("connections", c.ID, c.WorkspaceID, c)
}
func (a *Server) saveNetwork(n Network) error {
	return a.saveResource("networks", n.ID, n.WorkspaceID, n)
}

// endpointInfo resolves an endpoint's workspace, display name and hostname.
func (a *Server) endpointInfo(e Endpoint) (ws, name string, ok bool) {
	if a.store == nil {
		return "", "", false
	}
	switch e.Kind {
	case "project":
		p, err := a.store.Get(e.ID)
		if err != nil || p.Kind != "projects" {
			return "", "", false
		}
		return projectWorkspace(p), p.Name, true
	case "app":
		app, err := getResource[recipeApp](a, "recipe_apps", e.ID)
		return app.WorkspaceID, app.Name, err == nil
	case "service":
		s, err := getResource[Service](a, "services", e.ID)
		return s.WorkspaceID, s.Name, err == nil
	}
	return "", "", false
}

func endpointHost(kind, name string) string {
	if kind == "service" {
		return name
	}
	return appAlias(name)
}

func (a *Server) endpointAllowed(x *access, ws string, e Endpoint) bool {
	switch e.Kind {
	case "project":
		p, err := a.store.Get(e.ID)
		return err == nil && p.Kind == "projects" && x.can(p, "project.settings")
	case "app":
		return x.at(ws, "")["apps.manage"]
	case "service":
		return x.at(ws, "")["services.manage"]
	}
	return false
}

func recipePrimary(app recipeApp) string {
	if len(app.Recipe.HTTP) > 0 {
		return app.Recipe.HTTP[0].Container
	}
	if len(app.Recipe.Containers) > 0 {
		return app.Recipe.Containers[0].ID
	}
	return ""
}

// workloads lists the containers that currently stand for an endpoint.
func (a *Server) workloads(e Endpoint) []workload {
	out := []workload{}
	switch e.Kind {
	case "service":
		out = append(out, workload{Container: serviceContainer(e.ID), Primary: true})
	case "app":
		app, err := getResource[recipeApp](a, "recipe_apps", e.ID)
		if err != nil {
			return out
		}
		primary := recipePrimary(app)
		for _, c := range app.Recipe.Containers {
			out = append(out, workload{Container: recipeContainer(app.ID, c.ID), Suffix: c.ID, Primary: c.ID == primary})
		}
	case "project":
		for _, d := range a.store.List("deployments", "") {
			if d.ProjectID == e.ID && d.Status == "running" && d.Container != "" {
				out = append(out, a.projectWorkload(d.ProjectID, d.AppID, d.Container, d.PreviewID != ""))
			}
		}
	}
	return out
}

func (a *Server) projectWorkload(projectID, appID, container string, preview bool) workload {
	w := workload{Container: container, Preview: preview}
	apps := []Record{}
	for _, app := range a.store.List("apps", "") {
		if app.ProjectID == projectID {
			apps = append(apps, app)
		}
	}
	w.Primary = len(apps) == 0 || apps[0].ID == appID
	if len(apps) > 1 {
		for _, app := range apps {
			if app.ID == appID {
				w.Suffix = appAlias(app.Name)
			}
		}
	}
	return w
}

func (w workload) aliases(names []string) []string {
	if w.Preview {
		return nil
	}
	out := []string{}
	for _, n := range names {
		if w.Primary {
			out = append(out, n)
		}
		if w.Suffix != "" && !strings.Contains(n, ".") {
			out = append(out, n+"-"+w.Suffix)
		}
	}
	return out
}

// endpointJoins lists every network an endpoint belongs to and the names it answers to there.
func (a *Server) endpointJoins(e Endpoint) []netJoin {
	out := []netJoin{}
	ws, name, ok := a.endpointInfo(e)
	if !ok {
		return out
	}
	for _, c := range a.connections(ws) {
		if c.From == e {
			out = append(out, netJoin{Network: connectionNetwork(c.ID), Label: "startship.connection=" + c.ID})
		}
		if c.To == e {
			out = append(out, netJoin{Network: connectionNetwork(c.ID), Label: "startship.connection=" + c.ID, Names: []string{c.Alias, c.Alias + ".local"}})
		}
	}
	for _, n := range a.networks(ws) {
		if slices.Contains(n.Members, e) {
			host := endpointHost(e.Kind, name)
			out = append(out, netJoin{Network: workspaceNetwork(n.ID), Label: "startship.network=" + n.ID, Names: []string{host, host + ".local"}})
		}
	}
	return out
}

func (a *Server) ensureBridge(name, label string) error {
	if _, err := a.serviceDocker("network", "inspect", name); err == nil {
		return nil
	}
	_, err := a.serviceDocker("network", "create", "--driver", "bridge", "--label", "startship.managed=true", "--label", label, name)
	if err != nil {
		if _, inspect := a.serviceDocker("network", "inspect", name); inspect == nil {
			return nil
		}
	}
	return err
}

func alreadyJoined(err error) bool {
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "already exists") || strings.Contains(s, "already attached")
}

func (a *Server) joinWorkload(w workload, joins []netJoin) error {
	for _, j := range joins {
		if err := a.ensureBridge(j.Network, j.Label); err != nil {
			return err
		}
		args := []string{"network", "connect"}
		for _, alias := range w.aliases(j.Names) {
			args = append(args, "--alias", alias)
		}
		if _, err := a.serviceDocker(append(args, j.Network, w.Container)...); err != nil && !alreadyJoined(err) {
			return err
		}
	}
	return nil
}

// syncEndpoint attaches an endpoint's existing containers to all of its networks.
func (a *Server) syncEndpoint(e Endpoint) error {
	joins := a.endpointJoins(e)
	if len(joins) == 0 {
		return nil
	}
	var result error
	for _, w := range a.workloads(e) {
		if _, err := a.serviceDocker("container", "inspect", w.Container); err != nil {
			continue
		}
		if err := a.joinWorkload(w, joins); err != nil && result == nil {
			result = err
		}
	}
	return result
}

func (a *Server) leaveNetwork(network string, e Endpoint) {
	for _, w := range a.workloads(e) {
		a.serviceDocker("network", "disconnect", "-f", network, w.Container)
	}
}

func (a *Server) removeBridge(network string, members ...Endpoint) {
	for _, e := range members {
		a.leaveNetwork(network, e)
	}
	if _, err := a.serviceDocker("network", "inspect", network); err == nil {
		a.serviceDocker("network", "rm", network)
	}
}

func (a *Server) removeConnection(c Connection) {
	a.removeBridge(connectionNetwork(c.ID), c.From, c.To)
	a.deleteResource("connections", c.ID, c.WorkspaceID)
	a.notifyEndpoint(c.From)
}

func (a *Server) notifyEndpoint(e Endpoint) {
	if e.Kind == "project" {
		a.notifyProject(e.ID)
	}
}

// forgetEndpoint drops the connections and network memberships of a removed workload.
func (a *Server) forgetEndpoint(ws string, e Endpoint) {
	networkingMu.Lock()
	defer networkingMu.Unlock()
	for _, c := range a.connections(ws) {
		if c.From == e || c.To == e {
			a.removeConnection(c)
		}
	}
	for _, n := range a.networks(ws) {
		if slices.Contains(n.Members, e) {
			a.leaveNetwork(workspaceNetwork(n.ID), e)
			n.Members = slices.DeleteFunc(n.Members, func(m Endpoint) bool { return m == e })
			a.saveNetwork(n)
		}
	}
}

// connectionEnv is the environment a consumer receives for its outgoing connections.
func (a *Server) connectionEnv(e Endpoint) map[string]string {
	env := map[string]string{}
	ws, _, ok := a.endpointInfo(e)
	if !ok {
		return env
	}
	for _, c := range a.connections(ws) {
		if c.From == e {
			for k, v := range a.providedEnv(c) {
				env[k] = v
			}
		}
	}
	return env
}

func (a *Server) providedEnv(c Connection) map[string]string {
	prefix := envPrefix(c.Alias) + "_"
	port := 0
	switch c.To.Kind {
	case "service":
		s, err := getResource[Service](a, "services", c.To.ID)
		if err != nil {
			return nil
		}
		if e, ok := s.engineDefinition(); ok {
			return e.Connection(c.Alias, e.Port, s.Config)
		}
		return nil
	case "project":
		for _, app := range a.store.List("apps", "") {
			if app.ProjectID == c.To.ID && app.Port != 0 {
				port = app.Port
				break
			}
		}
	case "app":
		if app, err := getResource[recipeApp](a, "recipe_apps", c.To.ID); err == nil && len(app.Recipe.HTTP) > 0 {
			port = app.Recipe.HTTP[0].Port
		}
	}
	out := map[string]string{prefix + "HOST": c.Alias}
	if port != 0 {
		out[prefix+"PORT"] = strconv.Itoa(port)
		out[prefix+"URL"] = fmt.Sprintf("http://%s:%d", c.Alias, port)
	}
	return out
}

func maskConnectionEnv(env map[string]string) {
	for k := range env {
		if strings.HasSuffix(k, "_PASSWORD") || strings.HasSuffix(k, "_URL") || strings.HasSuffix(k, "_SECRET_KEY") {
			env[k] = "********"
		}
	}
}

type connectionView struct {
	Connection
	Env map[string]string `json:"env"`
}

func (a *Server) connectionViews(ws string, x *access) []connectionView {
	reveal := x.at(ws, "")["services.manage"]
	out := []connectionView{}
	for _, c := range a.connections(ws) {
		env := a.providedEnv(c)
		if env == nil {
			env = map[string]string{}
		}
		if !reveal {
			maskConnectionEnv(env)
		}
		out = append(out, connectionView{c, env})
	}
	return out
}

func (a *Server) connectionConflict(c Connection) string {
	for _, other := range a.connections(c.WorkspaceID) {
		if other.From == c.From && other.To == c.To {
			return "these resources are already connected"
		}
		if other.From == c.From && envPrefix(other.Alias) == envPrefix(c.Alias) {
			return "alias already used by another connection of this resource"
		}
	}
	return ""
}

func (a *Server) networkingAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) == 4 && p[1] == "workspaces" && (p[3] == "connections" || p[3] == "networks") {
		ws, err := a.store.Get(p[2])
		if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
			fail(w, 404, "not found")
			return true
		}
		switch {
		case r.Method == "GET" && p[3] == "connections":
			reply(w, 200, map[string]any{"connections": a.connectionViews(ws.ID, x)})
		case r.Method == "GET":
			reply(w, 200, map[string]any{"networks": a.networks(ws.ID)})
		case r.Method == "POST" && p[3] == "connections":
			a.createConnection(w, r, ws.ID, uid, x)
		case r.Method == "POST":
			var in struct{ Name string }
			if decode(w, r, &in) != nil || !networkName.MatchString(in.Name) {
				fail(w, 400, "invalid network name")
				return true
			}
			if !x.at(ws.ID, "")["networks.manage"] {
				forbidden(w)
				return true
			}
			networkingMu.Lock()
			defer networkingMu.Unlock()
			for _, n := range a.networks(ws.ID) {
				if n.Name == in.Name {
					fail(w, 409, "a network with that name already exists")
					return true
				}
			}
			n := Network{ID: randomID(), WorkspaceID: ws.ID, Name: in.Name, Members: []Endpoint{}, Created: time.Now().UTC(), CreatedBy: uid}
			if a.saveNetwork(n) != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.audit(r.Context(), uid, "network.create", n.Name, scopeWorkspace, ws.ID, nil)
			reply(w, 200, n)
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	if len(p) == 3 && p[1] == "connections" {
		if r.Method != "DELETE" {
			fail(w, 405, "method not allowed")
			return true
		}
		networkingMu.Lock()
		defer networkingMu.Unlock()
		c, err := getResource[Connection](a, "connections", p[2])
		if err != nil || !x.memberOf(c.WorkspaceID) {
			fail(w, 404, "not found")
			return true
		}
		if !a.endpointAllowed(x, c.WorkspaceID, c.From) || !a.endpointAllowed(x, c.WorkspaceID, c.To) {
			forbidden(w)
			return true
		}
		a.removeConnection(c)
		a.audit(r.Context(), uid, "connection.delete", c.Alias, scopeWorkspace, c.WorkspaceID, c)
		reply(w, 200, map[string]bool{"ok": true})
		return true
	}
	if len(p) >= 3 && p[1] == "networks" {
		networkingMu.Lock()
		defer networkingMu.Unlock()
		n, err := getResource[Network](a, "networks", p[2])
		if err != nil || !x.memberOf(n.WorkspaceID) {
			fail(w, 404, "not found")
			return true
		}
		if !x.at(n.WorkspaceID, "")["networks.manage"] {
			forbidden(w)
			return true
		}
		switch {
		case len(p) == 3 && r.Method == "PATCH":
			var in struct{ Name string }
			if decode(w, r, &in) != nil || !networkName.MatchString(in.Name) {
				fail(w, 400, "invalid network name")
				return true
			}
			for _, other := range a.networks(n.WorkspaceID) {
				if other.ID != n.ID && other.Name == in.Name {
					fail(w, 409, "a network with that name already exists")
					return true
				}
			}
			n.Name = in.Name
			if a.saveNetwork(n) != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.audit(r.Context(), uid, "network.rename", n.Name, scopeWorkspace, n.WorkspaceID, nil)
			reply(w, 200, n)
		case len(p) == 3 && r.Method == "DELETE":
			a.removeBridge(workspaceNetwork(n.ID), n.Members...)
			a.deleteResource("networks", n.ID, n.WorkspaceID)
			a.audit(r.Context(), uid, "network.delete", n.Name, scopeWorkspace, n.WorkspaceID, nil)
			reply(w, 200, map[string]bool{"ok": true})
		case len(p) == 4 && p[3] == "members" && r.Method == "POST", len(p) == 6 && p[3] == "members" && r.Method == "DELETE":
			var e Endpoint
			if len(p) == 6 {
				e = Endpoint{p[4], p[5]}
			} else if decode(w, r, &e) != nil {
				fail(w, 400, "invalid member")
				return true
			}
			ws, _, ok := a.endpointInfo(e)
			if !validEndpointKind(e.Kind) || !ok || ws != n.WorkspaceID {
				fail(w, 400, "member must be a project, app or service in this workspace")
				return true
			}
			if !a.endpointAllowed(x, ws, e) {
				forbidden(w)
				return true
			}
			action := "network.join"
			if r.Method == "POST" {
				if slices.Contains(n.Members, e) {
					fail(w, 409, "already a member")
					return true
				}
				n.Members = append(n.Members, e)
			} else {
				action = "network.leave"
				n.Members = slices.DeleteFunc(n.Members, func(m Endpoint) bool { return m == e })
				a.leaveNetwork(workspaceNetwork(n.ID), e)
			}
			if a.saveNetwork(n) != nil {
				fail(w, 500, "save failed")
				return true
			}
			if r.Method == "POST" {
				if err := a.syncEndpoint(e); err != nil {
					fail(w, 502, err.Error())
					return true
				}
			}
			a.audit(r.Context(), uid, action, n.Name, scopeWorkspace, n.WorkspaceID, e)
			reply(w, 200, n)
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	return false
}

func (a *Server) createConnection(w http.ResponseWriter, r *http.Request, ws, uid string, x *access) {
	var c Connection
	if decode(w, r, &c) != nil || !serviceName.MatchString(c.Alias) {
		fail(w, 400, "invalid alias")
		return
	}
	if !validEndpointKind(c.From.Kind) || !validEndpointKind(c.To.Kind) || c.From == c.To {
		fail(w, 400, "connect two different projects, apps or services")
		return
	}
	for _, e := range []Endpoint{c.From, c.To} {
		if owner, _, ok := a.endpointInfo(e); !ok || owner != ws {
			fail(w, 400, "both resources must belong to this workspace")
			return
		}
		if !a.endpointAllowed(x, ws, e) {
			forbidden(w)
			return
		}
	}
	networkingMu.Lock()
	defer networkingMu.Unlock()
	c.ID, c.WorkspaceID, c.Created, c.CreatedBy = randomID(), ws, time.Now().UTC(), uid
	if msg := a.connectionConflict(c); msg != "" {
		fail(w, 409, msg)
		return
	}
	if a.saveConnection(c) != nil {
		fail(w, 500, "save failed")
		return
	}
	for _, e := range []Endpoint{c.To, c.From} {
		if err := a.syncEndpoint(e); err != nil {
			fail(w, 502, err.Error())
			return
		}
	}
	a.notifyEndpoint(c.From)
	a.audit(r.Context(), uid, "connection.create", c.Alias, scopeWorkspace, ws, c)
	reply(w, 200, c)
}

// createArgs turns `docker run -d` arguments into `docker create` so networks can be joined before start.
func createArgs(args []string) []string {
	out := []string{"create"}
	for _, v := range args[1:] {
		if v != "-d" {
			out = append(out, v)
		}
	}
	return out
}
