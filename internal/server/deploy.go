package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sammwyy/startship/internal/pipeline"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type logWriter struct {
	a      *Server
	id     string
	group  string
	secret string
	mu     sync.Mutex
}

type commandTail struct{ text string }

func (t *commandTail) Write(b []byte) (int, error) {
	t.text += string(b)
	if len(t.text) > 2048 {
		t.text = t.text[len(t.text)-2048:]
	}
	return len(b), nil
}

func (l *logWriter) Write(b []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.a.live != nil {
		l.a.live.logMu.Lock()
		defer l.a.live.logMu.Unlock()
	}
	s := string(b)
	if l.secret != "" {
		s = strings.ReplaceAll(s, l.secret, "[REDACTED]")
	}
	end := 0
	appendLog := func(r *Record) { r.Log += s; end = len(r.Log) }
	e := l.a.store.Update(l.id, appendLog)
	if e == nil {
		l.a.live.publish(liveEvent{id: l.id, text: s, end: end})
	}
	if e == nil && l.group != "" {
		e = l.a.store.Update(l.group, appendLog)
		if e == nil {
			l.a.live.publish(liveEvent{id: l.group, text: s, end: end})
		}
	}
	return len(b), e
}
func command(dir string, out io.Writer, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, name, args...)
	c.Dir = dir
	c.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	if out == nil {
		out = io.Discard
	}
	tail := &commandTail{}
	c.Stdout = out
	c.Stderr = io.MultiWriter(out, tail)
	if err := c.Run(); err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.Join(strings.Fields(tail.text), " "))
	}
	return nil
}
func (a *Server) clone(app Record, dir string, out io.Writer) error {
	raw := app.GitURL
	if app.GitToken != "" {
		u, e := url.Parse(raw)
		if e != nil {
			return e
		}
		if u.Scheme != "https" {
			return fmt.Errorf("tokens require HTTPS")
		}
		u.User = url.UserPassword("x-access-token", app.GitToken)
		raw = u.String()
	}
	return command("", out, "git", "clone", "--depth", "1", "--branch", app.Branch, "--", raw, dir)
}
func (a *Server) enqueue(app Record) (Record, error) {
	return a.enqueueDeployment(app, Record{})
}
func (a *Server) enqueueDeployment(app Record, d Record) (Record, error) {
	if d.RedeployOf == "" {
		if _, e := a.projectSource(app); e != nil {
			return Record{}, e
		}
	}
	d.ID, d.Kind, d.Owner, d.Parent, d.AppID, d.Status, d.Created = randomID(), "deployments", app.Owner, app.ID, app.ID, "queued", time.Now()
	if e := a.store.Save(d); e != nil {
		return d, e
	}
	a.mu.Lock()
	q := a.queues[app.ID]
	if q == nil {
		q = make(chan string, 100)
		a.queues[app.ID] = q
		go func() {
			for id := range q {
				a.build(app.ID, id)
			}
		}()
	}
	select {
	case q <- d.ID:
		a.mu.Unlock()
		return d, nil
	default:
		a.mu.Unlock()
		a.store.Update(d.ID, func(r *Record) { r.Status = "failed"; r.Log = "deployment queue full" })
		return d, fmt.Errorf("deployment queue full")
	}
}
func checkoutRoot(dir, rootDir string) (string, error) {
	checkout, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	root, err := filepath.EvalSymlinks(filepath.Join(dir, rootDir))
	if err != nil {
		return "", err
	}
	rel, err := filepath.Rel(checkout, root)
	if err != nil || !safeRoot(rel) {
		return "", fmt.Errorf("root directory escapes repository")
	}
	return root, nil
}

func (a *Server) status(id, status string) {
	a.store.Update(id, func(r *Record) { r.Status = status })
	if d, err := a.store.Get(id); err == nil && d.ReleaseID != "" {
		a.store.Update(d.ReleaseID, func(r *Record) { r.Status = status })
	}
}
func (a *Server) build(appID, id string) {
	app, e := a.store.Get(appID)
	if e != nil {
		return
	}
	a.buildContainer(app, id, "", "")
}
func (a *Server) buildContainer(app Record, id, checkout, commit string) {
	var e error
	d, err := a.store.Get(id)
	if err != nil {
		return
	}
	logger := &logWriter{a: a, id: id, group: d.ReleaseID, secret: app.GitToken}
	defer func() {
		if e != nil {
			a.store.Update(id, func(r *Record) { r.Status = "failed"; now := time.Now(); r.Finished = &now })
		}
		if d.ReleaseID == "" {
			a.logDeploymentResult(logger, app, e)
		} else if e != nil {
			fmt.Fprintf(logger, "Container failed: %v\n", e)
		} else {
			fmt.Fprintln(logger, "Container healthy; ready for project activation")
		}
	}()
	if d.RedeployOf != "" {
		app.Pipeline = d.Pipeline
		fmt.Fprintf(logger, "Redeploying release %s (no clone, build or tests)\n", d.RedeployOf)
		if e = releaseAvailable(d); e != nil {
			return
		}
		network := "startship-" + app.ProjectID
		if e = ensureNetwork(network, logger); e != nil {
			return
		}
		a.store.Update(id, func(r *Record) { r.Network = network })
		if d.StaticDir != "" {
			e = a.activate(app, id)
		} else {
			e = a.startRelease(app, id, d.Image, network, logger)
		}
		return
	}
	a.status(id, "building")
	source, sourceErr := a.projectSource(app)
	if sourceErr != nil {
		e = sourceErr
		return
	}
	logger.secret = source.GitToken
	network := "startship-" + app.ProjectID
	if e = ensureNetwork(network, logger); e != nil {
		return
	}
	a.store.Update(id, func(r *Record) { r.Network = network })
	dir := checkout
	if dir == "" {
		dir = filepath.Join(a.data, "builds", id)
		if e = os.MkdirAll(filepath.Dir(dir), 0700); e != nil {
			return
		}
		defer os.RemoveAll(dir)
		if e = a.clone(source, dir, logger); e != nil {
			return
		}
		var sha []byte
		sha, e = exec.Command("git", "-C", dir, "rev-parse", "HEAD").Output()
		if e != nil {
			return
		}
		commit = strings.TrimSpace(string(sha))
		if e = os.RemoveAll(filepath.Join(dir, ".git")); e != nil {
			return
		}
	}
	a.store.Update(id, func(r *Record) { r.Commit = commit })
	var root string
	root, e = checkoutRoot(dir, app.RootDir)
	if e != nil {
		return
	}
	p := app.Pipeline
	if p == nil {
		kind := app.BuildType
		if kind == "auto" {
			kind = pipeline.DetectTemplate(root)
		}
		var resolved pipeline.Pipeline
		resolved, e = pipeline.Resolve(kind, root)
		if e != nil {
			return
		}
		if app.TestCommand != "" {
			resolved.Test.Commands = []string{app.TestCommand}
		}
		if app.Port != 0 {
			resolved.Deploy.Port = app.Port
		}
		p = &resolved
	}
	if e = p.Validate(); e != nil {
		return
	}
	app.Pipeline, app.Port = p, p.Deploy.Port
	a.store.Update(id, func(r *Record) { r.Pipeline = p; r.Port = app.Port })
	engine := externalEngine(p)
	buildLabel := p.Build.Image
	if engine {
		buildLabel = p.Build.Engine
	}
	fmt.Fprintf(logger, "==> 1/3 Build (%s)\n", buildLabel)
	image := "startship/" + app.ID + ":" + id
	dockerPath := p.Build.Dockerfile
	generated := !engine && dockerPath == ""
	staticDirect := !engine && p.Deploy.Static && len(p.Build.Commands) == 0 && len(p.Test.Commands) == 0 && generated
	if engine {
		if e = buildEngine(root, logger, app.ProjectID, image, p, d.NoCache); e != nil {
			return
		}
		a.store.Update(id, func(r *Record) { r.Image = image })
	} else if !staticDirect {
		if generated {
			var content string
			content, e = p.Dockerfile()
			if e != nil {
				return
			}
			dockerPath = "Dockerfile.startship"
			if e = os.WriteFile(filepath.Join(root, dockerPath), []byte(content), 0600); e != nil {
				return
			}
			defer os.Remove(filepath.Join(root, dockerPath))
		}
		target := ""
		if generated {
			target = "build"
		}
		if e = buildImage(root, logger, app.ProjectID, image, imageBuild{Dockerfile: dockerPath, NoCache: d.NoCache, Target: target}); e != nil {
			return
		}
		a.store.Update(id, func(r *Record) { r.Image = image })
	}
	fmt.Fprintln(logger, "==> 2/3 Test")
	if len(p.Test.Commands) > 0 {
		a.status(id, "testing")
		if generated {
			if e = buildImage(root, logger, app.ProjectID, image, imageBuild{Dockerfile: dockerPath, Target: "test"}); e != nil {
				return
			}
		}
		args := appRunArgs(app, network, true)
		testImage := image
		if engine && p.Test.Image != "" {
			testImage = image + "-test"
			defer command("", nil, "docker", "image", "rm", "-f", testImage)
			if e = engineStageImage(root, logger, app.ProjectID, image, testImage, p.Test.Image, ".", p); e != nil {
				return
			}
		}
		testCommand := "set -e\n" + strings.Join(p.Test.Commands, "\n")
		if engine {
			args = append(args, engineCommand(p, testImage, testCommand, p.Test.Image != "")...)
		} else {
			args = append(args, image, "sh", "-c", testCommand)
		}
		var output strings.Builder
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
		c := exec.CommandContext(ctx, "docker", args...)
		c.Stdout = io.MultiWriter(logger, &output)
		c.Stderr = c.Stdout
		runErr := c.Run()
		cancel()
		code := 0
		if runErr != nil {
			var exit *exec.ExitError
			if !errors.As(runErr, &exit) {
				e = runErr
				return
			}
			code = exit.ExitCode()
		}
		if code != p.Test.ExpectExitCode {
			e = fmt.Errorf("test expected exit code %d, got %d", p.Test.ExpectExitCode, code)
			return
		}
		if !strings.Contains(output.String(), p.Test.ExpectOutput) {
			e = fmt.Errorf("test expected output containing %q, got %q", p.Test.ExpectOutput, output.String())
			return
		}
	}
	fmt.Fprintf(logger, "==> 3/3 Deploy :%d\n", app.Port)
	if p.Deploy.Static {
		a.status(id, "starting")
		artifact := p.Deploy.Artifact
		if artifact == "" {
			artifact = "."
		}
		src := filepath.Join(root, artifact)
		if staticDirect {
			if _, e = checkoutRoot(root, artifact); e != nil {
				return
			}
		}
		if !staticDirect {
			extract := filepath.Join(a.data, "builds", id+"-artifact")
			if e = os.MkdirAll(extract, 0700); e != nil {
				return
			}
			defer os.RemoveAll(extract)
			name := "startship-artifact-" + id
			if e = command("", logger, "docker", "create", "--name", name, image, "sh"); e != nil {
				return
			}
			defer command("", nil, "docker", "rm", "-f", name)
			appDir := "/app"
			if engine {
				appDir = engineAppDir(p)
			}
			if e = command("", logger, "docker", "cp", name+":"+filepath.Join(appDir, artifact)+"/.", extract); e != nil {
				return
			}
			src = extract
		}
		dest := filepath.Join(a.data, "static", id)
		if e = copyStatic(src, dest); e != nil {
			return
		}
		a.store.Update(id, func(r *Record) { r.StaticDir = dest })
		e = a.activate(app, id)
		return
	}
	if generated {
		if e = buildImage(root, logger, app.ProjectID, image, imageBuild{Dockerfile: dockerPath, Target: "final"}); e != nil {
			return
		}
	}
	if engine && p.Deploy.Image != "" {
		final := image + "-runtime"
		if e = engineStageImage(root, logger, app.ProjectID, image, final, p.Deploy.Image, p.Deploy.Artifact, p); e != nil {
			return
		}
		image = final
		a.store.Update(id, func(r *Record) { r.Image = image })
	}
	e = a.startRelease(app, id, image, network, logger)
}

func releaseAvailable(d Record) error {
	if d.StaticDir != "" {
		info, err := os.Stat(d.StaticDir)
		if err != nil || !info.IsDir() {
			return fmt.Errorf("release static directory is gone; rebuild the app")
		}
		return nil
	}
	if d.Image == "" || exec.Command("docker", "image", "inspect", d.Image).Run() != nil {
		return fmt.Errorf("release image is gone; rebuild the app")
	}
	return nil
}

func (a *Server) startRelease(app Record, id, image, network string, logger io.Writer) (e error) {
	a.status(id, "starting")
	container := "startship-" + app.ID + "-" + id
	defer func() {
		if e != nil {
			command("", logger, "docker", "rm", "-f", container)
		}
	}()
	args := append(appRunArgs(app, network, false), "--name", container)
	if externalEngine(app.Pipeline) && app.Pipeline.Deploy.Command != "" {
		args = append(args, engineCommand(app.Pipeline, image, "exec "+app.Pipeline.Deploy.Command, app.Pipeline.Deploy.Image != "")...)
	} else {
		args = append(args, image)
	}
	if e = command("", logger, "docker", args...); e != nil {
		return
	}
	var ip string
	ip, e = containerIP(container, network)
	if e != nil {
		return
	}
	timeout := 60 * time.Second
	if app.Pipeline != nil && app.Pipeline.Deploy.HealthTimeout > 0 {
		timeout = time.Duration(app.Pipeline.Deploy.HealthTimeout) * time.Second
	}
	deadline := time.Now().Add(timeout)
	for {
		address := net.JoinHostPort(ip, fmt.Sprint(app.Port))
		if app.Pipeline != nil && app.Pipeline.Deploy.HealthPath != "" {
			client := http.Client{Timeout: time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
			var response *http.Response
			response, e = client.Get("http://" + address + app.Pipeline.Deploy.HealthPath)
			if e == nil {
				response.Body.Close()
				expected := app.Pipeline.Deploy.ExpectStatus
				if expected == 0 {
					expected = 200
				}
				if response.StatusCode != expected {
					e = fmt.Errorf("expected HTTP status %d, got %d", expected, response.StatusCode)
				}
			}
		} else {
			var c net.Conn
			c, e = net.DialTimeout("tcp", address, time.Second)
			if e == nil {
				c.Close()
			}
		}
		if e == nil {
			break
		}
		if time.Now().After(deadline) {
			command("", logger, "docker", "logs", "--tail", "100", container)
			e = fmt.Errorf("health check timed out after %s on port %d: %w", timeout, app.Port, e)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	a.store.Update(id, func(r *Record) { r.Container = container; r.IP = ip })
	return a.activate(app, id)
}

func (a *Server) logDeploymentResult(out io.Writer, app Record, err error) {
	if err != nil {
		fmt.Fprintf(out, "Deployment failed: %v\n", err)
		return
	}
	var hosts []string
	for _, route := range a.store.List("routes", "") {
		if route.AppID == app.ID {
			hosts = append(hosts, route.Host+route.PathPrefix)
		}
	}
	target := app.Name
	if len(hosts) > 0 {
		target = strings.Join(hosts, ", ")
	}
	fmt.Fprintf(out, "Deployment healthy; now serving %s\n", target)
}
func copyStatic(src, dest string) error {
	return filepath.WalkDir(src, func(p string, d os.DirEntry, e error) error {
		if e != nil {
			return e
		}
		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0700)
		}
		if d.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("static symlinks are not allowed: %s", rel)
		}
		if !d.Type().IsRegular() {
			return nil
		}
		in, e := os.Open(p)
		if e != nil {
			return e
		}
		defer in.Close()
		out, e := os.OpenFile(filepath.Join(dest, rel), os.O_CREATE|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		_, e = io.Copy(out, in)
		closeErr := out.Close()
		if e != nil {
			return e
		}
		return closeErr
	})
}
func (a *Server) activate(app Record, id string) error {
	if d, err := a.store.Get(id); err == nil && d.ReleaseID != "" {
		return a.store.Update(id, func(r *Record) { r.Status = "ready"; now := time.Now(); r.Finished = &now })
	}
	a.store.Lock()
	current, e := a.store.Get(app.ID)
	if e != nil {
		a.store.Unlock()
		return e
	}
	oldID := current.Active
	d, _ := a.store.Get(id)
	d.Status = "running"
	now := time.Now()
	d.Finished = &now
	current.Active = id
	tx, e := a.store.DB.Begin()
	if e == nil {
		for _, r := range []Record{d, current} {
			b, _ := json.Marshal(r)
			_, e = tx.Exec("UPDATE records SET body=? WHERE id=?", string(b), r.ID)
			if e != nil {
				break
			}
		}
		if e == nil {
			e = tx.Commit()
		} else {
			tx.Rollback()
		}
	}
	a.store.Unlock()
	if e != nil {
		return e
	}
	a.store.Notify(d, current)
	a.refresh()
	if oldID != "" {
		a.store.Update(oldID, func(r *Record) { r.Status = "superseded" })
		old, _ := a.store.Get(oldID)
		if old.Container != "" {
			command("", nil, "docker", "rm", "-f", old.Container)
		}
	}
	return nil
}
func containerIP(id, network string) (string, error) {
	if network == "" {
		network = "startship"
	}
	b, e := exec.Command("docker", "inspect", "--format", fmt.Sprintf("{{(index .NetworkSettings.Networks %q).IPAddress}}", network), id).Output()
	if e != nil {
		return "", e
	}
	ip := strings.TrimSpace(string(b))
	if net.ParseIP(ip) == nil {
		return "", fmt.Errorf("container has no %s IP", network)
	}
	return ip, nil
}
func (a *Server) removeActive(app Record) {
	d, _ := a.store.Get(app.Active)
	if d.Container != "" {
		command("", nil, "docker", "rm", "-f", d.Container)
	}
}
func (a *Server) restore() {
	if exec.Command("docker", "network", "inspect", "startship").Run() != nil {
		if e := command("", os.Stderr, "docker", "network", "create", "startship"); e != nil {
			fmt.Fprintln(os.Stderr, "Docker network unavailable:", e)
		}
	}
	if e := connectSelf("startship"); e != nil {
		fmt.Fprintln(os.Stderr, "Ensure startshipd is already attached to startship:", e)
	}
	for _, d := range a.store.List("deployments", "") {
		switch d.Status {
		case "running":
			if d.Container != "" {
				if d.Network != "" {
					if e := connectSelf(d.Network); e != nil {
						fmt.Fprintln(os.Stderr, "Cannot attach startshipd to project network:", e)
					}
				}
				ip, e := containerIP(d.Container, d.Network)
				a.store.Update(d.ID, func(r *Record) {
					if e == nil {
						r.IP = ip
					} else {
						r.Status = "failed"
						r.Log += "container unavailable after restart\n"
					}
				})
			}
		case "queued", "building", "testing", "starting", "ready":
			a.store.Update(d.ID, func(r *Record) { r.Status = "failed"; r.Log += "daemon restarted during deployment\n" })
			command("", nil, "docker", "rm", "-f", "startship-"+d.AppID+"-"+d.ID)
		}
	}
	for _, release := range a.store.List("releases", "") {
		interrupted := release.Status == "queued" || release.Status == "building" || release.Status == "testing" || release.Status == "starting"
		if release.Status == "running" {
			for _, id := range release.ContainerReleases {
				d, err := a.store.Get(id)
				if err != nil || d.Status != "running" {
					interrupted = true
				}
			}
		}
		if interrupted {
			a.store.Update(release.ID, func(r *Record) {
				r.Status = "failed"
				r.Log += "\nProject deployment interrupted by restart or a missing container\n"
				now := time.Now().UTC()
				r.Finished = &now
			})
		}
	}
	a.refresh()
}
