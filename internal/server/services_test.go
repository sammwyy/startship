package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/sammwyy/startship/internal/catalog"
)

func TestServicesAPI(t *testing.T) {
	f := newRBACFixture(t)
	f.a.dockerService = func(args ...string) (string, error) { return "ok", nil }
	base := "workspaces/" + f.workspace.ID + "/services"
	for _, body := range []string{`{"name":"BAD","engine":"redis"}`, `{"name":"cache","engine":"bogus"}`, `{"name":"cache","engine":"redis","version":"latest"}`, `{"name":"cache","engine":"redis","external_port":65536}`, `{"name":"cache","engine":"redis","external_port":80}`} {
		accountRequest(t, f.h, "POST", base, body, f.alice, 400)
	}
	s := decodeBody[Service](t, accountRequest(t, f.h, "POST", base, `{"name":"cache","engine":"postgresql","external_port":16379}`, f.alice, 201).Body.Bytes())
	route := "services/" + s.ID
	accountRequest(t, f.h, "POST", base, `{"name":"other","engine":"redis","external_port":16379}`, f.alice, 400)
	accountRequest(t, f.h, "PATCH", route, `{"name":"rename"}`, f.alice, 400)
	accountRequest(t, f.h, "PATCH", route, `{"version":"latest"}`, f.alice, 400)
	accountRequest(t, f.h, "PATCH", route, `{"config":{"password":""}}`, f.alice, 400)
	accountRequest(t, f.h, "PATCH", route, `{"config":{"password":"changed"}}`, f.alice, 200)
	accountRequest(t, f.h, "DELETE", "volumes/"+s.VolumeID, "", f.alice, 409)
	accountRequest(t, f.h, "POST", route+"/attachments", `{"project_id":"`+f.project.ID+`","alias":"INVALID"}`, f.alice, 400)
	accountRequest(t, f.h, "POST", route+"/attachments", `{"project_id":"`+f.project.ID+`","alias":"db"}`, f.alice, 200)
	accountRequest(t, f.h, "POST", route+"/attachments", `{"project_id":"`+f.project.ID+`","alias":"db"}`, f.alice, 409)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	public := decodeBody[Service](t, accountRequest(t, f.h, "GET", route, "", f.bob, 200).Body.Bytes())
	if public.Config["password"] != "" {
		t.Fatal("credentials exposed")
	}
	accountRequest(t, f.h, "PATCH", route, `{}`, f.bob, 403)
	accountRequest(t, f.h, "POST", route+"/attachments", `{"project_id":"`+f.second.ID+`","alias":"db"}`, f.bob, 403)
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	deny, _ := json.Marshal(override{Scope: scopeProject, ScopeID: f.second.ID, SubjectType: "user", SubjectID: f.bobID, Deny: []string{"project.settings"}})
	accountRequest(t, f.h, "PUT", "overrides", string(deny), f.alice, 200)
	accountRequest(t, f.h, "POST", route+"/attachments", `{"project_id":"`+f.second.ID+`","alias":"db"}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/volumes", `{"name":"no-access","project_id":"`+f.second.ID+`","mount_path":"/data"}`, f.bob, 403)
	for _, topic := range []string{"services", "volumes"} {
		data, _, _, why := f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: topic, WorkspaceID: f.workspace.ID})
		if why != "" || data == nil {
			t.Fatal(topic, why)
		}
		if !(&liveSub{Topic: topic, WorkspaceID: f.workspace.ID}).relevant(Record{Kind: topic, WorkspaceID: f.workspace.ID}) {
			t.Fatal("irrelevant")
		}
	}
	_, _, _, why := f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: "services", WorkspaceID: f.a.store.List("workspaces", f.rootID)[0].ID})
	if why != "forbidden" {
		t.Fatal(why)
	}
	accountRequest(t, f.h, "DELETE", route, "", f.alice, 200)
	v, err := getResource[Volume](f.a, "volumes", s.VolumeID)
	if err != nil || v.ServiceID != "" {
		t.Fatal("volume not retained", v, err)
	}
	accountRequest(t, f.h, "DELETE", "volumes/"+v.ID, "", f.alice, 200)
}

func TestServiceLifecycle(t *testing.T) {
	f := newRBACFixture(t)
	calls := []string{}
	f.a.dockerService = func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "ok", nil }
	e, _ := catalog.Lookup("redis")
	s := Service{ID: "lifecycle", WorkspaceID: f.workspace.ID, Name: "cache", Engine: e.ID, Version: e.Versions[0], VolumeID: "data", ExternalPort: 16379, Attachments: []Attachment{{ProjectID: f.project.ID, Alias: "cache"}}}
	f.a.saveVolume(Volume{ID: "data", WorkspaceID: f.workspace.ID, ServiceID: s.ID})
	f.a.saveService(s)
	for _, action := range []string{"recreate", "stop", "start", "restart"} {
		if err := f.a.serviceAction(s.ID, action); err != nil {
			t.Fatal(err)
		}
	}
	all := strings.Join(calls, "\n")
	for _, want := range []string{"pull redis:7.4", "--memory 1g --cpus 1", "-p 16379:6379", "-v startship-vol-data:/data", "--alias cache --alias cache.local", "redis:7.4 --appendonly yes", "stop startship-svc-lifecycle", "start startship-svc-lifecycle", "restart startship-svc-lifecycle"} {
		if !strings.Contains(all, want) {
			t.Fatal(want, all)
		}
	}
	s.Engine = "kafka"
	s.Version = "3.9.0"
	s.Config = map[string]string{"external_host": "broker.example"}
	f.a.saveService(s)
	if err := f.a.serviceAction(s.ID, "recreate"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-p 16379:9094", "KAFKA_ADVERTISED_LISTENERS=PLAINTEXT://startship-svc-lifecycle:9092,EXTERNAL://broker.example:16379"} {
		if !strings.Contains(strings.Join(calls, "\n"), want) {
			t.Fatal(want, calls)
		}
	}
	accountRequest(t, f.h, "DELETE", "services/"+s.ID+"?delete_volume=true", "", f.alice, 200)
	if _, err := getResource[Volume](f.a, "volumes", "data"); err == nil {
		t.Fatal("volume not deleted")
	}
	if !strings.Contains(strings.Join(calls, "\n"), "volume rm startship-vol-data") {
		t.Fatal(calls)
	}
	f.a.reserveServicePorts([]string{"127.0.0.1:18080", ":18081"})
	if err := f.a.validateService(Service{Name: "cache", ExternalPort: 18081}); err == nil {
		t.Fatal("configured daemon port allowed")
	}
	f.a.saveService(s)
	f.a.dockerService = func(args ...string) (string, error) { return "", fmt.Errorf("daemon unavailable") }
	accountRequest(t, f.h, "DELETE", "services/"+s.ID, "", f.alice, 500)
	if _, err := getResource[Service](f.a, "services", s.ID); err != nil {
		t.Fatal("deleted after docker failure")
	}
}
func TestServiceProjectArgs(t *testing.T) {
	f := newRBACFixture(t)
	e, _ := catalog.Lookup("postgresql")
	config, _ := catalog.Validate(e, "17", nil)
	s := Service{ID: "svc", WorkspaceID: f.workspace.ID, Engine: e.ID, Config: config, Attachments: []Attachment{{ProjectID: f.project.ID, Alias: "db"}}}
	if err := f.a.saveService(s); err != nil {
		t.Fatal(err)
	}
	v := Volume{ID: "volume", WorkspaceID: f.workspace.ID, ProjectID: f.project.ID, MountPath: "/data"}
	f.a.saveVolume(v)
	for _, test := range []bool{false, true} {
		args := strings.Join(f.a.projectRunArgs(Record{ID: "app", ProjectID: f.project.ID, Env: map[string]string{"DB_HOST": "override"}}, "network", test), " ")
		if !strings.Contains(args, "DB_HOST=override") || strings.Contains(args, "DB_HOST=db.local") || !strings.Contains(args, "DB_URL=postgres://") {
			t.Fatal(args)
		}
		if strings.Contains(args, "-v startship-vol-volume:/data") == test {
			t.Fatal(args)
		}
	}
	f.a.dockerService = func(args ...string) (string, error) { return "", fmt.Errorf("docker unavailable") }
	if err := f.a.serviceAction(s.ID, "recreate"); err == nil {
		t.Fatal("expected error")
	}
	failed, _ := getResource[Service](f.a, "services", s.ID)
	if failed.Status != "failed" || failed.LastError == "" {
		t.Fatal(failed)
	}
}
func TestVolumeValidation(t *testing.T) {
	f := newRBACFixture(t)
	f.a.dockerService = func(args ...string) (string, error) { return "", nil }
	base := "workspaces/" + f.workspace.ID + "/volumes"
	for _, mount := range []string{"/", "relative", "/a/../b", "/data:ro"} {
		accountRequest(t, f.h, "POST", base, `{"name":"data","project_id":"`+f.project.ID+`","mount_path":"`+mount+`"}`, f.alice, 400)
	}
	v := decodeBody[Volume](t, accountRequest(t, f.h, "POST", base, `{"name":"data","project_id":"`+f.project.ID+`","mount_path":"/data"}`, f.alice, 201).Body.Bytes())
	accountRequest(t, f.h, "DELETE", "volumes/"+v.ID, "", f.alice, 409)
	accountRequest(t, f.h, "PATCH", "volumes/"+v.ID, `{"project_id":"","mount_path":""}`, f.alice, 200)
	accountRequest(t, f.h, "DELETE", "volumes/"+v.ID, "", f.alice, 200)
}
