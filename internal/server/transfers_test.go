package server

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"slices"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/recipes"
)

func TestWorkspaceTransferGraphCopyAndMove(t *testing.T) {
	f := newRBACFixture(t)
	dst := f.a.store.List("workspaces", f.bobID)[0]
	f.a.dockerService = func(args ...string) (string, error) { return "", nil }
	var copies [][]string
	f.a.dockerStream = func(_ io.Writer, args ...string) error {
		copies = append(copies, args)
		return nil
	}
	s := Service{ID: "db", WorkspaceID: f.workspace.ID, Name: "db", VolumeID: "data", ExternalPort: 15432, Status: "running", Container: "real-db",
		Attachments: []Attachment{{f.project.ID, "database"}, {f.second.ID, "database"}}}
	v := Volume{ID: "data", WorkspaceID: f.workspace.ID, Name: "db-data", ServiceID: s.ID}
	if err := f.a.saveService(s); err != nil {
		t.Fatal(err)
	}
	if err := f.a.saveVolume(v); err != nil {
		t.Fatal(err)
	}
	child := Record{ID: "container", Kind: "apps", Parent: f.project.ID, Owner: f.aliceID, ProjectID: f.project.ID, Active: "active"}
	if err := f.a.store.Save(child); err != nil {
		t.Fatal(err)
	}
	if _, err := f.a.store.DB.Exec("INSERT INTO status_pages VALUES(?,?,?,?,?)", f.workspace.ID, "status", true, "Status", fmt.Sprintf("[%q]", f.project.ID)); err != nil {
		t.Fatal(err)
	}
	in := transferInput{Resource: transferRef{"projects", f.project.ID}, Destination: dst.ID}
	run := func(op string, status int) *http.Response {
		t.Helper()
		body, _ := json.Marshal(in)
		return accountRequest(t, f.h, "POST", "transfers/"+op, string(body), f.root, status).Result()
	}
	read := func(op string) []byte {
		t.Helper()
		response := run(op, 200)
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		return body
	}
	plan := decodeBody[struct {
		Linked []transferResource `json:"linked"`
		PlanID string             `json:"plan_id"`
	}](t, read("plan"))
	if len(plan.Linked) != 3 || plan.Linked[0].ID != "db" || plan.PlanID == "" {
		t.Fatalf("unexpected dependencies: %+v", plan)
	}
	in.PlanID = plan.PlanID
	copyResult := decodeBody[struct{ Resources []transferResource }](t, read("copy"))
	id := func(kind, name string) string {
		for _, n := range copyResult.Resources {
			if n.Kind == kind && n.Name == name {
				return n.ID
			}
		}
		t.Fatalf("missing %s %s", kind, name)
		return ""
	}
	cs, _ := getResource[Service](f.a, "services", id("services", "db"))
	cv, _ := getResource[Volume](f.a, "volumes", id("volumes", "db-data"))
	cp, _ := f.a.store.Get(id("projects", f.project.Name))
	if cs.ID == s.ID || cs.WorkspaceID != dst.ID || cs.VolumeID != cv.ID || cv.ServiceID != cs.ID ||
		len(cs.Attachments) != 2 || cs.Attachments[0].ProjectID != cp.ID || cs.ExternalPort != 0 || cs.Status != "stopped" ||
		cp.Parent != dst.ID || cp.Owner != dst.Owner || cp.Env["KEY"] != "secret" || len(copies) != 1 ||
		!slices.Contains(copies[0], volumeName(v.ID)+":/source:ro") {
		t.Fatalf("copy mismatched: service=%+v volume=%+v project=%+v commands=%v", cs, cv, cp, copies)
	}
	// A destination conflict must leave every source resource unchanged.
	run("move", 409).Body.Close()
	old, _ := f.a.store.Get(f.project.ID)
	if old.WorkspaceID != f.workspace.ID {
		t.Fatal("conflicting transfer modified source")
	}
	in.Destination = f.a.store.List("workspaces", f.rootID)[0].ID
	in.Include = &[]transferRef{}
	run("move", 409).Body.Close() // Can't split a project from a running service.
	in.Include = nil
	live := &liveClient{pending: map[string]liveEvent{}}
	f.a.live.clients[live] = true
	read("move")
	for _, workspace := range []string{f.workspace.ID, in.Destination} {
		for _, topic := range []string{"workspace", "services", "volumes"} {
			sub := liveSub{WorkspaceID: workspace, Topic: topic}
			found := false
			for _, e := range live.pending {
				found = found || sub.relevant(e.record)
			}
			if !found {
				t.Fatalf("no live %s change for workspace %s", topic, workspace)
			}
		}
	}
	moved, _ := f.a.store.Get(f.project.ID)
	movedChild, _ := f.a.store.Get(child.ID)
	movedService, _ := getResource[Service](f.a, "services", s.ID)
	movedVolume, _ := getResource[Volume](f.a, "volumes", v.ID)
	status, _ := f.a.statusConfig(f.workspace.ID)
	if moved.WorkspaceID != in.Destination || moved.Parent != in.Destination || movedChild.Owner != moved.Owner ||
		movedChild.Active != child.Active || movedService.WorkspaceID != in.Destination || movedService.Container != s.Container ||
		movedService.VolumeID != v.ID || movedVolume.WorkspaceID != in.Destination || len(status.Projects) != 0 {
		t.Fatalf("move mismatched: project=%+v child=%+v service=%+v volume=%+v status=%+v", moved, movedChild, movedService, movedVolume, status)
	}
}

func TestWorkspaceTransferPermissions(t *testing.T) {
	f := newRBACFixture(t)
	dst := f.a.store.List("workspaces", f.bobID)[0]
	body := fmt.Sprintf(`{"resource":{"kind":"projects","id":%q},"destination_workspace_id":%q}`, f.project.ID, dst.ID)
	f.assign(t, f.root, f.aliceID, "member", scopeWorkspace, dst.ID, 201)
	f.assign(t, f.root, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	for _, op := range []string{"copy", "move"} {
		accountRequest(t, f.h, "POST", "transfers/"+op, body, f.alice, 403)
		accountRequest(t, f.h, "POST", "transfers/"+op, body, f.bob, 403)
	}
	f.assign(t, f.root, f.aliceID, "contributor", scopeWorkspace, dst.ID, 201)
	if err := f.a.saveRecipeApp(recipeApp{ID: "installed", WorkspaceID: f.workspace.ID, Name: "Installed", Running: true, Status: "running",
		Recipe: recipes.Recipe{ID: "fixture"}, Ports: map[string]int{"http": 1234}}); err != nil {
		t.Fatal(err)
	}
	appBody := fmt.Sprintf(`{"resource":{"kind":"apps","id":"installed"},"destination_workspace_id":%q}`, dst.ID)
	accountRequest(t, f.h, "POST", "transfers/copy", appBody, f.alice, 403)
	response := accountRequest(t, f.h, "POST", "transfers/copy", appBody, f.root, 200)
	if !strings.Contains(response.Body.String(), "Copied apps are stopped") {
		t.Fatal(response.Body.String())
	}
	apps := listResources[recipeApp](f.a, "recipe_apps", dst.ID)
	if len(apps) != 1 || apps[0].ID == "installed" || apps[0].Running || apps[0].Ports["http"] != 0 {
		t.Fatalf("wrong app copy: %+v", apps)
	}
}

func TestWorkspaceTransferPartialMoveAndVolumeFailure(t *testing.T) {
	f := newRBACFixture(t)
	dst := f.a.store.List("workspaces", f.bobID)[0]
	v := Volume{ID: "mount", WorkspaceID: f.workspace.ID, Name: "cache", ProjectID: f.project.ID, MountPath: "/data"}
	if err := f.a.saveVolume(v); err != nil {
		t.Fatal(err)
	}
	move := fmt.Sprintf(`{"resource":{"kind":"projects","id":%q},"include":[],"destination_workspace_id":%q}`, f.project.ID, dst.ID)
	accountRequest(t, f.h, "POST", "transfers/move", move, f.root, 200)
	old, err := getResource[Volume](f.a, "volumes", v.ID)
	if err != nil || old.WorkspaceID != f.workspace.ID || old.ProjectID != "" || old.MountPath != "" {
		t.Fatalf("volume not detached: %+v %v", old, err)
	}
	var removed bool
	f.a.dockerService = func(args ...string) (string, error) {
		if slices.Equal(args[:2], []string{"volume", "rm"}) {
			removed = true
		}
		return "", nil
	}
	f.a.dockerStream = func(io.Writer, ...string) error { return fmt.Errorf("helper failed") }
	copyVolume := fmt.Sprintf(`{"resource":{"kind":"volumes","id":"mount"},"destination_workspace_id":%q}`, dst.ID)
	accountRequest(t, f.h, "POST", "transfers/copy", copyVolume, f.root, 500)
	if !removed || len(listResources[Volume](f.a, "volumes", dst.ID)) != 0 {
		t.Fatal("failed copy left destination resources")
	}
}
