package server

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"
)

type BackupPolicy struct {
	Enabled   bool   `json:"enabled"`
	Frequency string `json:"frequency"`
	Minute    int    `json:"minute"`
	Hour      int    `json:"hour"`
	Weekday   int    `json:"weekday"`
	Keep      int    `json:"keep"`
}
type Backup struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	ServiceID   string    `json:"service_id"`
	ServiceName string    `json:"service_name"`
	Engine      string    `json:"engine"`
	Version     string    `json:"version"`
	VolumeID    string    `json:"volume_id"`
	File        string    `json:"file"`
	SizeBytes   int64     `json:"size_bytes"`
	Status      string    `json:"status"`
	Trigger     string    `json:"trigger"`
	Error       string    `json:"error,omitempty"`
	Created     time.Time `json:"created"`
	Finished    time.Time `json:"finished"`
	CreatedBy   string    `json:"created_by"`
}

const backupVolumePurpose = "backups"

func validBackupPolicy(p BackupPolicy) error {
	switch p.Frequency {
	case "hourly", "daily", "weekly":
	default:
		return fmt.Errorf("frequency must be hourly, daily or weekly")
	}
	if p.Minute < 0 || p.Minute > 59 || p.Hour < 0 || p.Hour > 23 || p.Weekday < 0 || p.Weekday > 6 {
		return fmt.Errorf("invalid backup time")
	}
	if p.Keep < 1 || p.Keep > 100 {
		return fmt.Errorf("keep must be 1-100")
	}
	return nil
}

// nextBackup returns the first scheduled time strictly after t (UTC).
func nextBackup(p BackupPolicy, t time.Time) time.Time {
	t = t.UTC()
	switch p.Frequency {
	case "hourly":
		n := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), p.Minute, 0, 0, time.UTC)
		if !n.After(t) {
			n = n.Add(time.Hour)
		}
		return n
	case "weekly":
		n := time.Date(t.Year(), t.Month(), t.Day(), p.Hour, p.Minute, 0, 0, time.UTC)
		n = n.AddDate(0, 0, (p.Weekday-int(n.Weekday())+7)%7)
		if !n.After(t) {
			n = n.AddDate(0, 0, 7)
		}
		return n
	}
	n := time.Date(t.Year(), t.Month(), t.Day(), p.Hour, p.Minute, 0, 0, time.UTC)
	if !n.After(t) {
		n = n.AddDate(0, 0, 1)
	}
	return n
}

func backupImage() string {
	if v := os.Getenv("STARTSHIP_BACKUP_IMAGE"); v != "" {
		return v
	}
	return "busybox:1.37"
}
func backupFile(b Backup) string { return b.ServiceID + "/" + b.ID + ".tar.gz" }

func (a *Server) backupStream(ctx context.Context, w io.Writer, args ...string) error {
	if a.dockerStream != nil {
		return a.dockerStream(w, args...)
	}
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = w
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func (a *Server) backups(ws, service string) []Backup {
	out := []Backup{}
	for _, b := range listResources[Backup](a, "backups", ws) {
		if service == "" || b.ServiceID == service {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.After(out[j].Created) })
	return out
}

func (a *Server) backupVolume(ws string) (Volume, bool) {
	for _, v := range listResources[Volume](a, "volumes", ws) {
		if v.Purpose == backupVolumePurpose {
			return v, true
		}
	}
	return Volume{}, false
}

func (a *Server) ensureBackupVolume(ws, uid string) (Volume, error) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if v, ok := a.backupVolume(ws); ok {
		return v, nil
	}
	v := Volume{ID: randomID(), WorkspaceID: ws, Name: "service-backups", Purpose: backupVolumePurpose, Created: time.Now().UTC(), CreatedBy: uid}
	if _, err := a.serviceDocker("volume", "create", volumeName(v.ID)); err != nil {
		return v, err
	}
	return v, a.saveVolume(v)
}

func (a *Server) saveBackup(b Backup) error { return a.saveResource("backups", b.ID, b.WorkspaceID, b) }

// runBackup copies a service's data volume into the workspace backup volume.
// The caller holds the service lock.
func (a *Server) runBackup(s Service, trigger, uid string) (Backup, error) {
	b := Backup{ID: randomID(), WorkspaceID: s.WorkspaceID, ServiceID: s.ID, ServiceName: s.Name, Engine: s.Engine, Version: s.Version, Status: "running", Trigger: trigger, Created: time.Now().UTC(), CreatedBy: uid}
	b.File = backupFile(b)
	fail := func(err error) (Backup, error) {
		b.Status, b.Error, b.Finished = "failed", err.Error(), time.Now().UTC()
		a.saveBackup(b)
		return b, err
	}
	if s.VolumeID == "" {
		return fail(fmt.Errorf("service has no persistent volume"))
	}
	vol, err := a.ensureBackupVolume(s.WorkspaceID, uid)
	if err != nil {
		return fail(err)
	}
	b.VolumeID = vol.ID
	a.saveBackup(b)
	if _, err = a.serviceDocker("image", "inspect", backupImage()); err != nil {
		if _, err = a.serviceDocker("pull", backupImage()); err != nil {
			return fail(err)
		}
	}
	paused := false
	if s.Status == "running" {
		if _, err := a.serviceDocker("pause", serviceContainer(s.ID)); err == nil {
			paused = true
		}
	}
	out, err := a.serviceDocker("run", "--rm", "--label", "startship.managed=true", "-v", volumeName(s.VolumeID)+":/data:ro", "-v", volumeName(vol.ID)+":/backups", backupImage(), "sh", "-c", `set -e; mkdir -p "/backups/$1"; tar -czf "/backups/$2.part" -C /data .; mv "/backups/$2.part" "/backups/$2"; wc -c < "/backups/$2"`, "backup", s.ID, b.File)
	if paused {
		a.serviceDocker("unpause", serviceContainer(s.ID))
	}
	if err != nil {
		return fail(err)
	}
	fields := strings.Fields(out)
	if len(fields) > 0 {
		b.SizeBytes, _ = strconv.ParseInt(fields[len(fields)-1], 10, 64)
	}
	b.Status, b.Finished = "completed", time.Now().UTC()
	a.staleVolumeSizes()
	if err = a.saveBackup(b); err != nil {
		return b, err
	}
	if trigger == "scheduled" && s.Backup != nil {
		a.pruneBackups(s.WorkspaceID, s.ID, s.Backup.Keep)
	}
	return b, nil
}

func (a *Server) pruneBackups(workspace, service string, keep int) {
	n := 0
	for _, b := range a.backups(workspace, service) {
		if b.Trigger != "scheduled" || b.Status != "completed" {
			continue
		}
		if n++; n > keep {
			a.deleteBackup(b)
		}
	}
}

func (a *Server) deleteBackup(b Backup) error {
	if b.VolumeID != "" && b.Status != "failed" {
		if _, err := a.serviceDocker("run", "--rm", "--label", "startship.managed=true", "-v", volumeName(b.VolumeID)+":/backups", backupImage(), "rm", "-f", "/backups/"+b.File, "/backups/"+b.File+".part"); err != nil {
			return err
		}
	}
	a.staleVolumeSizes()
	a.deleteResource("backups", b.ID, b.WorkspaceID)
	return nil
}

func (a *Server) staleVolumeSizes() {
	a.volumeSizeMu.Lock()
	a.volumeSizeAt = time.Time{}
	a.volumeSizeMu.Unlock()
}

// restoreBackup replaces a service's data with a backup. The caller holds the service lock.
func (a *Server) restoreBackup(b Backup, s Service, uid string) (err error) {
	if s.VolumeID == "" {
		return fmt.Errorf("service has no persistent volume")
	}
	if _, err = a.runBackup(s, "pre-restore", uid); err != nil {
		return fmt.Errorf("safety backup failed: %w", err)
	}
	was := s.Status
	s.Status = "restoring"
	a.saveService(s)
	defer func() {
		s, _ = getResource[Service](a, "services", s.ID)
		if err != nil {
			s.Status, s.LastError = "failed", "restore failed: "+err.Error()
			a.saveService(s)
		}
	}()
	if _, err = a.serviceDocker("stop", serviceContainer(s.ID)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "no such container") {
		return err
	}
	if _, err = a.serviceDocker("run", "--rm", "--label", "startship.managed=true", "-v", volumeName(s.VolumeID)+":/data", "-v", volumeName(b.VolumeID)+":/backups:ro", backupImage(), "sh", "-c", `set -e; test -f "/backups/$1"; find /data -mindepth 1 -maxdepth 1 -exec rm -rf {} +; tar -xzf "/backups/$1" -C /data`, "restore", b.File); err != nil {
		return err
	}
	if was == "stopped" {
		s.Status = "stopped"
		return a.saveService(s)
	}
	if _, err = a.serviceDocker("start", serviceContainer(s.ID)); err != nil {
		return a.serviceAction(s.ID, "recreate")
	}
	if err = a.connectService(s); err != nil {
		return err
	}
	s.Status, s.LastError = "running", ""
	return a.saveService(s)
}

func (a *Server) scheduleBackups(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			a.runDueBackups(now)
		}
	}
}

func (a *Server) runDueBackups(now time.Time) {
	for _, s := range listResources[Service](a, "services", "") {
		p := s.Backup
		if p == nil || !p.Enabled || s.VolumeID == "" || s.BackupAt == nil || s.BackupAt.After(now) {
			continue
		}
		go func(id string) {
			lock := serviceLock(id)
			lock.Lock()
			defer lock.Unlock()
			s, err := getResource[Service](a, "services", id)
			if err != nil || s.Backup == nil || !s.Backup.Enabled || s.BackupAt == nil || s.BackupAt.After(time.Now()) {
				return
			}
			next := nextBackup(*s.Backup, time.Now())
			s.BackupAt = &next
			a.saveService(s)
			a.runBackup(s, "scheduled", "")
		}(s.ID)
	}
}

func (a *Server) backupsAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if r.Method != "GET" && (len(p) > 1 && p[1] == "backups" ||
		len(p) == 4 && p[1] == "services" && (p[3] == "backups" || p[3] == "backup-schedule")) {
		a.transferMu.Lock()
		defer a.transferMu.Unlock()
	}
	if len(p) == 4 && p[1] == "workspaces" && p[3] == "backups" {
		ws, err := a.store.Get(p[2])
		if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
			fail(w, 404, "not found")
		} else if r.Method != "GET" {
			fail(w, 405, "method not allowed")
		} else {
			reply(w, 200, a.backupsData(ws.ID, r.URL.Query().Get("service_id")))
		}
		return true
	}
	if len(p) == 4 && p[1] == "services" && (p[3] == "backups" || p[3] == "backup-schedule") {
		s, err := getResource[Service](a, "services", p[2])
		if err != nil || !x.visible(Record{ID: s.WorkspaceID, Kind: "workspaces"}) {
			fail(w, 404, "not found")
			return true
		}
		if p[3] == "backups" && r.Method == "GET" {
			reply(w, 200, a.backupsData(s.WorkspaceID, s.ID))
			return true
		}
		if !x.at(s.WorkspaceID, "")["services.manage"] {
			forbidden(w)
			return true
		}
		if p[3] == "backups" && r.Method == "POST" {
			if s.VolumeID == "" {
				fail(w, 400, "service has no persistent volume")
				return true
			}
			lock := serviceLock(s.ID)
			if !lock.TryLock() {
				fail(w, 409, "service is busy")
				return true
			}
			go func() {
				defer lock.Unlock()
				if s, err := getResource[Service](a, "services", s.ID); err == nil {
					a.runBackup(s, "manual", uid)
				}
			}()
			a.audit(r.Context(), uid, "backup.create", s.ID, scopeWorkspace, s.WorkspaceID, nil)
			reply(w, 202, map[string]bool{"ok": true})
			return true
		}
		if p[3] == "backup-schedule" && r.Method == "PUT" {
			var policy BackupPolicy
			if decode(w, r, &policy) != nil {
				fail(w, 400, "invalid JSON")
				return true
			}
			if err := validBackupPolicy(policy); err != nil {
				fail(w, 400, err.Error())
				return true
			}
			if policy.Enabled && s.VolumeID == "" {
				fail(w, 400, "service has no persistent volume")
				return true
			}
			lock := serviceLock(s.ID)
			lock.Lock()
			defer lock.Unlock()
			s, err = getResource[Service](a, "services", s.ID)
			if err != nil {
				fail(w, 404, "not found")
				return true
			}
			s.Backup, s.BackupAt = &policy, nil
			if policy.Enabled {
				next := nextBackup(policy, time.Now())
				s.BackupAt = &next
			}
			if a.saveService(s) != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.audit(r.Context(), uid, "backup.schedule", s.ID, scopeWorkspace, s.WorkspaceID, policy)
			reply(w, 200, publicService(s, x))
			return true
		}
		fail(w, 405, "method not allowed")
		return true
	}
	if len(p) < 3 || p[1] != "backups" {
		return false
	}
	b, err := getResource[Backup](a, "backups", p[2])
	if err != nil || !x.visible(Record{ID: b.WorkspaceID, Kind: "workspaces"}) {
		fail(w, 404, "not found")
		return true
	}
	if len(p) == 3 && r.Method == "GET" {
		reply(w, 200, b)
		return true
	}
	if !x.at(b.WorkspaceID, "")["services.manage"] {
		forbidden(w)
		return true
	}
	switch {
	case len(p) == 3 && r.Method == "DELETE":
		if b.Status == "running" {
			fail(w, 409, "backup is still running")
			return true
		}
		if err := a.deleteBackup(b); err != nil {
			fail(w, 500, err.Error())
			return true
		}
		a.audit(r.Context(), uid, "backup.delete", b.ID, scopeWorkspace, b.WorkspaceID, nil)
		reply(w, 200, map[string]bool{"ok": true})
	case len(p) == 4 && p[3] == "download" && r.Method == "GET":
		if b.Status != "completed" {
			fail(w, 409, "backup is not complete")
			return true
		}
		w.Header().Set("Content-Type", "application/gzip")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-%s.tar.gz"`, b.ServiceName, b.Created.Format("20060102-150405")))
		if b.SizeBytes > 0 {
			w.Header().Set("Content-Length", strconv.FormatInt(b.SizeBytes, 10))
		}
		a.audit(r.Context(), uid, "backup.download", b.ID, scopeWorkspace, b.WorkspaceID, nil)
		a.backupStream(r.Context(), w, "run", "--rm", "--label", "startship.managed=true", "-v", volumeName(b.VolumeID)+":/backups:ro", backupImage(), "cat", "/backups/"+b.File)
	case len(p) == 4 && p[3] == "restore" && r.Method == "POST":
		var input struct {
			ServiceID string `json:"service_id"`
		}
		if r.ContentLength != 0 && decode(w, r, &input) != nil {
			fail(w, 400, "invalid JSON")
			return true
		}
		if input.ServiceID == "" {
			input.ServiceID = b.ServiceID
		}
		if b.Status != "completed" {
			fail(w, 409, "backup is not complete")
			return true
		}
		s, err := getResource[Service](a, "services", input.ServiceID)
		if err != nil || s.WorkspaceID != b.WorkspaceID {
			fail(w, 404, "service not found")
			return true
		}
		if s.Engine != b.Engine || s.VolumeID == "" {
			fail(w, 400, "restore needs a "+b.Engine+" service with a persistent volume")
			return true
		}
		lock := serviceLock(s.ID)
		if !lock.TryLock() {
			fail(w, 409, "service is busy")
			return true
		}
		go func() {
			defer lock.Unlock()
			if s, err := getResource[Service](a, "services", s.ID); err == nil {
				a.restoreBackup(b, s, uid)
			}
		}()
		a.audit(r.Context(), uid, "backup.restore", b.ID, scopeWorkspace, b.WorkspaceID, map[string]string{"service_id": s.ID})
		reply(w, 202, map[string]bool{"ok": true})
	default:
		fail(w, 405, "method not allowed")
	}
	return true
}

func (a *Server) backupsData(ws, service string) map[string]any {
	data := map[string]any{"backups": a.backups(ws, service), "volume": nil}
	if v, ok := a.backupVolume(ws); ok {
		data["volume"] = v
	}
	return data
}
