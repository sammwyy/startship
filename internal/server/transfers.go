package server

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
)

type transferRef struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
}

type transferResource struct {
	transferRef
	Name string `json:"name"`
}

type transferInput struct {
	Resource    transferRef    `json:"resource"`
	Destination string         `json:"destination_workspace_id"`
	Include     *[]transferRef `json:"include"`
	PlanID      string         `json:"plan_id"`
}

type transferNode struct {
	transferResource
	project Record
	service Service
	volume  Volume
	app     recipeApp
}

func (n transferNode) workspace() string {
	switch n.Kind {
	case "projects":
		return projectWorkspace(n.project)
	case "services":
		return n.service.WorkspaceID
	case "volumes":
		return n.volume.WorkspaceID
	default:
		return n.app.WorkspaceID
	}
}

func (a *Server) transferNode(ref transferRef) (transferNode, error) {
	n := transferNode{transferResource: transferResource{transferRef: ref}}
	var err error
	switch ref.Kind {
	case "projects":
		n.project, err = a.store.Get(ref.ID)
		if err == nil && n.project.Kind != ref.Kind {
			err = sql.ErrNoRows
		}
		n.Name = n.project.Name
	case "apps":
		n.app, err = getResource[recipeApp](a, "recipe_apps", ref.ID)
		n.Name = n.app.Name
	case "services":
		n.service, err = getResource[Service](a, "services", ref.ID)
		n.Name = n.service.Name
	case "volumes":
		n.volume, err = getResource[Volume](a, "volumes", ref.ID)
		n.Name = n.volume.Name
	default:
		err = sql.ErrNoRows
	}
	return n, err
}

func (a *Server) transferGraph(root transferNode) []transferNode {
	ws := root.workspace()
	edges := map[transferRef][]transferRef{}
	link := func(from transferRef, kind, id string) {
		if id != "" {
			to := transferRef{kind, id}
			edges[from] = append(edges[from], to)
			edges[to] = append(edges[to], from)
		}
	}
	for _, s := range listResources[Service](a, "services", ws) {
		from := transferRef{"services", s.ID}
		link(from, "volumes", s.VolumeID)
	}
	for _, c := range a.connections(ws) {
		link(transferRef{c.From.Kind + "s", c.From.ID}, c.To.Kind+"s", c.To.ID)
	}
	for _, v := range listResources[Volume](a, "volumes", ws) {
		from := transferRef{"volumes", v.ID}
		link(from, "projects", v.ProjectID)
		link(from, "services", v.ServiceID)
	}
	result := []transferNode{root}
	seen := map[transferRef]bool{root.transferRef: true}
	for i := 0; i < len(result); i++ {
		for _, ref := range edges[result[i].transferRef] {
			if seen[ref] {
				continue
			}
			seen[ref] = true
			if n, err := a.transferNode(ref); err == nil && n.workspace() == ws {
				result = append(result, n)
			}
		}
	}
	return result
}

func (n transferNode) sourceAllowed(x *access) bool {
	ws := n.workspace()
	switch n.Kind {
	case "projects":
		return x.can(n.project, "project.view") && x.can(n.project, "project.delete") &&
			(len(n.project.Env) == 0 || x.can(n.project, "project.environment")) &&
			(n.project.GitToken == "" || x.can(n.project, "project.source"))
	case "apps":
		return x.at(ws, "")["apps.manage"]
	case "volumes":
		return x.at(ws, "")["services.manage"] && n.volume.Purpose != backupVolumePurpose &&
			x.a.resourceProject(x, ws, n.volume.ProjectID, n.volume.MountPath)
	default:
		return x.at(ws, "")["services.manage"]
	}
}

func (n transferNode) destinationAllowed(x *access, id string) bool {
	key := map[string]string{"projects": "projects.create", "apps": "apps.manage", "services": "services.manage", "volumes": "services.manage"}[n.Kind]
	return x.at(id, "")[key]
}

func (a *Server) transfersAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if p[1] != "transfers" {
		return false
	}
	if len(p) != 3 || !slices.Contains([]string{"plan", "copy", "move"}, p[2]) {
		fail(w, 404, "unknown transfer operation")
		return true
	}
	if r.Method != "POST" {
		fail(w, 405, "method not allowed")
		return true
	}
	var in transferInput
	if decode(w, r, &in) != nil || in.Resource.ID == "" {
		fail(w, 400, "invalid transfer request")
		return true
	}
	a.transferMu.Lock()
	defer a.transferMu.Unlock()
	a.resourcesMu.Lock()
	defer a.resourcesMu.Unlock()
	root, err := a.transferNode(in.Resource)
	if err != nil || root.workspace() == "" || !x.memberOf(root.workspace()) || root.Kind == "projects" && !x.visible(root.project) {
		fail(w, 404, "source resource not found")
		return true
	}
	if !root.sourceAllowed(x) {
		fail(w, 403, "you do not have permission to transfer this resource from the source workspace")
		return true
	}
	graph := a.transferGraph(root)
	hash := sha256.New()
	for _, n := range graph {
		fmt.Fprintf(hash, "%s/%s/%s/%s/%s;", n.Kind, n.ID, n.service.VolumeID, n.volume.ProjectID, n.volume.ServiceID)
	}
	planID := fmt.Sprintf("%x", hash.Sum(nil))
	if p[2] == "plan" {
		type dependency struct {
			transferResource
			Allowed bool `json:"allowed"`
		}
		linked := []dependency{}
		for _, n := range graph[1:] {
			allowed := n.sourceAllowed(x)
			resource := n.transferResource
			if n.Kind == "projects" && !x.visible(n.project) {
				resource = transferResource{transferRef: transferRef{Kind: n.Kind}}
			}
			linked = append(linked, dependency{resource, allowed})
		}
		reply(w, 200, map[string]any{"source_workspace_id": root.workspace(), "root": root.transferResource, "linked": linked, "plan_id": planID})
		return true
	}
	if in.PlanID != "" && in.PlanID != planID {
		fail(w, 409, "resource links changed; reopen the transfer to review them")
		return true
	}
	dst, err := a.store.Get(in.Destination)
	if err != nil || dst.Kind != "workspaces" || !x.visible(dst) {
		fail(w, 404, "destination workspace not found")
		return true
	}
	if dst.ID == root.workspace() {
		fail(w, 400, "choose a different workspace")
		return true
	}
	move := p[2] == "move"
	explicit := map[transferRef]bool{root.transferRef: true}
	if in.Include != nil {
		for _, ref := range *in.Include {
			explicit[ref] = true
		}
	}
	selected := []transferNode{}
	for _, n := range graph {
		if in.Include != nil && !explicit[n.transferRef] {
			continue
		}
		delete(explicit, n.transferRef)
		if !n.sourceAllowed(x) {
			fail(w, 403, "source workspace permissions do not allow transferring all selected "+n.Kind)
			return true
		}
		if !n.destinationAllowed(x, dst.ID) {
			fail(w, 403, "destination workspace permissions do not allow creating "+n.Kind)
			return true
		}
		selected = append(selected, n)
	}
	if len(explicit) != 0 {
		fail(w, 409, "resource links changed; review the transfer again")
		return true
	}
	locks := []*sync.Mutex{}
	defer func() {
		for _, lock := range locks {
			lock.Unlock()
		}
	}()
	for _, n := range graph {
		key := n.ID
		switch n.Kind {
		case "apps":
			key = "recipe:" + n.ID
		case "services":
		case "projects":
			for _, p := range append(a.previews(n.ID), n.project) {
				lock := &a.projectSleepState(p.ID).mu
				if !lock.TryLock() {
					fail(w, 409, "a selected project is busy; try again when it is idle")
					return true
				}
				locks = append(locks, lock)
			}
			continue
		default:
			continue
		}
		lock := serviceLock(key)
		if !lock.TryLock() {
			fail(w, 409, "a selected resource is busy; try again when it is idle")
			return true
		}
		locks = append(locks, lock)
	}
	a.certMu.Lock()
	defer a.certMu.Unlock()
	a.store.Lock()
	defer a.store.Unlock()
	for i, n := range graph {
		fresh, err := a.transferNode(n.transferRef)
		if err != nil || fresh.workspace() != root.workspace() {
			fail(w, 409, "resource changed; review the transfer again")
			return true
		}
		graph[i] = fresh
		for j := range selected {
			if selected[j].transferRef == fresh.transferRef {
				if !fresh.sourceAllowed(x) {
					fail(w, 403, "source workspace permissions do not allow transferring "+fresh.Kind)
					return true
				}
				selected[j] = fresh
				break
			}
		}
	}
	result, notes, err := a.transferResources(r.Context(), x, uid, root.workspace(), dst, graph, selected, move)
	if err != nil {
		var conflict transferConflict
		if errors.As(err, &conflict) {
			fail(w, 409, conflict.Error())
		} else {
			fail(w, 500, "transfer failed: "+err.Error())
		}
		return true
	}
	for _, ws := range []string{root.workspace(), dst.ID} {
		a.audit(r.Context(), uid, "workspace."+p[2], root.ID, scopeWorkspace, ws, map[string]string{"source": root.workspace(), "destination": dst.ID, "kind": root.Kind})
	}
	reply(w, 200, map[string]any{"resources": result, "notes": notes})
	return true
}

type transferConflict string

func (e transferConflict) Error() string { return string(e) }

type transferWrite struct {
	table  string
	record Record
	body   any
}

type transferVolume struct {
	from string
	to   string
}

func (a *Server) transferResources(ctx context.Context, x *access, uid, source string, dst Record, graph, selected []transferNode, move bool) ([]transferResource, []string, error) {
	ids := map[transferRef]string{}
	for _, n := range selected {
		id := n.ID
		if !move {
			id = randomID()
		}
		ids[n.transferRef] = id
	}
	now := time.Now().UTC()
	records := []transferWrite{}
	volumes := []transferVolume{}
	result := []transferResource{}
	notes := []string{}
	detached := []Record{}
	if move {
		var err error
		records, detached, err = a.splitTransferLinks(x, graph, ids)
		if err != nil {
			return nil, nil, err
		}
		if len(detached) > 0 {
			notes = append(notes, "Unselected project connections were detached.")
		}
	}
	sourceStatus, err := a.statusConfig(source)
	if err != nil {
		return nil, nil, err
	}
	var removeStatus bool
	for _, n := range selected {
		id := ids[n.transferRef]
		result = append(result, transferResource{transferRef{n.Kind, id}, n.Name})
		record := Record{ID: id, Kind: n.Kind, WorkspaceID: dst.ID}
		switch n.Kind {
		case "projects":
			p := n.project
			p.ID, p.WorkspaceID, p.Parent, p.Owner = id, dst.ID, dst.ID, dst.Owner
			if move {
				child, err := a.transferProjectChildren(x, p, source)
				if err != nil {
					return nil, nil, err
				}
				records = append(records, child...)
				if slices.Contains(sourceStatus.Projects, id) {
					removeStatus = true
					sourceStatus.Projects = slices.DeleteFunc(sourceStatus.Projects, func(v string) bool { return v == id })
				}
			} else {
				p.Active, p.Sleeping, p.ResourcesUpdated, p.Created = "", false, nil, now
				if p.WebhookSecret != "" {
					p.WebhookSecret = randomID()
				}
				notes = append(notes, "Copied projects have no deployments or domains. Deploy them to start their containers.")
			}
			records = append(records, transferWrite{"records", p, p})
		case "services":
			s := n.service
			s.ID, s.WorkspaceID, s.Updated = id, dst.ID, now
			if !move {
				s.Status, s.Container, s.LastError = "stopped", "", ""
				s.Created, s.CreatedBy, s.ExternalPort = now, uid, 0
				if s.Backup != nil && s.Backup.Enabled {
					next := nextBackup(*s.Backup, now)
					s.BackupAt = &next
				}
			} else {
				for _, b := range a.backups(source, s.ID) {
					if b.Status == "running" {
						return nil, nil, transferConflict("wait for service backups to finish")
					}
					notes = append(notes, "Existing service backups stay in the source workspace.")
				}
			}
			s.VolumeID = ids[transferRef{"volumes", s.VolumeID}]
			if !move && s.VolumeID == "" && n.service.VolumeID != "" {
				s.VolumeID = randomID()
				v := Volume{ID: s.VolumeID, WorkspaceID: dst.ID, ServiceID: id, Name: s.Name + "-data", Created: now, CreatedBy: uid}
				volumes = append(volumes, transferVolume{to: volumeName(v.ID)})
				records = append(records, transferWrite{"volumes", Record{ID: v.ID, Kind: "volumes", WorkspaceID: dst.ID}, v})
			}
			if err := a.validateService(s); err != nil {
				return nil, nil, transferConflict("service " + s.Name + ": " + err.Error())
			}
			records = append(records, transferWrite{"services", record, s})
			if !move {
				notes = append(notes, "Copied services are stopped and have no published ports.")
			}
		case "volumes":
			v := n.volume
			if v.Purpose == backupVolumePurpose {
				return nil, nil, transferConflict("workspace backup storage cannot be transferred")
			}
			v.ID, v.WorkspaceID = id, dst.ID
			v.ProjectID, v.ServiceID = ids[transferRef{"projects", v.ProjectID}], ids[transferRef{"services", v.ServiceID}]
			if v.ProjectID == "" {
				v.MountPath = ""
			}
			if !move {
				v.Created, v.CreatedBy = now, uid
				volumes = append(volumes, transferVolume{volumeName(n.ID), volumeName(id)})
			}
			records = append(records, transferWrite{"volumes", record, v})
		case "apps":
			app := n.app
			if app.Action != "" {
				return nil, nil, transferConflict("wait for app " + app.Name + " to finish its current action")
			}
			for _, other := range listResources[recipeApp](a, "recipe_apps", dst.ID) {
				if other.Name == app.Name {
					return nil, nil, transferConflict("an app named " + app.Name + " already exists in the destination")
				}
			}
			app.ID, app.WorkspaceID = id, dst.ID
			if move {
				for _, d := range app.Domains {
					if err := a.transferRoute(x, dst.ID, d.Host, d.CertificateID); err != nil {
						return nil, nil, err
					}
				}
			} else {
				app.Running, app.Status, app.Error, app.Created = false, "stopped", "", now
				app.Domains, app.Containers = []recipeDomain{}, map[string]appStatus{}
				for port := range app.Ports {
					app.Ports[port] = 0
				}
				for _, vol := range app.Recipe.Volumes {
					volumes = append(volumes, transferVolume{recipeVolume(n.ID, vol), recipeVolume(id, vol)})
				}
				notes = append(notes, "Copied apps are stopped and have no domains or published ports.")
			}
			record.Kind = "recipe_apps"
			records = append(records, transferWrite{"recipe_apps", record, app})
		}
	}
	for _, c := range a.connections(source) {
		from, to := ids[transferRef{c.From.Kind + "s", c.From.ID}], ids[transferRef{c.To.Kind + "s", c.To.ID}]
		if from == "" || to == "" {
			continue
		}
		if !move {
			c.ID, c.Created, c.CreatedBy = randomID(), now, uid
		}
		c.WorkspaceID, c.From.ID, c.To.ID = dst.ID, from, to
		records = append(records, transferWrite{"connections", Record{ID: c.ID, Kind: "connections", WorkspaceID: dst.ID}, c})
	}
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, name := range created {
				a.serviceDocker("volume", "rm", name)
			}
		}
	}()
	for _, v := range volumes {
		if v.from != "" {
			for _, n := range graph {
				if n.Kind == "projects" && a.projectTransferBusy(n.ID) {
					return nil, nil, transferConflict("wait for project deployments to finish before copying volumes")
				}
			}
			out, err := a.serviceDocker("ps", "-q", "--filter", "volume="+v.from)
			if err != nil {
				return nil, nil, err
			}
			if strings.TrimSpace(out) != "" {
				return nil, nil, transferConflict("stop workloads using the selected volumes before copying them")
			}
			if _, err = a.serviceDocker("volume", "inspect", v.from); err != nil {
				return nil, nil, err
			}
		}
		if _, err := a.serviceDocker("volume", "create", v.to); err != nil {
			return nil, nil, err
		}
		created = append(created, v.to)
		if v.from != "" {
			err := a.backupStream(ctx, nil, "run", "--rm", "--network", "none", "-v", v.from+":/source:ro", "-v", v.to+":/target", backupImage(), "sh", "-ec", "cp -a /source/. /target/")
			if err != nil {
				return nil, nil, err
			}
		}
	}
	if err := a.writeTransfer(ctx, records, source, sourceStatus.Projects, removeStatus); err != nil {
		return nil, nil, err
	}
	committed = true
	if removeStatus {
		a.live.publish(liveEvent{changeKey: "status_page:" + source})
	}
	for _, row := range records {
		if move && row.record.WorkspaceID == dst.ID {
			old := row.record
			old.WorkspaceID = source
			a.live.publish(liveEvent{changeKey: "transfer:" + source + ":" + old.ID, record: old})
		}
		a.store.Notify(row.record)
	}
	a.store.Notify(detached...)
	if move {
		a.refresh()
	}
	slices.Sort(notes)
	return result, slices.Compact(notes), nil
}

func (a *Server) splitTransferLinks(x *access, graph []transferNode, ids map[transferRef]string) ([]transferWrite, []Record, error) {
	writes := []transferWrite{}
	projects := []Record{}
	projectLink := func(n transferNode, moving bool, project string) (bool, error) {
		if project == "" || moving == (ids[transferRef{"projects", project}] != "") {
			return false, nil
		}
		p := x.project(project)
		if p.ID == "" || !x.can(p, "project.settings") || !x.at(n.workspace(), "")["services.manage"] {
			return false, transferConflict("disconnecting an omitted resource requires managing both ends of the connection")
		}
		if p.Active != "" || len(a.previews(p.ID)) > 0 || n.Kind == "services" && n.service.Container != "" {
			return false, transferConflict("include connected workloads in the move, or disconnect them and remove their existing deployments first")
		}
		for _, kind := range []string{"apps", "releases", "deployments"} {
			for _, r := range a.store.List(kind, p.Owner) {
				if r.ProjectID == p.ID && (r.Active != "" || slices.Contains([]string{"queued", "building", "testing", "starting", "running"}, r.Status)) {
					return false, transferConflict("include connected workloads in the move while they have deployments")
				}
			}
		}
		projects = append(projects, p)
		return true, nil
	}
	if len(graph) > 0 {
		ws := graph[0].workspace()
		for _, c := range a.connections(ws) {
			if (ids[transferRef{c.From.Kind + "s", c.From.ID}] != "") != (ids[transferRef{c.To.Kind + "s", c.To.ID}] != "") {
				return nil, nil, transferConflict("move connected resources together, or remove the connection " + c.Alias + " first")
			}
		}
		for _, n := range a.networks(ws) {
			members := slices.DeleteFunc(slices.Clone(n.Members), func(e Endpoint) bool { return ids[transferRef{e.Kind + "s", e.ID}] != "" })
			if len(members) != len(n.Members) {
				n.Members = members
				writes = append(writes, transferWrite{"networks", Record{ID: n.ID, Kind: "networks", WorkspaceID: ws}, n})
			}
		}
	}
	for _, n := range graph {
		moving := ids[n.transferRef] != ""
		row := Record{ID: n.ID, Kind: n.Kind, WorkspaceID: n.workspace()}
		switch n.Kind {
		case "services":
			s := n.service
			if s.VolumeID != "" && moving != (ids[transferRef{"volumes", s.VolumeID}] != "") {
				return nil, nil, transferConflict("move a service and its data volume together")
			}
		case "volumes":
			v := n.volume
			if v.ServiceID != "" && moving != (ids[transferRef{"services", v.ServiceID}] != "") {
				return nil, nil, transferConflict("move a service and its data volume together")
			}
			detach, err := projectLink(n, moving, v.ProjectID)
			if err != nil {
				return nil, nil, err
			}
			if !moving && detach {
				v.ProjectID, v.MountPath = "", ""
				writes = append(writes, transferWrite{"volumes", row, v})
			}
		}
	}
	return writes, projects, nil
}

func (a *Server) transferRoute(x *access, dst, host, certID string) error {
	if !x.admin && !a.routeAllowed(dst, host) {
		return transferConflict("domain " + host + " is not allowed in the destination; remove its route before moving")
	}
	if certID != "" {
		c, err := a.certificate(certID)
		if err != nil || c.WorkspaceID != dst {
			return transferConflict("remove the certificate binding for " + host + " before moving")
		}
	}
	return nil
}

func (a *Server) transferProjectChildren(x *access, p Record, source string) ([]transferWrite, error) {
	if len(a.assignments("a.scope=? AND a.scope_id=?", scopeProject, p.ID))+len(a.overrides(scopeProject, p.ID)) > 0 &&
		(!x.at(source, "")["members.manage"] || !x.at(p.WorkspaceID, "")["members.manage"]) {
		return nil, transferConflict("moving a project with its own access rules requires permission to manage members in both workspaces")
	}
	out := []transferWrite{}
	for _, cert := range a.certificates(source) {
		if cert.PreviewID == "" {
			continue
		}
		preview, err := a.store.Get(cert.PreviewID)
		if err != nil || preview.ProjectID != p.ID {
			continue
		}
		if !x.at(source, "")["workspace.domains"] || !x.at(p.WorkspaceID, "")["workspace.domains"] || cert.Status == "issuing" {
			return nil, transferConflict("moving preview certificates requires managing workspace domains in both workspaces and waiting for issuance to finish")
		}
		cert.WorkspaceID = p.WorkspaceID
		out = append(out, transferWrite{"certificates", Record{ID: cert.ID, Kind: "certificates", WorkspaceID: p.WorkspaceID}, cert})
	}
	if a.projectTransferBusy(p.ID) {
		return nil, transferConflict("wait for project deployments to finish")
	}
	for _, kind := range []string{"apps", "routes", "releases", "deployments", "previews"} {
		for _, child := range a.store.List(kind, "") {
			if child.ProjectID != p.ID {
				continue
			}
			if kind == "routes" {
				if !x.at(p.WorkspaceID, "")["domains.manage"] {
					return nil, transferConflict("moving project routes requires domain management permission in the destination")
				}
				if err := a.transferRoute(x, p.WorkspaceID, child.Host, child.CertificateID); err != nil {
					return nil, err
				}
			}
			child.Owner = p.Owner
			if child.WorkspaceID != "" {
				child.WorkspaceID = p.WorkspaceID
			}
			out = append(out, transferWrite{"records", child, child})
		}
	}
	return out, nil
}

func (a *Server) projectTransferBusy(id string) bool {
	for _, kind := range []string{"releases", "deployments"} {
		for _, child := range a.store.List(kind, "") {
			if child.ProjectID == id && (slices.Contains([]string{"queued", "building", "testing", "starting"}, child.Status) ||
				kind == "releases" && child.Status == "ready") {
				return true
			}
		}
	}
	return false
}

func (a *Server) writeTransfer(ctx context.Context, records []transferWrite, source string, statusProjects []string, removeStatus bool) error {
	tx, err := a.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, row := range records {
		body, err := json.Marshal(row.body)
		if err != nil {
			return err
		}
		r := row.record
		if row.table == "records" {
			_, err = tx.Exec("INSERT INTO records VALUES(?,?,?,?,?) ON CONFLICT(id) DO UPDATE SET owner=excluded.owner,parent=excluded.parent,body=excluded.body", r.ID, r.Kind, r.Owner, r.Parent, string(body))
		} else if row.table == "certificates" {
			_, err = tx.Exec("UPDATE certificates SET workspace_id=?,body=? WHERE id=?", r.WorkspaceID, string(body), r.ID)
		} else {
			_, err = tx.Exec("INSERT INTO "+row.table+" VALUES(?,?,?) ON CONFLICT(id) DO UPDATE SET workspace_id=excluded.workspace_id,body=excluded.body", r.ID, r.WorkspaceID, string(body))
		}
		if err != nil {
			return fmt.Errorf("save %s: %w", row.table, err)
		}
	}
	if removeStatus {
		body, _ := json.Marshal(statusProjects)
		if _, err := tx.Exec("UPDATE status_pages SET projects=? WHERE workspace_id=?", string(body), source); err != nil {
			return err
		}
	}
	return tx.Commit()
}
