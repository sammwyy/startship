package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func decodeBody[T any](t *testing.T, body []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(body, &v); err != nil {
		t.Fatal(err, string(body))
	}
	return v
}

type rbacFixture struct {
	a                          *Server
	h                          http.Handler
	root, alice, bob           *http.Cookie
	rootID, aliceID, bobID     string
	workspace, project, second Record
}

func newRBACFixture(t *testing.T) rbacFixture {
	t.Helper()
	a := testServer(t)
	f := rbacFixture{a: a, h: a.api()}
	f.rootID, _ = a.createUser("root", "password123")
	f.aliceID, _ = a.createUser("alice", "password123")
	f.bobID, _ = a.createUser("bob", "password123")
	f.root, f.alice, f.bob = accountLogin(t, f.h, "root", "password123"), accountLogin(t, f.h, "alice", "password123"), accountLogin(t, f.h, "bob", "password123")
	f.workspace = a.store.List("workspaces", f.aliceID)[0]
	for i, name := range []string{"api", "web"} {
		p := decodeBody[Record](t, accountRequest(t, f.h, "POST", "projects", `{"name":"`+name+`","workspace_id":"`+f.workspace.ID+`","env":{"KEY":"secret"}}`, f.alice, 200).Body.Bytes())
		if i == 0 {
			f.project = p
		} else {
			f.second = p
		}
	}
	return f
}

func (f rbacFixture) assign(t *testing.T, cookie *http.Cookie, user, role, scope, id string, status int) assignment {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"user_id": user, "role_id": role, "scope": scope, "scope_id": id})
	w := accountRequest(t, f.h, "POST", "assignments", string(body), cookie, status)
	if status != 201 {
		return assignment{}
	}
	return decodeBody[assignment](t, w.Body.Bytes())
}

func (f rbacFixture) projects(t *testing.T, cookie *http.Cookie) []string {
	t.Helper()
	out := []string{}
	for _, p := range decodeBody[[]apiRecord](t, accountRequest(t, f.h, "GET", "projects?workspace_id="+f.workspace.ID, "", cookie, 200).Body.Bytes()) {
		out = append(out, p.Name)
	}
	return out
}

func TestPlatformAdminBootstrapAndProtection(t *testing.T) {
	f := newRBACFixture(t)
	me := decodeBody[map[string]any](t, accountRequest(t, f.h, "GET", "me", "", f.root, 200).Body.Bytes())
	if me["platform_admin"] != true {
		t.Fatal("first user is not Platform Admin", me)
	}
	if me := decodeBody[map[string]any](t, accountRequest(t, f.h, "GET", "me", "", f.alice, 200).Body.Bytes()); me["platform_admin"] == true || len(me["permissions"].([]any)) != 0 {
		t.Fatal("plain users must not hold platform permissions", me)
	}
	accountRequest(t, f.h, "PATCH", "roles/"+platformAdminRole, `{"name":"Nope"}`, f.root, 409)
	accountRequest(t, f.h, "DELETE", "roles/"+platformAdminRole, "", f.root, 409)
	held := f.a.assignments("a.user_id=? AND a.role_id=?", f.rootID, platformAdminRole)
	accountRequest(t, f.h, "DELETE", "assignments/"+held[0].ID, "", f.root, 409)
	accountRequest(t, f.h, "DELETE", "users/"+f.rootID, "", f.root, 409)
	f.assign(t, f.alice, f.aliceID, platformAdminRole, scopePlatform, "", 403)
	f.assign(t, f.root, f.aliceID, platformAdminRole, scopeWorkspace, f.workspace.ID, 400)
	second := f.assign(t, f.root, f.aliceID, platformAdminRole, scopePlatform, "", 201)
	accountRequest(t, f.h, "DELETE", "assignments/"+held[0].ID, "", f.root, 200)
	accountRequest(t, f.h, "DELETE", "assignments/"+second.ID, "", f.alice, 409)
	if len(decodeBody[[]apiRecord](t, accountRequest(t, f.h, "GET", "projects", "", f.alice, 200).Body.Bytes())) != 2 {
		t.Fatal("Platform Admin should see every project")
	}
}

func TestFirewallBuiltinPermissions(t *testing.T) {
	for _, role := range builtinRoles {
		wantView := role.ID == platformAdminRole || role.ID == "administrator" || role.ID == "contributor"
		wantManage := role.ID == platformAdminRole || role.ID == "administrator"
		if slices.Contains(role.Permissions, "firewall.view") != wantView || slices.Contains(role.Permissions, "firewall.manage") != wantManage || slices.Contains(role.Permissions, "network.firewall") != (role.ID == platformAdminRole) {
			t.Fatal(role.ID, role.Permissions)
		}
	}
	if permissionByKey["firewall.view"].Scope != scopeWorkspace || permissionByKey["firewall.manage"].Scope != scopeWorkspace || permissionByKey["network.firewall"].Scope != scopePlatform {
		t.Fatal("wrong permission scopes")
	}
}

func TestPlainUsersOnlyManageThemselves(t *testing.T) {
	f := newRBACFixture(t)
	accountRequest(t, f.h, "POST", "workspaces", `{"name":"New"}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "users", `{"username":"eve","password":"password123"}`, f.bob, 403)
	accountRequest(t, f.h, "PATCH", "users/"+f.aliceID, `{"password":"hijacked123"}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "roles", `{"name":"Mine"}`, f.bob, 403)
	accountRequest(t, f.h, "GET", "audit", "", f.bob, 403)
	accountRequest(t, f.h, "GET", "workspaces/"+f.workspace.ID, "", f.bob, 404)
	accountRequest(t, f.h, "GET", "projects/"+f.project.ID, "", f.bob, 404)
	f.bob = accountRequest(t, f.h, "PATCH", "me", `{"username":"bobby","current_password":"password123"}`, f.bob, 200).Result().Cookies()[0]

	creator := decodeBody[Role](t, accountRequest(t, f.h, "POST", "roles", `{"name":"Workspace creator","permissions":["workspaces.create"]}`, f.root, 200).Body.Bytes())
	f.assign(t, f.root, f.bobID, creator.ID, scopePlatform, "", 201)
	ws := decodeBody[apiRecord](t, accountRequest(t, f.h, "POST", "workspaces", `{"name":"Bob's team"}`, f.bob, 200).Body.Bytes())
	if !slices.Contains(ws.Permissions, "members.manage") {
		t.Fatal("workspace creator should administer the new workspace", ws.Permissions)
	}
}

func TestWorkspaceRolesInheritIntoProjects(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	if got := f.projects(t, f.bob); len(got) != 2 {
		t.Fatal("member should see every project", got)
	}
	p := decodeBody[apiRecord](t, accountRequest(t, f.h, "GET", "projects/"+f.project.ID, "", f.bob, 200).Body.Bytes())
	if p.Env != nil || slices.Contains(p.Permissions, "deploy") || !slices.Contains(p.Permissions, "logs.view") {
		t.Fatal("member is read-only without secrets", p)
	}
	accountRequest(t, f.h, "GET", "projects/"+f.project.ID+"/traffic", "", f.bob, 200)
	accountRequest(t, f.h, "POST", "projects/"+f.project.ID+"/deploy", "{}", f.bob, 403)
	accountRequest(t, f.h, "PATCH", "projects/"+f.project.ID, `{"port":9000}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "projects", `{"name":"x","workspace_id":"`+f.workspace.ID+`"}`, f.bob, 403)

	contributor := f.assign(t, f.alice, f.bobID, "contributor", scopeProject, f.project.ID, 201)
	accountRequest(t, f.h, "PATCH", "projects/"+f.project.ID, `{"port":9000}`, f.bob, 200)
	accountRequest(t, f.h, "PATCH", "projects/"+f.second.ID, `{"port":9000}`, f.bob, 403)
	accountRequest(t, f.h, "PATCH", "workspaces/"+f.workspace.ID, `{"name":"Taken"}`, f.bob, 403)
	accountRequest(t, f.h, "DELETE", "projects/"+f.project.ID, "", f.bob, 403)
	f.assign(t, f.bob, f.bobID, "administrator", scopeWorkspace, f.workspace.ID, 403)
	accountRequest(t, f.h, "DELETE", "assignments/"+contributor.ID, "", f.bob, 403)
	accountRequest(t, f.h, "DELETE", "assignments/"+contributor.ID, "", f.alice, 200)
	accountRequest(t, f.h, "PATCH", "projects/"+f.project.ID, `{"port":9001}`, f.bob, 403)
}

func TestGuestsSeeOnlyGrantedProjects(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "guest", scopeWorkspace, f.workspace.ID, 201)
	if got := f.projects(t, f.bob); len(got) != 0 {
		t.Fatal("guest should see no projects", got)
	}
	if ws := decodeBody[[]apiRecord](t, accountRequest(t, f.h, "GET", "workspaces", "", f.bob, 200).Body.Bytes()); len(ws) != 2 {
		t.Fatal("guest should still see the workspace they belong to", ws)
	}
	f.assign(t, f.alice, f.bobID, "member", scopeProject, f.second.ID, 201)
	if got := f.projects(t, f.bob); !slices.Equal(got, []string{"web"}) {
		t.Fatal("guest should see only the granted project", got)
	}
	accountRequest(t, f.h, "GET", "projects/"+f.project.ID, "", f.bob, 404)
	members := decodeBody[map[string]any](t, accountRequest(t, f.h, "GET", "workspaces/"+f.workspace.ID+"/members", "", f.alice, 200).Body.Bytes())
	if !strings.Contains(mustJSON(members), `"project_name":"web"`) || !strings.Contains(mustJSON(members), `"username":"bob"`) {
		t.Fatal(members)
	}
}

func TestOverridesDenyAndAllow(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	put := func(cookie *http.Cookie, scope, id, subjectType, subject string, allow, deny []string, status int) {
		t.Helper()
		body, _ := json.Marshal(override{Scope: scope, ScopeID: id, SubjectType: subjectType, SubjectID: subject, Allow: allow, Deny: deny})
		accountRequest(t, f.h, "PUT", "overrides", string(body), cookie, status)
	}
	perms := func(id string) []string {
		return decodeBody[apiRecord](t, accountRequest(t, f.h, "GET", "projects/"+id, "", f.bob, 200).Body.Bytes()).Permissions
	}
	put(f.alice, scopeProject, f.project.ID, "role", "contributor", nil, []string{"deploy", "project.environment"}, 200)
	if p := perms(f.project.ID); slices.Contains(p, "deploy") || slices.Contains(p, "project.environment") || !slices.Contains(perms(f.second.ID), "deploy") {
		t.Fatal("project role deny should only affect that project", p)
	}
	accountRequest(t, f.h, "POST", "projects/"+f.project.ID+"/deploy", "{}", f.bob, 403)
	put(f.alice, scopeProject, f.project.ID, "user", f.bobID, []string{"deploy"}, nil, 200)
	if !slices.Contains(perms(f.project.ID), "deploy") {
		t.Fatal("user allow should beat role deny")
	}
	put(f.alice, scopeWorkspace, f.workspace.ID, "user", f.bobID, nil, []string{"logs.view"}, 200)
	if slices.Contains(perms(f.second.ID), "logs.view") {
		t.Fatal("workspace deny should inherit into projects")
	}
	put(f.alice, scopeProject, f.project.ID, "role", "contributor", []string{"workspace.manage"}, nil, 400)
	put(f.alice, scopeWorkspace, f.workspace.ID, "role", "contributor", []string{"users.manage"}, nil, 400)
	put(f.alice, scopeProject, f.project.ID, "role", "contributor", []string{"deploy"}, []string{"deploy"}, 400)
	put(f.bob, scopeProject, f.project.ID, "user", f.bobID, []string{"members.manage"}, nil, 403)
	put(f.alice, scopeProject, f.project.ID, "user", f.bobID, nil, nil, 200)
	if len(f.a.overrides(scopeProject, f.project.ID)) != 1 {
		t.Fatal("empty override should be removed")
	}
}

func TestCustomRolesCloneAndAudit(t *testing.T) {
	f := newRBACFixture(t)
	accountRequest(t, f.h, "POST", "roles", `{"name":"Bad","icon":"skull"}`, f.root, 400)
	accountRequest(t, f.h, "POST", "roles", `{"name":"Bad","color":"red"}`, f.root, 400)
	accountRequest(t, f.h, "POST", "roles", `{"name":"Bad","permissions":["fly"]}`, f.root, 400)
	accountRequest(t, f.h, "PATCH", "roles/member", `{"name":"Changed"}`, f.root, 409)
	role := decodeBody[Role](t, accountRequest(t, f.h, "POST", "roles", `{"clone_of":"member"}`, f.root, 200).Body.Bytes())
	if role.Name != "Member copy" || !slices.Equal(role.Permissions, []string{"project.view", "logs.view", "traffic.view"}) || role.Builtin {
		t.Fatal(role)
	}
	role = decodeBody[Role](t, accountRequest(t, f.h, "PATCH", "roles/"+role.ID, `{"name":"Deployer","color":"#AA00ff","icon":"rocket","permissions":["deploy","project.view"]}`, f.root, 200).Body.Bytes())
	if role.Color != "#aa00ff" || !slices.Equal(role.Permissions, []string{"project.view", "deploy"}) {
		t.Fatal(role)
	}
	f.assign(t, f.alice, f.bobID, role.ID, scopeProject, f.project.ID, 201)
	f.assign(t, f.alice, f.rootID, role.ID, scopeProject, f.second.ID, 201)
	f.assign(t, f.alice, f.bobID, role.ID, scopeProject, f.project.ID, 409)
	if got := f.a.roles()[role.ID].Members; got != 2 {
		t.Fatal("role should be held by two users", got)
	}
	if got := f.projects(t, f.bob); !slices.Equal(got, []string{"api"}) {
		t.Fatal(got)
	}
	accountRequest(t, f.h, "DELETE", "roles/"+role.ID, "", f.root, 200)
	if got := f.projects(t, f.bob); len(got) != 0 {
		t.Fatal("deleting a role removes its assignments", got)
	}
	entries := decodeBody[[]map[string]any](t, accountRequest(t, f.h, "GET", "audit", "", f.root, 200).Body.Bytes())
	actions := []string{}
	for _, e := range entries {
		actions = append(actions, e["actor"].(string)+":"+e["action"].(string))
	}
	for _, want := range []string{"root:role.create", "root:role.update", "alice:assignment.create", "root:role.delete"} {
		if !slices.Contains(actions, want) {
			t.Fatal("missing audit entry", want, actions)
		}
	}
	scoped := decodeBody[[]map[string]any](t, accountRequest(t, f.h, "GET", "audit?workspace_id="+f.workspace.ID, "", f.alice, 200).Body.Bytes())
	for _, e := range scoped {
		if e["scope"] == scopePlatform {
			t.Fatal("workspace audit leaked platform entries")
		}
	}
	accountRequest(t, f.h, "GET", "audit?workspace_id="+f.workspace.ID, "", f.bob, 403)
}

func TestUserAdministration(t *testing.T) {
	f := newRBACFixture(t)
	created := decodeBody[map[string]string](t, accountRequest(t, f.h, "POST", "users", `{"username":"eve","password":"password123"}`, f.root, 201).Body.Bytes())
	accountRequest(t, f.h, "POST", "users", `{"username":"eve","password":"password123"}`, f.root, 409)
	eve := accountLogin(t, f.h, "eve", "password123")
	if me := decodeBody[map[string]any](t, accountRequest(t, f.h, "GET", "me", "", eve, 200).Body.Bytes()); me["must_change_password"] != true {
		t.Fatal("admin-created users must choose a password", me)
	}
	if ws := f.a.store.List("workspaces", created["id"]); len(ws) != 0 {
		t.Fatal("admin-created users should not get a personal workspace")
	}
	accountRequest(t, f.h, "PATCH", "users/"+created["id"], `{"password":"temporary123"}`, f.root, 200)
	accountRequest(t, f.h, "GET", "me", "", eve, 401)
	manager := decodeBody[Role](t, accountRequest(t, f.h, "POST", "roles", `{"name":"Users","permissions":["users.manage"]}`, f.root, 200).Body.Bytes())
	f.assign(t, f.root, f.aliceID, manager.ID, scopePlatform, "", 201)
	accountRequest(t, f.h, "PATCH", "users/"+f.rootID, `{"password":"hijacked123"}`, f.alice, 403)
	f.assign(t, f.alice, f.bobID, manager.ID, scopeWorkspace, f.workspace.ID, 201)
	if slices.Contains(f.a.access(f.bobID).list(f.workspace.ID, ""), "users.manage") || f.a.access(f.bobID).platform("users.manage") {
		t.Fatal("platform permissions only apply from a platform assignment")
	}
	accountRequest(t, f.h, "DELETE", "users/"+created["id"], "", f.alice, 200)
}

func TestAccessMigrationForExistingInstalls(t *testing.T) {
	a := testServer(t)
	a.store.DB.Exec("DELETE FROM settings WHERE key='access_migrated'")
	a.store.DB.Exec("INSERT INTO users(id,username,password) VALUES('u1','old','x'),('u2','other','x')")
	a.store.Save(Record{ID: "w1", Kind: "workspaces", Owner: "u2", Name: "Legacy"})
	if err := migrateAccess(a.store); err != nil {
		t.Fatal(err)
	}
	if !a.access("u1").admin || !a.access("u2").at("w1", "")["members.manage"] || a.access("u2").admin {
		t.Fatal("migration should make the first user Platform Admin and owners workspace administrators")
	}
}

func mustJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

func TestSocketActsAsPlatformAdmin(t *testing.T) {
	f := newRBACFixture(t)
	local := func() *http.Request {
		r := httptest.NewRequest("GET", "/api/me", nil)
		return r.WithContext(context.WithValue(r.Context(), socketContextKey{}, true))
	}
	if got := f.a.user(local()); got != f.rootID {
		t.Fatal("socket should act as the Platform Admin", got)
	}
	f.assign(t, f.root, f.aliceID, platformAdminRole, scopePlatform, "", 201)
	held := f.a.assignments("a.user_id=? AND a.role_id=?", f.rootID, platformAdminRole)
	accountRequest(t, f.h, "DELETE", "assignments/"+held[0].ID, "", f.root, 200)
	if got := f.a.user(local()); got != f.aliceID {
		t.Fatal("socket should follow the Platform Admin role, not the first account", got)
	}
}
