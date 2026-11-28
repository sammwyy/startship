package server

import (
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/catalog"
)

func TestConnectionsAndNetworksAPI(t *testing.T) {
	f := newRBACFixture(t)
	calls, networks := []string{}, map[string]bool{}
	f.a.dockerService = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) > 2 && args[0] == "network" {
			name := args[len(args)-1]
			switch args[1] {
			case "inspect":
				if !networks[name] {
					return "", errors.New("no such network")
				}
			case "create":
				networks[name] = true
			case "rm":
				delete(networks, name)
			}
		}
		return "ok", nil
	}
	e, _ := catalog.Lookup("postgresql")
	config, _ := catalog.Validate(e, "17", nil)
	s := Service{ID: "db", WorkspaceID: f.workspace.ID, Name: "main-db", Engine: e.ID, Config: config}
	f.a.saveService(s)
	app := recipeApp{ID: "wiki", WorkspaceID: f.workspace.ID, Name: "Team Wiki", Recipe: builtinRecipes["gitea"]}
	f.a.saveRecipeApp(app)
	base := "workspaces/" + f.workspace.ID
	link := func(from, to, alias string, user *http.Cookie, code int) {
		accountRequest(t, f.h, "POST", base+"/connections", `{"from":`+from+`,"to":`+to+`,"alias":"`+alias+`"}`, user, code)
	}
	project, service, wiki := `{"kind":"project","id":"`+f.project.ID+`"}`, `{"kind":"service","id":"db"}`, `{"kind":"app","id":"wiki"}`
	link(project, project, "self", f.alice, 400)
	link(project, `{"kind":"service","id":"missing"}`, "db", f.alice, 400)
	link(project, service, "Bad", f.alice, 400)
	link(project, service, "db", f.alice, 200)
	link(project, service, "other", f.alice, 409)
	link(project, wiki, "db", f.alice, 409)
	link(project, wiki, "wiki", f.alice, 200)
	link(wiki, service, "db", f.alice, 200)
	env := f.a.connectionEnv(Endpoint{"project", f.project.ID})
	if !strings.HasPrefix(env["DB_URL"], "postgres://") || env["WIKI_HOST"] != "wiki" || env["WIKI_URL"] != "http://wiki:3000" {
		t.Fatal(env)
	}
	if env := f.a.connectionEnv(Endpoint{"app", "wiki"}); env["DB_HOST"] != "db.local" {
		t.Fatal(env)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "network create --driver bridge --label startship.managed=true --label startship.connection=") ||
		!strings.Contains(joined, "network connect --alias db --alias db.local --alias main-db --alias main-db.local startship-link-") || !strings.Contains(joined, "startship-svc-db") {
		t.Fatal(joined)
	}
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	link(`{"kind":"project","id":"`+f.second.ID+`"}`, service, "db", f.bob, 403)
	accountRequest(t, f.h, "POST", base+"/networks", `{"name":"backend"}`, f.bob, 403)
	r := accountRequest(t, f.h, "GET", base+"/connections", "", f.bob, 200)
	if strings.Contains(r.Body.String(), config["password"]) {
		t.Fatal("connection credentials exposed to members")
	}
	accountRequest(t, f.h, "POST", base+"/networks", `{"name":"Bad Name"}`, f.alice, 400)
	n := decodeBody[Network](t, accountRequest(t, f.h, "POST", base+"/networks", `{"name":"backend"}`, f.alice, 200).Body.Bytes())
	accountRequest(t, f.h, "POST", base+"/networks", `{"name":"backend"}`, f.alice, 409)
	calls = nil
	accountRequest(t, f.h, "POST", "networks/"+n.ID+"/members", service, f.alice, 200)
	accountRequest(t, f.h, "POST", "networks/"+n.ID+"/members", service, f.alice, 409)
	accountRequest(t, f.h, "POST", "networks/"+n.ID+"/members", wiki, f.alice, 200)
	if want := "network connect --alias main-db --alias main-db.local startship-net-" + n.ID + " startship-svc-db"; !strings.Contains(strings.Join(calls, "\n"), want) {
		t.Fatal(want, calls)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "--alias team-wiki --alias team-wiki-gitea") {
		t.Fatal("app hostname missing", calls)
	}
	accountRequest(t, f.h, "PATCH", "networks/"+n.ID, `{"name":"data"}`, f.alice, 200)
	accountRequest(t, f.h, "DELETE", "networks/"+n.ID+"/members/app/wiki", "", f.alice, 200)
	f.a.forgetEndpoint(f.workspace.ID, Endpoint{"service", "db"})
	if len(f.a.connections(f.workspace.ID)) != 1 {
		t.Fatal("service connections survived its removal", f.a.connections(f.workspace.ID))
	}
	if left, _ := getResource[Network](f.a, "networks", n.ID); len(left.Members) != 0 {
		t.Fatal(left)
	}
	c := f.a.connections(f.workspace.ID)[0]
	calls = nil
	accountRequest(t, f.h, "DELETE", "connections/"+c.ID, "", f.alice, 200)
	if !strings.Contains(strings.Join(calls, "\n"), "network rm startship-link-"+c.ID) {
		t.Fatal(calls)
	}
	accountRequest(t, f.h, "DELETE", "networks/"+n.ID, "", f.alice, 200)
	if len(f.a.connections(f.workspace.ID)) != 0 || len(f.a.networks(f.workspace.ID)) != 0 {
		t.Fatal("networking not cleaned up")
	}
}

func TestWorkloadAliases(t *testing.T) {
	names := []string{"api", "api.local"}
	if got := (workload{Preview: true, Primary: true}).aliases(names); len(got) != 0 {
		t.Fatal(got)
	}
	if got := strings.Join((workload{Primary: true, Suffix: "web"}).aliases(names), " "); got != "api api-web api.local" {
		t.Fatal(got)
	}
	if got := strings.Join((workload{Suffix: "worker"}).aliases(names), " "); got != "api-worker" {
		t.Fatal(got)
	}
	if got := strings.Join(createArgs([]string{"run", "-d", "--name", "x", "image"}), " "); got != "create --name x image" {
		t.Fatal(got)
	}
}
