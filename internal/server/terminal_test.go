package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
)

func terminalProjectPath(t *testing.T, f rbacFixture, project Record, name string) string {
	t.Helper()
	for _, r := range []Record{
		{ID: name, Kind: "apps", ProjectID: project.ID, Active: name + "-active"},
		{ID: name + "-active", Kind: "deployments", ProjectID: project.ID, AppID: name, Container: "docker-" + name},
	} {
		if err := f.a.store.Save(r); err != nil {
			t.Fatal(err)
		}
	}
	return "containers/" + name + "/exec"
}

func TestTerminalPermissions(t *testing.T) {
	f := newRBACFixture(t)
	project := terminalProjectPath(t, f, f.project, "project-container")
	other := terminalProjectPath(t, f, f.second, "other-container")
	if err := f.a.saveService(Service{ID: "svc", WorkspaceID: f.workspace.ID, Container: "docker-service"}); err != nil {
		t.Fatal(err)
	}
	app := recipeApp{ID: "install", WorkspaceID: f.workspace.ID, Recipe: builtinRecipes["minecraft"]}
	if err := f.a.saveRecipeApp(app); err != nil {
		t.Fatal(err)
	}
	service := "services/svc/exec"
	recipe := "workspaces/" + f.workspace.ID + "/apps/install/containers/minecraft/exec"
	for _, path := range []string{project, service, recipe} {
		accountRequest(t, f.h, "GET", path, "", nil, 401)
		accountRequest(t, f.h, "GET", path, "", f.bob, 404)
		accountRequest(t, f.h, "GET", path, "", f.alice, 200)
	}
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	for _, path := range []string{project, service, recipe} {
		accountRequest(t, f.h, "GET", path, "", f.bob, 403)
	}
	o := override{Scope: scopeProject, ScopeID: f.project.ID, SubjectType: "user", SubjectID: f.bobID, Allow: []string{"containers.exec"}}
	body, _ := json.Marshal(o)
	accountRequest(t, f.h, "PUT", "overrides", string(body), f.alice, 200)
	accountRequest(t, f.h, "GET", project, "", f.bob, 200)
	for _, path := range []string{other, service, recipe} {
		accountRequest(t, f.h, "GET", path, "", f.bob, 403)
	}
	o.Allow, o.Deny = nil, []string{"containers.exec"}
	body, _ = json.Marshal(o)
	accountRequest(t, f.h, "PUT", "overrides", string(body), f.alice, 200)
	accountRequest(t, f.h, "GET", project, "", f.bob, 403)
	accountRequest(t, f.h, "GET", strings.Replace(recipe, f.workspace.ID, "different-workspace", 1), "", f.alice, 404)
	req := httptest.NewRequest("GET", "/api/"+project, nil)
	req.AddCookie(f.alice)
	req.Header.Set("Origin", "https://other.test")
	req.Header.Set("Upgrade", "websocket")
	w := httptest.NewRecorder()
	f.h.ServeHTTP(w, req)
	if w.Code != 403 {
		t.Fatalf("cross-origin upgrade: got %d, want 403", w.Code)
	}
}

func TestTerminalShellPreference(t *testing.T) {
	for _, tc := range []struct {
		available string
		calls     []string
		command   []string
	}{
		{"bash", []string{"bash"}, []string{"bash", "-i"}},
		{"sh", []string{"bash", "sh"}, []string{"sh", "-i"}},
		{"", []string{"bash", "sh"}, nil},
	} {
		var calls []string
		got := terminalShell(func(shell string) bool {
			calls = append(calls, shell)
			return shell == tc.available
		})
		if !slices.Equal(calls, tc.calls) || !slices.Equal(got, tc.command) {
			t.Fatalf("available %q: tried %v, command %v", tc.available, calls, got)
		}
	}
}

func TestTerminalRoundTrip(t *testing.T) {
	dir := t.TempDir()
	docker := `#!/bin/sh
case "$*" in
  'container inspect -- docker-project-container') echo '[{"Id":"resolved","State":{"Running":true}}]' ;;
  'exec -- resolved bash -c :') exit 1 ;;
  'exec -- resolved sh -c :') exit 0 ;;
  'exec -it -e TERM=xterm-256color -e _STARTSHIP_TERMINAL_SESSION='*' -- resolved sh -i')
    stty -echo
    echo 'initial:' "$(stty size)"
    read -r input
    printf 'input: %s\nresized: %s\n' "$input" "$(stty size)"
    exit 7 ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "docker"), []byte(docker), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	f := newRBACFixture(t)
	path := terminalProjectPath(t, f, f.project, "project-container")
	server := httptest.NewServer(f.h)
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, server.URL+"/api/"+path, &websocket.DialOptions{
		HTTPHeader: http.Header{"Cookie": {f.alice.String()}},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer conn.CloseNow()
	if err = wsjson.Write(ctx, conn, terminalMessage{Type: "start", Rows: 25, Cols: 90}); err != nil {
		t.Fatal(err)
	}
	var output strings.Builder
	for !strings.Contains(output.String(), "initial: 25 90") {
		kind, b, err := conn.Read(ctx)
		if err != nil || kind != websocket.MessageBinary {
			t.Fatalf("waiting for initial size: %s: %v", b, err)
		}
		output.Write(b)
	}
	if err = wsjson.Write(ctx, conn, terminalMessage{Type: "resize", Rows: 32, Cols: 104}); err != nil {
		t.Fatal(err)
	}
	if err = conn.Write(ctx, websocket.MessageBinary, []byte("hello\n")); err != nil {
		t.Fatal(err)
	}
	for {
		kind, b, err := conn.Read(ctx)
		if err != nil {
			t.Fatal(err)
		}
		if kind == websocket.MessageText {
			event := decodeBody[struct {
				Type string
				Code int
			}](t, b)
			if event.Type != "exit" || event.Code != 7 {
				t.Fatalf("unexpected exit message %s", b)
			}
			break
		}
		output.Write(b)
	}
	if got := output.String(); !strings.Contains(got, "input: hello") || !strings.Contains(got, "resized: 32 104") {
		t.Fatalf("unexpected terminal output %q", got)
	}
}
