package server

import (
	"encoding/json"
	"net/http"
	"slices"
	"strings"
)

func forbidden(w http.ResponseWriter) { fail(w, 403, "you do not have permission to do that") }

// scopeTarget validates an assignment or override scope and returns its workspace and project.
func (a *Server) scopeTarget(x *access, scope, id string) (string, string, bool) {
	switch scope {
	case scopePlatform:
		return "", "", id == ""
	case scopeWorkspace:
		r, err := a.store.Get(id)
		return id, "", err == nil && r.Kind == "workspaces"
	case scopeProject:
		p := x.project(id)
		return projectWorkspace(p), p.ID, p.ID != ""
	}
	return "", "", false
}

func (x *access) manages(scope, workspaceID, projectID string) bool {
	if scope == scopePlatform {
		return x.platform("users.manage")
	}
	return x.at(workspaceID, projectID)["members.manage"]
}

// accessAPI serves users, roles, assignments, overrides, members and the audit log.
func (a *Server) accessAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	switch p[1] {
	case "permissions":
		reply(w, 200, map[string]any{"permissions": permissionCatalog, "icons": roleIcons})
	case "roles":
		a.rolesAPI(w, r, p, uid, x)
	case "users":
		a.usersAPI(w, r, p, uid, x)
	case "assignments":
		a.assignmentsAPI(w, r, p, uid, x)
	case "overrides":
		a.overridesAPI(w, r, uid, x)
	case "audit":
		a.auditAPI(w, r, x)
	case "effective":
		q := r.URL.Query()
		target, wid, pid := q.Get("user_id"), q.Get("workspace_id"), q.Get("project_id")
		if pid != "" {
			wid = projectWorkspace(x.project(pid))
		}
		if target == "" {
			target = uid
		}
		if target != uid && !x.admin && !x.platform("users.manage") && (wid == "" || !x.at(wid, pid)["members.manage"]) {
			forbidden(w)
			return true
		}
		reply(w, 200, map[string]any{"user_id": target, "permissions": a.access(target).list(wid, pid)})
	case "workspaces":
		if len(p) < 4 || p[3] != "members" && p[3] != "domains" || p[3] == "members" && len(p) != 4 || len(p) > 5 {
			return false
		}
		ws, err := a.store.Get(p[2])
		if err != nil || ws.Kind != "workspaces" || !x.memberOf(ws.ID) {
			fail(w, 404, "not found")
			return true
		}
		if p[3] == "domains" {
			a.domainsAPI(w, r, p, uid, ws, x)
			return true
		}
		a.membersAPI(w, ws, x)
	default:
		return false
	}
	return true
}

func (a *Server) membersAPI(w http.ResponseWriter, ws Record, x *access) {
	projects := map[string]string{}
	ids := []any{ws.ID}
	marks := []string{"?"}
	for _, p := range a.store.List("projects", "") {
		if projectWorkspace(p) == ws.ID {
			projects[p.ID] = p.Name
			ids = append(ids, p.ID)
			marks = append(marks, "?")
		}
	}
	type entry struct {
		assignment
		ProjectName string `json:"project_name,omitempty"`
	}
	type member struct {
		UserID   string  `json:"user_id"`
		Username string  `json:"username"`
		Roles    []entry `json:"roles"`
	}
	list := []*member{}
	byUser := map[string]*member{}
	for _, v := range a.assignments("((a.scope='workspace' AND a.scope_id=?) OR (a.scope='project' AND a.scope_id IN ("+strings.Join(append(marks[1:], "''"), ",")+"))) AND u.id IS NOT NULL", ids...) {
		m := byUser[v.UserID]
		if m == nil {
			m = &member{UserID: v.UserID, Username: v.Username}
			byUser[v.UserID] = m
			list = append(list, m)
		}
		m.Roles = append(m.Roles, entry{v, projects[v.ScopeID]})
	}
	reply(w, 200, map[string]any{"members": list, "can_manage": x.at(ws.ID, "")["members.manage"]})
}

func (a *Server) rolesAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) {
	roles := a.roles()
	if r.Method == "GET" {
		if len(p) == 3 {
			role, ok := roles[p[2]]
			if !ok {
				fail(w, 404, "role not found")
				return
			}
			reply(w, 200, role)
			return
		}
		reply(w, 200, a.roleList())
		return
	}
	if !x.platform("roles.manage") {
		forbidden(w)
		return
	}
	var old Role
	if len(p) == 3 {
		var ok bool
		if old, ok = roles[p[2]]; !ok {
			fail(w, 404, "role not found")
			return
		}
		if old.Builtin {
			fail(w, 409, "built-in roles cannot be changed; clone one instead")
			return
		}
	}
	if r.Method == "DELETE" && old.ID != "" {
		a.store.Lock()
		defer a.store.Unlock()
		tx, err := a.store.DB.Begin()
		if err != nil {
			fail(w, 500, "delete failed")
			return
		}
		defer tx.Rollback()
		tx.Exec("DELETE FROM role_assignments WHERE role_id=?", old.ID)
		tx.Exec("DELETE FROM permission_overrides WHERE subject_type='role' AND subject_id=?", old.ID)
		tx.Exec("DELETE FROM roles WHERE id=?", old.ID)
		if tx.Commit() != nil {
			fail(w, 500, "delete failed")
			return
		}
		a.audit(r.Context(), uid, "role.delete", old.Name, scopePlatform, "", map[string]any{"role_id": old.ID, "members": old.Members})
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	if (r.Method == "POST" && old.ID != "") || (r.Method != "POST" && (r.Method != "PATCH" || old.ID == "")) {
		fail(w, 405, "method not allowed")
		return
	}
	var v struct {
		Name        *string   `json:"name"`
		Color       *string   `json:"color"`
		Icon        *string   `json:"icon"`
		Permissions *[]string `json:"permissions"`
		CloneOf     string    `json:"clone_of"`
	}
	if decode(w, r, &v) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	next := old
	if old.ID == "" {
		next = Role{ID: randomID(), Color: "#7c5cff", Icon: "star", Permissions: []string{}, Created: nowStamp()}
		if v.CloneOf != "" {
			source, ok := roles[v.CloneOf]
			if !ok {
				fail(w, 400, "role to clone not found")
				return
			}
			next.Name, next.Color, next.Icon, next.Permissions = source.Name+" copy", source.Color, source.Icon, slices.Clone(source.Permissions)
		}
	}
	if v.Name != nil {
		next.Name = *v.Name
	}
	var err error
	if next.Name, err = cleanName(next.Name); err != nil {
		fail(w, 400, err.Error())
		return
	}
	if v.Color != nil {
		next.Color = strings.ToLower(*v.Color)
	}
	if !roleColor.MatchString(next.Color) {
		fail(w, 400, "color must be a #rrggbb hex value")
		return
	}
	if v.Icon != nil {
		next.Icon = *v.Icon
	}
	if !slices.Contains(roleIcons, next.Icon) {
		fail(w, 400, "unknown role icon")
		return
	}
	if v.Permissions != nil {
		if err = validPermissions(*v.Permissions); err != nil {
			fail(w, 400, err.Error())
			return
		}
		next.Permissions = normalizePermissions(*v.Permissions)
	}
	if !x.admin {
		for _, k := range next.Permissions {
			if permissionByKey[k].Scope == scopePlatform && !slices.Contains(old.Permissions, k) {
				fail(w, 403, "only a Platform Admin can grant platform permissions")
				return
			}
		}
	}
	perms, _ := json.Marshal(next.Permissions)
	if _, err = a.store.DB.Exec("INSERT INTO roles VALUES(?,?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET name=excluded.name,color=excluded.color,icon=excluded.icon,permissions=excluded.permissions", next.ID, next.Name, next.Color, next.Icon, string(perms), next.Created); err != nil {
		fail(w, 500, "save failed")
		return
	}
	action, detail := "role.create", map[string]any{"role_id": next.ID, "permissions": next.Permissions, "color": next.Color, "icon": next.Icon}
	if old.ID != "" {
		action = "role.update"
		detail["before"] = map[string]any{"name": old.Name, "permissions": old.Permissions, "color": old.Color, "icon": old.Icon}
	} else if v.CloneOf != "" {
		detail["clone_of"] = v.CloneOf
	}
	a.audit(r.Context(), uid, action, next.Name, scopePlatform, "", detail)
	reply(w, 200, a.roles()[next.ID])
}

func (a *Server) usersAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) {
	manage := x.platform("users.manage")
	if r.Method == "GET" && len(p) == 2 {
		type user struct {
			ID                 string   `json:"id"`
			Username           string   `json:"username"`
			MustChangePassword bool     `json:"must_change_password,omitempty"`
			PlatformRoles      []string `json:"platform_roles,omitempty"`
		}
		rows, err := a.store.DB.Query("SELECT id,username,must_change_password FROM users ORDER BY username COLLATE NOCASE")
		if err != nil {
			fail(w, 500, "users unavailable")
			return
		}
		defer rows.Close()
		out := []user{}
		for rows.Next() {
			var u user
			if rows.Scan(&u.ID, &u.Username, &u.MustChangePassword) == nil {
				if !manage {
					u.MustChangePassword = false
				}
				out = append(out, u)
			}
		}
		rows.Close()
		if manage {
			for i := range out {
				for _, v := range a.assignments("a.user_id=? AND a.scope='platform'", out[i].ID) {
					out[i].PlatformRoles = append(out[i].PlatformRoles, v.RoleID)
				}
			}
		}
		reply(w, 200, out)
		return
	}
	if !manage {
		forbidden(w)
		return
	}
	if r.Method == "POST" && len(p) == 2 {
		var v struct{ Username, Password string }
		if decode(w, r, &v) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		if len(v.Password) > 72 {
			fail(w, 400, "password must be between 8 and 72 bytes")
			return
		}
		id, err := a.insertUser(v.Username, v.Password, true, false)
		if err != nil {
			status := 400
			if strings.Contains(err.Error(), "UNIQUE") {
				status, err = 409, errUsernameTaken
			}
			fail(w, status, err.Error())
			return
		}
		a.audit(r.Context(), uid, "user.create", strings.TrimSpace(v.Username), scopePlatform, "", map[string]any{"user_id": id})
		reply(w, 201, map[string]string{"id": id, "username": strings.TrimSpace(v.Username)})
		return
	}
	if len(p) != 3 {
		fail(w, 404, "not found")
		return
	}
	target, name := p[2], a.username(p[2])
	if name == "" {
		fail(w, 404, "user not found")
		return
	}
	targetAdmin := a.access(target).admin
	if targetAdmin && !x.admin {
		fail(w, 403, "only a Platform Admin can change another Platform Admin")
		return
	}
	switch r.Method {
	case "PATCH":
		var v struct {
			Username *string `json:"username"`
			Password *string `json:"password"`
		}
		if decode(w, r, &v) != nil || (v.Username == nil && v.Password == nil) {
			fail(w, 400, "provide a username or password")
			return
		}
		changes := []string{}
		if v.Username != nil {
			next := strings.TrimSpace(*v.Username)
			if len(next) < 1 || len(next) > 100 {
				fail(w, 400, "username must be between 1 and 100 bytes")
				return
			}
			if _, err := a.store.DB.Exec("UPDATE users SET username=? WHERE id=?", next, target); err != nil {
				fail(w, 409, errUsernameTaken.Error())
				return
			}
			changes = append(changes, "username")
			name = next
		}
		if v.Password != nil {
			if err := a.resetPassword(target, *v.Password); err != nil {
				fail(w, 400, err.Error())
				return
			}
			changes = append(changes, "password")
		}
		a.audit(r.Context(), uid, "user.update", name, scopePlatform, "", map[string]any{"user_id": target, "changed": changes})
		reply(w, 200, map[string]string{"id": target, "username": name})
	case "DELETE":
		if target == uid {
			fail(w, 409, "you cannot delete your own account")
			return
		}
		a.store.Lock()
		defer a.store.Unlock()
		if targetAdmin && a.platformAdmins() <= 1 {
			fail(w, 409, "the last Platform Admin cannot be deleted")
			return
		}
		tx, err := a.store.DB.Begin()
		if err != nil {
			fail(w, 500, "delete failed")
			return
		}
		defer tx.Rollback()
		for _, q := range []string{"DELETE FROM sessions WHERE user_id=?", "DELETE FROM role_assignments WHERE user_id=?", "DELETE FROM permission_overrides WHERE subject_type='user' AND subject_id=?", "DELETE FROM users WHERE id=?"} {
			if _, err = tx.Exec(q, target); err != nil {
				fail(w, 500, "delete failed")
				return
			}
		}
		if tx.Commit() != nil {
			fail(w, 500, "delete failed")
			return
		}
		a.audit(r.Context(), uid, "user.delete", name, scopePlatform, "", map[string]any{"user_id": target})
		a.publishAccount(target)
		reply(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 405, "method not allowed")
	}
}

func (a *Server) assignmentsAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) {
	roles := a.roles()
	switch {
	case r.Method == "GET" && len(p) == 2:
		q := r.URL.Query()
		out := []assignment{}
		for _, v := range a.assignments("(?='' OR a.user_id=?) AND (?='' OR a.scope=?) AND (?='' OR a.scope_id=?)", q.Get("user_id"), q.Get("user_id"), q.Get("scope"), q.Get("scope"), q.Get("scope_id"), q.Get("scope_id")) {
			wid, pid, ok := a.scopeTarget(x, v.Scope, v.ScopeID)
			if v.UserID == uid || (ok && x.manages(v.Scope, wid, pid)) || x.platform("audit.view") {
				out = append(out, v)
			}
		}
		reply(w, 200, out)
	case r.Method == "POST" && len(p) == 2:
		var v assignment
		if decode(w, r, &v) != nil {
			fail(w, 400, "invalid JSON")
			return
		}
		role, ok := roles[v.RoleID]
		if !ok || a.username(v.UserID) == "" {
			fail(w, 400, "unknown user or role")
			return
		}
		wid, pid, ok := a.scopeTarget(x, v.Scope, v.ScopeID)
		if !ok || (role.ID == platformAdminRole && v.Scope != scopePlatform) {
			fail(w, 400, "invalid scope")
			return
		}
		if (role.ID == platformAdminRole && !x.admin) || !x.manages(v.Scope, wid, pid) || !x.grantable(role.Permissions, v.Scope, wid, pid) {
			forbidden(w)
			return
		}
		if len(a.assignments("a.user_id=? AND a.role_id=? AND a.scope=? AND a.scope_id=?", v.UserID, v.RoleID, v.Scope, v.ScopeID)) > 0 {
			fail(w, 409, "the user already has this role here")
			return
		}
		tx, err := a.store.DB.Begin()
		if err != nil {
			fail(w, 500, "save failed")
			return
		}
		defer tx.Rollback()
		id, err := a.grant(tx, v.UserID, v.RoleID, v.Scope, v.ScopeID, uid)
		if err != nil || tx.Commit() != nil {
			fail(w, 500, "save failed")
			return
		}
		a.audit(r.Context(), uid, "assignment.create", a.username(v.UserID)+" → "+role.Name, v.Scope, v.ScopeID, map[string]any{"assignment_id": id, "user_id": v.UserID, "role_id": role.ID})
		reply(w, 201, a.assignments("a.id=?", id)[0])
	case r.Method == "DELETE" && len(p) == 3:
		a.store.Lock()
		defer a.store.Unlock()
		found := a.assignments("a.id=?", p[2])
		if len(found) == 0 {
			fail(w, 404, "assignment not found")
			return
		}
		v, role := found[0], roles[found[0].RoleID]
		wid, pid, _ := a.scopeTarget(x, v.Scope, v.ScopeID)
		if (role.ID == platformAdminRole && !x.admin) || !x.manages(v.Scope, wid, pid) || !x.grantable(role.Permissions, v.Scope, wid, pid) {
			forbidden(w)
			return
		}
		if role.ID == platformAdminRole && a.platformAdmins() <= 1 {
			fail(w, 409, "at least one user must keep the Platform Admin role")
			return
		}
		a.store.DB.Exec("DELETE FROM role_assignments WHERE id=?", v.ID)
		a.audit(r.Context(), uid, "assignment.delete", v.Username+" → "+role.Name, v.Scope, v.ScopeID, map[string]any{"assignment_id": v.ID, "user_id": v.UserID, "role_id": role.ID})
		reply(w, 200, map[string]bool{"ok": true})
	default:
		fail(w, 405, "method not allowed")
	}
}

func (a *Server) overridesAPI(w http.ResponseWriter, r *http.Request, uid string, x *access) {
	if r.Method == "GET" {
		scope, id := r.URL.Query().Get("scope"), r.URL.Query().Get("scope_id")
		wid, pid, ok := a.scopeTarget(x, scope, id)
		if !ok || scope == scopePlatform || !x.memberOf(wid) || (pid != "" && !x.at(wid, pid)["project.view"] && !x.at(wid, pid)["members.manage"]) {
			fail(w, 404, "not found")
			return
		}
		reply(w, 200, a.overrides(scope, id))
		return
	}
	if r.Method != "PUT" {
		fail(w, 405, "method not allowed")
		return
	}
	var v override
	if decode(w, r, &v) != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	wid, pid, ok := a.scopeTarget(x, v.Scope, v.ScopeID)
	if !ok || v.Scope == scopePlatform {
		fail(w, 400, "overrides apply to a workspace or a project")
		return
	}
	subject := ""
	switch v.SubjectType {
	case "role":
		role, found := a.roles()[v.SubjectID]
		if !found || role.Fixed {
			fail(w, 400, "unknown role")
			return
		}
		subject = role.Name
	case "user":
		subject = a.username(v.SubjectID)
	}
	if subject == "" {
		fail(w, 400, "unknown role or user")
		return
	}
	if validPermissions(v.Allow) != nil || validPermissions(v.Deny) != nil {
		fail(w, 400, "unknown permission")
		return
	}
	v.Allow, v.Deny = normalizePermissions(v.Allow), normalizePermissions(v.Deny)
	for _, k := range append(slices.Clone(v.Allow), v.Deny...) {
		s := permissionByKey[k].Scope
		if s == scopePlatform || (v.Scope == scopeProject && s != scopeProject) || slices.Contains(v.Allow, k) && slices.Contains(v.Deny, k) {
			fail(w, 400, "permission "+k+" cannot be overridden here")
			return
		}
	}
	if !x.at(wid, pid)["members.manage"] || !x.grantable(append(slices.Clone(v.Allow), v.Deny...), v.Scope, wid, pid) {
		forbidden(w)
		return
	}
	target := v.SubjectType + " " + subject
	if len(v.Allow) == 0 && len(v.Deny) == 0 {
		a.store.DB.Exec("DELETE FROM permission_overrides WHERE scope=? AND scope_id=? AND subject_type=? AND subject_id=?", v.Scope, v.ScopeID, v.SubjectType, v.SubjectID)
		a.audit(r.Context(), uid, "override.delete", target, v.Scope, v.ScopeID, map[string]any{"subject_type": v.SubjectType, "subject_id": v.SubjectID})
		reply(w, 200, map[string]bool{"ok": true})
		return
	}
	allow, _ := json.Marshal(v.Allow)
	deny, _ := json.Marshal(v.Deny)
	if _, err := a.store.DB.Exec("INSERT INTO permission_overrides VALUES(?,?,?,?,?,?,?,?) ON CONFLICT DO UPDATE SET allow=excluded.allow,deny=excluded.deny,updated=excluded.updated,updated_by=excluded.updated_by", v.Scope, v.ScopeID, v.SubjectType, v.SubjectID, string(allow), string(deny), nowStamp(), uid); err != nil {
		fail(w, 500, "save failed")
		return
	}
	a.audit(r.Context(), uid, "override.update", target, v.Scope, v.ScopeID, map[string]any{"subject_type": v.SubjectType, "subject_id": v.SubjectID, "allow": v.Allow, "deny": v.Deny})
	reply(w, 200, v)
}

func (a *Server) auditAPI(w http.ResponseWriter, r *http.Request, x *access) {
	if r.Method != "GET" {
		fail(w, 405, "method not allowed")
		return
	}
	wid := r.URL.Query().Get("workspace_id")
	if wid == "" {
		if !x.platform("audit.view") {
			forbidden(w)
			return
		}
		a.replyActivity(w, scopePlatform, "", true)
		return
	}
	if !x.platform("audit.view") && !x.at(wid, "")["members.manage"] {
		forbidden(w)
		return
	}
	a.replyActivity(w, scopeWorkspace, wid, true)
}
