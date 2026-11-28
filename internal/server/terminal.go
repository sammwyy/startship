package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"github.com/creack/pty"
)

type terminalTarget struct {
	container, workspace, project, name string
}

func (a *Server) resolveTerminalTarget(p []string) (terminalTarget, bool) {
	var t terminalTarget
	switch {
	case len(p) == 4 && p[1] == "containers" && p[3] == "exec":
		if c, err := a.store.Get(p[2]); err == nil && c.Kind == "apps" {
			if project, err := a.store.Get(c.ProjectID); err == nil && project.Kind == "projects" {
				t = terminalTarget{workspace: projectWorkspace(project), project: project.ID, name: c.ID}
				if d, err := a.store.Get(c.Active); err == nil && d.Kind == "deployments" && d.AppID == c.ID {
					t.container = d.Container
				}
			}
		}
	case len(p) == 4 && p[1] == "services" && p[3] == "exec":
		if s, err := getResource[Service](a, "services", p[2]); err == nil {
			t = terminalTarget{container: s.Container, workspace: s.WorkspaceID, name: s.ID}
		}
	case len(p) == 8 && p[1] == "workspaces" && p[3] == "apps" && p[5] == "containers" && p[7] == "exec":
		if app, err := getResource[recipeApp](a, "recipe_apps", p[4]); err == nil && app.WorkspaceID == p[2] {
			for _, c := range app.Recipe.Containers {
				if c.ID == p[6] {
					t = terminalTarget{container: recipeContainer(app.ID, c.ID), workspace: app.WorkspaceID, name: app.ID + "/" + c.ID}
					break
				}
			}
		}
	default:
		return t, false
	}
	return t, true
}

func (t terminalTarget) allowed(x *access) bool {
	return x.memberOf(t.workspace) && x.at(t.workspace, t.project)["containers.exec"]
}

type terminalMessage struct {
	Type    string   `json:"type"`
	Rows    uint16   `json:"rows"`
	Cols    uint16   `json:"cols"`
	Command []string `json:"command"`
}

func (m terminalMessage) size() *pty.Winsize {
	if m.Rows == 0 || m.Cols == 0 || m.Rows > 1000 || m.Cols > 1000 {
		return nil
	}
	return &pty.Winsize{Rows: m.Rows, Cols: m.Cols}
}

func terminalContainer(ctx context.Context, name string) (string, error) {
	b, err := exec.CommandContext(ctx, "docker", "container", "inspect", "--", name).Output()
	var containers []struct {
		ID         string
		State      struct{ Running, Paused bool }
		HostConfig struct {
			Privileged bool
			PidMode    string
		}
		Mounts []struct{ Type string }
	}
	if err != nil || json.Unmarshal(b, &containers) != nil || len(containers) != 1 || containers[0].ID == "" ||
		!containers[0].State.Running || containers[0].State.Paused {
		return "", errors.New("the container is not running")
	}
	c := containers[0]
	if c.HostConfig.Privileged || (c.HostConfig.PidMode != "" && c.HostConfig.PidMode != "private") {
		return "", errors.New("terminals are unavailable for containers with host privileges")
	}
	for _, m := range c.Mounts {
		if m.Type != "volume" && m.Type != "tmpfs" {
			return "", errors.New("terminals are unavailable for containers with host mounts")
		}
	}
	return c.ID, nil
}

func terminalShell(probe func(string) bool) []string {
	for _, shell := range []string{"bash", "sh"} {
		if probe(shell) {
			return []string{shell, "-i"}
		}
	}
	return nil
}

func stopTerminal(container, session string) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	const killSession = `for proc in /proc/[0-9]*; do
		if [ -r "$proc/environ" ] && tr '\000' '\n' 2>/dev/null < "$proc/environ" | grep -Fxq -- "$1"; then
			kill -KILL "${proc##*/}" 2>/dev/null || :
		fi
	done`
	for _, shell := range []string{"sh", "bash"} {
		if exec.CommandContext(ctx, "docker", "exec", "--", container, shell, "-c", killSession, "terminal", session).Run() == nil {
			return
		}
	}
}

func (a *Server) terminalAPI(w http.ResponseWriter, r *http.Request, p []string, s sessionIdentity, x *access) bool {
	target, match := a.resolveTerminalTarget(p)
	if !match {
		return false
	}
	if target.workspace == "" || !x.memberOf(target.workspace) {
		fail(w, 404, "container not found")
		return true
	}
	if !target.allowed(x) {
		forbidden(w)
		return true
	}
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		fail(w, 405, "method not allowed")
		return true
	}
	if origin := r.Header.Get("Origin"); origin != "" && origin != "http://"+r.Host && origin != "https://"+r.Host {
		forbidden(w)
		return true
	}
	if target.container == "" {
		fail(w, 409, "no runtime container is available")
		return true
	}
	if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		reply(w, 200, map[string]bool{"ok": true})
		return true
	}
	conn, err := websocket.Accept(w, r, nil)
	if err == nil {
		defer conn.CloseNow()
		a.serveTerminal(r, p, s, target, conn)
	}
	return true
}

func (a *Server) terminalAuthorized(r *http.Request, p []string, s sessionIdentity, target terminalTarget) bool {
	identity := s
	if s.sessionID != "" {
		var valid bool
		a.store.DB.QueryRow("SELECT EXISTS(SELECT 1 FROM sessions WHERE id=? AND user_id=? AND expires>? AND refresh_digest IS NOT NULL)",
			s.sessionID, s.userID, time.Now().Unix()).Scan(&valid)
		if !valid {
			return false
		}
	} else {
		identity = a.authenticate(r)
		if identity.userID != s.userID || identity.token != nil && tokenDenied(r, p, identity.token) != "" {
			return false
		}
	}
	current, _ := a.resolveTerminalTarget(p)
	return current == target && target.allowed(a.accessFor(identity))
}

func (a *Server) serveTerminal(r *http.Request, p []string, s sessionIdentity, target terminalTarget, conn *websocket.Conn) {
	ctx, cancel := context.WithCancelCause(r.Context())
	defer cancel(nil)
	conn.SetReadLimit(64 << 10)
	startup, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	var start terminalMessage
	if err := wsjson.Read(startup, conn, &start); err != nil {
		return
	}
	size := start.size()
	if start.Type != "start" || size == nil || len(start.Command) > 128 {
		conn.Close(websocket.StatusPolicyViolation, "invalid terminal setup")
		return
	}
	id, err := terminalContainer(startup, target.container)
	if err != nil {
		conn.Close(websocket.StatusPolicyViolation, err.Error())
		return
	}
	command := start.Command
	if len(command) == 0 {
		command = terminalShell(func(shell string) bool {
			return exec.CommandContext(startup, "docker", "exec", "--", id, shell, "-c", ":").Run() == nil
		})
		if len(command) == 0 {
			conn.Close(websocket.StatusPolicyViolation, "no bash or sh shell is available")
			return
		}
	}
	if !a.terminalAuthorized(r, p, s, target) {
		conn.Close(websocket.StatusPolicyViolation, "terminal access ended")
		return
	}
	sessionTag := "_STARTSHIP_TERMINAL_SESSION=" + randomID()
	args := append([]string{"exec", "-it", "-e", "TERM=xterm-256color", "-e", sessionTag, "--", id}, command...)
	cmd := exec.CommandContext(ctx, "docker", args...)
	terminal, err := pty.StartWithSize(cmd, size)
	if err != nil {
		conn.Close(websocket.StatusInternalError, "could not start the terminal")
		return
	}
	defer terminal.Close()
	stop()
	scope, scopeID := scopeWorkspace, target.workspace
	if target.project != "" {
		scope, scopeID = scopeProject, target.project
	}
	a.audit(ctx, s.userID, "containers.exec", target.name, scope, scopeID, nil)

	inputs := make(chan terminalInput, 16)
	go func() {
		defer cancel(nil)
		readTerminalInput(ctx, conn, inputs)
	}()
	go func() {
		defer cancel(nil)
		writeTerminalInput(ctx, terminal, inputs)
	}()
	go func() {
		checks := time.NewTicker(10 * time.Second)
		defer checks.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-checks.C:
				if !a.terminalAuthorized(r, p, s, target) {
					cancel(errors.New("terminal access ended"))
					return
				}
			}
		}
	}()
	buf := make([]byte, 32<<10)
	for {
		n, err := terminal.Read(buf)
		if n > 0 {
			write, done := context.WithTimeout(ctx, 5*time.Second)
			writeErr := conn.Write(write, websocket.MessageBinary, buf[:n])
			done()
			if writeErr != nil {
				cancel(nil)
				break
			}
		}
		if err != nil {
			break
		}
	}
	err = cmd.Wait()
	// Closing the Docker attach stream does not stop the processes it started.
	stopTerminal(id, sessionTag)
	if cause := context.Cause(ctx); cause != nil {
		if !errors.Is(cause, context.Canceled) {
			conn.Close(websocket.StatusPolicyViolation, cause.Error())
		}
		return
	}
	code := 0
	if err != nil {
		code = cmd.ProcessState.ExitCode()
	}
	write, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	_ = wsjson.Write(write, conn, map[string]any{"type": "exit", "code": code})
	conn.Close(websocket.StatusNormalClosure, "")
}

type terminalInput struct {
	data []byte
	size *pty.Winsize
}

func readTerminalInput(ctx context.Context, conn *websocket.Conn, inputs chan<- terminalInput) {
	for {
		kind, b, err := conn.Read(ctx)
		if err != nil {
			return
		}
		input := terminalInput{data: b}
		if kind == websocket.MessageText {
			var m terminalMessage
			if json.Unmarshal(b, &m) != nil || m.Type != "resize" {
				return
			}
			input = terminalInput{size: m.size()}
			if input.size == nil {
				return
			}
		}
		select {
		case inputs <- input:
		default:
			return
		}
	}
}

func writeTerminalInput(ctx context.Context, terminal *os.File, inputs <-chan terminalInput) {
	for {
		select {
		case <-ctx.Done():
			return
		case input := <-inputs:
			if input.size != nil {
				if pty.Setsize(terminal, input.size) != nil {
					return
				}
			} else if n, err := terminal.Write(input.data); err != nil || n != len(input.data) {
				return
			}
		}
	}
}
