package server

import (
	"encoding/json"
	"fmt"
)

// A source belongs to a project. Apps only select a build root within it.
func (a *Server) projectSource(app Record) (Record, error) {
	p, err := a.store.Get(app.ProjectID)
	if err != nil || p.Kind != "projects" || p.Owner != app.Owner {
		return Record{}, fmt.Errorf("project source unavailable")
	}
	if p.GitURL == "" || p.SourceConflict {
		return Record{}, fmt.Errorf("configure the project's shared source before deploying")
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	return p, nil
}

// Promote an existing shared app source, never pick one from conflicting repos.
// Explicit project configuration resolves a conflict without deleting any apps.
func migrateProjectSources(s *Store) error {
	s.Lock()
	defer s.Unlock()
	projects, apps := s.List("projects", ""), s.List("apps", "")
	var changes []Record
	for _, p := range projects {
		if p.GitURL == "" {
			var source Record
			conflict := false
			for _, app := range apps {
				if app.ProjectID != p.ID || app.Owner != p.Owner || app.GitURL == "" {
					continue
				}
				if app.Branch == "" {
					app.Branch = "main"
				}
				if source.GitURL == "" {
					source = app
				} else if source.GitURL != app.GitURL || source.Branch != app.Branch || source.GitToken != app.GitToken {
					conflict = true
				}
			}
			if conflict {
				p.SourceConflict = true
				changes = append(changes, p)
				continue
			}
			p.GitURL, p.Branch, p.GitToken = source.GitURL, source.Branch, source.GitToken
		}
		if p.GitURL == "" {
			continue
		}
		if p.Branch == "" {
			p.Branch = "main"
		}
		if p.WebhookSecret == "" {
			p.WebhookSecret = randomID()
		}
		p.SourceConflict = false
		changes = append(changes, p)
		for _, app := range apps {
			if app.ProjectID == p.ID && app.Owner == p.Owner && (app.GitURL != "" || app.Branch != "" || app.GitToken != "") {
				app.GitURL, app.Branch, app.GitToken = "", "", ""
				changes = append(changes, app)
			}
		}
	}
	if len(changes) == 0 {
		return nil
	}
	tx, err := s.DB.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, r := range changes {
		body, err := json.Marshal(r)
		if err != nil {
			return err
		}
		if _, err = tx.Exec("UPDATE records SET body=? WHERE id=?", string(body), r.ID); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.Notify(changes...)
	return nil
}
