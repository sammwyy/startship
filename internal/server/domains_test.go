package server

import (
	"testing"
)

func TestWorkspaceDomains(t *testing.T) {
	f := newRBACFixture(t)
	base := "workspaces/" + f.workspace.ID + "/domains"
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	accountRequest(t, f.h, "POST", base, `{"name":"example.com"}`, f.bob, 403)
	accountRequest(t, f.h, "POST", base, `{"name":"bad_domain"}`, f.alice, 400)
	exact := decodeBody[WorkspaceDomain](t, accountRequest(t, f.h, "POST", base, `{"name":"Example.com."}`, f.alice, 201).Body.Bytes())
	wild := decodeBody[WorkspaceDomain](t, accountRequest(t, f.h, "POST", base, `{"name":"*.apps.example.com"}`, f.alice, 201).Body.Bytes())
	if exact.Name != "example.com" || !exact.Apex || exact.Subdomains || wild.Name != "apps.example.com" || wild.Apex || !wild.Subdomains {
		t.Fatal(exact, wild)
	}
	accountRequest(t, f.h, "POST", base, `{"name":"example.com"}`, f.alice, 409)
	other := f.a.store.List("workspaces", f.rootID)[0]
	accountRequest(t, f.h, "POST", "workspaces/"+other.ID+"/domains", `{"name":"shop.apps.example.com"}`, f.root, 409)
	accountRequest(t, f.h, "POST", "workspaces/"+other.ID+"/domains", `{"name":"other.com","subdomains":true}`, f.root, 201)

	f.a.store.Save(Record{ID: "web", Kind: "apps", Owner: f.aliceID, ProjectID: f.project.ID, Name: "web"})
	route := func(host string, status int) {
		t.Helper()
		accountRequest(t, f.h, "POST", "routes", `{"project_id":"`+f.project.ID+`","container_id":"web","host":"`+host+`","path_prefix":"/"}`, f.bob, status)
	}
	route("example.com", 200)
	route("shop.apps.example.com", 200)
	route("deep.shop.apps.example.com", 200)
	route("apps.example.com", 403)
	route("www.example.com", 403)
	route("x.other.com", 403)
	external := decodeBody[Record](t, accountRequest(t, f.h, "POST", "routes", `{"project_id":"`+f.project.ID+`","container_id":"web","host":"anything.test","path_prefix":"/"}`, f.root, 200).Body.Bytes())
	// A contributor may retain an existing hostname, but cannot introduce an
	// unregistered hostname when editing the path, destination, or port.
	accountRequest(t, f.h, "PATCH", "routes/"+external.ID, `{"path_prefix":"/status","port":9000}`, f.bob, 200)
	accountRequest(t, f.h, "PATCH", "routes/"+external.ID, `{"host":"another.test"}`, f.bob, 403)

	list := decodeBody[struct {
		Domains   []WorkspaceDomain `json:"domains"`
		CanManage bool              `json:"can_manage"`
	}](t, accountRequest(t, f.h, "GET", base, "", f.bob, 200).Body.Bytes())
	if list.CanManage || len(list.Domains) != 2 || list.Domains[0].Routes != 2 || list.Domains[1].Routes != 1 {
		t.Fatal(list)
	}
	accountRequest(t, f.h, "GET", base, "", f.root, 200)
	accountRequest(t, f.h, "DELETE", base+"/"+wild.ID, "", f.alice, 409)
	for _, r := range f.a.store.List("routes", "") {
		if wild.matches(r.Host) {
			accountRequest(t, f.h, "DELETE", "routes/"+r.ID, "", f.alice, 200)
		}
	}
	accountRequest(t, f.h, "DELETE", base+"/"+wild.ID, "", f.bob, 403)
	accountRequest(t, f.h, "DELETE", base+"/"+wild.ID, "", f.alice, 200)
	var n int
	f.a.store.DB.QueryRow("SELECT count(*) FROM audit_log WHERE action LIKE 'domain.%' AND actor_id=?", f.aliceID).Scan(&n)
	if n != 3 {
		t.Fatal("domain audit entries", n)
	}
}
