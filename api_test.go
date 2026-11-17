package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestRecordCreationTimes(t *testing.T) {
	a := testServer(t)
	start := time.Now()
	uid, err := a.createUser("alice", "password123")
	if err != nil {
		t.Fatal(err)
	}
	seeded := a.store.list("workspaces", uid)
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
	before := len(a.store.list("projects", uid))
	request("POST", "projects", map[string]any{"workspace_id": "missing"}, 400)
	if len(a.store.list("projects", uid)) != before {
		t.Fatal("invalid create persisted a project")
	}
}
