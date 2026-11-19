package server

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProjectSleepSettings(t *testing.T) {
	a := testServer(t)
	uid, err := a.createUser("sleeper", "password123")
	if err != nil {
		t.Fatal(err)
	}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"sleeper","password":"password123"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	workspace := a.store.List("workspaces", uid)[0]
	p := Record{ID: "sleep-project", Kind: "projects", Owner: uid, WorkspaceID: workspace.ID, Parent: workspace.ID, Name: "Sleep"}
	if err := a.store.Save(p); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		sleeping bool
		body     string
		status   int
		after    int
	}{
		{false, `{"sleeping":true,"sleep_after":10}`, 200, 10},
		{true, `{"sleeping":false,"sleep_after":0}`, 200, 0},
		{true, `{}`, 200, 0},
		{false, `{"sleep_after":10080}`, 200, 10080},
		{false, `{"sleep_after":-1}`, 400, 10080},
		{false, `{"sleep_after":10081}`, 400, 10080},
	} {
		if err := a.store.Update(p.ID, func(r *Record) { r.Sleeping = tc.sleeping }); err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest("PATCH", "/api/projects/"+p.ID, strings.NewReader(tc.body))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != tc.status {
			t.Fatal(tc.body, w.Code, w.Body.String())
		}
		current, err := a.store.Get(p.ID)
		if err != nil || current.Sleeping != tc.sleeping || current.SleepAfter != tc.after {
			t.Fatalf("%s: %+v %v", tc.body, current, err)
		}
	}
}

func TestRecordCreationTimes(t *testing.T) {
	a := testServer(t)
	start := time.Now()
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	seeded := a.store.List("workspaces", uid)
	if len(seeded) != 1 || seeded[0].Created.Before(start) || seeded[0].Created.After(time.Now()) {
		t.Fatalf("invalid seeded workspace creation time: %+v", seeded)
	}
	h := a.api()
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "/api/login", strings.NewReader(`{"username":"alice","password":"password123"}`)))
	if w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	cookie := w.Result().Cookies()[0]
	request := func(method, path string, body map[string]any, status int) Record {
		t.Helper()
		b, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		r := httptest.NewRequest(method, "/api/"+path, strings.NewReader(string(b)))
		r.AddCookie(cookie)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatal(method, path, w.Code, w.Body.String())
		}
		var record Record
		if err := json.Unmarshal(w.Body.Bytes(), &record); err != nil {
			t.Fatal(err)
		}
		return record
	}
	ids := map[string]string{}
	for _, kind := range []string{"workspaces", "projects", "apps", "routes"} {
		t.Run(kind, func(t *testing.T) {
			body := map[string]any{"name": kind, "created": "2000-01-01T00:00:00Z"}
			switch kind {
			case "projects":
				body["workspace_id"] = ids["workspaces"]
				body["git_url"] = "file:///local/repo"
			case "apps":
				body["project_id"] = ids["projects"]
			case "routes":
				body["project_id"] = ids["projects"]
				body["app_id"] = ids["apps"]
				body["host"] = "test.local"
			}
			before := time.Now()
			created := request("POST", kind, body, 200)
			if created.Created.Before(before) || created.Created.After(time.Now()) {
				t.Fatalf("creation time not server-generated: %v", created.Created)
			}
			ids[kind] = created.ID
			path := kind + "/" + created.ID
			for _, method := range []string{"PATCH", "PUT"} {
				for _, update := range []map[string]any{{"name": "renamed"}, {"created": "2000-01-01T00:00:00Z"}, {"created": nil}} {
					updated := request(method, path, update, 200)
					if !updated.Created.Equal(created.Created) {
						t.Fatalf("%s changed creation time: %v != %v", method, updated.Created, created.Created)
					}
				}
			}
			persisted := request("GET", path, nil, 200)
			if !persisted.Created.Equal(created.Created) {
				t.Fatalf("persisted creation time changed: %v", persisted.Created)
			}
		})
	}
	before := len(a.store.List("projects", uid))
	request("POST", "projects", map[string]any{"workspace_id": "missing"}, 400)
	if len(a.store.List("projects", uid)) != before {
		t.Fatal("invalid create persisted a project")
	}
}
