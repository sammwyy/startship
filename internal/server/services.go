package server

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sammwyy/startship/internal/catalog"
)

type Attachment struct {
	ProjectID string `json:"project_id"`
	Alias     string `json:"alias"`
}
type Service struct {
	ID           string            `json:"id"`
	WorkspaceID  string            `json:"workspace_id"`
	Name         string            `json:"name"`
	Engine       string            `json:"engine"`
	Version      string            `json:"version"`
	Config       map[string]string `json:"config"`
	ExternalPort int               `json:"external_port"`
	VolumeID     string            `json:"volume_id"`
	Attachments  []Attachment      `json:"attachments"`
	Status       string            `json:"status"`
	LastError    string            `json:"last_error"`
	Container    string            `json:"container"`
	Created      time.Time         `json:"created"`
	Updated      time.Time         `json:"updated"`
	CreatedBy    string            `json:"created_by"`
}
type Volume struct {
	ID          string    `json:"id"`
	WorkspaceID string    `json:"workspace_id"`
	Name        string    `json:"name"`
	ProjectID   string    `json:"project_id"`
	MountPath   string    `json:"mount_path"`
	ServiceID   string    `json:"service_id"`
	SizeBytes   int64     `json:"size_bytes"`
	SizeUpdated time.Time `json:"size_updated"`
	Created     time.Time `json:"created"`
	CreatedBy   string    `json:"created_by"`
}

func migrateServices(s *Store) error {
	_, err := s.DB.Exec(`CREATE TABLE IF NOT EXISTS services(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,body TEXT NOT NULL); CREATE TABLE IF NOT EXISTS volumes(id TEXT PRIMARY KEY,workspace_id TEXT NOT NULL,body TEXT NOT NULL)`)
	return err
}
func listResources[T any](a *Server, table, ws string) []T {
	out := []T{}
	rows, err := a.store.DB.Query("SELECT body FROM "+table+" WHERE (?='' OR workspace_id=?) ORDER BY id", ws, ws)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var b string
		var v T
		if rows.Scan(&b) == nil && json.Unmarshal([]byte(b), &v) == nil {
			out = append(out, v)
		}
	}
	return out
}
func getResource[T any](a *Server, table, id string) (T, error) {
	var v T
	var b string
	err := a.store.DB.QueryRow("SELECT body FROM "+table+" WHERE id=?", id).Scan(&b)
	if err == nil {
		err = json.Unmarshal([]byte(b), &v)
	}
	return v, err
}
func (a *Server) saveResource(table, id, ws string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = a.store.DB.Exec("INSERT INTO "+table+" VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET body=excluded.body", id, ws, string(b))
	if err == nil {
		a.store.Notify(Record{ID: id, Kind: table, WorkspaceID: ws})
	}
	return err
}
func (a *Server) saveService(s Service) error {
	s.Updated = time.Now().UTC()
	return a.saveResource("services", s.ID, s.WorkspaceID, s)
}
func (a *Server) saveVolume(v Volume) error { return a.saveResource("volumes", v.ID, v.WorkspaceID, v) }
func (a *Server) publicServices(ws string, x *access) []Service {
	out := listResources[Service](a, "services", ws)
	for i := range out {
		out[i] = publicService(out[i], x)
	}
	return out
}
func publicService(s Service, x *access) Service {
	if !x.at(s.WorkspaceID, "")["services.manage"] {
		e, _ := catalog.Lookup(s.Engine)
		c := map[string]string{}
		for _, f := range e.Fields {
			if f.Type != "password" {
				c[f.Key] = s.Config[f.Key]
			}
		}
		s.Config = c
		s.LastError = ""
	}
	return s
}

var serviceName = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,62}$`)

func validMount(m string) bool {
	return strings.HasPrefix(m, "/") && path.Clean(m) == m && m != "/" && !strings.ContainsAny(m, "\x00:\n")
}

var serviceLocks sync.Map

func serviceLock(id string) *sync.Mutex {
	v, _ := serviceLocks.LoadOrStore(id, &sync.Mutex{})
	return v.(*sync.Mutex)
}
func (a *Server) serviceDocker(args ...string) (string, error) {
	if a.dockerService != nil {
		return a.dockerService(args...)
	}
	b, err := exec.Command("docker", args...).CombinedOutput()
	if err != nil {
		return string(b), fmt.Errorf("docker %s: %w: %s", args[0], err, strings.TrimSpace(string(b)))
	}
	return string(b), nil
}
func volumeName(id string) string       { return "startship-vol-" + id }
func serviceContainer(id string) string { return "startship-svc-" + id }
func (a *Server) serviceNetwork(name string) error {
	if _, err := a.serviceDocker("network", "inspect", name); err == nil {
		return nil
	}
	_, err := a.serviceDocker("network", "create", "--driver", "bridge", name)
	return err
}
func (a *Server) connectService(s Service) error {
	for _, at := range s.Attachments {
		n := "startship-" + at.ProjectID
		if err := a.serviceNetwork(n); err != nil {
			return err
		}
		a.serviceDocker("network", "disconnect", n, serviceContainer(s.ID))
		if _, err := a.serviceDocker("network", "connect", "--alias", at.Alias, "--alias", at.Alias+".local", "--alias", serviceContainer(s.ID), n, serviceContainer(s.ID)); err != nil {
			return err
		}
	}
	return nil
}
func (a *Server) serviceAction(id, action string) error {
	s, err := getResource[Service](a, "services", id)
	if err != nil {
		return err
	}
	e, _ := catalog.Lookup(s.Engine)
	update := func(status string) { s.Status = status; a.saveService(s) }
	defer func() {
		if err != nil {
			s.Status = "failed"
			s.LastError = err.Error()
			a.saveService(s)
		}
	}()
	if action == "recreate" {
		update("pulling")
		_, err = a.serviceDocker("pull", e.Image+":"+s.Version)
		if err != nil {
			return err
		}
		a.serviceDocker("rm", "-f", serviceContainer(id))
		update("starting")
		if err = a.serviceNetwork("startship-services"); err != nil {
			return err
		}
		args := []string{"run", "-d", "--name", serviceContainer(id), "--hostname", serviceContainer(id), "--network", "startship-services", "--restart", "unless-stopped", "--label", "startship.managed=true", "--label", "startship.service=" + id, "--memory", limitEnv("STARTSHIP_SERVICE_MEMORY", "1g"), "--cpus", limitEnv("STARTSHIP_SERVICE_CPUS", "1")}
		if s.ExternalPort != 0 {
			port := e.Port
			if e.ID == "kafka" {
				port = 9094
			}
			args = append(args, "-p", fmt.Sprintf("%d:%d", s.ExternalPort, port))
		}
		if s.VolumeID != "" {
			args = append(args, "-v", volumeName(s.VolumeID)+":"+e.DataPath)
		}
		env := e.Env(s.Config)
		if e.ID == "kafka" {
			host := serviceContainer(id)
			env["KAFKA_ADVERTISED_LISTENERS"] = "PLAINTEXT://" + host + ":9092"
			if s.ExternalPort != 0 {
				env["KAFKA_LISTENERS"] += ",EXTERNAL://:9094"
				env["KAFKA_LISTENER_SECURITY_PROTOCOL_MAP"] += ",EXTERNAL:PLAINTEXT"
				env["KAFKA_ADVERTISED_LISTENERS"] += fmt.Sprintf(",EXTERNAL://%s:%d", s.Config["external_host"], s.ExternalPort)
			}
		}
		for k, v := range env {
			args = append(args, "-e", k+"="+v)
		}
		args = append(args, e.Image+":"+s.Version)
		args = append(args, e.Command()...)
		_, err = a.serviceDocker(args...)
	} else {
		_, err = a.serviceDocker(action, serviceContainer(id))
	}
	if err != nil {
		return err
	}
	s.Container = serviceContainer(id)
	s.LastError = ""
	if action == "stop" {
		update("stopped")
		return nil
	}
	err = a.connectService(s)
	if err == nil {
		update("running")
	}
	return err
}
func (a *Server) launchService(id, action string) {
	go func() { lock := serviceLock(id); lock.Lock(); defer lock.Unlock(); a.serviceAction(id, action) }()
}
func (a *Server) reconcileServices() {
	for _, s := range listResources[Service](a, "services", "") {
		if s.Status == "running" || s.Status == "pending" || s.Status == "pulling" || s.Status == "starting" {
			go func(s Service) {
				lock := serviceLock(s.ID)
				lock.Lock()
				defer lock.Unlock()
				if _, err := a.serviceDocker("container", "inspect", serviceContainer(s.ID)); err != nil || s.Status != "running" {
					a.serviceAction(s.ID, "recreate")
				} else {
					a.connectService(s)
				}
			}(s)
		}
	}
}
func (a *Server) projectRunArgs(app Record, network string, test bool) []string {
	env := map[string]string{}
	for _, s := range listResources[Service](a, "services", "") {
		e, ok := catalog.Lookup(s.Engine)
		if !ok {
			continue
		}
		for _, at := range s.Attachments {
			if at.ProjectID == app.ProjectID {
				for k, v := range e.Connection(at.Alias, e.Port, s.Config) {
					env[k] = v
				}
			}
		}
	}
	for k, v := range app.Env {
		env[k] = v
	}
	app.Env = env
	args := appRunArgs(app, network, test)
	if !test {
		for _, v := range listResources[Volume](a, "volumes", "") {
			if v.ProjectID == app.ProjectID {
				args = append(args, "-v", volumeName(v.ID)+":"+v.MountPath)
			}
		}
	}
	return args
}
func (a *Server) detachProjectServices(id string) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	for _, s := range listResources[Service](a, "services", "") {
		lock := serviceLock(s.ID)
		lock.Lock()
		s, err := getResource[Service](a, "services", s.ID)
		if err != nil {
			lock.Unlock()
			continue
		}
		out := []Attachment{}
		for _, at := range s.Attachments {
			if at.ProjectID != id {
				out = append(out, at)
			} else {
				a.serviceDocker("network", "disconnect", "startship-"+id, serviceContainer(s.ID))
			}
		}
		s.Attachments = out
		a.saveService(s)
		lock.Unlock()
	}
	for _, v := range listResources[Volume](a, "volumes", "") {
		if v.ProjectID == id {
			v.ProjectID = ""
			v.MountPath = ""
			a.saveVolume(v)
		}
	}
}
func (a *Server) refreshVolumeSizes(ws string) {
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	a.volumeSizeMu.Lock()
	defer a.volumeSizeMu.Unlock()
	if time.Since(a.volumeSizeAt) < 30*time.Second {
		return
	}
	a.volumeSizeAt = time.Now()
	b, err := a.serviceDocker("system", "df", "-v", "--format", "{{json .}}")
	if err != nil {
		return
	}
	var data struct {
		Volumes []struct {
			Name string
			Size string
		}
	}
	if json.Unmarshal([]byte(b), &data) != nil {
		return
	}
	for _, v := range listResources[Volume](a, "volumes", "") {
		for _, d := range data.Volumes {
			if d.Name == volumeName(v.ID) {
				v.SizeBytes = parseVolumeSize(d.Size)
				v.SizeUpdated = time.Now().UTC()
				a.saveVolume(v)
			}
		}
	}
}
func parseVolumeSize(s string) int64 {
	for _, u := range []struct {
		s string
		n float64
	}{{"GB", 1e9}, {"MB", 1e6}, {"kB", 1e3}, {"B", 1}} {
		if strings.HasSuffix(s, u.s) {
			v, _ := strconv.ParseFloat(strings.TrimSpace(strings.TrimSuffix(s, u.s)), 64)
			return int64(v * u.n)
		}
	}
	return 0
}
