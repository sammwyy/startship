package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
)

func DetectCoolify(ctx context.Context, host Host) DetectedPlatform {
	res := DetectedPlatform{
		Platform: PlatformCoolify,
		Name:     PlatformLabel(PlatformCoolify),
		Status:   "not_found",
		Summary:  "No Coolify installation detected on this host.",
		Projects: []DiscoveredProject{},
		Services: []DiscoveredService{},
	}

	containers, err := listDockerContainers(ctx, host)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		res.Summary = "Unable to inspect Docker containers."
		return res
	}

	var pgContainer, appContainer string
	for _, c := range containers {
		nameLower := strings.ToLower(c.Names)
		if nameLower == "coolify-db" || strings.HasPrefix(nameLower, "coolify-db") {
			if pgContainer == "" || strings.EqualFold(c.State, "running") {
				pgContainer = c.Names
			}
		} else if nameLower == "coolify" {
			appContainer = c.Names
		}
	}

	baseDir := "/data/coolify"
	var envFromFile map[string]string
	if b, err := host.ReadFile(filepath.Join(baseDir, "source", ".env")); err == nil {
		res.DataDir = baseDir
		envFromFile = ParseDotEnv(string(b))
	} else if _, err := host.Stat(baseDir); err == nil {
		res.DataDir = baseDir
	}

	if pgContainer == "" && res.DataDir == "" {
		return res
	}

	res.Detected = true
	res.Container = pgContainer
	res.Status = "detected"

	if pgContainer == "" {
		res.Warnings = append(res.Warnings, "Coolify directory /data/coolify found, but no coolify-db container is running.")
		res.Summary = "Coolify files detected, but its PostgreSQL container was not found."
		return res
	}

	pgEnv := inspectContainerEnv(ctx, host, pgContainer)
	var appEnv map[string]string
	if appContainer != "" {
		appEnv = inspectContainerEnv(ctx, host, appContainer)
	}

	pgUser := firstNonEmpty(envFromFile["DB_USERNAME"], pgEnv["POSTGRES_USER"], "coolify")
	pgDB := firstNonEmpty(envFromFile["DB_DATABASE"], pgEnv["POSTGRES_DB"], "coolify")

	var appKeys []string
	for _, raw := range []string{
		envFromFile["APP_KEY"],
		appEnv["APP_KEY"],
	} {
		if raw != "" {
			appKeys = append(appKeys, raw)
		}
	}
	for _, prev := range []string{
		envFromFile["APP_PREVIOUS_KEYS"],
		appEnv["APP_PREVIOUS_KEYS"],
	} {
		for _, k := range strings.Split(prev, ",") {
			if k = strings.TrimSpace(k); k != "" {
				appKeys = append(appKeys, k)
			}
		}
	}

	query := `SELECT json_build_object(
		'teams', COALESCE((SELECT json_agg(json_build_object('id', id, 'name', name)) FROM teams), '[]'::json),
		'projects', COALESCE((SELECT json_agg(json_build_object('id', id, 'uuid', uuid, 'name', name, 'team_id', team_id)) FROM projects), '[]'::json),
		'environments', COALESCE((SELECT json_agg(json_build_object('id', id, 'name', name, 'project_id', project_id)) FROM environments), '[]'::json),
		'applications', COALESCE((SELECT json_agg(json_build_object(
			'id', id,
			'uuid', uuid,
			'name', name,
			'description', description,
			'fqdn', fqdn,
			'git_repository', git_repository,
			'git_full_url', git_full_url,
			'git_branch', git_branch,
			'build_pack', build_pack,
			'install_command', install_command,
			'build_command', build_command,
			'start_command', start_command,
			'ports_exposes', ports_exposes,
			'base_directory', base_directory,
			'publish_directory', publish_directory,
			'dockerfile_location', dockerfile_location,
			'docker_registry_image_name', docker_registry_image_name,
			'docker_registry_image_tag', docker_registry_image_tag,
			'health_check_path', health_check_path,
			'environment_id', environment_id
		)) FROM applications WHERE deleted_at IS NULL), '[]'::json),
		'env_vars', COALESCE((SELECT json_agg(json_build_object(
			'resourceable_type', resourceable_type,
			'resourceable_id', resourceable_id,
			'key', key,
			'value', value,
			'is_preview', is_preview
		)) FROM environment_variables WHERE is_preview = false), '[]'::json),
		'volumes', COALESCE((SELECT json_agg(json_build_object(
			'name', name,
			'mount_path', mount_path,
			'host_path', host_path,
			'resource_type', resource_type,
			'resource_id', resource_id
		)) FROM local_persistent_volumes), '[]'::json),
		'postgresqls', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'user', postgres_user, 'password', postgres_password, 'database', postgres_db
		)) FROM standalone_postgresqls WHERE deleted_at IS NULL), '[]'::json),
		'mysqls', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'user', mysql_user, 'password', mysql_password, 'database', mysql_database
		)) FROM standalone_mysqls WHERE deleted_at IS NULL), '[]'::json),
		'mariadbs', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'user', mariadb_user, 'password', mariadb_password, 'database', mariadb_database
		)) FROM standalone_mariadbs WHERE deleted_at IS NULL), '[]'::json),
		'mongodbs', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'user', mongo_initdb_root_username, 'password', mongo_initdb_root_password, 'database', mongo_initdb_database
		)) FROM standalone_mongodbs WHERE deleted_at IS NULL), '[]'::json),
		'redis', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'password', redis_password
		)) FROM standalone_redis WHERE deleted_at IS NULL), '[]'::json),
		'clickhouses', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'image', image, 'environment_id', environment_id,
			'public_port', public_port, 'user', clickhouse_admin_user, 'password', clickhouse_admin_password
		)) FROM standalone_clickhouses WHERE deleted_at IS NULL), '[]'::json),
		'services', COALESCE((SELECT json_agg(json_build_object(
			'id', id, 'uuid', uuid, 'name', name, 'description', description, 'service_type', service_type, 'environment_id', environment_id
		)) FROM services WHERE deleted_at IS NULL), '[]'::json),
		'deployments', COALESCE((SELECT json_agg(d) FROM (
			SELECT id, application_id, deployment_uuid, commit, commit_message, status, logs, created_at, updated_at,
			       ROW_NUMBER() OVER (PARTITION BY application_id ORDER BY created_at DESC) AS rn
			FROM application_deployment_queues
		) d WHERE d.rn <= 10), '[]'::json)
	)::text;`

	rawJSON, err := queryPostgresJSON(ctx, host, pgContainer, pgUser, pgDB, query)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		res.Summary = "Coolify detected, but querying its PostgreSQL database failed."
		return res
	}

	var dump struct {
		Teams []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"teams"`
		Projects []struct {
			ID     int64  `json:"id"`
			UUID   string `json:"uuid"`
			Name   string `json:"name"`
			TeamID int64  `json:"team_id"`
		} `json:"projects"`
		Environments []struct {
			ID        int64  `json:"id"`
			Name      string `json:"name"`
			ProjectID int64  `json:"project_id"`
		} `json:"environments"`
		Applications []struct {
			ID                 int64  `json:"id"`
			UUID               string `json:"uuid"`
			Name               string `json:"name"`
			Description        string `json:"description"`
			FQDN               string `json:"fqdn"`
			GitRepository      string `json:"git_repository"`
			GitFullURL         string `json:"git_full_url"`
			GitBranch          string `json:"git_branch"`
			BuildPack          string `json:"build_pack"`
			InstallCommand     string `json:"install_command"`
			BuildCommand       string `json:"build_command"`
			StartCommand       string `json:"start_command"`
			PortsExposes       string `json:"ports_exposes"`
			BaseDirectory      string `json:"base_directory"`
			PublishDirectory   string `json:"publish_directory"`
			DockerfileLocation string `json:"dockerfile_location"`
			DockerImageName    string `json:"docker_registry_image_name"`
			DockerImageTag     string `json:"docker_registry_image_tag"`
			HealthCheckPath    string `json:"health_check_path"`
			EnvironmentID      int64  `json:"environment_id"`
		} `json:"applications"`
		EnvVars []struct {
			ResourceableType string `json:"resourceable_type"`
			ResourceableID   int64  `json:"resourceable_id"`
			Key              string `json:"key"`
			Value            string `json:"value"`
		} `json:"env_vars"`
		Volumes []struct {
			Name         string `json:"name"`
			MountPath    string `json:"mount_path"`
			HostPath     string `json:"host_path"`
			ResourceType string `json:"resource_type"`
			ResourceID   int64  `json:"resource_id"`
		} `json:"volumes"`
		Postgresqls []coolifyDBRow `json:"postgresqls"`
		Mysqls      []coolifyDBRow `json:"mysqls"`
		Mariadbs    []coolifyDBRow `json:"mariadbs"`
		Mongodbs    []coolifyDBRow `json:"mongodbs"`
		Redis       []coolifyDBRow `json:"redis"`
		Clickhouses []coolifyDBRow `json:"clickhouses"`
		Services    []struct {
			ID            int64  `json:"id"`
			UUID          string `json:"uuid"`
			Name          string `json:"name"`
			Description   string `json:"description"`
			ServiceType   string `json:"service_type"`
			EnvironmentID int64  `json:"environment_id"`
		} `json:"services"`
		Deployments []struct {
			ID            int64  `json:"id"`
			ApplicationID string `json:"application_id"`
			UUID          string `json:"deployment_uuid"`
			Commit        string `json:"commit"`
			CommitMessage string `json:"commit_message"`
			Status        string `json:"status"`
			Logs          string `json:"logs"`
			CreatedAt     string `json:"created_at"`
			UpdatedAt     string `json:"updated_at"`
		} `json:"deployments"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &dump); err != nil {
		res.Status = "error"
		res.Error = fmt.Sprintf("decode coolify database snapshot: %v", err)
		res.Summary = "Coolify database returned unexpected data."
		return res
	}

	teamNames := map[int64]string{}
	for _, t := range dump.Teams {
		teamNames[t.ID] = t.Name
	}
	type projInfo struct {
		Name string
		Team string
	}
	projByID := map[int64]projInfo{}
	for _, p := range dump.Projects {
		projByID[p.ID] = projInfo{Name: p.Name, Team: teamNames[p.TeamID]}
	}
	type envInfo struct {
		Name      string
		ProjectID int64
	}
	envByID := map[int64]envInfo{}
	for _, e := range dump.Environments {
		envByID[e.ID] = envInfo{Name: e.Name, ProjectID: e.ProjectID}
	}

	type resKey struct {
		typ string
		id  int64
	}
	envByResource := map[resKey]map[string]string{}
	decryptWarnings := map[resKey]int{}
	for _, ev := range dump.EnvVars {
		rk := resKey{typ: shortLaravelType(ev.ResourceableType), id: ev.ResourceableID}
		if envByResource[rk] == nil {
			envByResource[rk] = map[string]string{}
		}
		val := ev.Value
		if dec, err := DecryptCoolify(ev.Value, appKeys); err == nil {
			val = dec
		} else {
			decryptWarnings[rk]++
		}
		envByResource[rk][ev.Key] = val
	}

	volsByResource := map[resKey][]DiscoveredVolume{}
	for _, v := range dump.Volumes {
		if v.MountPath == "" {
			continue
		}
		rk := resKey{typ: shortLaravelType(v.ResourceType), id: v.ResourceID}
		srcType := "volume"
		srcName := v.Name
		if v.HostPath != "" {
			srcType = "bind"
			srcName = v.HostPath
		}
		if srcName == "" {
			continue
		}
		volsByResource[rk] = append(volsByResource[rk], DiscoveredVolume{
			Name:       firstNonEmpty(v.Name, filepath.Base(v.MountPath)),
			SourceType: srcType,
			SourceName: srcName,
			MountPath:  v.MountPath,
		})
	}

	logsByApp := map[string][]DiscoveredLog{}
	for _, dep := range dump.Deployments {
		content := formatCoolifyLogs(dep.Logs)
		if content == "" {
			content = fmt.Sprintf("Imported Coolify deployment %s (%s)", dep.UUID, dep.Status)
		}
		logsByApp[dep.ApplicationID] = append(logsByApp[dep.ApplicationID], DiscoveredLog{
			Title:      dep.CommitMessage,
			Status:     dep.Status,
			Commit:     dep.Commit,
			CreatedAt:  parseTimeFlexible(dep.CreatedAt),
			FinishedAt: parseTimeFlexible(dep.UpdatedAt),
			Content:    content,
		})
	}

	servicesByEnv := map[int64][]string{}
	addDB := func(engine, laravelType string, rows []coolifyDBRow) {
		for _, row := range rows {
			ei := envByID[row.EnvironmentID]
			pi := projByID[ei.ProjectID]
			cfg := map[string]string{}
			var warnings []string
			if row.User != "" {
				cfg["user"] = row.User
			}
			if row.Password != "" {
				if dec, err := DecryptCoolify(row.Password, appKeys); err == nil {
					cfg["password"] = dec
				} else {
					cfg["password"] = row.Password
					warnings = append(warnings, "Database password could not be decrypted; check APP_KEY.")
				}
			}
			if row.Database != "" {
				cfg["database"] = row.Database
			}
			rk := resKey{typ: laravelType, id: row.ID}
			vols := append([]DiscoveredVolume{}, volsByResource[rk]...)
			container := findMatchingContainer(containers, row.UUID, row.Name)
			if container != "" {
				vols = append(vols, inspectContainerMounts(ctx, host, container)...)
			}
			vols = dedupVolumes(vols)

			srcID := firstNonEmpty(row.UUID, fmt.Sprintf("%s-%d", engine, row.ID))
			res.Services = append(res.Services, DiscoveredService{
				ID:            srcID,
				Platform:      PlatformCoolify,
				SourceOrg:     pi.Team,
				SourceProject: pi.Name,
				Environment:   ei.Name,
				Name:          row.Name,
				Engine:        engine,
				RawEngine:     firstNonEmpty(row.Image, engine),
				Image:         row.Image,
				Version:       ParseImageTag(row.Image),
				ExternalPort:  row.PublicPort,
				Config:        cfg,
				ConfigKeys:    sortedMapKeys(cfg),
				Volumes:       vols,
				ContainerName: container,
				Compatible:    true,
				Warnings:      warnings,
			})
			servicesByEnv[row.EnvironmentID] = append(servicesByEnv[row.EnvironmentID], srcID)
		}
	}
	addDB("postgresql", "StandalonePostgresql", dump.Postgresqls)
	addDB("mysql", "StandaloneMysql", dump.Mysqls)
	addDB("mariadb", "StandaloneMariadb", dump.Mariadbs)
	addDB("mongodb", "StandaloneMongodb", dump.Mongodbs)
	addDB("redis", "StandaloneRedis", dump.Redis)
	addDB("clickhouse", "StandaloneClickhouse", dump.Clickhouses)

	for _, app := range dump.Applications {
		ei := envByID[app.EnvironmentID]
		pi := projByID[ei.ProjectID]
		rk := resKey{typ: "Application", id: app.ID}
		env := envByResource[rk]

		var warnings []string
		if n := decryptWarnings[rk]; n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d environment variable(s) could not be decrypted; check APP_KEY.", n))
		}

		gitURL := firstNonEmpty(app.GitFullURL, app.GitRepository)
		if gitURL != "" && !strings.Contains(gitURL, "://") && !strings.HasPrefix(gitURL, "git@") {
			gitURL = "https://github.com/" + strings.TrimPrefix(gitURL, "/")
		}
		dockerImg := app.DockerImageName
		if dockerImg != "" && app.DockerImageTag != "" {
			dockerImg += ":" + app.DockerImageTag
		}

		sourceKind := "git"
		if app.BuildPack == "dockercompose" {
			sourceKind = "compose"
		} else if app.BuildPack == "static" {
			sourceKind = "static"
		} else if gitURL == "" && dockerImg != "" {
			sourceKind = "docker"
			warnings = append(warnings, "Docker-image-only application: configure a Git source in Startship to redeploy.")
		}

		port := parseFirstPort(app.PortsExposes)
		doms := parseCoolifyFQDNs(app.FQDN, port)

		vols := append([]DiscoveredVolume{}, volsByResource[rk]...)
		container := findMatchingContainer(containers, app.UUID, app.Name)
		if container != "" {
			vols = append(vols, inspectContainerMounts(ctx, host, container)...)
		}
		vols = dedupVolumes(vols)

		appIDStr := strconv.FormatInt(app.ID, 10)
		logs := append([]DiscoveredLog{}, logsByApp[appIDStr]...)
		if liveLogs := captureContainerLogs(ctx, host, container, 200); liveLogs != "" {
			logs = append(logs, DiscoveredLog{
				Title:   "Container runtime logs snapshot",
				Status:  "running",
				Content: liveLogs,
			})
		}

		res.Projects = append(res.Projects, DiscoveredProject{
			ID:             firstNonEmpty(app.UUID, appIDStr),
			Platform:       PlatformCoolify,
			SourceOrg:      pi.Team,
			SourceProject:  pi.Name,
			Environment:    ei.Name,
			Name:           app.Name,
			Description:    app.Description,
			SourceKind:     sourceKind,
			GitURL:         gitURL,
			Branch:         firstNonEmpty(app.GitBranch, "main"),
			RootDir:        cleanRootDir(app.BaseDirectory),
			BuildType:      app.BuildPack,
			Dockerfile:     strings.TrimPrefix(app.DockerfileLocation, "/"),
			DockerImage:    dockerImg,
			InstallCommand: app.InstallCommand,
			BuildCommand:   app.BuildCommand,
			StartCommand:   app.StartCommand,
			OutputDir:      app.PublishDirectory,
			Port:           port,
			HealthPath:     app.HealthCheckPath,
			Env:            env,
			EnvKeys:        sortedMapKeys(env),
			EnvCount:       len(env),
			Domains:        doms,
			Volumes:        vols,
			Logs:           logs,
			LogCount:       len(logs),
			ContainerName:  container,
			LinkedServices: servicesByEnv[app.EnvironmentID],
			Compatible:     true,
			Warnings:       warnings,
		})
	}

	for _, svc := range dump.Services {
		ei := envByID[svc.EnvironmentID]
		pi := projByID[ei.ProjectID]
		engine := NormalizeEngine(svc.ServiceType, svc.Name)
		rk := resKey{typ: "Service", id: svc.ID}
		if engine != "" {
			env := envByResource[rk]
			cfg := extractServiceConfig(engine, env)
			vols := append([]DiscoveredVolume{}, volsByResource[rk]...)
			container := findMatchingContainer(containers, svc.UUID, svc.Name)
			if container != "" {
				vols = append(vols, inspectContainerMounts(ctx, host, container)...)
			}
			vols = dedupVolumes(vols)
			srcID := firstNonEmpty(svc.UUID, fmt.Sprintf("service-%d", svc.ID))
			res.Services = append(res.Services, DiscoveredService{
				ID:            srcID,
				Platform:      PlatformCoolify,
				SourceOrg:     pi.Team,
				SourceProject: pi.Name,
				Environment:   ei.Name,
				Name:          svc.Name,
				Engine:        engine,
				RawEngine:     firstNonEmpty(svc.ServiceType, svc.Name),
				Config:        cfg,
				ConfigKeys:    sortedMapKeys(cfg),
				Volumes:       vols,
				ContainerName: container,
				Compatible:    true,
			})
		}
	}

	res.Summary = fmt.Sprintf("Found %d project(s) and %d service(s).", len(res.Projects), len(res.Services))
	return res
}

type coolifyDBRow struct {
	ID            int64  `json:"id"`
	UUID          string `json:"uuid"`
	Name          string `json:"name"`
	Image         string `json:"image"`
	EnvironmentID int64  `json:"environment_id"`
	PublicPort    int    `json:"public_port"`
	User          string `json:"user"`
	Password      string `json:"password"`
	Database      string `json:"database"`
}

func shortLaravelType(full string) string {
	if idx := strings.LastIndex(full, `\`); idx >= 0 {
		return full[idx+1:]
	}
	return full
}

func parseFirstPort(raw string) int {
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if idx := strings.Index(part, ":"); idx >= 0 {
			part = part[:idx]
		}
		if n, err := strconv.Atoi(part); err == nil && n > 0 && n <= 65535 {
			return n
		}
	}
	return 0
}

func parseCoolifyFQDNs(raw string, defaultPort int) []DiscoveredDomain {
	var out []DiscoveredDomain
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		if !strings.Contains(entry, "://") {
			entry = "https://" + entry
		}
		u, err := url.Parse(entry)
		if err != nil || u.Hostname() == "" {
			continue
		}
		port := defaultPort
		if p := u.Port(); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				port = n
			}
		}
		prefix := u.Path
		if prefix == "" {
			prefix = "/"
		}
		out = append(out, DiscoveredDomain{
			Host:       strings.ToLower(u.Hostname()),
			PathPrefix: prefix,
			HTTPS:      strings.EqualFold(u.Scheme, "https"),
			Port:       port,
		})
	}
	return out
}

func formatCoolifyLogs(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var entries []struct {
		Output    string `json:"output"`
		Timestamp string `json:"timestamp"`
		Hidden    bool   `json:"hidden"`
	}
	if json.Unmarshal([]byte(raw), &entries) == nil && len(entries) > 0 {
		var b strings.Builder
		for _, e := range entries {
			if e.Hidden || e.Output == "" {
				continue
			}
			if e.Timestamp != "" {
				b.WriteString("[")
				b.WriteString(e.Timestamp)
				b.WriteString("] ")
			}
			b.WriteString(e.Output)
			if !strings.HasSuffix(e.Output, "\n") {
				b.WriteByte('\n')
			}
		}
		out := strings.TrimSpace(b.String())
		const maxLogBytes = 64 * 1024
		if len(out) > maxLogBytes {
			out = out[len(out)-maxLogBytes:]
		}
		return out
	}
	const maxLogBytes = 64 * 1024
	if len(raw) > maxLogBytes {
		return raw[len(raw)-maxLogBytes:]
	}
	return raw
}
