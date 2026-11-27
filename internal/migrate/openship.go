package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func DetectOpenship(ctx context.Context, host Host) DetectedPlatform {
	res := DetectedPlatform{
		Platform: PlatformOpenship,
		Name:     PlatformLabel(PlatformOpenship),
		Status:   "not_found",
		Summary:  "No Openship installation detected on this host.",
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

	var pgContainer, apiContainer string
	var allContainers = map[string]dockerContainerInfo{}
	for _, c := range containers {
		allContainers[c.Names] = c
		labels := parseLabels(c.Labels)
		nameLower := strings.ToLower(c.Names)
		isOpenship := strings.EqualFold(labels["com.docker.compose.project"], "openship") ||
			strings.HasPrefix(nameLower, "openship-") ||
			strings.Contains(strings.ToLower(c.Image), "openship")
		if !isOpenship {
			continue
		}
		svc := strings.ToLower(labels["com.docker.compose.service"])
		if svc == "postgres" || strings.Contains(nameLower, "postgres") {
			if pgContainer == "" || strings.EqualFold(c.State, "running") {
				pgContainer = c.Names
			}
		}
		if svc == "api" || strings.Contains(nameLower, "api") {
			if apiContainer == "" || strings.EqualFold(c.State, "running") {
				apiContainer = c.Names
			}
		}
	}

	envPaths := []string{
		filepath.Join(host.HomeDir(), ".openship", "compose", ".env"),
		"/etc/openship/.env",
		"/opt/openship/.env",
	}
	var envFromFile map[string]string
	for _, p := range envPaths {
		if b, err := host.ReadFile(p); err == nil {
			res.DataDir = filepath.Dir(p)
			envFromFile = ParseDotEnv(string(b))
			break
		}
	}

	if pgContainer == "" && res.DataDir == "" {
		return res
	}

	res.Detected = true
	res.Container = pgContainer
	res.Status = "detected"

	if pgContainer == "" {
		res.Warnings = append(res.Warnings, "Openship config directory found, but no Openship PostgreSQL container is running.")
		res.Summary = "Openship files detected, but its PostgreSQL container was not found."
		return res
	}

	pgEnv := inspectContainerEnv(ctx, host, pgContainer)
	var apiEnv map[string]string
	if apiContainer != "" {
		apiEnv = inspectContainerEnv(ctx, host, apiContainer)
	}

	pgUser := firstNonEmpty(pgEnv["POSTGRES_USER"], envFromFile["POSTGRES_USER"], "openship")
	pgDB := firstNonEmpty(pgEnv["POSTGRES_DB"], envFromFile["POSTGRES_DB"], "openship")

	var secrets []string
	for _, s := range []string{
		apiEnv["BETTER_AUTH_SECRET"],
		envFromFile["BETTER_AUTH_SECRET"],
		"change-me-in-production",
	} {
		if s != "" {
			secrets = append(secrets, s)
		}
	}

	query := `SELECT json_build_object(
		'orgs', COALESCE((SELECT json_agg(json_build_object('id', id, 'name', name)) FROM organization), '[]'::json),
		'projects', COALESCE((SELECT json_agg(json_build_object(
			'id', id,
			'organization_id', organization_id,
			'name', name,
			'slug', slug,
			'environment_name', environment_name,
			'git_provider', git_provider,
			'git_owner', git_owner,
			'git_repo', git_repo,
			'git_branch', git_branch,
			'git_url', git_url,
			'local_path', local_path,
			'clone_token_encrypted', clone_token_encrypted,
			'framework', framework,
			'install_command', install_command,
			'build_command', build_command,
			'output_directory', output_directory,
			'root_directory', root_directory,
			'start_command', start_command,
			'port', port,
			'has_server', has_server,
			'volumes', volumes
		)) FROM project WHERE deleted_at IS NULL), '[]'::json),
		'env_vars', COALESCE((SELECT json_agg(json_build_object(
			'project_id', project_id,
			'service_id', service_id,
			'key', key,
			'value', value,
			'environment', environment
		)) FROM env_var), '[]'::json),
		'services', COALESCE((SELECT json_agg(json_build_object(
			'id', id,
			'project_id', project_id,
			'kind', kind,
			'name', name,
			'image', image,
			'ports', ports,
			'environment', environment,
			'volumes', volumes
		)) FROM service), '[]'::json),
		'domains', COALESCE((SELECT json_agg(json_build_object(
			'project_id', project_id,
			'hostname', hostname,
			'target_port', target_port,
			'target_path', target_path
		)) FROM domain WHERE owner_type = 'project' AND project_id IS NOT NULL), '[]'::json),
		'deployments', COALESCE((SELECT json_agg(d) FROM (
			SELECT id, project_id, branch, commit_sha, commit_message, status, created_at,
			       ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY created_at DESC) AS rn
			FROM deployment
		) d WHERE d.rn <= 10), '[]'::json)
	)::text;`

	rawJSON, err := queryPostgresJSON(ctx, host, pgContainer, pgUser, pgDB, query)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		res.Summary = "Openship detected, but querying its PostgreSQL database failed."
		return res
	}

	var dump struct {
		Orgs []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"orgs"`
		Projects []struct {
			ID                  string   `json:"id"`
			OrganizationID      string   `json:"organization_id"`
			Name                string   `json:"name"`
			Slug                string   `json:"slug"`
			EnvironmentName     string   `json:"environment_name"`
			GitProvider         string   `json:"git_provider"`
			GitOwner            string   `json:"git_owner"`
			GitRepo             string   `json:"git_repo"`
			GitBranch           string   `json:"git_branch"`
			GitURL              string   `json:"git_url"`
			LocalPath           string   `json:"local_path"`
			CloneTokenEncrypted string   `json:"clone_token_encrypted"`
			Framework           string   `json:"framework"`
			InstallCommand      string   `json:"install_command"`
			BuildCommand        string   `json:"build_command"`
			OutputDirectory     string   `json:"output_directory"`
			RootDirectory       string   `json:"root_directory"`
			StartCommand        string   `json:"start_command"`
			Port                int      `json:"port"`
			HasServer           *bool    `json:"has_server"`
			Volumes             []string `json:"volumes"`
		} `json:"projects"`
		EnvVars []struct {
			ProjectID   string `json:"project_id"`
			ServiceID   string `json:"service_id"`
			Key         string `json:"key"`
			Value       string `json:"value"`
			Environment string `json:"environment"`
		} `json:"env_vars"`
		Services []struct {
			ID          string            `json:"id"`
			ProjectID   string            `json:"project_id"`
			Kind        string            `json:"kind"`
			Name        string            `json:"name"`
			Image       string            `json:"image"`
			Ports       []string          `json:"ports"`
			Environment map[string]string `json:"environment"`
			Volumes     []string          `json:"volumes"`
		} `json:"services"`
		Domains []struct {
			ProjectID  string `json:"project_id"`
			Hostname   string `json:"hostname"`
			TargetPort int    `json:"target_port"`
			TargetPath string `json:"target_path"`
		} `json:"domains"`
		Deployments []struct {
			ID            string `json:"id"`
			ProjectID     string `json:"project_id"`
			Branch        string `json:"branch"`
			CommitSHA     string `json:"commit_sha"`
			CommitMessage string `json:"commit_message"`
			Status        string `json:"status"`
			CreatedAt     string `json:"created_at"`
		} `json:"deployments"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &dump); err != nil {
		res.Status = "error"
		res.Error = fmt.Sprintf("decode openship database snapshot: %v", err)
		res.Summary = "Openship database returned unexpected data."
		return res
	}

	orgNames := map[string]string{}
	for _, o := range dump.Orgs {
		orgNames[o.ID] = o.Name
	}

	projectEnv := map[string]map[string]string{}
	serviceEnv := map[string]map[string]string{}
	envDecryptWarnings := map[string]int{}
	for _, ev := range dump.EnvVars {
		val := ev.Value
		if dec, err := DecryptOpenship(ev.Value, secrets); err == nil {
			val = dec
		} else {
			envDecryptWarnings[ev.ProjectID]++
		}
		if ev.ServiceID != "" {
			if serviceEnv[ev.ServiceID] == nil {
				serviceEnv[ev.ServiceID] = map[string]string{}
			}
			serviceEnv[ev.ServiceID][ev.Key] = val
		} else {
			if projectEnv[ev.ProjectID] == nil {
				projectEnv[ev.ProjectID] = map[string]string{}
			}
			projectEnv[ev.ProjectID][ev.Key] = val
		}
	}

	domainsByProject := map[string][]DiscoveredDomain{}
	for _, d := range dump.Domains {
		if d.Hostname == "" {
			continue
		}
		prefix := d.TargetPath
		if prefix == "" {
			prefix = "/"
		}
		domainsByProject[d.ProjectID] = append(domainsByProject[d.ProjectID], DiscoveredDomain{
			Host:       d.Hostname,
			PathPrefix: prefix,
			HTTPS:      true,
			Port:       d.TargetPort,
		})
	}

	logsByProject := map[string][]DiscoveredLog{}
	for _, dep := range dump.Deployments {
		content := fmt.Sprintf("Imported Openship deployment %s (branch %s, status %s)", dep.ID, dep.Branch, dep.Status)
		if dep.CommitMessage != "" {
			content += "\nCommit: " + dep.CommitMessage
		}
		logsByProject[dep.ProjectID] = append(logsByProject[dep.ProjectID], DiscoveredLog{
			Title:     dep.CommitMessage,
			Status:    dep.Status,
			Commit:    dep.CommitSHA,
			CreatedAt: parseTimeFlexible(dep.CreatedAt),
			Content:   content,
		})
	}

	linkedServicesByProject := map[string][]string{}
	projectNames := map[string]string{}
	projectOrgs := map[string]string{}
	for _, p := range dump.Projects {
		projectNames[p.ID] = p.Name
		projectOrgs[p.ID] = orgNames[p.OrganizationID]
	}

	for _, svc := range dump.Services {
		engine := NormalizeEngine("", svc.Image)
		if engine == "" {
			continue
		}
		envMap := map[string]string{}
		for k, v := range svc.Environment {
			envMap[k] = v
		}
		for k, v := range serviceEnv[svc.ID] {
			envMap[k] = v
		}
		cfg := extractServiceConfig(engine, envMap)
		vols := parseVolumeStrings(svc.Volumes)
		container := findMatchingContainer(containers, svc.ID, svc.Name)
		if container != "" {
			vols = append(vols, inspectContainerMounts(ctx, host, container)...)
		}
		vols = dedupVolumes(vols)

		ds := DiscoveredService{
			ID:            svc.ID,
			Platform:      PlatformOpenship,
			SourceOrg:     projectOrgs[svc.ProjectID],
			SourceProject: projectNames[svc.ProjectID],
			Name:          svc.Name,
			Engine:        engine,
			RawEngine:     svc.Image,
			Image:         svc.Image,
			Version:       ParseImageTag(svc.Image),
			Config:        cfg,
			ConfigKeys:    sortedMapKeys(cfg),
			Volumes:       vols,
			ContainerName: container,
			Compatible:    true,
		}
		res.Services = append(res.Services, ds)
		linkedServicesByProject[svc.ProjectID] = append(linkedServicesByProject[svc.ProjectID], svc.ID)
	}

	for _, p := range dump.Projects {
		gitURL := strings.TrimSpace(p.GitURL)
		if gitURL == "" && p.GitOwner != "" && p.GitRepo != "" {
			switch strings.ToLower(p.GitProvider) {
			case "gitlab":
				gitURL = fmt.Sprintf("https://gitlab.com/%s/%s.git", p.GitOwner, p.GitRepo)
			case "bitbucket":
				gitURL = fmt.Sprintf("https://bitbucket.org/%s/%s.git", p.GitOwner, p.GitRepo)
			default:
				gitURL = fmt.Sprintf("https://github.com/%s/%s.git", p.GitOwner, p.GitRepo)
			}
		}

		sourceKind := "git"
		buildType := "auto"
		if p.HasServer != nil && !*p.HasServer {
			sourceKind = "static"
			buildType = "static"
		} else if gitURL == "" && p.LocalPath != "" {
			sourceKind = "local"
		}

		var warnings []string
		if n := envDecryptWarnings[p.ID]; n > 0 {
			warnings = append(warnings, fmt.Sprintf("%d environment variable(s) could not be decrypted; check BETTER_AUTH_SECRET", n))
		}
		if gitURL == "" {
			warnings = append(warnings, "No Git repository URL found; configure source after import to deploy.")
		}

		var gitToken string
		if p.CloneTokenEncrypted != "" {
			if dec, err := DecryptOpenship(p.CloneTokenEncrypted, secrets); err == nil {
				gitToken = dec
			}
		}

		vols := parseVolumeStrings(p.Volumes)
		container := findMatchingContainer(containers, p.ID, p.Slug)
		if container != "" {
			vols = append(vols, inspectContainerMounts(ctx, host, container)...)
		}
		vols = dedupVolumes(vols)

		logs := logsByProject[p.ID]
		if liveLogs := captureContainerLogs(ctx, host, container, 200); liveLogs != "" {
			logs = append(logs, DiscoveredLog{
				Title:   "Container runtime logs snapshot",
				Status:  "running",
				Content: liveLogs,
			})
		}

		env := projectEnv[p.ID]
		dp := DiscoveredProject{
			ID:             p.ID,
			Platform:       PlatformOpenship,
			SourceOrg:      orgNames[p.OrganizationID],
			SourceProject:  p.Name,
			Environment:    p.EnvironmentName,
			Name:           p.Name,
			SourceKind:     sourceKind,
			GitURL:         gitURL,
			GitToken:       gitToken,
			Branch:         firstNonEmpty(p.GitBranch, "main"),
			RootDir:        cleanRootDir(p.RootDirectory),
			BuildType:      buildType,
			InstallCommand: p.InstallCommand,
			BuildCommand:   p.BuildCommand,
			StartCommand:   p.StartCommand,
			OutputDir:      p.OutputDirectory,
			Port:           p.Port,
			Env:            env,
			EnvKeys:        sortedMapKeys(env),
			EnvCount:       len(env),
			Domains:        domainsByProject[p.ID],
			Volumes:        vols,
			Logs:           logs,
			LogCount:       len(logs),
			ContainerName:  container,
			LinkedServices: linkedServicesByProject[p.ID],
			Compatible:     true,
			Warnings:       warnings,
		}
		res.Projects = append(res.Projects, dp)
	}

	res.Summary = fmt.Sprintf("Found %d project(s) and %d service(s).", len(res.Projects), len(res.Services))
	return res
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

func parseVolumeStrings(raw []string) []DiscoveredVolume {
	var out []DiscoveredVolume
	for _, entry := range raw {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) < 2 {
			continue
		}
		src := strings.TrimSpace(parts[0])
		dst := strings.TrimSpace(parts[1])
		if src == "" || dst == "" || !strings.HasPrefix(dst, "/") {
			continue
		}
		srcType := "volume"
		if strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") {
			srcType = "bind"
		}
		out = append(out, DiscoveredVolume{
			Name:       filepath.Base(dst),
			SourceType: srcType,
			SourceName: src,
			MountPath:  dst,
		})
	}
	return out
}

func findMatchingContainer(containers []dockerContainerInfo, ids ...string) string {
	for _, id := range ids {
		id = strings.ToLower(strings.TrimSpace(id))
		if id == "" || len(id) < 3 {
			continue
		}
		for _, c := range containers {
			if strings.Contains(strings.ToLower(c.Names), id) {
				return c.Names
			}
		}
	}
	return ""
}

func extractServiceConfig(engine string, env map[string]string) map[string]string {
	cfg := map[string]string{}
	switch engine {
	case "postgresql":
		if v := env["POSTGRES_USER"]; v != "" {
			cfg["user"] = v
		}
		if v := env["POSTGRES_PASSWORD"]; v != "" {
			cfg["password"] = v
		}
		if v := env["POSTGRES_DB"]; v != "" {
			cfg["database"] = v
		}
	case "mysql", "mariadb":
		if v := firstNonEmpty(env["MYSQL_USER"], env["MARIADB_USER"]); v != "" {
			cfg["user"] = v
		}
		if v := firstNonEmpty(env["MYSQL_PASSWORD"], env["MARIADB_PASSWORD"], env["MYSQL_ROOT_PASSWORD"], env["MARIADB_ROOT_PASSWORD"]); v != "" {
			cfg["password"] = v
		}
		if v := firstNonEmpty(env["MYSQL_DATABASE"], env["MARIADB_DATABASE"]); v != "" {
			cfg["database"] = v
		}
	case "mongodb":
		if v := env["MONGO_INITDB_ROOT_USERNAME"]; v != "" {
			cfg["user"] = v
		}
		if v := env["MONGO_INITDB_ROOT_PASSWORD"]; v != "" {
			cfg["password"] = v
		}
		if v := env["MONGO_INITDB_DATABASE"]; v != "" {
			cfg["database"] = v
		}
	case "redis", "valkey":
		if v := firstNonEmpty(env["REDIS_PASSWORD"], env["VALKEY_PASSWORD"]); v != "" {
			cfg["password"] = v
		}
	case "rabbitmq":
		if v := env["RABBITMQ_DEFAULT_USER"]; v != "" {
			cfg["user"] = v
		}
		if v := env["RABBITMQ_DEFAULT_PASS"]; v != "" {
			cfg["password"] = v
		}
	case "clickhouse":
		if v := env["CLICKHOUSE_USER"]; v != "" {
			cfg["user"] = v
		}
		if v := env["CLICKHOUSE_PASSWORD"]; v != "" {
			cfg["password"] = v
		}
		if v := env["CLICKHOUSE_DB"]; v != "" {
			cfg["database"] = v
		}
	}
	return cfg
}
