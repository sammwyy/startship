package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

func previewSlug(branch string) string {
	var b strings.Builder
	for _, c := range strings.ToLower(branch) {
		if c >= 'a' && c <= 'z' || c >= '0' && c <= '9' {
			b.WriteRune(c)
		} else {
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.TrimRight(slug[:40], "-")
	}
	if slug == "" {
		slug = "branch"
	}
	return slug
}

func shortHash(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))[:6]
}

func previewHost(slug, host string) string {
	label, rest, dotted := strings.Cut(host, ".")
	if !dotted {
		return slug + "." + host
	}
	label = slug + "--" + label
	if len(label) > 63 {
		label = strings.TrimRight(label[:56], "-") + "-" + shortHash(label)
	}
	return label + "." + rest
}

func (a *Server) previews(project string) []Record {
	result := []Record{}
	for _, p := range a.store.List("previews", "") {
		if p.ProjectID == project {
			result = append(result, p)
		}
	}
	return result
}

func (a *Server) previewSleepAfter(p Record) int {
	project, _ := a.store.Get(p.ProjectID)
	if project.SleepAfter > 0 {
		return project.SleepAfter
	}
	return 5
}

func (a *Server) previewForBranch(project Record, branch string) (Record, error) {
	a.store.Lock()
	defer a.store.Unlock()
	used := map[string]bool{}
	for _, p := range a.previews(project.ID) {
		if p.Branch == branch {
			return p, nil
		}
		used[p.HostSlug] = true
	}
	slug := previewSlug(branch)
	if used[slug] {
		base := slug
		slug = base + "-" + shortHash(branch)
		for n := 1; used[slug]; n++ {
			slug = base + "-" + shortHash(fmt.Sprintf("%s:%d", branch, n))
		}
	}
	p := Record{ID: randomID(), Kind: "previews", Owner: project.Owner, Parent: project.ID, ProjectID: project.ID, Name: project.Name + ":" + branch, Branch: branch, HostSlug: slug, Status: "queued", Created: time.Now().UTC()}
	return p, a.store.Save(p)
}

func (a *Server) removePreview(p Record) error {
	state := a.projectSleepState(p.ID)
	state.mu.Lock()
	defer state.mu.Unlock()
	if err := a.store.Delete(p); err != nil {
		return err
	}
	a.certMu.Lock()
	for _, c := range a.certificates("") {
		if c.PreviewID == p.ID {
			if cancel, ok := a.issuing.Load(c.ID); ok {
				if cancel, ok := cancel.(context.CancelFunc); ok {
					cancel()
				}
			}
			if _, err := a.store.DB.Exec("DELETE FROM certificates WHERE id=?", c.ID); err != nil {
				a.certMu.Unlock()
				return err
			}
			a.store.Notify(Record{ID: c.ID, Kind: "certificates", WorkspaceID: c.WorkspaceID})
		}
	}
	a.certMu.Unlock()
	for _, kind := range []string{"deployments", "releases"} {
		for _, d := range a.store.List(kind, "") {
			if d.PreviewID != p.ID {
				continue
			}
			if d.Container != "" {
				command("", nil, "docker", "rm", "-f", d.Container)
			}
			if err := a.store.Delete(d); err != nil {
				return err
			}
		}
	}
	a.refresh()
	return nil
}

func (a *Server) activatePreview(job Record, revisions []string) error {
	state := a.projectSleepState(job.PreviewID)
	state.mu.Lock()
	defer state.mu.Unlock()
	a.store.Lock()
	defer a.store.Unlock()
	p, err := a.store.Get(job.PreviewID)
	if err != nil {
		return fmt.Errorf("preview no longer exists")
	}
	previous, _ := a.store.Get(p.Active)
	changes := []Record{}
	now := time.Now().UTC()
	for _, id := range revisions {
		d, e := a.store.Get(id)
		if e != nil || d.Status != "ready" {
			return fmt.Errorf("preview container is not ready")
		}
		d.Status, d.Finished = "running", &now
		changes = append(changes, d)
	}
	p.Active, p.Sleeping, p.Status = job.ID, false, "running"
	current, _ := a.store.Get(job.ID)
	p.Commit = current.Commit
	p.Addresses = nil
	current.Status, current.Finished = "running", &now
	changes = append(changes, p, current)
	retired := []Record{}
	if previous.ID != "" {
		previous.Status = "superseded"
		changes = append(changes, previous)
		for _, id := range previous.ContainerReleases {
			d, _ := a.store.Get(id)
			d.Status = "superseded"
			changes = append(changes, d)
			retired = append(retired, d)
		}
	}
	tx, err := a.store.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range changes {
		body, e := json.Marshal(r)
		if e != nil {
			return e
		}
		if _, err = tx.Exec("UPDATE records SET body=? WHERE id=?", string(body), r.ID); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	a.store.Notify(changes...)
	a.projectActivity(p.ID)
	a.refresh()
	for _, d := range retired {
		if d.Container != "" {
			command("", nil, "docker", "rm", "-f", d.Container)
		}
	}
	return nil
}
