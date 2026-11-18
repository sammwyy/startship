package server

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"
)

func TestNextBackup(t *testing.T) {
	at := time.Date(2026, 11, 18, 10, 30, 0, 0, time.UTC) // Wednesday
	for _, c := range []struct {
		p    BackupPolicy
		want time.Time
	}{
		{BackupPolicy{Frequency: "hourly", Minute: 45}, time.Date(2026, 11, 18, 10, 45, 0, 0, time.UTC)},
		{BackupPolicy{Frequency: "hourly", Minute: 30}, time.Date(2026, 11, 18, 11, 30, 0, 0, time.UTC)},
		{BackupPolicy{Frequency: "daily", Hour: 3}, time.Date(2026, 11, 19, 3, 0, 0, 0, time.UTC)},
		{BackupPolicy{Frequency: "daily", Hour: 23, Minute: 5}, time.Date(2026, 11, 18, 23, 5, 0, 0, time.UTC)},
		{BackupPolicy{Frequency: "weekly", Weekday: 3, Hour: 10, Minute: 30}, time.Date(2026, 11, 25, 10, 30, 0, 0, time.UTC)},
		{BackupPolicy{Frequency: "weekly", Weekday: 1, Hour: 2}, time.Date(2026, 11, 23, 2, 0, 0, 0, time.UTC)},
	} {
		if got := nextBackup(c.p, at); !got.Equal(c.want) {
			t.Fatalf("%+v: got %s want %s", c.p, got, c.want)
		}
	}
	for _, p := range []BackupPolicy{{Frequency: "monthly", Keep: 1}, {Frequency: "daily", Hour: 24, Keep: 1}, {Frequency: "daily", Keep: 0}, {Frequency: "hourly", Minute: 60, Keep: 1}} {
		if validBackupPolicy(p) == nil {
			t.Fatalf("accepted %+v", p)
		}
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	for i := 0; i < 200; i++ {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out")
}

func TestBackupsAPI(t *testing.T) {
	f := newRBACFixture(t)
	var calls []string
	f.a.dockerService = func(args ...string) (string, error) {
		f.a.mu.Lock()
		calls = append(calls, strings.Join(args, " "))
		f.a.mu.Unlock()
		if args[0] == "run" && args[len(args)-3] == "backup" {
			return "2048\n", nil
		}
		return "ok", nil
	}
	f.a.dockerStream = func(w io.Writer, args ...string) error {
		_, err := io.WriteString(w, "archive:"+args[len(args)-1])
		return err
	}
	joined := func() string { f.a.mu.Lock(); defer f.a.mu.Unlock(); return strings.Join(calls, "\n") }
	s := decodeBody[Service](t, accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/services", `{"name":"db","engine":"postgresql"}`, f.alice, 201).Body.Bytes())
	mem := decodeBody[Service](t, accountRequest(t, f.h, "POST", "workspaces/"+f.workspace.ID+"/services", `{"name":"cache","engine":"redis","persistent":false}`, f.alice, 201).Body.Bytes())
	if mem.VolumeID == "" {
		accountRequest(t, f.h, "POST", "services/"+mem.ID+"/backups", "", f.alice, 400)
	}
	route := "services/" + s.ID
	accountRequest(t, f.h, "PUT", route+"/backup-schedule", `{"enabled":true,"frequency":"monthly","keep":3}`, f.alice, 400)
	got := decodeBody[Service](t, accountRequest(t, f.h, "PUT", route+"/backup-schedule", `{"enabled":true,"frequency":"daily","hour":3,"keep":2}`, f.alice, 200).Body.Bytes())
	if got.Backup == nil || got.BackupAt == nil || got.BackupAt.Hour() != 3 {
		t.Fatalf("schedule not saved: %+v", got)
	}
	f.assign(t, f.alice, f.bobID, "member", scopeWorkspace, f.workspace.ID, 201)
	accountRequest(t, f.h, "PUT", route+"/backup-schedule", `{"enabled":false,"frequency":"daily","keep":2}`, f.bob, 403)
	accountRequest(t, f.h, "POST", route+"/backups", "", f.bob, 403)
	accountRequest(t, f.h, "POST", route+"/backups", "", f.alice, 202)
	list := func() []Backup { return f.a.backups(f.workspace.ID, s.ID) }
	waitFor(t, func() bool { l := list(); return len(l) == 1 && l[0].Status == "completed" })
	b := list()[0]
	if b.SizeBytes != 2048 || b.Trigger != "manual" || b.File != s.ID+"/"+b.ID+".tar.gz" {
		t.Fatalf("unexpected backup %+v", b)
	}
	vol, ok := f.a.backupVolume(f.workspace.ID)
	if !ok || vol.ID != b.VolumeID || !strings.Contains(joined(), "volume create "+volumeName(vol.ID)) {
		t.Fatal("backup volume not created", vol, joined())
	}
	if !strings.Contains(joined(), "-v "+volumeName(s.VolumeID)+":/data:ro -v "+volumeName(vol.ID)+":/backups") {
		t.Fatal("backup did not mount volumes", joined())
	}
	accountRequest(t, f.h, "PATCH", "volumes/"+vol.ID, `{"project_id":"`+f.project.ID+`","mount_path":"/data"}`, f.alice, 400)
	accountRequest(t, f.h, "DELETE", "volumes/"+vol.ID, "", f.alice, 409)
	data, _, _, why := f.a.liveData(context.Background(), f.bobID, &liveSub{Topic: "backups", WorkspaceID: f.workspace.ID})
	if why != "" || len(data.(map[string]any)["backups"].([]Backup)) != 1 {
		t.Fatal("live snapshot", why, data)
	}
	if !(&liveSub{Topic: "backups", WorkspaceID: f.workspace.ID}).relevant(Record{Kind: "backups", WorkspaceID: f.workspace.ID}) {
		t.Fatal("irrelevant")
	}
	accountRequest(t, f.h, "GET", "backups/"+b.ID+"/download", "", f.bob, 403)
	if body := accountRequest(t, f.h, "GET", "backups/"+b.ID+"/download", "", f.alice, 200).Body.String(); body != "archive:/backups/"+b.File {
		t.Fatal("download", body)
	}
	accountRequest(t, f.h, "POST", "backups/"+b.ID+"/restore", `{}`, f.bob, 403)
	accountRequest(t, f.h, "POST", "backups/"+b.ID+"/restore", `{"service_id":"`+mem.ID+`"}`, f.alice, 400)
	accountRequest(t, f.h, "POST", "backups/"+b.ID+"/restore", `{}`, f.alice, 202)
	waitFor(t, func() bool {
		cur, _ := getResource[Service](f.a, "services", s.ID)
		return len(list()) == 2 && cur.Status != "restoring" && strings.Contains(joined(), "restore "+b.File)
	})
	if list()[0].Trigger != "pre-restore" {
		t.Fatal("no safety backup", list())
	}
	if !strings.Contains(joined(), "stop "+serviceContainer(s.ID)) || !strings.Contains(joined(), "start "+serviceContainer(s.ID)) {
		t.Fatal("restore did not cycle the service", joined())
	}

	// scheduled backups rotate only scheduled copies
	for i := 0; i < 3; i++ {
		cur, _ := getResource[Service](f.a, "services", s.ID)
		past := time.Now().Add(-time.Minute)
		cur.BackupAt = &past
		f.a.saveService(cur)
		before := len(list())
		f.a.runDueBackups(time.Now())
		waitFor(t, func() bool { return len(list()) > before || i >= 2 && len(list()) == before })
	}
	scheduled := 0
	for _, x := range list() {
		if x.Trigger == "scheduled" {
			scheduled++
		}
	}
	if scheduled != 2 || len(list()) != 4 {
		t.Fatalf("rotation kept %d scheduled of %d", scheduled, len(list()))
	}
	cur, _ := getResource[Service](f.a, "services", s.ID)
	if cur.BackupAt == nil || !cur.BackupAt.After(time.Now()) {
		t.Fatal("next backup not advanced")
	}
	accountRequest(t, f.h, "DELETE", "backups/"+b.ID, "", f.bob, 403)
	accountRequest(t, f.h, "DELETE", "backups/"+b.ID, "", f.alice, 200)
	if !strings.Contains(joined(), "rm -f /backups/"+b.File) {
		t.Fatal("archive not removed")
	}
}
