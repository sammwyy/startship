package server

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/recipes"
)

func TestRecipeRunArgsEntrypoint(t *testing.T) {
	c := recipes.Container{ID: "matrix", Image: "example/matrix:1", Command: []string{"-c", "exec server"}}
	args := recipeRunArgs(recipeApp{ID: "test"}, c, nil)
	if slices.Contains(args, "--entrypoint") || !slices.Equal(args[len(args)-3:], []string{c.Image, "-c", "exec server"}) {
		t.Fatal(args)
	}
	c.Entrypoint = "/bin/sh"
	args = recipeRunArgs(recipeApp{ID: "test"}, c, nil)
	if !slices.Equal(args[len(args)-5:], []string{"--entrypoint", "/bin/sh", c.Image, "-c", "exec server"}) {
		t.Fatal(args)
	}
}

func TestRecipeAppIsolationAndMounts(t *testing.T) {
	a := &Server{}
	var calls []string
	a.dockerService = func(args ...string) (string, error) {
		if args[0] == "inspect" {
			return "", context.Canceled
		}
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	fileInitialized := false
	a.recipeFile = func(_ recipeApp, _ recipes.File, op, value string) (string, error) {
		fileInitialized = op == "init" && strings.Contains(value, "max-players=20")
		return "", nil
	}
	for _, id := range []string{"first", "second"} {
		app := recipeApp{ID: id, Recipe: builtinRecipes["minecraft"], Ports: map[string]int{"game": 25570}, Running: true}
		if err := a.actRecipeApp(&app, "install"); err != nil {
			t.Fatal(err)
		}
	}
	out := strings.Join(calls, "\n")
	for _, id := range []string{"first", "second"} {
		for _, want := range []string{"--network startship-app-" + id + " --network-alias minecraft", "source=startship-app-" + id + "-vol-data,destination=/data", "-p 25570:25565/tcp"} {
			if !strings.Contains(out, want) {
				t.Fatal("missing argument", want, out)
			}
		}
	}
	if !fileInitialized || strings.Contains(out, "startship-services") || strings.Contains(out, "network connect") {
		t.Fatal(fileInitialized, out)
	}
}

func TestRecipeAppPermissionsConfigAndDomains(t *testing.T) {
	f := newRBACFixture(t)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	mc, web := builtinRecipes["minecraft"], builtinRecipes["gitea"]
	mc.HTTP = web.HTTP
	mc.HTTP = append([]recipes.Endpoint(nil), mc.HTTP...)
	mc.HTTP[0].Container = "minecraft"
	app := recipeApp{ID: "install", WorkspaceID: f.workspace.ID, Name: "game", Recipe: mc, Config: map[string]any{"eula": "TRUE"}, Domains: []recipeDomain{}, Ports: map[string]int{"game": 25565}}
	if err := f.a.saveRecipeApp(app); err != nil {
		t.Fatal(err)
	}
	f.a.recipeFile = func(_ recipeApp, _ recipes.File, op, input string) (string, error) {
		if op == "write" && !strings.Contains(input, "max-players=8") {
			t.Fatal(input)
		}
		return "max-players=20\n", nil
	}
	base := "workspaces/" + f.workspace.ID
	path := base + "/apps/install"
	accountRequest(t, f.h, "POST", base+"/apps", `{"name":"collision","recipe":"minecraft","config":{"eula":"TRUE"}}`, f.alice, 409)
	accountRequest(t, f.h, "POST", base+"/apps", `{"name":"denied","recipe":"gitea"}`, f.bob, 403)
	accountRequest(t, f.h, "GET", path+"/config", "", f.bob, 403)
	accountRequest(t, f.h, "GET", path+"/files/unknown", "", f.alice, 404)
	accountRequest(t, f.h, "PUT", path+"/files/server-properties", `{"values":{"max-players":0}}`, f.alice, 400)
	accountRequest(t, f.h, "PUT", path+"/files/server-properties", `{"values":{"max-players":8}}`, f.alice, 200)
	accountRequest(t, f.h, "POST", path+"/domains", `{"endpoint":"web","host":"test.example.com"}`, f.alice, 403)
	domain := decodeBody[WorkspaceDomain](t, accountRequest(t, f.h, "POST", base+"/domains", `{"name":"example.com","subdomains":true}`, f.alice, 201).Body.Bytes())
	binding := decodeBody[recipeDomain](t, accountRequest(t, f.h, "POST", path+"/domains", `{"endpoint":"web","host":"test.example.com"}`, f.alice, 201).Body.Bytes())
	accountRequest(t, f.h, "POST", path+"/domains", `{"endpoint":"web","host":"test.example.com"}`, f.alice, 409)
	accountRequest(t, f.h, "DELETE", base+"/domains/"+domain.ID, "", f.alice, 409)
	targets := f.a.recipeAppTargets()
	if len(targets) != 1 || targets[0].Route.ID != binding.ID || targets[0].Port != 3000 || !f.a.certAllowed(binding.Host) {
		t.Fatal(targets)
	}
	data, _, _, why := f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: "recipe_apps", WorkspaceID: f.workspace.ID})
	b, _ := json.Marshal(data)
	if why != "" || !strings.Contains(string(b), `"config":null`) || !strings.Contains(string(b), `"can_manage":false`) {
		t.Fatal(why, string(b))
	}
	accountRequest(t, f.h, "DELETE", path+"/domains/"+binding.ID, "", f.alice, 200)
	accountRequest(t, f.h, "DELETE", base+"/domains/"+domain.ID, "", f.alice, 200)
}
