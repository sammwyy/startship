package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/recipes"
)

type recipeApp struct {
	ID          string               `json:"id"`
	WorkspaceID string               `json:"workspace_id"`
	Name        string               `json:"name"`
	Recipe      recipes.Recipe       `json:"recipe"`
	Config      map[string]any       `json:"config"`
	Ports       map[string]int       `json:"ports"`
	Domains     []recipeDomain       `json:"domains"`
	Containers  map[string]appStatus `json:"containers"`
	Running     bool                 `json:"running"`
	Action      string               `json:"action,omitempty"`
	Status      string               `json:"status"`
	Error       string               `json:"error,omitempty"`
	Created     time.Time            `json:"created"`
}

type recipeDomain struct {
	ID            string `json:"id"`
	Endpoint      string `json:"endpoint"`
	Host          string `json:"host"`
	CertificateID string `json:"certificate_id,omitempty"`
	ForceHTTPS    bool   `json:"force_https,omitempty"`
}

type appStatus struct {
	Status string `json:"status"`
	Health string `json:"health,omitempty"`
	IP     string `json:"ip,omitempty"`
}

func recipeNetwork(id string) string            { return "startship-app-" + id }
func recipeContainer(id, service string) string { return recipeNetwork(id) + "-" + service }
func recipeVolume(id, volume string) string     { return recipeNetwork(id) + "-vol-" + volume }

var builtinRecipes = recipes.Builtins()

func (a *Server) appDocker(args ...string) (string, error) {
	if a.dockerService != nil {
		return a.dockerService(args...)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	b, err := exec.CommandContext(ctx, "docker", args...).CombinedOutput()
	if err != nil {
		return string(b), fmt.Errorf("Docker %s failed: %w", args[0], err)
	}
	return string(b), nil
}

func (a *Server) appFile(app recipeApp, file recipes.File, mode, input string) (string, error) {
	const script = `
set -eu
mode=$1
target=/data/$2
parent=${target%/*}
check=$parent
while [ "$check" != /data ]; do
  [ ! -L "$check" ] || exit 20
  check=${check%/*}
done
[ ! -L "$target" ] || exit 20
if [ "$mode" = read ]; then
  [ -e "$target" ] || exit 21
  [ -f "$target" ] || exit 20
  head -c 1048577 "$target"
  exit
fi
if [ "$mode" = init ] && [ -e "$target" ]; then
  [ -f "$target" ] || exit 20
  exit
fi
mkdir -p "$parent"
if [ -e "$target" ]; then [ -f "$target" ] || exit 20; fi
tmp=$(mktemp "$parent/.startship-XXXXXX")
trap 'rm -f "$tmp"' EXIT
cat > "$tmp"
if [ -e "$target" ]; then
  chmod "$(stat -c '%a' "$target")" "$tmp"
  chown "$(stat -c '%u:%g' "$target")" "$tmp"
else
  chmod 0644 "$tmp"
fi
mv -fT "$tmp" "$target"
`
	args := []string{"run", "--rm", "-i", "--network", "none", "--read-only", "--security-opt", "no-new-privileges", "--mount",
		"type=volume,source=" + recipeVolume(app.ID, file.Volume) + ",destination=/data",
		"busybox:1.37", "sh", "-c", script, "startship-config", mode, file.Path}
	if a.recipeFile != nil {
		return a.recipeFile(app, file, mode, input)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdin = strings.NewReader(input)
	b, err := cmd.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && exit.ExitCode() == 21 {
			return "", errRecipeFileMissing
		}
		return "", fmt.Errorf("could not %s config file", mode)
	}
	if len(b) > 1<<20 {
		return "", fmt.Errorf("config file exceeds 1 MiB")
	}
	return string(b), nil
}

var errRecipeFileMissing = errors.New("config file not found")

func recipeRunArgs(app recipeApp, c recipes.Container) []string {
	args := []string{"run", "-d", "--name", recipeContainer(app.ID, c.ID), "--network", recipeNetwork(app.ID),
		"--network-alias", c.ID, "--restart", "unless-stopped", "--security-opt", "no-new-privileges",
		"--label", "startship.managed=true", "--label", "startship.recipe-app=" + app.ID}
	for _, id := range slices.Sorted(maps.Keys(c.Mounts)) {
		args = append(args, "--mount", "type=volume,source="+recipeVolume(app.ID, id)+",destination="+c.Mounts[id])
	}
	for _, p := range app.Recipe.Ports {
		if n := app.Ports[p.ID]; p.Container == c.ID && n != 0 {
			args = append(args, "-p", fmt.Sprintf("%d:%d/%s", n, p.Port, p.Protocol))
		}
	}
	for _, k := range slices.Sorted(maps.Keys(c.Env)) {
		args = append(args, "-e", k+"="+recipes.Expand(c.Env[k], app.Config))
	}
	if c.Healthcheck != "" {
		args = append(args, "--health-cmd", c.Healthcheck, "--health-interval", "5s", "--health-timeout", "3s", "--health-start-period", "10s")
	}
	if c.Entrypoint != "" {
		args = append(args, "--entrypoint", c.Entrypoint)
	}
	args = append(args, c.Image)
	for _, v := range c.Command {
		args = append(args, recipes.Expand(v, app.Config))
	}
	return args
}

func (a *Server) saveRecipeApp(app recipeApp) error {
	return a.saveResource("recipe_apps", app.ID, app.WorkspaceID, app)
}

func (a *Server) recipeInspect(app recipeApp, c recipes.Container) appStatus {
	format := "{{json .State}}|{{with index .NetworkSettings.Networks " + strconv.Quote(recipeNetwork(app.ID)) + "}}{{.IPAddress}}{{end}}"
	b, err := a.appDocker("inspect", "--format", format, recipeContainer(app.ID, c.ID))
	if err != nil {
		return appStatus{Status: "missing"}
	}
	b = strings.TrimSpace(b)
	separator := strings.LastIndexByte(b, '|')
	if separator < 0 {
		return appStatus{Status: "unknown"}
	}
	state, ip := b[:separator], b[separator+1:]
	var s struct {
		Status string
		Health *struct{ Status string }
	}
	if json.Unmarshal([]byte(state), &s) != nil {
		return appStatus{Status: "unknown"}
	}
	out := appStatus{Status: s.Status, IP: ip}
	if s.Health != nil {
		out.Health = s.Health.Status
	}
	return out
}

func (a *Server) sampleRecipe(app *recipeApp) {
	app.Containers = map[string]appStatus{}
	app.Status = "stopped"
	if app.Running {
		app.Status = "running"
	}
	for _, c := range app.Recipe.Containers {
		s := a.recipeInspect(*app, c)
		app.Containers[c.ID] = s
		if app.Running {
			switch {
			case s.Status != "running" || s.Health == "unhealthy":
				app.Status = "degraded"
			case s.Health == "starting" && app.Status == "running":
				app.Status = "starting"
			}
		}
	}
}

func (a *Server) recipePrepare(app recipeApp, pull bool) error {
	n := recipeNetwork(app.ID)
	if _, err := a.appDocker("network", "inspect", n); err != nil {
		if _, err = a.appDocker("network", "create", "--driver", "bridge", "--label", "startship.recipe-app="+app.ID, n); err != nil {
			return err
		}
	}
	if len(app.Recipe.HTTP) != 0 && a.dockerService == nil {
		if err := connectSelf(n); err != nil {
			return err
		}
	}
	for _, v := range app.Recipe.Volumes {
		if _, err := a.appDocker("volume", "create", "--label", "startship.recipe-app="+app.ID, recipeVolume(app.ID, v)); err != nil {
			return err
		}
	}
	for _, f := range app.Recipe.Files {
		input, err := f.Write("", f.Defaults)
		if err != nil {
			return err
		}
		if _, err = a.appFile(app, f, "init", input); err != nil {
			return err
		}
	}
	if pull {
		for _, c := range app.Recipe.Containers {
			if _, err := a.appDocker("pull", c.Image); err != nil {
				return err
			}
		}
	}
	return nil
}

func (a *Server) actRecipeApp(app *recipeApp, action string) error {
	if action != "stop" {
		if err := a.recipePrepare(*app, action == "install" || action == "update"); err != nil {
			return err
		}
	}
	var result error
	for _, c := range app.Recipe.Containers {
		n := recipeContainer(app.ID, c.ID)
		status := a.recipeInspect(*app, c)
		var args []string
		switch {
		case action == "stop":
			if status.Status == "running" || status.Status == "restarting" {
				args = []string{"stop", n}
			}
		case !app.Running && status.Status == "missing":
			continue
		case action == "update" || action == "configure":
			if status.Status != "missing" {
				if _, err := a.appDocker("rm", "-f", "-v", n); err != nil {
					result = errors.Join(result, err)
					continue
				}
			}
			if app.Running {
				args = recipeRunArgs(*app, c)
			}
		case status.Status == "missing":
			args = recipeRunArgs(*app, c)
		case action == "restart":
			args = []string{"restart", n}
		case status.Status != "running":
			args = []string{"start", n}
		}
		if len(args) != 0 {
			_, err := a.appDocker(args...)
			result = errors.Join(result, err)
		}
	}
	return result
}

func (a *Server) launchRecipeApp(id string) {
	go func() {
		lock := serviceLock("recipe:" + id)
		lock.Lock()
		defer lock.Unlock()
		app, err := getResource[recipeApp](a, "recipe_apps", id)
		if err != nil {
			return
		}
		action := app.Action
		if action == "" {
			if !app.Running {
				return
			}
			action = "start"
		}
		err = a.actRecipeApp(&app, action)
		a.sampleRecipe(&app)
		app.Action = ""
		if err != nil {
			app.Status, app.Error = "failed", err.Error()
		} else {
			app.Error = ""
		}
		a.saveRecipeApp(app)
		a.refresh()
	}()
}

func (a *Server) watchRecipeApps(ctx context.Context) {
	for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
		if app.Running || app.Action != "" {
			a.launchRecipeApp(app.ID)
		}
	}
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			changed := false
			for _, old := range listResources[recipeApp](a, "recipe_apps", "") {
				lock := serviceLock("recipe:" + old.ID)
				if !lock.TryLock() {
					continue
				}
				app, err := getResource[recipeApp](a, "recipe_apps", old.ID)
				if err == nil && app.Status != "pending" && app.Status != "working" {
					before, status := app.Containers, app.Status
					a.sampleRecipe(&app)
					if status == "failed" {
						app.Status = status
					}
					if status != app.Status || !maps.Equal(before, app.Containers) {
						a.saveRecipeApp(app)
						changed = true
					}
				}
				lock.Unlock()
			}
			if changed {
				a.refresh()
			}
		}
	}
}

func (a *Server) removeRecipeApp(app recipeApp, purge bool) error {
	for _, c := range app.Recipe.Containers {
		n := recipeContainer(app.ID, c.ID)
		if a.recipeInspect(app, c).Status != "missing" {
			if _, err := a.appDocker("rm", "-f", "-v", n); err != nil {
				return err
			}
		}
	}
	n := recipeNetwork(app.ID)
	if _, err := a.appDocker("network", "inspect", n); err == nil {
		if self := selfContainer(); self != "" && len(app.Recipe.HTTP) != 0 {
			a.appDocker("network", "disconnect", n, self)
		}
		if _, err := a.appDocker("network", "rm", n); err != nil {
			return err
		}
	}
	if purge {
		for _, v := range app.Recipe.Volumes {
			n := recipeVolume(app.ID, v)
			if _, err := a.appDocker("volume", "inspect", n); err == nil {
				if _, err := a.appDocker("volume", "rm", n); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (a *Server) recipeAppTargets() []Target {
	var targets []Target
	for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
		for _, d := range app.Domains {
			for _, e := range app.Recipe.HTTP {
				if e.ID != d.Endpoint {
					continue
				}
				s := app.Containers[e.Container]
				if !app.Running || s.Status != "running" || s.Health == "starting" || s.Health == "unhealthy" {
					s.Status = "unavailable"
				}
				targets = append(targets, Target{
					Route:      Record{ID: d.ID, Host: d.Host, PathPrefix: "/", WorkspaceID: app.WorkspaceID, CertificateID: d.CertificateID, ForceHTTPS: d.ForceHTTPS},
					Deployment: Record{Status: s.Status, IP: s.IP}, Port: e.Port,
				})
			}
		}
	}
	return targets
}
