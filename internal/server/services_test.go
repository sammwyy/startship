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
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/connections", `{"from":{"kind":"project","id":"`+f.project.ID+`"},"to":{"kind":"service","id":"`+s.ID+`"},"alias":"INVALID"}`, f.alice, 400)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/connections", `{"from":{"kind":"project","id":"`+f.project.ID+`"},"to":{"kind":"service","id":"`+s.ID+`"},"alias":"db"}`, f.alice, 200)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/connections", `{"from":{"kind":"project","id":"`+f.project.ID+`"},"to":{"kind":"service","id":"`+s.ID+`"},"alias":"db"}`, f.alice, 409)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	public := decodeBody[Service](t, accountRequest(t, f.h, "GET", route, "", f.bob, 200).Body.Bytes())
	if public.Config["password"] != "" {
		t.Fatal("credentials exposed")
	}
	accountRequest(t, f.h, "PATCH", route, `{}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/connections", `{"from":{"kind":"project","id":"`+f.second.ID+`"},"to":{"kind":"service","id":"`+s.ID+`"},"alias":"db"}`, f.bob, 403)
	f.assign(t, f.alice, f.bobID, "contributor", scopeWorkspace, f.workspace.ID, 201)
	deny, _ := json.Marshal(override{Scope: scopeProject, ScopeID: f.second.ID, SubjectType: "user", SubjectID: f.bobID, Deny: []string{"project.settings"}})
	accountRequest(t, f.h, "PUT", "overrides", string(deny), f.alice, 200)
	accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/connections", `{"from":{"kind":"project","id":"`+f.second.ID+`"},"to":{"kind":"service","id":"`+s.ID+`"},"alias":"db"}`, f.bob, 403)
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
	s := Service{ID: "lifecycle", WorkspaceID: f.workspace.ID, Name: "cache", Engine: e.ID, Version: e.Versions[0], Config: map[string]string{"password": "test-password"}, VolumeID: "data", ExternalPort: 16379}
	f.a.saveConnection(Connection{ID: "c1", WorkspaceID: f.workspace.ID, From: Endpoint{"project", f.project.ID}, To: Endpoint{"service", s.ID}, Alias: "cache"})
	f.a.saveVolume(Volume{ID: "data", WorkspaceID: f.workspace.ID, ServiceID: s.ID})
	f.a.saveService(s)
	for _, action := range []string{"recreate", "stop", "start", "restart"} {
		if err := f.a.serviceAction(s.ID, action); err != nil {
			t.Fatal(err)
		}
	}
	all := strings.Join(calls, "\n")
	for _, want := range []string{"pull redis:7.4", "--memory 1g --cpus 1", "-p 16379:6379", "-v startship-vol-data:/data", "--alias cache --alias cache.local", "redis:7.4 --appendonly yes --requirepass test-password", "stop startship-svc-lifecycle", "start startship-svc-lifecycle", "restart startship-svc-lifecycle"} {
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
	s.Engine = "opensearch"
	s.Version = "2.19.1"
	s.Config = map[string]string{"user": "admin", "password": "strong-password"}
	f.a.saveService(s)
	if err := f.a.serviceAction(s.ID, "recreate"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"--memory 2g --cpus 1", "OPENSEARCH_INITIAL_ADMIN_PASSWORD=strong-password", "node.store.allow_mmap=false"} {
		if !strings.Contains(strings.Join(calls, "\n"), want) {
			t.Fatal(want)
		}
	}
	calls = nil
	accountRequest(t, f.h, "DELETE", "services/"+s.ID+"?delete_volume=true", "", f.alice, 200)
	if !strings.Contains(strings.Join(calls, "\n"), "rm -f -v startship-svc-lifecycle") {
		t.Fatal("anonymous service volumes not removed", calls)
	}
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

func TestServiceRuntimeOverrides(t *testing.T) {
	f := newRBACFixture(t)
	var run string
	f.a.dockerService = func(args ...string) (string, error) {
		if args[0] == "run" {
			run = strings.Join(args, " ")
		}
		return "ok", nil
	}
	e, _ := catalog.Lookup("minio")
	e.Entrypoint = "/bin/sh"
	s := Service{ID: "objects", WorkspaceID: f.workspace.ID, Name: "objects", Engine: e.ID, Definition: &e, Version: e.Versions[0], Config: map[string]string{"access_key": "access", "secret_key": "fixture-secret"}}
	if err := f.a.saveService(s); err != nil {
		t.Fatal(err)
	}
	if err := f.a.serviceAction(s.ID, "recreate"); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"-e MINIO_ROOT_USER=access", "-e MINIO_ROOT_PASSWORD=fixture-secret", "--entrypoint /bin/sh " + e.Image + ":" + s.Version + " server /data"} {
		if !strings.Contains(run, want) {
			t.Errorf("Docker run did not include %q", want)
		}
	}
}

func TestSeaweedSecretVisibility(t *testing.T) {
	f := newRBACFixture(t)
	e, _ := catalog.Lookup("seaweedfs")
	config, err := catalog.Validate(e, e.Versions[0], nil)
	if err != nil {
		t.Fatal(err)
	}
	s := Service{ID: "objects", Name: "objects", WorkspaceID: f.workspace.ID, Engine: e.ID, Version: e.Versions[0], Config: config}
	f.a.saveConnection(Connection{ID: "c1", WorkspaceID: f.workspace.ID, From: Endpoint{"project", f.project.ID}, To: Endpoint{"service", s.ID}, Alias: "objects"})
	f.a.saveService(s)
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	r := accountRequest(t, f.h, "GET", "workspaces/"+f.workspace.ID+"/connections", "", f.bob, 200)
	if strings.Contains(r.Body.String(), config["secret_key"]) || !strings.Contains(r.Body.String(), `"OBJECTS_SECRET_KEY":"********"`) {
		t.Fatal("S3 secret exposed or masking missing")
	}
	r = accountRequest(t, f.h, "GET", "workspaces/"+f.workspace.ID+"/connections", "", f.alice, 200)
	if !strings.Contains(r.Body.String(), config["secret_key"]) {
		t.Fatal("manager lost S3 secret")
	}
	calls := []string{}
	f.a.dockerService = func(args ...string) (string, error) { calls = append(calls, strings.Join(args, " ")); return "ok", nil }
	if err := f.a.serviceAction(s.ID, "recreate"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "--entrypoint /bin/sh chrislusf/seaweedfs:3.85 -c") {
		t.Fatal("missing Seaweed bootstrap entrypoint")
	}
}

func TestServiceNetworkNames(t *testing.T) {
	f := newRBACFixture(t)
	calls := []string{}
	f.a.dockerService = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "ok", nil
	}
	s := Service{ID: "named", WorkspaceID: f.workspace.ID, Name: "primary-db"}
	f.a.saveService(s)
	if err := f.a.connectService(s); err != nil || len(calls) != 0 {
		t.Fatal("unconnected service should not join networks", err, calls)
	}
	f.a.saveConnection(Connection{ID: "link", WorkspaceID: f.workspace.ID, From: Endpoint{"project", f.project.ID}, To: Endpoint{"service", s.ID}, Alias: "db"})
	if err := f.a.connectService(s); err != nil {
		t.Fatal(err)
	}
	want := "network connect --alias db --alias db.local startship-link-link startship-svc-named"
	if !strings.Contains(strings.Join(calls, "\n"), want) {
		t.Fatal(want, calls)
	}
	f.a.dockerService = func(args ...string) (string, error) {
		if len(args) > 1 && args[0] == "network" && args[1] == "connect" {
			return "", fmt.Errorf("network unavailable")
		}
		return "ok", nil
	}
	if err := f.a.connectService(s); err == nil || err.Error() != "network unavailable" {
		t.Fatal("missing connect error", err)
	}
}
func TestProjectResourceChangeTimestamp(t *testing.T) {
	f := newRBACFixture(t)
	f.a.dockerService = func(args ...string) (string, error) { return "ok", nil }
	r := accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/volumes", `{"name":"files","project_id":"`+f.project.ID+`","mount_path":"/data"}`, f.alice, 201)
	var v Volume
	json.Unmarshal(r.Body.Bytes(), &v)
	p, _ := f.a.store.Get(f.project.ID)
	if p.ResourcesUpdated == nil {
		t.Fatal("volume attachment did not mark project changed")
	}
	first := *p.ResourcesUpdated
	accountRequest(t, f.h, "PATCH", "volumes/"+v.ID, `{"project_id":"","mount_path":""}`, f.alice, 200)
	p, _ = f.a.store.Get(f.project.ID)
	if p.ResourcesUpdated == nil || !p.ResourcesUpdated.After(first) {
		t.Fatal("volume detach did not persist a newer resource timestamp")
	}
}
func TestServiceProjectArgs(t *testing.T) {
	f := newRBACFixture(t)
	e, _ := catalog.Lookup("postgresql")
	config, _ := catalog.Validate(e, "17", nil)
	s := Service{ID: "svc", WorkspaceID: f.workspace.ID, Engine: e.ID, Config: config}
	f.a.saveConnection(Connection{ID: "c1", WorkspaceID: f.workspace.ID, From: Endpoint{"project", f.project.ID}, To: Endpoint{"service", s.ID}, Alias: "db"})
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
