package server

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Permissions are fixed in code; roles switch them on and overrides allow or deny them per scope.
type Permission struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Group       string `json:"group"`
	Scope       string `json:"scope"`
}

const (
	scopePlatform     = "platform"
	scopeWorkspace    = "workspace"
	scopeProject      = "project"
	platformAdminRole = "platform-admin"
)

var permissionCatalog = []Permission{
	{"firewall.view", "View firewall", "Read workspace firewall policies, lists, events and statistics.", "Workspace", scopeWorkspace},
	{"firewall.manage", "Manage firewall", "Change workspace firewall policies, rules, lists and bans.", "Workspace", scopeWorkspace},
	{"network.firewall", "Manage network firewall", "Manage host-wide network firewall rules.", "Platform", scopePlatform},
	{"services.manage", "Manage services", "Deploy services from the catalog, manage volumes and their credentials.", "Workspace", scopeWorkspace},
	{"workspaces.create", "Create workspaces", "Create new workspaces and administer them.", "Platform", scopePlatform},
	{"users.manage", "Manage users", "Create users, reset passwords, delete accounts and assign platform roles.", "Platform", scopePlatform},
	{"roles.manage", "Manage roles", "Create, edit and delete custom roles. Platform permissions stay reserved to Platform Admins.", "Platform", scopePlatform},
	{"audit.view", "View audit log", "Read every change to users, roles, members and overrides.", "Platform", scopePlatform},
	{"workspace.manage", "Manage workspace", "Rename and delete the workspace.", "Workspace", scopeWorkspace},
	{"workspace.domains", "Manage domains", "Add and remove the domains projects in the workspace may route.", "Workspace", scopeWorkspace},
	{"projects.create", "Create projects", "Add projects to the workspace.", "Workspace", scopeWorkspace},
	{"members.manage", "Manage members", "Add members, assign roles and edit permission overrides.", "Workspace", scopeProject},
	{"project.view", "View projects", "See projects, containers, releases and domains.", "Projects", scopeProject},
	{"logs.view", "View logs", "Read build output and runtime logs.", "Projects", scopeProject},
	{"activity.view", "View activity", "Read workspace-wide or project-specific action history at the granted scope.", "Workspace", scopeProject},
	{"traffic.view", "View traffic", "See HTTP traffic, analytics and resource usage.", "Projects", scopeProject},
	{"deploy", "Deploy", "Deploy new releases and redeploy earlier ones.", "Projects", scopeProject},
	{"project.settings", "Edit settings", "Change build settings, root directory, port and tests.", "Projects", scopeProject},
	{"project.environment", "Manage environment", "View and edit environment variables and secrets.", "Projects", scopeProject},
	{"project.source", "Manage source", "Change repository, branch, access token and webhook secret.", "Projects", scopeProject},
	{"domains.manage", "Manage domains", "Connect and disconnect domain routes.", "Projects", scopeProject},
	{"project.delete", "Delete projects", "Remove projects and their containers.", "Projects", scopeProject},
}

var permissionByKey = func() map[string]Permission {
	out := map[string]Permission{}
	for _, p := range permissionCatalog {
		out[p.Key] = p
	}
	return out
}()

var roleIcons = []string{"crown", "shield", "wrench", "eye", "user", "star", "bolt", "rocket", "code", "key", "flag", "heart", "globe", "terminal"}

type Role struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Color       string   `json:"color"`
	Icon        string   `json:"icon"`
	Permissions []string `json:"permissions"`
	Builtin     bool     `json:"builtin"`
	Fixed       bool     `json:"fixed,omitempty"`
	Members     int      `json:"members"`
	Created     string   `json:"created,omitempty"`
}

type assignment struct {
	ID        string `json:"id"`
	UserID    string `json:"user_id"`
	Username  string `json:"username,omitempty"`
	RoleID    string `json:"role_id"`
	Scope     string `json:"scope"`
	ScopeID   string `json:"scope_id"`
	Created   string `json:"created"`
	CreatedBy string `json:"created_by,omitempty"`
}

type override struct {
	Scope       string   `json:"scope"`
	ScopeID     string   `json:"scope_id"`
	SubjectType string   `json:"subject_type"`
	SubjectID   string   `json:"subject_id"`
	Allow       []string `json:"allow"`
	Deny        []string `json:"deny"`
	Updated     string   `json:"updated,omitempty"`
	UpdatedBy   string   `json:"updated_by,omitempty"`
}

func allPermissionKeys() []string {
	out := make([]string, len(permissionCatalog))
	for i, p := range permissionCatalog {
		out[i] = p.Key
	}
	return out
}

var (
	projectPermissions = []string{"project.view", "logs.view", "traffic.view", "deploy", "project.settings", "project.environment", "project.source", "domains.manage"}
	builtinRoles       = []Role{
		{ID: platformAdminRole, Name: "Platform Admin", Color: "#f5a524", Icon: "crown", Permissions: allPermissionKeys(), Fixed: true},
		{ID: "administrator", Name: "Administrator", Color: "#e5484d", Icon: "shield", Permissions: append([]string{"activity.view", "firewall.view", "firewall.manage", "services.manage", "workspace.manage", "workspace.domains", "projects.create", "members.manage", "project.delete"}, projectPermissions...)},
		{ID: "contributor", Name: "Contributor", Color: "#3e63dd", Icon: "wrench", Permissions: append([]string{"firewall.view", "services.manage", "projects.create"}, projectPermissions...)},
		{ID: "member", Name: "Member", Color: "#30a46c", Icon: "eye", Permissions: []string{"project.view", "logs.view", "traffic.view"}},
		{ID: "guest", Name: "Guest", Color: "#8b8d98", Icon: "user", Permissions: []string{}},
	}
	roleColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
)

func nowStamp() string { return time.Now().UTC().Format(time.RFC3339) }

func (a *Server) roles() map[string]Role {
	out := map[string]Role{}
	for _, r := range a.roleList() {
		out[r.ID] = r
	}
	return out
}

func (a *Server) roleList() []Role {
	counts := map[string]int{}
	if rows, err := a.store.DB.Query("SELECT role_id,count(DISTINCT user_id) FROM role_assignments GROUP BY role_id"); err == nil {
		for rows.Next() {
			var id string
			var n int
			if rows.Scan(&id, &n) == nil {
				counts[id] = n
			}
		}
		rows.Close()
	}
	out := []Role{}
	for _, r := range builtinRoles {
		r.Builtin, r.Members, r.Permissions = true, counts[r.ID], slices.Clone(r.Permissions)
		out = append(out, r)
	}
	rows, err := a.store.DB.Query("SELECT id,name,color,icon,permissions,created FROM roles ORDER BY created,rowid")
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var r Role
		var perms string
		if rows.Scan(&r.ID, &r.Name, &r.Color, &r.Icon, &perms, &r.Created) == nil {
			json.Unmarshal([]byte(perms), &r.Permissions)
			r.Permissions, r.Members = normalizePermissions(r.Permissions), counts[r.ID]
			out = append(out, r)
		}
	}
	return out
}

// normalizePermissions drops unknown keys and keeps catalog order.
func normalizePermissions(keys []string) []string {
	out := []string{}
	for _, p := range permissionCatalog {
		if slices.Contains(keys, p.Key) {
			out = append(out, p.Key)
		}
	}
	return out
}

func validPermissions(keys []string) error {
	for _, k := range keys {
		if _, ok := permissionByKey[k]; !ok {
			return fmt.Errorf("unknown permission %q", k)
		}
	}
	return nil
}

func (a *Server) assignments(where string, args ...any) []assignment {
	rows, err := a.store.DB.Query("SELECT a.id,a.user_id,coalesce(u.username,''),a.role_id,a.scope,a.scope_id,a.created,a.created_by FROM role_assignments a LEFT JOIN users u ON u.id=a.user_id WHERE "+where+" ORDER BY a.created,a.rowid", args...)
	out := []assignment{}
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v assignment
		if rows.Scan(&v.ID, &v.UserID, &v.Username, &v.RoleID, &v.Scope, &v.ScopeID, &v.Created, &v.CreatedBy) == nil {
			out = append(out, v)
		}
	}
	return out
}

func (a *Server) overrides(scope, scopeID string) []override {
	rows, err := a.store.DB.Query("SELECT scope,scope_id,subject_type,subject_id,allow,deny,updated,updated_by FROM permission_overrides WHERE scope=? AND scope_id=? ORDER BY updated", scope, scopeID)
	out := []override{}
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var v override
		var allow, deny string
		if rows.Scan(&v.Scope, &v.ScopeID, &v.SubjectType, &v.SubjectID, &allow, &deny, &v.Updated, &v.UpdatedBy) == nil {
			json.Unmarshal([]byte(allow), &v.Allow)
			json.Unmarshal([]byte(deny), &v.Deny)
			out = append(out, v)
		}
	}
	return out
}

func (a *Server) platformAdmins() int {
	var n int
	a.store.DB.QueryRow("SELECT count(DISTINCT a.user_id) FROM role_assignments a JOIN users u ON u.id=a.user_id WHERE a.role_id=? AND a.scope=?", platformAdminRole, scopePlatform).Scan(&n)
	return n
}

func (a *Server) username(uid string) string {
	var name string
	a.store.DB.QueryRow("SELECT username FROM users WHERE id=?", uid).Scan(&name)
	return name
}

// access evaluates one user's permissions for the duration of a request.
type access struct {
	a        *Server
	uid      string
	admin    bool
	held     []assignment
	roles    map[string]Role
	cache    map[string]map[string]bool
	ovr      map[string][]override
	projects map[string]Record
	token    *tokenScope
	root     bool
}

func (a *Server) access(uid string) *access {
	x := &access{a: a, uid: uid, held: a.assignments("a.user_id=?", uid), roles: a.roles(), cache: map[string]map[string]bool{}, ovr: map[string][]override{}, projects: map[string]Record{}}
	for _, v := range x.held {
		if v.RoleID == platformAdminRole && v.Scope == scopePlatform {
			x.admin = true
		}
	}
	return x
}

// accessFor is the caller's access: a personal access token keeps only the permissions its scope
// allows, so it never reaches further than its owner or than what it was created for.
func (a *Server) accessFor(s sessionIdentity) *access {
	x := a.access(s.userID)
	if s.token != nil {
		x.token, x.admin = s.token, false
		for _, v := range x.held {
			if v.RoleID == platformAdminRole && v.Scope == scopePlatform {
				x.root = true
			}
		}
	}
	return x
}

func (x *access) overridesAt(scope, id string) []override {
	key := scope + "/" + id
	if v, ok := x.ovr[key]; ok {
		return v
	}
	x.ovr[key] = x.a.overrides(scope, id)
	return x.ovr[key]
}

// at computes Discord-style effective permissions: roles held at the platform, the workspace
// and the project add up, then overrides on the workspace and then the project deny and allow,
// role overrides before the user's own, so the narrowest and most personal rule wins.
func (x *access) at(workspaceID, projectID string) map[string]bool {
	key := workspaceID + "/" + projectID
	if v, ok := x.cache[key]; ok {
		return v
	}
	out := map[string]bool{}
	if x.admin || x.root {
		for _, p := range permissionCatalog {
			out[p.Key] = true
		}
		x.cache[key] = x.masked(out, workspaceID)
		return out
	}
	held := map[string]bool{}
	for _, v := range x.held {
		if !(v.Scope == scopePlatform || (workspaceID != "" && v.Scope == scopeWorkspace && v.ScopeID == workspaceID) || (projectID != "" && v.Scope == scopeProject && v.ScopeID == projectID)) {
			continue
		}
		held[v.RoleID] = true
		for _, k := range x.roles[v.RoleID].Permissions {
			if permissionByKey[k].Scope == scopePlatform && (v.Scope != scopePlatform || workspaceID != "" || projectID != "") {
				continue
			}
			out[k] = true
		}
	}
	apply := func(scope, id string) {
		var allow, deny []string
		for _, o := range x.overridesAt(scope, id) {
			if o.SubjectType == "role" && held[o.SubjectID] {
				allow, deny = append(allow, o.Allow...), append(deny, o.Deny...)
			}
		}
		for _, k := range deny {
			delete(out, k)
		}
		for _, k := range allow {
			out[k] = true
		}
		for _, o := range x.overridesAt(scope, id) {
			if o.SubjectType == "user" && o.SubjectID == x.uid {
				for _, k := range o.Deny {
					delete(out, k)
				}
				for _, k := range o.Allow {
					out[k] = true
				}
			}
		}
	}
	if workspaceID != "" {
		apply(scopeWorkspace, workspaceID)
	}
	if projectID != "" {
		apply(scopeProject, projectID)
	}
	x.cache[key] = x.masked(out, workspaceID)
	return out
}

func (x *access) masked(perms map[string]bool, workspaceID string) map[string]bool {
	if x.token != nil {
		for k := range perms {
			if !x.token.allows(k, workspaceID) {
				delete(perms, k)
			}
		}
	}
	return perms
}

func (x *access) list(workspaceID, projectID string) []string {
	perms := x.at(workspaceID, projectID)
	out := []string{}
	for _, p := range permissionCatalog {
		if perms[p.Key] && (p.Scope != scopePlatform || (workspaceID == "" && projectID == "")) {
			out = append(out, p.Key)
		}
	}
	return out
}

func (x *access) platform(perm string) bool { return x.at("", "")[perm] }

func (x *access) project(id string) Record {
	if p, ok := x.projects[id]; ok {
		return p
	}
	p, err := x.a.store.Get(id)
	if err != nil || p.Kind != "projects" {
		p = Record{}
	}
	x.projects[id] = p
	return p
}

func projectWorkspace(p Record) string {
	if p.WorkspaceID != "" {
		return p.WorkspaceID
	}
	return p.Parent
}

// scope resolves the workspace and project a record belongs to.
func (x *access) scope(r Record) (string, string) {
	switch r.Kind {
	case "workspaces":
		return r.ID, ""
	case "projects":
		return projectWorkspace(r), r.ID
	}
	id := r.ProjectID
	if id == "" && r.AppID != "" {
		if app, err := x.a.store.Get(r.AppID); err == nil {
			id = app.ProjectID
		}
	}
	p := x.project(id)
	if p.ID == "" {
		return "", ""
	}
	return projectWorkspace(p), p.ID
}

func (x *access) can(r Record, perm string) bool {
	w, p := x.scope(r)
	if w == "" {
		// Records outside any workspace predate roles: only their owner reaches them.
		return r.Owner != "" && r.Owner == x.uid && (x.token == nil || x.token.all && x.token.admin)
	}
	return x.at(w, p)[perm]
}

func (x *access) permissionsFor(r Record) []string {
	w, p := x.scope(r)
	if w == "" {
		out := []string{}
		for _, p := range permissionCatalog {
			if p.Scope != scopePlatform && x.can(r, p.Key) {
				out = append(out, p.Key)
			}
		}
		return out
	}
	return x.list(w, p)
}

func (x *access) memberOf(workspaceID string) bool {
	if x.token != nil && !x.token.reaches(workspaceID) {
		return false
	}
	if x.admin || x.root || len(x.list(workspaceID, "")) > 0 {
		return true
	}
	for _, v := range x.held {
		if (v.Scope == scopeWorkspace && v.ScopeID == workspaceID) || (v.Scope == scopeProject && projectWorkspace(x.project(v.ScopeID)) == workspaceID) {
			return true
		}
	}
	return false
}

func (x *access) visible(r Record) bool {
	if r.Kind == "workspaces" {
		return x.memberOf(r.ID)
	}
	return x.can(r, "project.view")
}

// grantable reports whether the caller holds every permission a role would grant at a scope,
// so nobody can hand out (or take away) more than they have.
func (x *access) grantable(perms []string, scope, workspaceID, projectID string) bool {
	if x.admin {
		return true
	}
	mine := x.at(workspaceID, projectID)
	for _, k := range perms {
		p := permissionByKey[k]
		if scope != scopePlatform && p.Scope == scopePlatform {
			continue
		}
		if scope == scopeProject && p.Scope == scopeWorkspace {
			continue
		}
		if !mine[k] {
			return false
		}
	}
	return true
}

func (a *Server) grant(tx *sql.Tx, uid, role, scope, scopeID, by string) (string, error) {
	id := randomID()
	_, err := tx.Exec("INSERT OR IGNORE INTO role_assignments VALUES(?,?,?,?,?,?,?)", id, uid, role, scope, scopeID, nowStamp(), by)
	return id, err
}

// removeScopeAccess drops assignments and overrides attached to a deleted workspace or project.
func (a *Server) removeScopeAccess(scope, id string) {
	a.store.DB.Exec("DELETE FROM role_assignments WHERE scope=? AND scope_id=?", scope, id)
	a.store.DB.Exec("DELETE FROM permission_overrides WHERE scope=? AND scope_id=?", scope, id)
}

func cleanName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 60 {
		return "", fmt.Errorf("role name must be 1-60 characters")
	}
	return name, nil
}

var projectFieldPermissions = map[string]string{"env": "project.environment", "git_url": "project.source", "git_token": "project.source", "branch": "project.source", "sleep_after": "project.settings"}

// canUpdate maps the fields a PATCH touches to the permissions that guard them.
func (x *access) canUpdate(r Record, fields map[string]json.RawMessage) bool {
	switch r.Kind {
	case "workspaces":
		return x.can(r, "workspace.manage")
	case "routes":
		return x.can(r, "domains.manage")
	case "apps":
		return x.can(r, "project.settings")
	}
	for field := range fields {
		perm := projectFieldPermissions[field]
		if perm == "" {
			perm = "project.settings"
		}
		if !x.can(r, perm) {
			return false
		}
	}
	return true
}

// publicRecord hides what the caller may not read and reports what they may do.
func (x *access) publicRecord(r Record) apiRecord {
	out := publicRecord(r)
	if r.Kind == "routes" && x.a.storedCertificate(r.Host) != nil {
		out.URL = certificateRouteURL(r)
	}
	if r.Kind == "projects" {
		if !x.can(r, "project.environment") {
			out.Env = nil
		}
		if !x.can(r, "project.source") {
			out.WebhookSecret = ""
		}
	}
	if (r.Kind == "deployments" || r.Kind == "releases") && !x.can(r, "logs.view") {
		out.Log = ""
	}
	if r.Kind == "workspaces" || r.Kind == "projects" {
		out.Permissions = x.permissionsFor(r)
	}
	return out
}
