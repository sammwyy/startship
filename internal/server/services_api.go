package server

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/catalog"
)

func (a *Server) reserveServicePorts(groups ...[]string) {
	a.serviceReservedPorts = map[int]bool{}
	for _, entries := range groups {
		for _, entry := range entries {
			_, port, err := net.SplitHostPort(entry)
			if err == nil {
				n, _ := strconv.Atoi(port)
				a.serviceReservedPorts[n] = true
			}
		}
	}
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
		if a.serviceReservedPorts[s.ExternalPort] {
			return fmt.Errorf("daemon port is reserved")
		}
		ports := []string{}
		if a.serviceReservedPorts == nil {
			ports = []string{"8080", "80", "443"}
		}
		for _, key := range []string{"STARTSHIP_DASHBOARD_ADDR", "STARTSHIP_HTTP_ADDR", "STARTSHIP_HTTPS_ADDR"} {
			if v := os.Getenv(key); v != "" {
				ports = append(ports, v[strings.LastIndex(v, ":")+1:])
			}
		}
		for _, p := range ports {
			if strconv.Itoa(s.ExternalPort) == p {
				return fmt.Errorf("daemon port is reserved")
			}
		}
	}
	return nil
}
func (a *Server) resourceProject(x *access, ws, id, mount string) bool {
	if id == "" {
		return mount == ""
	}
	p, err := a.store.Get(id)
	return err == nil && p.Kind == "projects" && projectWorkspace(p) == ws && x.can(p, "project.settings")
}
func (a *Server) servicesAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) == 2 && p[1] == "catalog" {
		if r.Method != "GET" {
			fail(w, 405, "method not allowed")
		} else {
			reply(w, 200, catalog.All())
		}
		return true
	}
	if len(p) == 4 && p[1] == "projects" && p[3] == "services" {
		pr, err := a.store.Get(p[2])
		if err != nil || pr.Kind != "projects" || !x.visible(pr) {
			fail(w, 404, "not found")
			return true
		}
		if r.Method != "GET" {
			fail(w, 405, "method not allowed")
			return true
		}
		attached := []any{}
		available := []Service{}
		for _, s := range listResources[Service](a, "services", projectWorkspace(pr)) {
			found := false
			for _, at := range s.Attachments {
				if at.ProjectID == pr.ID {
					found = true
					e, _ := catalog.Lookup(s.Engine)
					env := e.Connection(at.Alias, e.Port, s.Config)
					if !x.at(s.WorkspaceID, "")["services.manage"] {
						for k := range env {
							if strings.HasSuffix(k, "_PASSWORD") || strings.HasSuffix(k, "_URL") || strings.HasSuffix(k, "_SECRET_KEY") {
								env[k] = "********"
							}
						}
					}
					attached = append(attached, map[string]any{"service": publicService(s, x), "alias": at.Alias, "env": env})
				}
			}
			if !found {
				available = append(available, publicService(s, x))
			}
		}
		reply(w, 200, map[string]any{"attached": attached, "available": available})
		return true
	}
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
			e, ok := catalog.Lookup(input.Engine)
			if !ok {
				fail(w, 400, "unknown engine")
				return true
			}
			if input.Version == "" {
				input.Version = e.Versions[0]
			}
			config, err := catalog.Validate(e, input.Version, input.Config)
			s := Service{ID: randomID(), WorkspaceID: ws.ID, Name: input.Name, Engine: e.ID, Version: input.Version, Config: config, ExternalPort: input.ExternalPort, VolumeID: input.VolumeID, Status: "pending", Attachments: []Attachment{}, Created: time.Now().UTC(), CreatedBy: uid}
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
			a.audit(uid, "service.create", s.ID, scopeWorkspace, ws.ID, nil)
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
			a.audit(uid, "volume.create", v.ID, scopeWorkspace, ws.ID, nil)
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
			if decode(w, r, &input) != nil || v.ServiceID != "" {
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
			a.audit(uid, "volume.attach", v.ID, scopeWorkspace, v.WorkspaceID, nil)
			a.notifyProject(old)
			a.notifyProject(v.ProjectID)
			reply(w, 200, v)
		case "DELETE":
			if v.ServiceID != "" || v.ProjectID != "" {
				fail(w, 409, "volume is in use")
				return true
			}
			if _, err = a.serviceDocker("volume", "rm", volumeName(v.ID)); err != nil {
				fail(w, 500, err.Error())
				return true
			}
			a.deleteResource("volumes", v.ID, v.WorkspaceID)
			a.audit(uid, "volume.delete", v.ID, scopeWorkspace, v.WorkspaceID, nil)
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
			if input.Version != nil {
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
			e, _ := catalog.Lookup(s.Engine)
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
			a.deleteResource("services", s.ID, s.WorkspaceID)
			for _, at := range s.Attachments {
				a.notifyProject(at.ProjectID)
			}
			a.audit(uid, "service.delete", s.ID, scopeWorkspace, s.WorkspaceID, nil)
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
		reply(w, 202, map[string]bool{"ok": true})
		return true
	}
	if p[3] == "attachments" {
		var at Attachment
		if len(p) == 4 && r.Method == "POST" {
			if decode(w, r, &at) != nil || !serviceName.MatchString(at.Alias) {
				fail(w, 400, "invalid alias")
				return true
			}
		} else if len(p) == 5 && r.Method == "DELETE" {
			at.ProjectID = p[4]
		} else {
			fail(w, 405, "method not allowed")
			return true
		}
		if !a.resourceProject(x, s.WorkspaceID, at.ProjectID, "") {
			forbidden(w)
			return true
		}
		if at.Alias != "" {
			for _, other := range listResources[Service](a, "services", s.WorkspaceID) {
				for _, existing := range other.Attachments {
					if existing.ProjectID == at.ProjectID && (existing.Alias == at.Alias || strings.EqualFold(strings.ReplaceAll(existing.Alias, "-", "_"), strings.ReplaceAll(at.Alias, "-", "_")) || other.ID == s.ID) {
						fail(w, 409, "project attachment or alias already exists")
						return true
					}
				}
			}
			s.Attachments = append(s.Attachments, at)
		} else {
			out := []Attachment{}
			for _, v := range s.Attachments {
				if v.ProjectID != at.ProjectID {
					out = append(out, v)
				}
			}
			s.Attachments = out
			if _, err = a.serviceDocker("network", "disconnect", "startship-"+at.ProjectID, serviceContainer(s.ID)); err != nil {
				fail(w, 500, err.Error())
				return true
			}
		}
		if at.Alias != "" && s.Status == "running" {
			if err = a.connectService(s); err != nil {
				fail(w, 500, err.Error())
				return true
			}
		}
		if a.saveService(s) != nil {
			fail(w, 500, "save failed")
			return true
		}
		a.notifyProject(at.ProjectID)
		a.audit(uid, "service.attach", s.ID, scopeWorkspace, s.WorkspaceID, at)
		reply(w, 200, s)
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
