package server

import (
	"github.com/sammwyy/startship/internal/detect"
	"net/http"
	"os"
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
	reply(w, 200, detect.Candidates(dir))
}
