package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/catalog"
	"github.com/sammwyy/startship/internal/config"
)

func (a *Server) reserveServicePorts(groups ...[]string) {
	a.serviceReservedPorts = listenPorts(groups...)
}

func listenPorts(groups ...[]string) map[int]bool {
	ports := map[int]bool{}
	for _, entries := range groups {
		for _, entry := range entries {
			_, port, err := net.SplitHostPort(entry)
			if err == nil {
				n, _ := strconv.Atoi(port)
				if n > 0 {
					ports[n] = true
				}
			}
		}
	}
	return ports
}

func (a *Server) validateService(s Service) error {
	if !serviceName.MatchString(s.Name) {
		return fmt.Errorf("invalid name")
	}
	if s.ExternalPort < 0 || s.ExternalPort > 65535 {
		return fmt.Errorf("invalid external_port")
	}
	for _, v := range listResources[Service](a, "services", "") {
		if v.ID != s.ID && (v.WorkspaceID == s.WorkspaceID && v.Name == s.Name || s.ExternalPort != 0 && v.ExternalPort == s.ExternalPort) {
			return fmt.Errorf("name or port already in use")
		}
	}
	if s.ExternalPort != 0 {
		if a.daemonPortReserved(s.ExternalPort) {
			return fmt.Errorf("daemon port is reserved")
		}
		for _, app := range listResources[recipeApp](a, "recipe_apps", "") {
			if recipePortUsed(app, s.ExternalPort, "tcp") {
				return fmt.Errorf("port already used by an app")
			}
		}
	}
	return nil
}
func (a *Server) daemonPortReserved(n int) bool {
	if a.serviceReservedPorts != nil {
		return a.serviceReservedPorts[n]
	}
	defaults := config.Defaults()
	return listenPorts(defaults.DashboardListen, defaults.HTTPListen, defaults.HTTPSListen, []string{os.Getenv("STARTSHIP_DASHBOARD_ADDR"), os.Getenv("STARTSHIP_HTTP_ADDR"), os.Getenv("STARTSHIP_HTTPS_ADDR")})[n]
}
func (a *Server) resourceProject(x *access, ws, id, mount string) bool {
	if id == "" {
		return mount == ""
	}
	p, err := a.store.Get(id)
	return err == nil && p.Kind == "projects" && projectWorkspace(p) == ws && x.can(p, "project.settings")
}
func (a *Server) servicesAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) == 4 && p[1] == "workspaces" && (p[3] == "services" || p[3] == "volumes") {
		ws, err := a.store.Get(p[2])
		if err != nil || ws.Kind != "workspaces" || !x.visible(ws) {
			fail(w, 404, "not found")
			return true
		}
		if r.Method == "GET" {
			if p[3] == "services" {
				reply(w, 200, map[string]any{"services": a.publicServices(ws.ID, x)})
			} else {
				a.refreshVolumeSizes(ws.ID)
				reply(w, 200, map[string]any{"volumes": listResources[Volume](a, "volumes", ws.ID)})
			}
			return true
		}
		if !x.at(ws.ID, "")["services.manage"] {
			forbidden(w)
			return true
		}
		if r.Method != "POST" {
			fail(w, 405, "method not allowed")
			return true
		}
		a.resourcesMu.Lock()
		defer a.resourcesMu.Unlock()
		if p[3] == "services" {
			var input struct {
				Name         string            `json:"name"`
				Engine       string            `json:"engine"`
				Version      string            `json:"version"`
				Config       map[string]string `json:"config"`
				ExternalPort int               `json:"external_port"`
				VolumeID     string            `json:"volume_id"`
			}
			if decode(w, r, &input) != nil {
				fail(w, 400, "invalid JSON")
				return true
			}
			index, _ := a.serviceRegistry()
			e, ok := index.Lookup(input.Engine)
			if !ok {
				fail(w, 400, "unknown engine")
				return true
			}
			if input.Version == "" {
				input.Version = e.Versions[0]
			}
			config, err := catalog.Validate(e, input.Version, input.Config)
			s := Service{ID: randomID(), WorkspaceID: ws.ID, Name: input.Name, Engine: e.ID, Definition: &e, Version: input.Version, Config: config, ExternalPort: input.ExternalPort, VolumeID: input.VolumeID, Status: "pending", Created: time.Now().UTC(), CreatedBy: uid}
			s.Updated = s.Created
			if err == nil {
				err = a.validateService(s)
			}
			if err != nil {
				fail(w, 400, err.Error())
				return true
			}
			var vol Volume
			if s.VolumeID != "" {
				vol, err = getResource[Volume](a, "volumes", s.VolumeID)
				if err != nil || vol.WorkspaceID != ws.ID || vol.ServiceID != "" || vol.ProjectID != "" || e.DataPath == "" {
					fail(w, 400, "volume unavailable")
					return true
				}
			} else if e.DataPath != "" {
				vol = Volume{ID: randomID(), WorkspaceID: ws.ID, Name: s.Name + "-data", Created: s.Created, CreatedBy: uid}
				if _, err = a.serviceDocker("volume", "create", volumeName(vol.ID)); err != nil {
					fail(w, 500, err.Error())
					return true
				}
				s.VolumeID = vol.ID
			}
			if vol.ID != "" {
				vol.ServiceID = s.ID
				if err = a.saveVolume(vol); err != nil {
					fail(w, 500, "save failed")
					return true
				}
			}
			if err = a.saveService(s); err != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.audit(r.Context(), uid, "service.create", s.ID, scopeWorkspace, ws.ID, nil)
			a.launchService(s.ID, "recreate")
			reply(w, 201, s)
		} else {
			var input struct {
				Name      string `json:"name"`
				ProjectID string `json:"project_id"`
				MountPath string `json:"mount_path"`
			}
			if decode(w, r, &input) != nil || !serviceName.MatchString(input.Name) || input.ProjectID == "" && input.MountPath != "" || input.ProjectID != "" && !validMount(input.MountPath) {
				fail(w, 400, "invalid volume")
				return true
			}
			if !a.resourceProject(x, ws.ID, input.ProjectID, input.MountPath) {
				forbidden(w)
				return true
			}
			v := Volume{ID: randomID(), WorkspaceID: ws.ID, Name: input.Name, ProjectID: input.ProjectID, MountPath: input.MountPath, Created: time.Now().UTC(), CreatedBy: uid}
			if a.mountConflict(v) {
				fail(w, 409, "mount already in use")
				return true
			}
			if _, err = a.serviceDocker("volume", "create", volumeName(v.ID)); err != nil {
				fail(w, 500, err.Error())
				return true
			}
			if err = a.saveVolume(v); err != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.audit(r.Context(), uid, "volume.create", v.ID, scopeWorkspace, ws.ID, nil)
			a.notifyProject(v.ProjectID)
			reply(w, 201, v)
		}
		return true
	}
	if len(p) < 3 || (p[1] != "services" && p[1] != "volumes") {
		return false
	}
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	if p[1] == "volumes" {
		v, err := getResource[Volume](a, "volumes", p[2])
		if err != nil || !x.visible(Record{ID: v.WorkspaceID, Kind: "workspaces"}) {
			fail(w, 404, "not found")
			return true
		}
		if !x.at(v.WorkspaceID, "")["services.manage"] {
			forbidden(w)
			return true
		}
		if len(p) != 3 {
			fail(w, 404, "not found")
			return true
		}
		switch r.Method {
		case "PATCH":
			var input struct {
				ProjectID string `json:"project_id"`
				MountPath string `json:"mount_path"`
			}
			if decode(w, r, &input) != nil || v.ServiceID != "" || v.Purpose == backupVolumePurpose {
				fail(w, 400, "invalid volume")
				return true
			}
			if !a.resourceProject(x, v.WorkspaceID, v.ProjectID, v.MountPath) || !a.resourceProject(x, v.WorkspaceID, input.ProjectID, input.MountPath) {
				forbidden(w)
				return true
			}
			if input.ProjectID != "" && !validMount(input.MountPath) {
				fail(w, 400, "invalid mount_path")
				return true
			}
			old := v.ProjectID
			v.ProjectID = input.ProjectID
			v.MountPath = input.MountPath
			if a.mountConflict(v) {
				fail(w, 409, "mount already in use")
				return true
			}
			if a.saveVolume(v) != nil {
				fail(w, 500, "save failed")
				return true
			}
			if old != "" && old != v.ProjectID {
				a.audit(r.Context(), uid, "volume.detach", v.Name, scopeProject, old, nil)
			}
			if v.ProjectID != "" {
				a.audit(r.Context(), uid, "volume.attach", v.Name, scopeProject, v.ProjectID, nil)
			}
			a.notifyProject(old)
			a.notifyProject(v.ProjectID)
			reply(w, 200, v)
		case "DELETE":
			if v.ServiceID != "" || v.ProjectID != "" {
				fail(w, 409, "volume is in use")
				return true
			}
			if v.Purpose == backupVolumePurpose && len(a.backups(v.WorkspaceID, "")) > 0 {
				fail(w, 409, "volume still holds backups")
				return true
			}
			if _, err = a.serviceDocker("volume", "rm", volumeName(v.ID)); err != nil {
				fail(w, 500, err.Error())
				return true
			}
			a.deleteResource("volumes", v.ID, v.WorkspaceID)
			a.audit(r.Context(), uid, "volume.delete", v.ID, scopeWorkspace, v.WorkspaceID, nil)
			reply(w, 200, map[string]bool{"ok": true})
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	s, err := getResource[Service](a, "services", p[2])
	if err != nil || !x.visible(Record{ID: s.WorkspaceID, Kind: "workspaces"}) {
		fail(w, 404, "not found")
		return true
	}
	if len(p) == 3 && r.Method == "GET" {
		reply(w, 200, publicService(s, x))
		return true
	}
	if !x.at(s.WorkspaceID, "")["services.manage"] {
		forbidden(w)
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
	if len(p) == 3 {
		switch r.Method {
		case "PATCH":
			var input struct {
				Version      *string           `json:"version"`
				Config       map[string]string `json:"config"`
				ExternalPort *int              `json:"external_port"`
				Name         *string           `json:"name"`
			}
			if decode(w, r, &input) != nil || input.Name != nil {
				fail(w, 400, "invalid patch; name is immutable")
				return true
			}
			e, _ := s.engineDefinition()
			if input.Version != nil && *input.Version != s.Version {
				index, _ := a.serviceRegistry()
				if candidate, ok := index.Lookup(s.Engine); ok && slices.Contains(candidate.Versions, *input.Version) {
					e = candidate
					s.Definition = &e
				}
				s.Version = *input.Version
			}
			if input.ExternalPort != nil {
				s.ExternalPort = *input.ExternalPort
			}
			merged := map[string]string{}
			for k, v := range s.Config {
				merged[k] = v
			}
			for k, v := range input.Config {
				merged[k] = v
			}
			s.Config, err = catalog.Validate(e, s.Version, merged)
			if err == nil {
				err = a.validateService(s)
			}
			if err != nil {
				fail(w, 400, err.Error())
				return true
			}
			s.Status = "pending"
			s.Updated = time.Now().UTC()
			if a.saveService(s) != nil {
				fail(w, 500, "save failed")
				return true
			}
			a.launchService(s.ID, "recreate")
			a.audit(r.Context(), uid, "service.update", s.Name, scopeWorkspace, s.WorkspaceID, nil)
			reply(w, 200, s)
		case "DELETE":
			_, existsErr := a.serviceDocker("container", "inspect", serviceContainer(s.ID))
			if existsErr != nil && !strings.Contains(strings.ToLower(existsErr.Error()), "no such container") && !strings.Contains(strings.ToLower(existsErr.Error()), "no such object") {
				fail(w, 500, existsErr.Error())
				return true
			}
			if existsErr == nil {
				_, err = a.serviceDocker("rm", "-f", "-v", serviceContainer(s.ID))
			}
			if err != nil {
				fail(w, 500, err.Error())
				return true
			}
			if s.VolumeID != "" {
				v, e := getResource[Volume](a, "volumes", s.VolumeID)
				if e == nil {
					v.ServiceID = ""
					if r.URL.Query().Get("delete_volume") == "true" {
						if _, err = a.serviceDocker("volume", "rm", volumeName(v.ID)); err != nil {
							fail(w, 500, err.Error())
							return true
						}
						a.deleteResource("volumes", v.ID, v.WorkspaceID)
					} else {
						a.saveVolume(v)
					}
				}
			}
			a.forgetEndpoint(s.WorkspaceID, Endpoint{"service", s.ID})
			a.deleteResource("services", s.ID, s.WorkspaceID)
			a.audit(r.Context(), uid, "service.delete", s.ID, scopeWorkspace, s.WorkspaceID, nil)
			reply(w, 200, map[string]bool{"ok": true})
		default:
			fail(w, 405, "method not allowed")
		}
		return true
	}
	if len(p) == 4 && p[3] == "logs" && r.Method == "GET" {
		tail := 200
		if v := r.URL.Query().Get("tail"); v != "" {
			tail, err = strconv.Atoi(v)
		}
		if err != nil || tail < 1 || tail > 10000 {
			fail(w, 400, "invalid tail")
			return true
		}
		log, err := a.serviceDocker("logs", "--tail", strconv.Itoa(tail), serviceContainer(s.ID))
		if err != nil {
			fail(w, 500, err.Error())
		} else {
			reply(w, 200, map[string]string{"log": log})
		}
		return true
	}
	if len(p) == 4 && (p[3] == "start" || p[3] == "stop" || p[3] == "restart") && r.Method == "POST" {
		a.launchService(s.ID, p[3])
		a.audit(r.Context(), uid, "service."+p[3], s.Name, scopeWorkspace, s.WorkspaceID, nil)
		reply(w, 202, map[string]bool{"ok": true})
		return true
	}
	fail(w, 404, "not found")
	return true
}
func (a *Server) mountConflict(v Volume) bool {
	if v.ProjectID == "" {
		return false
	}
	for _, other := range listResources[Volume](a, "volumes", v.WorkspaceID) {
		if other.ID != v.ID && other.ProjectID == v.ProjectID && other.MountPath == v.MountPath {
			return true
		}
	}
	return false
}
func (a *Server) notifyProject(id string) {
	if id != "" {
		now := time.Now().UTC()
		a.store.Update(id, func(p *Record) { p.ResourcesUpdated = &now })
	}
}
func (a *Server) deleteResource(table, id, ws string) {
	a.store.DB.Exec("DELETE FROM "+table+" WHERE id=?", id)
	a.store.Notify(Record{ID: id, Kind: table, WorkspaceID: ws})
}
