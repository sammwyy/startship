package server

import (
	"github.com/sammwyy/startship/internal/pipeline"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

func (a *Server) detectAPI(w http.ResponseWriter, r *http.Request, x *access) {
	var v Record
	if decode(w, r, &v) != nil || v.ProjectID == "" || v.GitURL != "" || v.GitToken != "" || v.Branch != "" {
		fail(w, 400, "detect requires project_id; configure the source on the project")
		return
	}
	project := x.project(v.ProjectID)
	if !x.can(project, "project.settings") {
		fail(w, 400, "project source unavailable")
		return
	}
	source, e := a.projectSource(Record{ProjectID: v.ProjectID, Owner: project.Owner})
	if e != nil {
		fail(w, 400, e.Error())
		return
	}
	dir, e := os.MkdirTemp(a.data, "detect-")
	if e != nil {
		fail(w, 500, e.Error())
		return
	}
	defer os.RemoveAll(dir)
	if e = a.clone(source, dir, nil); e != nil {
		fail(w, 400, "clone failed")
		return
	}
	candidates := []pipelineCandidate{}
	roots := []string{"."}
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() && !strings.HasPrefix(entry.Name(), ".") {
			roots = append(roots, entry.Name())
		}
	}
	for _, relative := range roots {
		root := filepath.Join(dir, relative)
		kind := pipeline.DetectTemplate(root)
		if kind == "" {
			continue
		}
		c, err := resolvedCandidate(root, relative, kind, Record{})
		if err != nil {
			fail(w, 400, err.Error())
			return
		}
		candidates = append(candidates, c)
	}
	reply(w, 200, candidates)
}
