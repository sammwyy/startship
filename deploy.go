package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
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
	s := string(b)
	if l.secret != "" {
		s = strings.ReplaceAll(s, l.secret, "[REDACTED]")
	}
	e := l.a.store.update(l.id, func(r *Record) { r.Log += s })
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
	if _, e := a.projectSource(app); e != nil {
		return Record{}, e
	}
	d := Record{ID: randomID(), Kind: "deployments", Owner: app.Owner, Parent: app.ID, AppID: app.ID, Status: "queued", Created: time.Now()}
	if e := a.store.save(d); e != nil {
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
		a.store.update(d.ID, func(r *Record) { r.Status = "failed"; r.Log = "deployment queue full" })
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

func (a *Server) status(id, status string) { a.store.update(id, func(r *Record) { r.Status = status }) }
func (a *Server) build(appID, id string) {
	app, e := a.store.get(appID)
	if e != nil {
		return
	}
	logger := &logWriter{a: a, id: id, secret: app.GitToken}
	var container string
	defer func() {
		if e != nil {
			a.store.update(id, func(r *Record) { r.Status = "failed"; now := time.Now(); r.Finished = &now })
			if container != "" {
				command("", logger, "docker", "rm", "-f", container)
			}
		}
		a.logDeploymentResult(logger, app, e)
	}()
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
	a.store.update(id, func(r *Record) { r.Network = network })
	dir := filepath.Join(a.data, "builds", id)
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
	a.store.update(id, func(r *Record) { r.Commit = strings.TrimSpace(string(sha)) })
	if e = os.RemoveAll(filepath.Join(dir, ".git")); e != nil {
		return
	}
	var root string
	root, e = checkoutRoot(dir, app.RootDir)
	if e != nil {
		return
	}
	kind := app.BuildType
	if kind == "auto" {
		kind = detect(root)
	}
	if kind == "" {
		e = fmt.Errorf("no supported app detected")
		return
	}
	if kind == "static" {
		if app.TestCommand != "" {
			image := "startship/" + app.ID + ":" + id
			content := fmt.Sprintf("FROM alpine:3.23\nWORKDIR /app\nCOPY . .\nENV PORT=%d\n", app.Port)
			e = os.WriteFile(filepath.Join(root, "Dockerfile.startship-test"), []byte(content), 0600)
			if e != nil {
				return
			}
			e = buildImage(root, logger, image, "Dockerfile.startship-test")
			if e != nil {
				return
			}
			a.status(id, "testing")
			args := appRunArgs(app, network, true)
			args = append(args, image, "sh", "-c", app.TestCommand)
			e = command("", logger, "docker", args...)
			os.Remove(filepath.Join(root, "Dockerfile.startship-test"))
			a.store.update(id, func(r *Record) { r.Image = image })
			if e != nil {
				return
			}
		}
		dest := filepath.Join(a.data, "static", id)
		if e = copyStatic(root, dest); e != nil {
			return
		}
		a.store.update(id, func(r *Record) { r.StaticDir = dest })
		e = a.activate(app, id)
		return
	}
	if kind != "dockerfile" {
		var content string
		content, e = dockerfile(kind, app.Port, root)
		if e != nil {
			return
		}
		if e = os.WriteFile(filepath.Join(root, "Dockerfile.startship"), []byte(content), 0600); e != nil {
			return
		}
	}
	image := "startship/" + app.ID + ":" + id
	a.store.update(id, func(r *Record) { r.Image = image })
	dockerPath := "Dockerfile"
	if kind != "dockerfile" {
		dockerPath = "Dockerfile.startship"
	}
	if e = buildImage(root, logger, image, dockerPath); e != nil {
		return
	}
	if app.TestCommand != "" {
		a.status(id, "testing")
		args := appRunArgs(app, network, true)
		args = append(args, image, "sh", "-c", app.TestCommand)
		if e = command("", logger, "docker", args...); e != nil {
			return
		}
	}
	a.status(id, "starting")
	container = "startship-" + app.ID + "-" + id
	args := append(appRunArgs(app, network, false), "--name", container)
	args = append(args, image)
	if e = command("", logger, "docker", args...); e != nil {
		return
	}
	var ip string
	ip, e = containerIP(container, network)
	if e != nil {
		return
	}
	deadline := time.Now().Add(60 * time.Second)
	for {
		var c net.Conn
		c, e = net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(app.Port)), time.Second)
		if e == nil {
			c.Close()
			break
		}
		if time.Now().After(deadline) {
			command("", logger, "docker", "logs", "--tail", "100", container)
			e = fmt.Errorf("health check timed out after 60s on port %d: %w", app.Port, e)
			return
		}
		time.Sleep(300 * time.Millisecond)
	}
	a.store.update(id, func(r *Record) { r.Container = container; r.IP = ip })
	e = a.activate(app, id)
}

func (a *Server) logDeploymentResult(out io.Writer, app Record, err error) {
	if err != nil {
		fmt.Fprintf(out, "Deployment failed: %v\n", err)
		return
	}
	var hosts []string
	for _, route := range a.store.list("routes", "") {
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
	a.store.mu.Lock()
	current, e := a.store.get(app.ID)
	if e != nil {
		a.store.mu.Unlock()
		return e
	}
	oldID := current.Active
	d, _ := a.store.get(id)
	d.Status = "running"
	now := time.Now()
	d.Finished = &now
	current.Active = id
	tx, e := a.store.db.Begin()
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
	a.store.mu.Unlock()
	if e != nil {
		return e
	}
	a.refresh()
	if oldID != "" {
		a.store.update(oldID, func(r *Record) { r.Status = "superseded" })
		old, _ := a.store.get(oldID)
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
	d, _ := a.store.get(app.Active)
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
	for _, d := range a.store.list("deployments", "") {
		switch d.Status {
		case "running":
			if d.Container != "" {
				if d.Network != "" {
					if e := connectSelf(d.Network); e != nil {
						fmt.Fprintln(os.Stderr, "Cannot attach startshipd to project network:", e)
					}
				}
				ip, e := containerIP(d.Container, d.Network)
				a.store.update(d.ID, func(r *Record) {
					if e == nil {
						r.IP = ip
					} else {
						r.Status = "failed"
						r.Log += "container unavailable after restart\n"
					}
				})
			}
		case "queued", "building", "testing", "starting":
			a.store.update(d.ID, func(r *Record) { r.Status = "failed"; r.Log += "daemon restarted during deployment\n" })
			command("", nil, "docker", "rm", "-f", "startship-"+d.AppID+"-"+d.ID)
		}
	}
	a.refresh()
}
