package server

import (
	"fmt"
)

// A source belongs to a project. Apps only select a build root within it.
func (a *Server) projectSource(app Record) (Record, error) {
	p, err := a.store.Get(app.ProjectID)
	if err != nil || p.Kind != "projects" || p.Owner != app.Owner {
		return Record{}, fmt.Errorf("project source unavailable")
	}
	if p.GitURL == "" {
		return Record{}, fmt.Errorf("configure the project's shared source before deploying")
	}
	if p.Branch == "" {
		p.Branch = "main"
	}
	return p, nil
}
