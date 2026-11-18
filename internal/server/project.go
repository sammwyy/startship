package server

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/sammwyy/startship/internal/detect"
	"github.com/sammwyy/startship/internal/pipeline"
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

func migrateProjectConfig(s *Store) error {
	for _, p := range s.List("projects", "") {
		if p.ConfigVersion != 0 {
			continue
		}
		var containers []Record
		for _, c := range s.List("apps", p.Owner) {
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
		if err := s.Save(p); err != nil {
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
	if detect.ProcfileWeb(dir) != "" {
		return true
	}
	var p struct {
		Scripts map[string]string
		Main    string
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil || json.Unmarshal(b, &p) != nil {
		return false
	}
	if p.Scripts["start"] != "" || detect.ProcfileWeb(dir) != "" {
		return true
	}
	return false
}

// Workspace manifests are not servers; stop descending after finding a runnable root.
type pipelineCandidate struct {
	RootDir   string             `json:"root_dir"`
	BuildType string             `json:"build_type"`
	Port      int                `json:"port"`
	Template  string             `json:"template"`
	Pipeline  *pipeline.Pipeline `json:"pipeline"`
}

func resolvedCandidate(dir, root, kind string, p Record) (pipelineCandidate, error) {
	var resolved pipeline.Pipeline
	var err error
	if p.BuildType != "" && p.BuildType != "auto" && p.Pipeline != nil {
		resolved = *p.Pipeline
	} else {
		resolved, err = pipeline.Resolve(kind, dir)
		if err != nil {
			return pipelineCandidate{}, err
		}
		if p.Pipeline != nil {
			resolved.Test = p.Pipeline.Test
			if externalEngine(p.Pipeline) {
				port := resolved.Deploy.Port
				resolved.Deploy = p.Pipeline.Deploy
				if resolved.Deploy.Port == 0 {
					resolved.Deploy.Port = port
				}
			}
		} else if p.TestCommand != "" {
			resolved.Test.Commands = []string{p.TestCommand}
		}
	}
	if p.Port != 0 {
		resolved.Deploy.Port = p.Port
	}
	if p.Pipeline != nil {
		resolved.Deploy.HealthTimeout = p.Pipeline.Deploy.HealthTimeout
		resolved.Build.Engine = p.Pipeline.Build.Engine
		if p.Pipeline.Deploy.Port != 0 {
			resolved.Deploy.Port = p.Pipeline.Deploy.Port
		}
		if p.Pipeline.Deploy.HealthPath != "" {
			resolved.Deploy.HealthPath = p.Pipeline.Deploy.HealthPath
			resolved.Deploy.ExpectStatus = p.Pipeline.Deploy.ExpectStatus
		}
	}
	if !externalEngine(&resolved) && kind == "dockerfile" && p.Port == 0 && (p.Pipeline == nil || p.Pipeline.Deploy.Port == 0) {
		b, _ := os.ReadFile(filepath.Join(dir, resolved.Build.Dockerfile))
		if match := exposedPort.FindSubmatch(b); len(match) > 1 {
			resolved.Deploy.Port, _ = strconv.Atoi(string(match[1]))
		}
	}
	return pipelineCandidate{RootDir: root, BuildType: kind, Port: resolved.Deploy.Port, Template: kind, Pipeline: &resolved}, resolved.Validate()
}

func projectCandidates(checkout string, p Record) ([]pipelineCandidate, error) {
	root, err := checkoutRoot(checkout, p.RootDir)
	if err != nil {
		return nil, err
	}
	if p.BuildType != "" && p.BuildType != "auto" {
		c, err := resolvedCandidate(root, filepath.Clean(p.RootDir), p.BuildType, p)
		return []pipelineCandidate{c}, err
	}
	var found []pipelineCandidate
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
		kind := pipeline.DetectTemplate(path)
		if !runnable(path, kind) {
			return nil
		}
		rel, _ = filepath.Rel(checkout, path)
		c, err := resolvedCandidate(path, rel, kind, p)
		if err != nil {
			return err
		}
		found = append(found, c)
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
	if p.Pipeline != nil {
		b, _ := json.Marshal(p.Pipeline)
		_ = json.Unmarshal(b, &next.Pipeline)
	}
	if next.RootDir == "" {
		next.RootDir = "."
	}
	if next.Branch == "" {
		next.Branch = "main"
	}
	if err := a.store.Save(next); err != nil {
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
		a.store.Update(next.ID, func(r *Record) { r.Status = "failed"; r.Log = "project deployment queue full" })
		return next, fmt.Errorf("project deployment queue full")
	}
}

func (a *Server) projectReleaseAvailable(release Record) error {
	if len(release.ContainerReleases) == 0 {
		return fmt.Errorf("release has no container artifacts; deploy the project first")
	}
	for _, id := range release.ContainerReleases {
		d, err := a.store.Get(id)
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
	job, err := a.store.Get(id)
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
			d, _ := a.store.Get(revision)
			if d.Container != "" {
				command("", nil, "docker", "rm", "-f", d.Container)
			}
			if d.StaticDir != "" && d.RedeployOf == "" {
				os.RemoveAll(d.StaticDir)
			}
			a.store.Update(revision, func(r *Record) { r.Status = "failed" })
		}
		a.store.Update(id, func(r *Record) { r.Status = "failed"; now := time.Now().UTC(); r.Finished = &now })
	}()
	if current, e := a.store.Get(p.ID); e != nil || current.Owner != p.Owner {
		err = fmt.Errorf("project no longer exists")
		return
	}
	a.status(id, "building")
	checkout, commit := "", job.Commit
	var plan []pipelineCandidate
	var artifacts []Record
	if job.RedeployOf != "" {
		source, e := a.store.Get(job.RedeployOf)
		if e != nil {
			err = e
			return
		}
		if err = a.projectReleaseAvailable(source); err != nil {
			return
		}
		commit = source.Commit
		for _, revision := range source.ContainerReleases {
			d, _ := a.store.Get(revision)
			artifacts = append(artifacts, d)
			port := d.Port
			if p.Port != 0 {
				port = p.Port
			}
			plan = append(plan, pipelineCandidate{RootDir: d.RootDir, BuildType: d.BuildType, Port: port, Pipeline: d.Pipeline})
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
	a.store.Update(id, func(r *Record) { r.Commit = commit })
	fmt.Fprintf(logger, "Detected %d container(s); building one project deployment at %s\n", len(plan), commit)
	existing := a.store.List("apps", p.Owner)
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
		c.Pipeline = candidate.Pipeline
		c.ReleaseID = id
		if _, e := a.store.Get(c.ID); e != nil {
			if err = a.store.Save(c); err != nil {
				return
			}
		}
		d := Record{ID: randomID(), Kind: "deployments", Owner: p.Owner, Parent: c.ID, AppID: c.ID, ProjectID: p.ID, ReleaseID: id, Status: "queued", Created: time.Now().UTC(), RootDir: c.RootDir, BuildType: c.BuildType, Port: c.Port, Name: c.Name, NoCache: job.NoCache}
		d.Pipeline = c.Pipeline
		if job.RedeployOf != "" {
			source := artifacts[i]
			d.RedeployOf, d.Image, d.StaticDir, d.Commit = source.ID, source.Image, source.StaticDir, source.Commit
		}
		if err = a.store.Save(d); err != nil {
			return
		}
		revisions = append(revisions, d.ID)
		containers = append(containers, c)
		a.store.Update(id, func(r *Record) { r.ContainerReleases = append([]string(nil), revisions...) })
		fmt.Fprintf(logger, "\n--- Container %s · %s · :%d ---\n", c.Name, c.BuildType, c.Port)
		a.buildContainer(c, d.ID, checkout, commit)
		built, e := a.store.Get(d.ID)
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
	a.store.Lock()
	defer a.store.Unlock()
	p, err := a.store.Get(projectID)
	if err != nil {
		return err
	}
	job, err := a.store.Get(releaseID)
	if err != nil {
		return err
	}
	var changes, retired []Record
	wanted := map[string]bool{}
	now := time.Now().UTC()
	for i, c := range containers {
		wanted[c.ID] = true
		d, e := a.store.Get(revisions[i])
		if e != nil {
			return e
		}
		if d.Status != "ready" {
			return fmt.Errorf("container is not ready")
		}
		d.Status, d.Finished, c.Active = "running", &now, d.ID
		changes = append(changes, c, d)
	}
	for _, old := range a.store.List("apps", p.Owner) {
		if old.ProjectID != projectID {
			continue
		}
		if old.Active != "" {
			d, e := a.store.Get(old.Active)
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
		previous, e := a.store.Get(p.Active)
		if e == nil && previous.Kind == "releases" {
			previous.Status = "superseded"
			changes = append(changes, previous)
		}
	}
	p.Active, job.Status, job.Finished = releaseID, "running", &now
	changes = append(changes, p, job)
	tx, err := a.store.DB.Begin()
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
	a.store.Notify(changes...)
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
		source, err := a.store.Get(id)
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
		if d, err := a.store.Get(c.Active); err == nil {
			result.Status, result.Container, result.Image, result.Commit, result.ReleaseID = d.Status, d.Container, d.Image, d.Commit, d.ReleaseID
		}
	}
	// Configuration belongs to the project, not to this read-only runtime view.
	result.Env, result.TestCommand, result.AutoDeploy = nil, "", false
	return result
}

func (a *Server) containerLogs(w http.ResponseWriter, r *http.Request, c Record) {
	text, err := a.containerLogText(r.Context(), c)
	if err != nil {
		fail(w, 502, "runtime logs unavailable")
		return
	}
	reply(w, 200, map[string]string{"log": text})
}
func (a *Server) containerLogText(parent context.Context, c Record) (string, error) {
	d, err := a.store.Get(c.Active)
	if err != nil {
		return "Container is not deployed yet.", nil
	}
	if d.StaticDir != "" {
		return "Static container: files are served directly; no runtime process logs.", nil
	}
	if d.Container == "" {
		return "No runtime output available.", nil
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	output, err := exec.CommandContext(ctx, "docker", "logs", "--tail", "200", d.Container).CombinedOutput()
	return string(output), err
}
