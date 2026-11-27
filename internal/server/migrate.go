package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/catalog"
	"github.com/sammwyy/startship/internal/migrate"
	"github.com/sammwyy/startship/internal/routing"
)

const (
	migrationJobsSettingKey = "migration_jobs"
	maxMigrationJobsHistory = 50
)

func (a *Server) migrationHost() migrate.Host {
	if a.migrateHost != nil {
		return a.migrateHost
	}
	return migrate.OSHost{}
}

func (a *Server) migrateAPI(w http.ResponseWriter, r *http.Request, p []string, uid string, x *access) bool {
	if len(p) < 4 || p[1] != "system" || p[2] != "migrations" {
		return false
	}
	if !x.admin {
		fail(w, 403, "only Platform Admins can manage migrations")
		return true
	}

	switch p[3] {
	case "detect":
		if len(p) != 4 {
			fail(w, 404, "not found")
			return true
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			fail(w, 405, "method not allowed")
			return true
		}
		a.handleMigrateDetect(w, r)
	case "import":
		if len(p) != 4 {
			fail(w, 404, "not found")
			return true
		}
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			fail(w, 405, "method not allowed")
			return true
		}
		a.handleMigrateImport(w, r, uid)
	case "jobs":
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			fail(w, 405, "method not allowed")
			return true
		}
		if len(p) == 4 {
			reply(w, 200, map[string]any{"jobs": a.listMigrationJobs()})
			return true
		}
		if len(p) == 5 {
			job, ok := a.getMigrationJob(p[4])
			if !ok {
				fail(w, 404, "migration job not found")
				return true
			}
			reply(w, 200, job)
			return true
		}
		fail(w, 404, "not found")
	default:
		fail(w, 404, "not found")
	}
	return true
}

func (a *Server) handleMigrateDetect(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	defer cancel()

	host := a.migrationHost()
	platformParam := strings.TrimSpace(r.URL.Query().Get("platform"))
	var platforms []migrate.DetectedPlatform
	if platformParam != "" {
		plat, ok := migrate.ParsePlatform(platformParam)
		if !ok {
			fail(w, 400, "unsupported platform (expected openship, dokploy, or coolify)")
			return
		}
		platforms = []migrate.DetectedPlatform{migrate.RedactPlatform(migrate.DetectPlatform(ctx, host, plat))}
	} else {
		raw := migrate.DetectAll(ctx, host)
		platforms = make([]migrate.DetectedPlatform, len(raw))
		for i, p := range raw {
			platforms[i] = migrate.RedactPlatform(p)
		}
	}
	reply(w, 200, map[string]any{"platforms": platforms})
}

func (a *Server) handleMigrateImport(w http.ResponseWriter, r *http.Request, uid string) {
	var req migrate.ImportRequest
	if err := decode(w, r, &req); err != nil {
		fail(w, 400, "invalid JSON")
		return
	}
	plat, ok := migrate.ParsePlatform(string(req.Platform))
	if !ok {
		fail(w, 400, "unsupported platform (expected openship, dokploy, or coolify)")
		return
	}
	req.Platform = plat

	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		if len(req.Projects) > 0 || len(req.Services) > 0 {
			mode = "cherry_pick"
		} else {
			mode = "all"
		}
	}
	if mode != "all" && mode != "cherry_pick" {
		fail(w, 400, "mode must be 'all' or 'cherry_pick'")
		return
	}
	req.Mode = mode

	if req.TargetWorkspaceID != "" {
		ws, err := a.store.Get(req.TargetWorkspaceID)
		if err != nil || ws.Kind != "workspaces" {
			fail(w, 404, "target workspace not found")
			return
		}
	}
	for _, ps := range req.Projects {
		if ps.TargetWorkspaceID != "" {
			ws, err := a.store.Get(ps.TargetWorkspaceID)
			if err != nil || ws.Kind != "workspaces" {
				fail(w, 404, "target workspace not found: "+ps.TargetWorkspaceID)
				return
			}
		}
	}
	for _, ss := range req.Services {
		if ss.TargetWorkspaceID != "" {
			ws, err := a.store.Get(ss.TargetWorkspaceID)
			if err != nil || ws.Kind != "workspaces" {
				fail(w, 404, "target workspace not found: "+ss.TargetWorkspaceID)
				return
			}
		}
	}

	if req.TargetWorkspaceID == "" && strings.TrimSpace(req.CreateWorkspaceName) == "" {
		allOverrides := mode == "cherry_pick" && (len(req.Projects) > 0 || len(req.Services) > 0)
		for _, ps := range req.Projects {
			if ps.TargetWorkspaceID == "" {
				allOverrides = false
				break
			}
		}
		if allOverrides {
			for _, ss := range req.Services {
				if ss.TargetWorkspaceID == "" {
					allOverrides = false
					break
				}
			}
		}
		if !allOverrides {
			fail(w, 400, "target_workspace_id or create_workspace_name is required")
			return
		}
	}

	detectCtx, cancel := context.WithTimeout(r.Context(), 45*time.Second)
	detected := migrate.DetectPlatform(detectCtx, a.migrationHost(), plat)
	cancel()
	if !detected.Detected {
		msg := "platform not detected on this host"
		if detected.Error != "" {
			msg = detected.Error
		}
		fail(w, 404, msg)
		return
	}

	selectedProjects, selectedServices, err := resolveMigrationSelections(req, detected)
	if err != nil {
		fail(w, 400, err.Error())
		return
	}
	if len(selectedProjects) == 0 && len(selectedServices) == 0 {
		fail(w, 400, "no projects or services selected for migration")
		return
	}

	now := time.Now().UTC()
	job := &migrate.MigrationJob{
		ID:                randomID(),
		Platform:          plat,
		Mode:              mode,
		TargetWorkspaceID: req.TargetWorkspaceID,
		Status:            "running",
		CreatedBy:         uid,
		StartedAt:         now,
		Projects:          []migrate.MigratedProjectResult{},
		Services:          []migrate.MigratedServiceResult{},
		Logs:              []string{fmt.Sprintf("Started %s migration (%s): %d project(s), %d service(s)", migrate.PlatformLabel(plat), mode, len(selectedProjects), len(selectedServices))},
		Warnings:          []string{},
	}
	a.upsertMigrationJob(job)

	syncMode := r.URL.Query().Get("sync") == "true" || r.URL.Query().Get("sync") == "1"
	if syncMode {
		a.executeMigration(r.Context(), uid, job.ID, req, selectedProjects, selectedServices)
		finalJob, _ := a.getMigrationJob(job.ID)
		reply(w, 200, finalJob)
		return
	}

	go a.executeMigration(context.Background(), uid, job.ID, req, selectedProjects, selectedServices)
	reply(w, 202, cloneMigrationJob(job))
}

type resolvedProjectPlan struct {
	source            migrate.DiscoveredProject
	targetName        string
	targetWorkspaceID string
	sel               migrate.ProjectSelection
}

type resolvedServicePlan struct {
	source            migrate.DiscoveredService
	targetName        string
	targetWorkspaceID string
	sel               migrate.ServiceSelection
}

func resolveMigrationSelections(req migrate.ImportRequest, detected migrate.DetectedPlatform) ([]resolvedProjectPlan, []resolvedServicePlan, error) {
	projByID := map[string]migrate.DiscoveredProject{}
	for _, p := range detected.Projects {
		projByID[p.ID] = p
	}
	svcByID := map[string]migrate.DiscoveredService{}
	for _, s := range detected.Services {
		svcByID[s.ID] = s
	}

	var projects []resolvedProjectPlan
	var services []resolvedServicePlan

	if req.Mode == "all" {
		projOverrides := map[string]migrate.ProjectSelection{}
		for _, sel := range req.Projects {
			projOverrides[sel.SourceID] = sel
		}
		svcOverrides := map[string]migrate.ServiceSelection{}
		for _, sel := range req.Services {
			svcOverrides[sel.SourceID] = sel
		}
		for _, p := range detected.Projects {
			sel := projOverrides[p.ID]
			name := strings.TrimSpace(sel.TargetName)
			if name == "" {
				name = p.Name
			}
			projects = append(projects, resolvedProjectPlan{
				source:            p,
				targetName:        name,
				targetWorkspaceID: sel.TargetWorkspaceID,
				sel:               sel,
			})
		}
		for _, s := range detected.Services {
			sel := svcOverrides[s.ID]
			name := strings.TrimSpace(sel.TargetName)
			if name == "" {
				name = s.Name
			}
			services = append(services, resolvedServicePlan{
				source:            s,
				targetName:        name,
				targetWorkspaceID: sel.TargetWorkspaceID,
				sel:               sel,
			})
		}
		return projects, services, nil
	}

	seenProj := map[string]bool{}
	for _, sel := range req.Projects {
		src, ok := projByID[sel.SourceID]
		if !ok {
			return nil, nil, fmt.Errorf("project not found in %s discovery: %s", req.Platform, sel.SourceID)
		}
		if seenProj[sel.SourceID] {
			continue
		}
		seenProj[sel.SourceID] = true
		name := strings.TrimSpace(sel.TargetName)
		if name == "" {
			name = src.Name
		}
		projects = append(projects, resolvedProjectPlan{
			source:            src,
			targetName:        name,
			targetWorkspaceID: sel.TargetWorkspaceID,
			sel:               sel,
		})
	}

	seenSvc := map[string]bool{}
	for _, sel := range req.Services {
		src, ok := svcByID[sel.SourceID]
		if !ok {
			return nil, nil, fmt.Errorf("service not found in %s discovery: %s", req.Platform, sel.SourceID)
		}
		if seenSvc[sel.SourceID] {
			continue
		}
		seenSvc[sel.SourceID] = true
		name := strings.TrimSpace(sel.TargetName)
		if name == "" {
			name = src.Name
		}
		services = append(services, resolvedServicePlan{
			source:            src,
			targetName:        name,
			targetWorkspaceID: sel.TargetWorkspaceID,
			sel:               sel,
		})
	}
	return projects, services, nil
}

func overrideBool(item *bool, global *bool, def bool) bool {
	if item != nil {
		return *item
	}
	return migrate.BoolDefault(global, def)
}

func (a *Server) executeMigration(ctx context.Context, uid, jobID string, req migrate.ImportRequest, projects []resolvedProjectPlan, services []resolvedServicePlan) {
	defaultWSID := req.TargetWorkspaceID
	defaultWSName := ""
	if defaultWSID != "" {
		if ws, err := a.store.Get(defaultWSID); err == nil {
			defaultWSName = ws.Name
		}
	} else if name := strings.TrimSpace(req.CreateWorkspaceName); name != "" {
		ws := Record{
			ID:      randomID(),
			Kind:    "workspaces",
			Owner:   uid,
			Name:    name,
			Created: time.Now().UTC(),
		}
		if err := a.store.Save(ws); err != nil {
			fin := time.Now().UTC()
			a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
				j.Status = "failed"
				j.Error = "failed to create target workspace: " + err.Error()
				j.FinishedAt = &fin
			})
			return
		}
		defaultWSID = ws.ID
		defaultWSName = ws.Name
		a.audit(ctx, uid, "workspace.create", ws.ID, scopeWorkspace, ws.ID, map[string]string{"source": "migration"})
		a.live.publish(liveEvent{id: "workspaces"})
	}

	a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
		j.TargetWorkspaceID = defaultWSID
		j.TargetWorkspaceName = defaultWSName
		if defaultWSName != "" {
			j.Logs = append(j.Logs, fmt.Sprintf("Target workspace: %s (%s)", defaultWSName, defaultWSID))
		}
	})

	host := a.migrationHost()
	index, _ := a.serviceRegistry()
	migratedServices := map[string]Service{} // source service ID -> created Startship Service
	serviceResultIdx := map[string]int{}     // source service ID -> index in job.Services
	explicitAttach := map[string][]string{}  // source project ID -> []source service ID
	hadErrors := false
	hadWarnings := false
	routerDirty := false

	for _, sp := range services {
		wsID := sp.targetWorkspaceID
		if wsID == "" {
			wsID = defaultWSID
		}
		copyVol := overrideBool(sp.sel.CopyVolumes, req.CopyVolumes, true)
		startSvc := overrideBool(sp.sel.Start, req.StartServices, true)

		a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
			j.Logs = append(j.Logs, fmt.Sprintf("Migrating service %s (%s)...", sp.targetName, sp.source.Engine))
		})

		res := a.migrateSingleService(ctx, uid, wsID, sp, index, host, copyVol, startSvc)
		if res.Status == "failed" {
			hadErrors = true
		}
		if len(res.Warnings) > 0 {
			hadWarnings = true
		}
		if res.ServiceID != "" {
			if svc, err := getResource[Service](a, "services", res.ServiceID); err == nil {
				migratedServices[sp.source.ID] = svc
				for _, srcProjID := range sp.sel.AttachProjects {
					explicitAttach[srcProjID] = append(explicitAttach[srcProjID], sp.source.ID)
				}
			}
		}

		a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
			serviceResultIdx[sp.source.ID] = len(j.Services)
			j.Services = append(j.Services, res)
		})
	}

	for _, pp := range projects {
		wsID := pp.targetWorkspaceID
		if wsID == "" {
			wsID = defaultWSID
		}
		copyVol := overrideBool(pp.sel.CopyVolumes, req.CopyVolumes, true)
		impDom := overrideBool(pp.sel.ImportDomains, req.ImportDomains, true)
		impEnv := overrideBool(pp.sel.ImportEnv, req.ImportEnv, true)
		impLogs := overrideBool(pp.sel.ImportLogs, req.ImportLogs, true)
		deploy := overrideBool(pp.sel.Deploy, req.DeployProjects, false)

		a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
			j.Logs = append(j.Logs, fmt.Sprintf("Migrating project %s...", pp.targetName))
		})

		res, domainsAdded, linkedSrcSvcs := a.migrateSingleProject(ctx, uid, wsID, pp, host, copyVol, impDom, impEnv, impLogs, deploy, migratedServices, explicitAttach[pp.source.ID])
		if domainsAdded {
			routerDirty = true
		}
		if res.Status == "failed" {
			hadErrors = true
		}
		if len(res.Warnings) > 0 {
			hadWarnings = true
		}

		a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
			j.Projects = append(j.Projects, res)
			for _, srcSvcID := range linkedSrcSvcs {
				if idx, ok := serviceResultIdx[srcSvcID]; ok && idx < len(j.Services) {
					j.Services[idx].AttachedProjects++
				}
			}
		})
	}

	if routerDirty {
		a.refresh()
	}

	finalStatus := "completed"
	if hadErrors && len(projects)+len(services) > 0 {
		allFailed := true
		if job, ok := a.getMigrationJob(jobID); ok {
			for _, p := range job.Projects {
				if p.Status != "failed" {
					allFailed = false
				}
			}
			for _, s := range job.Services {
				if s.Status != "failed" {
					allFailed = false
				}
			}
		}
		if allFailed {
			finalStatus = "failed"
		} else {
			finalStatus = "completed_with_warnings"
		}
	} else if hadWarnings {
		finalStatus = "completed_with_warnings"
	}

	fin := time.Now().UTC()
	a.updateMigrationJob(jobID, func(j *migrate.MigrationJob) {
		j.Status = finalStatus
		j.FinishedAt = &fin
		j.Logs = append(j.Logs, fmt.Sprintf("Migration finished with status: %s", finalStatus))
	})
	a.audit(ctx, uid, "system.migrate", jobID, scopePlatform, "", map[string]string{
		"platform": string(req.Platform),
		"mode":     req.Mode,
		"status":   finalStatus,
	})
}

func (a *Server) migrateSingleService(ctx context.Context, uid, wsID string, sp resolvedServicePlan, index catalog.Registry, host migrate.Host, copyVolumes, startServices bool) migrate.MigratedServiceResult {
	res := migrate.MigratedServiceResult{
		SourceID:    sp.source.ID,
		SourceName:  sp.source.Name,
		WorkspaceID: wsID,
		Engine:      sp.source.Engine,
	}

	engine, ok := index.Lookup(sp.source.Engine)
	if !ok {
		res.Status = "failed"
		res.Error = fmt.Sprintf("engine %q is not in Startship's service catalog", sp.source.Engine)
		return res
	}

	version := pickCatalogVersion(engine.Versions, sp.source.Version)
	name := a.uniqueServiceName(wsID, migrate.SanitizeServiceName(sp.targetName))
	res.ServiceName = name

	rawCfg := map[string]string{}
	for _, field := range engine.Fields {
		if v, exists := sp.source.Config[field.Key]; exists && v != "" {
			rawCfg[field.Key] = v
		}
	}
	cfg, err := catalog.Validate(engine, version, rawCfg)
	if err != nil {
		res.Warnings = append(res.Warnings, "source service config adjusted to Startship defaults: "+err.Error())
		cfg, err = catalog.Validate(engine, version, map[string]string{})
		if err != nil {
			res.Status = "failed"
			res.Error = "invalid service config: " + err.Error()
			return res
		}
	}

	now := time.Now().UTC()
	svc := Service{
		ID:          randomID(),
		WorkspaceID: wsID,
		Name:        name,
		Engine:      engine.ID,
		Definition:  &engine,
		Version:     version,
		Config:      cfg,
		Status:      "pending",
		Attachments: []Attachment{},
		Created:     now,
		Updated:     now,
		CreatedBy:   uid,
	}
	if sp.source.ExternalPort >= 1024 && sp.source.ExternalPort <= 65535 && a.validateService(svc) == nil {
		cand := svc
		cand.ExternalPort = sp.source.ExternalPort
		if a.validateService(cand) == nil {
			svc.ExternalPort = sp.source.ExternalPort
		} else {
			res.Warnings = append(res.Warnings, fmt.Sprintf("skipped external port %d (already in use)", sp.source.ExternalPort))
		}
	}

	if engine.DataPath != "" {
		vol := Volume{
			ID:          randomID(),
			WorkspaceID: wsID,
			Name:        name + "-data",
			ServiceID:   svc.ID,
			Created:     now,
			CreatedBy:   uid,
		}
		var srcVol *migrate.DiscoveredVolume
		for i := range sp.source.Volumes {
			v := sp.source.Volumes[i]
			if v.MountPath == engine.DataPath || strings.HasPrefix(v.MountPath, engine.DataPath) {
				srcVol = &v
				break
			}
		}
		if srcVol == nil && len(sp.source.Volumes) > 0 {
			srcVol = &sp.source.Volumes[0]
		}

		if copyVolumes && srcVol != nil && srcVol.SourceName != "" {
			if err := migrate.CopyVolumeData(ctx, host, srcVol.SourceType, srcVol.SourceName, volumeName(vol.ID)); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("volume copy from %s failed (created empty volume): %v", srcVol.SourceName, err))
				_, _ = a.serviceDocker("volume", "create", volumeName(vol.ID))
			} else {
				res.VolumesCopied++
			}
		} else {
			if _, err := a.serviceDocker("volume", "create", volumeName(vol.ID)); err != nil {
				res.Warnings = append(res.Warnings, "docker volume create skipped: "+err.Error())
			}
		}
		if err := a.saveVolume(vol); err != nil {
			res.Status = "failed"
			res.Error = "failed to save service volume: " + err.Error()
			return res
		}
		svc.VolumeID = vol.ID
	}

	if err := a.saveService(svc); err != nil {
		res.Status = "failed"
		res.Error = "failed to save service: " + err.Error()
		return res
	}
	a.audit(ctx, uid, "service.create", svc.ID, scopeWorkspace, wsID, map[string]string{"source": "migration"})
	if startServices {
		a.launchService(svc.ID, "recreate")
	}
	res.ServiceID = svc.ID
	if len(res.Warnings) > 0 {
		res.Status = "warning"
	} else {
		res.Status = "ok"
	}
	return res
}

func (a *Server) migrateSingleProject(
	ctx context.Context,
	uid, wsID string,
	pp resolvedProjectPlan,
	host migrate.Host,
	copyVolumes, importDomains, importEnv, importLogs, deployProjects bool,
	migratedServices map[string]Service,
	extraServiceIDs []string,
) (migrate.MigratedProjectResult, bool, []string) {
	res := migrate.MigratedProjectResult{
		SourceID:    pp.source.ID,
		SourceName:  pp.source.Name,
		ProjectName: pp.targetName,
		WorkspaceID: wsID,
	}

	now := time.Now().UTC()
	branch := strings.TrimSpace(pp.source.Branch)
	if branch == "" {
		branch = "main"
	}
	buildType := strings.TrimSpace(pp.source.BuildType)
	if buildType == "" {
		buildType = "auto"
	}
	port := pp.source.Port
	if port <= 0 {
		port = 3000
	}
	env := map[string]string{}
	if importEnv {
		for k, v := range pp.source.Env {
			if strings.TrimSpace(k) != "" {
				env[k] = v
			}
		}
	}

	detectedBuildType, pipe := migrate.BuildPipelineFromDiscovered(pp.source)
	if buildType == "auto" && detectedBuildType != "" {
		buildType = detectedBuildType
	}

	proj := Record{
		ID:            randomID(),
		Kind:          "projects",
		Owner:         uid,
		Parent:        wsID,
		WorkspaceID:   wsID,
		Name:          pp.targetName,
		GitURL:        pp.source.GitURL,
		GitToken:      pp.source.GitToken,
		Branch:        branch,
		RootDir:       pp.source.RootDir,
		BuildType:     buildType,
		Pipeline:      pipe,
		Port:          port,
		Env:           env,
		AutoDeploy:    pp.source.AutoDeploy,
		WebhookSecret: randomID(),
		Created:       now,
	}
	if err := a.store.Save(proj); err != nil {
		res.Status = "failed"
		res.Error = "failed to save project: " + err.Error()
		return res, false, nil
	}
	res.ProjectID = proj.ID
	res.EnvCount = len(env)
	res.Warnings = append(res.Warnings, pp.source.Warnings...)

	if copyVolumes {
		for i, srcVol := range pp.source.Volumes {
			if !validMount(srcVol.MountPath) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped volume with invalid mount path %q", srcVol.MountPath))
				continue
			}
			vName := migrate.SanitizeServiceName(fmt.Sprintf("%s-vol-%d", pp.targetName, i+1))
			vol := Volume{
				ID:          randomID(),
				WorkspaceID: wsID,
				Name:        vName,
				ProjectID:   proj.ID,
				MountPath:   srcVol.MountPath,
				Created:     now,
				CreatedBy:   uid,
			}
			if a.mountConflict(vol) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped duplicate mount path %s", srcVol.MountPath))
				continue
			}
			if srcVol.SourceName != "" {
				if err := migrate.CopyVolumeData(ctx, host, srcVol.SourceType, srcVol.SourceName, volumeName(vol.ID)); err != nil {
					res.Warnings = append(res.Warnings, fmt.Sprintf("volume copy %s -> %s failed: %v", srcVol.SourceName, srcVol.MountPath, err))
					_, _ = a.serviceDocker("volume", "create", volumeName(vol.ID))
				}
			} else {
				_, _ = a.serviceDocker("volume", "create", volumeName(vol.ID))
			}
			if err := a.saveVolume(vol); err != nil {
				res.Warnings = append(res.Warnings, "failed to save volume "+srcVol.MountPath+": "+err.Error())
				continue
			}
			res.VolumesCopied++
		}
	}

	domainsAdded := false
	if importDomains {
		for _, dom := range pp.source.Domains {
			hostNorm := routing.NormalizeHost(dom.Host)
			if hostNorm == "" || !domainPattern.MatchString(hostNorm) {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped invalid domain %q", dom.Host))
				continue
			}
			if err := a.ensureWorkspaceDomain(wsID, hostNorm, uid); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("skipped domain %s: %v", hostNorm, err))
				continue
			}
			pathPrefix := dom.PathPrefix
			if pathPrefix == "" {
				pathPrefix = "/"
			}
			route := Record{
				ID:          randomID(),
				Kind:        "routes",
				Owner:       uid,
				Parent:      proj.ID,
				WorkspaceID: wsID,
				ProjectID:   proj.ID,
				Host:        hostNorm,
				PathPrefix:  pathPrefix,
				StripPrefix: dom.StripPrefix,
				ForceHTTPS:  dom.HTTPS,
				Created:     now,
			}
			if err := a.store.Save(route); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("failed to save route %s: %v", hostNorm, err))
				continue
			}
			res.DomainsImported++
			domainsAdded = true
		}
	}

	if importLogs {
		for _, entry := range pp.source.Logs {
			created := entry.CreatedAt
			if created.IsZero() {
				created = now
			}
			fin := entry.FinishedAt
			if fin.IsZero() {
				fin = created
			}
			rel := Record{
				ID:          randomID(),
				Kind:        "releases",
				Owner:       uid,
				Parent:      proj.ID,
				WorkspaceID: wsID,
				ProjectID:   proj.ID,
				Name:        proj.Name,
				Status:      mapMigratedReleaseStatus(entry.Status),
				Commit:      entry.Commit,
				Log:         entry.Content,
				Created:     created,
				Finished:    &fin,
			}
			if err := a.store.Save(rel); err == nil {
				res.LogsImported++
			}
		}
	}

	linkIDs := append([]string(nil), pp.source.LinkedServices...)
	linkIDs = append(linkIDs, extraServiceIDs...)
	seenAttach := map[string]bool{}
	var linkedSrcSvcs []string
	for _, srcSvcID := range linkIDs {
		if seenAttach[srcSvcID] {
			continue
		}
		seenAttach[srcSvcID] = true
		svc, ok := migratedServices[srcSvcID]
		if !ok || svc.WorkspaceID != wsID {
			continue
		}
		latest, err := getResource[Service](a, "services", svc.ID)
		if err != nil {
			continue
		}
		already := false
		for _, att := range latest.Attachments {
			if att.ProjectID == proj.ID {
				already = true
				break
			}
		}
		if !already {
			latest.Attachments = append(latest.Attachments, Attachment{
				ProjectID: proj.ID,
				Alias:     latest.Name,
			})
			if err := a.saveService(latest); err == nil {
				linkedSrcSvcs = append(linkedSrcSvcs, srcSvcID)
			}
		}
	}

	if deployProjects && proj.GitURL != "" {
		if dep, err := a.enqueueProject(proj, Record{}); err != nil {
			res.Warnings = append(res.Warnings, "auto-deploy skipped: "+err.Error())
		} else {
			res.DeploymentID = dep.ID
		}
	}

	a.audit(ctx, uid, "project.create", proj.ID, scopeProject, wsID, map[string]string{"source": "migration"})
	a.notifyProject(proj.ID)
	if len(res.Warnings) > 0 {
		res.Status = "warning"
	} else {
		res.Status = "ok"
	}
	return res, domainsAdded, linkedSrcSvcs
}

func (a *Server) ensureWorkspaceDomain(wsID, host, uid string) error {
	if a.routeAllowed(wsID, host) {
		return nil
	}
	if a.dashboardDomain != "" && strings.EqualFold(routing.NormalizeHost(a.dashboardDomain), host) {
		return fmt.Errorf("conflicts with dashboard domain")
	}
	cand := WorkspaceDomain{
		ID:          randomID(),
		WorkspaceID: wsID,
		Name:        host,
		Apex:        true,
		Subdomains:  false,
		Created:     time.Now().UTC().Format(time.RFC3339),
		CreatedBy:   uid,
	}
	for _, existing := range a.domains("") {
		if existing.WorkspaceID != wsID && existing.overlaps(cand) {
			return fmt.Errorf("domain already claimed by another workspace")
		}
	}
	_, err := a.store.DB.Exec(
		`INSERT INTO workspace_domains(id,workspace_id,name,apex,subdomains,created,created_by) VALUES(?,?,?,?,?,?,?)`,
		cand.ID, cand.WorkspaceID, cand.Name, cand.Apex, cand.Subdomains, cand.Created, cand.CreatedBy,
	)
	return err
}

func (a *Server) uniqueServiceName(wsID, base string) string {
	existing := map[string]bool{}
	for _, s := range listResources[Service](a, "services", wsID) {
		existing[s.Name] = true
	}
	if !existing[base] {
		return base
	}
	for i := 2; i < 100; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if len(cand) > 32 {
			cand = fmt.Sprintf("%s-%d", base[:32-len(fmt.Sprintf("-%d", i))], i)
		}
		if !existing[cand] {
			return cand
		}
	}
	return fmt.Sprintf("svc-%s", randomID()[:6])
}

func pickCatalogVersion(versions []string, desired string) string {
	if len(versions) == 0 {
		return desired
	}
	desired = strings.TrimSpace(desired)
	if desired == "" {
		return versions[0]
	}
	if slices.Contains(versions, desired) {
		return desired
	}
	major := strings.SplitN(desired, ".", 2)[0]
	for _, v := range versions {
		if v == major || strings.HasPrefix(v, major+".") || strings.HasPrefix(v, major+"-") {
			return v
		}
	}
	return versions[0]
}

func mapMigratedReleaseStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "ready", "done", "finished", "success", "succeeded", "live":
		return "live"
	case "failed", "error", "errored", "cancelled", "canceled":
		return "failed"
	default:
		return "migrated"
	}
}

func (a *Server) ensureMigrationJobsLoadedLocked() {
	if a.migrationJobsLoaded {
		return
	}
	a.migrationJobsLoaded = true
	if a.store == nil || a.store.DB == nil {
		return
	}
	var raw string
	if err := a.store.DB.QueryRow(`SELECT value FROM settings WHERE key=?`, migrationJobsSettingKey).Scan(&raw); err != nil || raw == "" {
		return
	}
	var jobs []*migrate.MigrationJob
	if json.Unmarshal([]byte(raw), &jobs) == nil {
		a.migrationJobs = jobs
	}
}

func (a *Server) persistMigrationJobsLocked() {
	if a.store == nil || a.store.DB == nil {
		return
	}
	if len(a.migrationJobs) > maxMigrationJobsHistory {
		a.migrationJobs = a.migrationJobs[:maxMigrationJobsHistory]
	}
	b, err := json.Marshal(a.migrationJobs)
	if err != nil {
		return
	}
	_, _ = a.store.DB.Exec(`INSERT OR REPLACE INTO settings(key,value) VALUES(?,?)`, migrationJobsSettingKey, string(b))
}

func (a *Server) upsertMigrationJob(job *migrate.MigrationJob) {
	a.migrationsMu.Lock()
	a.ensureMigrationJobsLoadedLocked()
	cp := cloneMigrationJob(job)
	replaced := false
	for i, existing := range a.migrationJobs {
		if existing.ID == cp.ID {
			a.migrationJobs[i] = cp
			replaced = true
			break
		}
	}
	if !replaced {
		a.migrationJobs = append([]*migrate.MigrationJob{cp}, a.migrationJobs...)
	}
	a.persistMigrationJobsLocked()
	a.migrationsMu.Unlock()
	if a.live != nil {
		a.live.publish(liveEvent{id: "system_migrations", changeKey: "system_migrations"})
	}
}

func (a *Server) updateMigrationJob(id string, fn func(*migrate.MigrationJob)) {
	a.migrationsMu.Lock()
	a.ensureMigrationJobsLoadedLocked()
	for _, j := range a.migrationJobs {
		if j.ID == id {
			fn(j)
			break
		}
	}
	a.persistMigrationJobsLocked()
	a.migrationsMu.Unlock()
	if a.live != nil {
		a.live.publish(liveEvent{id: "system_migrations", changeKey: "system_migrations"})
	}
}

func (a *Server) listMigrationJobs() []migrate.MigrationJob {
	a.migrationsMu.Lock()
	defer a.migrationsMu.Unlock()
	a.ensureMigrationJobsLoadedLocked()
	out := make([]migrate.MigrationJob, 0, len(a.migrationJobs))
	for _, j := range a.migrationJobs {
		out = append(out, *cloneMigrationJob(j))
	}
	return out
}

func (a *Server) getMigrationJob(id string) (migrate.MigrationJob, bool) {
	a.migrationsMu.Lock()
	defer a.migrationsMu.Unlock()
	a.ensureMigrationJobsLoadedLocked()
	for _, j := range a.migrationJobs {
		if j.ID == id {
			return *cloneMigrationJob(j), true
		}
	}
	return migrate.MigrationJob{}, false
}

func cloneMigrationJob(j *migrate.MigrationJob) *migrate.MigrationJob {
	if j == nil {
		return nil
	}
	b, _ := json.Marshal(j)
	var cp migrate.MigrationJob
	_ = json.Unmarshal(b, &cp)
	return &cp
}
