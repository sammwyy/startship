package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func (s *Store) migrateProjectConfig() error {
	for _, p := range s.list("projects", "") {
		if p.ConfigVersion != 0 {
			continue
		}
		var containers []Record
		for _, c := range s.list("apps", p.Owner) {
			if c.ProjectID == p.ID {
				containers = append(containers, c)
			}
		}
		p.RootDir, p.BuildType, p.ConfigVersion = ".", "auto", 1
		if len(containers) > 0 {
			p.Env, p.TestCommand = maps.Clone(containers[0].Env), containers[0].TestCommand
			for _, c := range containers {
				p.AutoDeploy = p.AutoDeploy || c.AutoDeploy
				if !maps.Equal(p.Env, c.Env) || p.TestCommand != c.TestCommand || containers[0].AutoDeploy != c.AutoDeploy {
					p.ConfigConflict = true
				}
			}
			if len(containers) == 1 {
				p.RootDir, p.BuildType, p.Port = containers[0].RootDir, containers[0].BuildType, containers[0].Port
			}
		}
		if err := s.save(p); err != nil {
			return err
		}
	}
	return nil
}

var exposedPort = regexp.MustCompile(`(?im)^\s*EXPOSE\s+(\d+)`)

func runnable(dir, kind string) bool {
	if kind != "node" {
		return kind != ""
	}
	var p struct {
		Scripts map[string]string
		Main    string
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return false
	}
	if p.Scripts["start"] != "" || procfileWeb(dir) != "" {
		return true
	}
	return false
}

// Workspace manifests are not servers; stop descending after finding a runnable root.
func projectCandidates(checkout string, p Record) ([]Candidate, error) {
	root, err := checkoutRoot(checkout, p.RootDir)
	if err != nil {
		return nil, err
	}
	if p.BuildType != "" && p.BuildType != "auto" {
		port := p.Port
		if port == 0 {
			port = 8080
		}
		return []Candidate{{RootDir: filepath.Clean(p.RootDir), BuildType: p.BuildType, Port: port}}, nil
	}
	var found []Candidate
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if !entry.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		if rel != "." && (strings.HasPrefix(entry.Name(), ".") || strings.Contains("|node_modules|vendor|target|dist|build|", "|"+entry.Name()+"|") || strings.Count(filepath.ToSlash(rel), "/") >= 3) {
			return filepath.SkipDir
		}
		kind := detect(path)
		if !runnable(path, kind) {
			return nil
		}
		port := p.Port
		if port == 0 {
			port = 8080
			if kind == "node" {
				port = 3000
			}
			if kind == "dockerfile" {
				b, _ := os.ReadFile(filepath.Join(path, "Dockerfile"))
				if match := exposedPort.FindSubmatch(b); len(match) > 1 {
					port, _ = strconv.Atoi(string(match[1]))
				}
			}
		}
		if port < 1 || port > 65535 {
			return fmt.Errorf("invalid detected port in %s", rel)
		}
		rel, _ = filepath.Rel(checkout, path)
		found = append(found, Candidate{RootDir: rel, BuildType: kind, Port: port})
		return filepath.SkipDir
	})
	if err == nil && len(found) == 0 {
		err = fmt.Errorf("no runnable containers detected; add a Dockerfile, supported server entrypoint, or static index.html")
	}
	return found, err
}

func (a *Server) enqueueProject(p Record, next Record) (Record, error) {
	if p.ConfigConflict {
		return Record{}, fmt.Errorf("confirm shared project settings and environment before deploying; existing container settings differ")
	}
	if next.RedeployOf == "" && (p.GitURL == "" || p.SourceConflict) {
		return Record{}, fmt.Errorf("configure the project's source before deploying")
	}
	next.ID, next.Kind, next.Owner, next.Parent, next.ProjectID = randomID(), "releases", p.Owner, p.ID, p.ID
	next.Status, next.Created, next.Name = "queued", time.Now().UTC(), p.Name
	next.GitURL, next.GitToken, next.Branch = p.GitURL, p.GitToken, p.Branch
	next.RootDir, next.BuildType, next.Port, next.Env, next.TestCommand = p.RootDir, p.BuildType, p.Port, maps.Clone(p.Env), p.TestCommand
	if next.RootDir == "" {
		next.RootDir = "."
	}
	if next.Branch == "" {
		next.Branch = "main"
	}
	if err := a.store.save(next); err != nil {
		return next, err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	key := "project:" + p.ID
	q := a.queues[key]
	if q == nil {
		q = make(chan string, 100)
		a.queues[key] = q
		go func() {
			for id := range q {
				a.buildProject(id)
			}
		}()
	}
	select {
	case q <- next.ID:
		return next, nil
	default:
		a.store.update(next.ID, func(r *Record) { r.Status = "failed"; r.Log = "project deployment queue full" })
		return next, fmt.Errorf("project deployment queue full")
	}
}

func (a *Server) projectReleaseAvailable(release Record) error {
	if len(release.ContainerReleases) == 0 {
		return fmt.Errorf("release has no container artifacts; deploy the project first")
	}
	for _, id := range release.ContainerReleases {
		d, err := a.store.get(id)
		if err != nil || d.ReleaseID != release.ID || d.Owner != release.Owner || d.ProjectID != release.ProjectID {
			return fmt.Errorf("container artifact unavailable")
		}
		if err := releaseAvailable(d); err != nil {
			return err
		}
	}
	return nil
}

func (a *Server) buildProject(id string) {
	job, err := a.store.get(id)
	if err != nil {
		return
	}
	p := job
	p.ID, p.Kind = job.ProjectID, "projects"
	logger := &logWriter{a: a, id: id, secret: job.GitToken}
	var containers []Record
	var revisions []string
	defer func() {
		if err == nil {
			return
		}
		fmt.Fprintf(logger, "Project deployment failed: %v\nPrevious deployment remains active.\n", err)
		for _, revision := range revisions {
			d, _ := a.store.get(revision)
			if d.Container != "" {
				command("", nil, "docker", "rm", "-f", d.Container)
			}
			if d.StaticDir != "" && d.RedeployOf == "" {
				os.RemoveAll(d.StaticDir)
			}
			a.store.update(revision, func(r *Record) { r.Status = "failed" })
		}
		a.store.update(id, func(r *Record) { r.Status = "failed"; now := time.Now().UTC(); r.Finished = &now })
	}()
	if current, e := a.store.get(p.ID); e != nil || current.Owner != p.Owner {
		err = fmt.Errorf("project no longer exists")
		return
	}
	a.status(id, "building")
	checkout, commit := "", job.Commit
	var plan []Candidate
	var artifacts []Record
	if job.RedeployOf != "" {
		source, e := a.store.get(job.RedeployOf)
		if e != nil {
			err = e
			return
		}
		if err = a.projectReleaseAvailable(source); err != nil {
			return
		}
		commit = source.Commit
		for _, revision := range source.ContainerReleases {
			d, _ := a.store.get(revision)
			artifacts = append(artifacts, d)
			port := d.Port
			if p.Port != 0 {
				port = p.Port
			}
			plan = append(plan, Candidate{RootDir: d.RootDir, BuildType: d.BuildType, Port: port})
		}
		fmt.Fprintf(logger, "Redeploying project release %.8s without rebuilding\n", source.ID)
	} else {
		checkout = filepath.Join(a.data, "builds", id)
		if err = os.MkdirAll(filepath.Dir(checkout), 0700); err != nil {
			return
		}
		defer os.RemoveAll(checkout)
		if err = a.clone(p, checkout, logger); err != nil {
			return
		}
		var sha []byte
		sha, err = exec.Command("git", "-C", checkout, "rev-parse", "HEAD").Output()
		if err != nil {
			return
		}
		commit = strings.TrimSpace(string(sha))
		plan, err = projectCandidates(checkout, p)
		if err != nil {
			return
		}
		if err = os.RemoveAll(filepath.Join(checkout, ".git")); err != nil {
			return
		}
	}
	a.store.update(id, func(r *Record) { r.Commit = commit })
	fmt.Fprintf(logger, "Detected %d container(s); building one project deployment at %s\n", len(plan), commit)
	existing := a.store.list("apps", p.Owner)
	for i, candidate := range plan {
		c := Record{ID: randomID(), Kind: "apps", Owner: p.Owner, Parent: p.ID, ProjectID: p.ID, Name: p.Name, Created: time.Now().UTC()}
		if candidate.RootDir != "." {
			c.Name = filepath.ToSlash(candidate.RootDir)
		}
		for _, old := range existing {
			if old.ProjectID == p.ID && filepath.Clean(old.RootDir) == filepath.Clean(candidate.RootDir) {
				c.ID, c.Created = old.ID, old.Created
				break
			}
		}
		c.RootDir, c.BuildType, c.Port, c.Env, c.TestCommand = candidate.RootDir, candidate.BuildType, candidate.Port, maps.Clone(p.Env), p.TestCommand
		c.ReleaseID = id
		if _, e := a.store.get(c.ID); e != nil {
			if err = a.store.save(c); err != nil {
				return
			}
		}
		d := Record{ID: randomID(), Kind: "deployments", Owner: p.Owner, Parent: c.ID, AppID: c.ID, ProjectID: p.ID, ReleaseID: id, Status: "queued", Created: time.Now().UTC(), RootDir: c.RootDir, BuildType: c.BuildType, Port: c.Port, Name: c.Name, NoCache: job.NoCache}
		if job.RedeployOf != "" {
			source := artifacts[i]
			d.RedeployOf, d.Image, d.StaticDir, d.Commit = source.ID, source.Image, source.StaticDir, source.Commit
		}
		if err = a.store.save(d); err != nil {
			return
		}
		revisions = append(revisions, d.ID)
		containers = append(containers, c)
		a.store.update(id, func(r *Record) { r.ContainerReleases = append([]string(nil), revisions...) })
		fmt.Fprintf(logger, "\n--- Container %s · %s · :%d ---\n", c.Name, c.BuildType, c.Port)
		a.buildContainer(c, d.ID, checkout, commit)
		built, e := a.store.get(d.ID)
		if e != nil || built.Status != "ready" {
			err = fmt.Errorf("container %s did not become healthy", c.Name)
			return
		}
	}
	err = a.activateProject(p.ID, id, containers, revisions)
	if err == nil {
		fmt.Fprintln(logger, "Project deployment healthy; all containers switched together")
	}
}

func (a *Server) activateProject(projectID, releaseID string, containers []Record, revisions []string) error {
	a.store.mu.Lock()
	defer a.store.mu.Unlock()
	p, err := a.store.get(projectID)
	if err != nil {
		return err
	}
	job, err := a.store.get(releaseID)
	if err != nil {
		return err
	}
	var changes, retired []Record
	wanted := map[string]bool{}
	now := time.Now().UTC()
	for i, c := range containers {
		wanted[c.ID] = true
		d, e := a.store.get(revisions[i])
		if e != nil {
			return e
		}
		if d.Status != "ready" {
			return fmt.Errorf("container is not ready")
		}
		d.Status, d.Finished, c.Active = "running", &now, d.ID
		changes = append(changes, c, d)
	}
	for _, old := range a.store.list("apps", p.Owner) {
		if old.ProjectID != projectID {
			continue
		}
		if old.Active != "" {
			d, e := a.store.get(old.Active)
			if e == nil {
				d.Status = "superseded"
				changes = append(changes, d)
				retired = append(retired, d)
			}
		}
		if !wanted[old.ID] {
			old.Active = ""
			changes = append(changes, old)
		}
	}
	if p.Active != "" {
		previous, e := a.store.get(p.Active)
		if e == nil && previous.Kind == "releases" {
			previous.Status = "superseded"
			changes = append(changes, previous)
		}
	}
	p.Active, job.Status, job.Finished = releaseID, "running", &now
	changes = append(changes, p, job)
	tx, err := a.store.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, record := range changes {
		b, e := json.Marshal(record)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("UPDATE records SET body=? WHERE id=?", string(b), record.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.refresh()
	for _, d := range retired {
		if d.Container != "" {
			command("", nil, "docker", "rm", "-f", d.Container)
		}
	}
	return nil
}

func (a *Server) projectDeployAPI(w http.ResponseWriter, r *http.Request, p Record, action string) {
	var options struct {
		NoCache      bool   `json:"no_cache"`
		DeploymentID string `json:"deployment_id"`
	}
	if err := decode(w, r, &options); err != nil && err != io.EOF {
		fail(w, 400, "invalid deployment options")
		return
	}
	next := Record{NoCache: options.NoCache}
	if action == "redeploy" {
		id := options.DeploymentID
		if id == "" {
			id = p.Active
		}
		source, err := a.store.get(id)
		if err != nil || source.Kind != "releases" || source.Owner != p.Owner || source.ProjectID != p.ID {
			fail(w, 404, "project release not found")
			return
		}
		if source.Status != "running" && source.Status != "superseded" {
			fail(w, 409, "only successful project releases can be redeployed")
			return
		}
		if err := a.projectReleaseAvailable(source); err != nil {
			fail(w, 409, err.Error())
			return
		}
		next.RedeployOf, next.Commit = source.ID, source.Commit
	}
	d, err := a.enqueueProject(p, next)
	if err != nil {
		fail(w, 409, err.Error())
		return
	}
	reply(w, 202, publicRecord(d))
}

func (a *Server) containerRecord(c Record) apiRecord {
	result := publicRecord(c)
	if c.Active != "" {
		if d, err := a.store.get(c.Active); err == nil {
			result.Status, result.Container, result.Image, result.Commit, result.ReleaseID = d.Status, d.Container, d.Image, d.Commit, d.ReleaseID
		}
	}
	// Configuration belongs to the project, not to this read-only runtime view.
	result.Env, result.TestCommand, result.AutoDeploy = nil, "", false
	return result
}

func (a *Server) containerLogs(w http.ResponseWriter, r *http.Request, c Record) {
	d, err := a.store.get(c.Active)
	if err != nil {
		reply(w, 200, map[string]string{"log": "Container is not deployed yet."})
		return
	}
	if d.StaticDir != "" {
		reply(w, 200, map[string]string{"log": "Static container: files are served directly; no runtime process logs."})
		return
	}
	if d.Container == "" {
		reply(w, 200, map[string]string{"log": "No runtime output available."})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "logs", "--tail", "200", d.Container).CombinedOutput()
	if err != nil {
		fail(w, 502, "runtime logs unavailable")
		return
	}
	reply(w, 200, map[string]string{"log": string(output)})
}
