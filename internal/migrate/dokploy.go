package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
)

func DetectDokploy(ctx context.Context, host Host) DetectedPlatform {
	res := DetectedPlatform{
		Platform: PlatformDokploy,
		Name:     PlatformLabel(PlatformDokploy),
		Status:   "not_found",
		Summary:  "No Dokploy installation detected on this host.",
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
		labels := parseLabels(c.Labels)
		swarmSvc := strings.ToLower(labels["com.docker.swarm.service.name"])
		if swarmSvc == "dokploy-postgres" || strings.HasPrefix(nameLower, "dokploy-postgres") {
			if pgContainer == "" || strings.EqualFold(c.State, "running") {
				pgContainer = c.Names
			}
		} else if swarmSvc == "dokploy" || nameLower == "dokploy" || strings.HasPrefix(nameLower, "dokploy.1.") {
			if appContainer == "" || strings.EqualFold(c.State, "running") {
				appContainer = c.Names
			}
		}
	}

	baseDir := "/etc/dokploy"
	if _, err := host.Stat(baseDir); err == nil {
		res.DataDir = baseDir
	}

	if pgContainer == "" && res.DataDir == "" {
		return res
	}

	res.Detected = true
	res.Container = pgContainer
	res.Status = "detected"

	if pgContainer == "" {
		res.Warnings = append(res.Warnings, "Dokploy directory /etc/dokploy found, but no dokploy-postgres container is running.")
		res.Summary = "Dokploy files detected, but its PostgreSQL container was not found."
		return res
	}

	pgEnv := inspectContainerEnv(ctx, host, pgContainer)
	var appEnv map[string]string
	if appContainer != "" {
		appEnv = inspectContainerEnv(ctx, host, appContainer)
	}

	pgUser := firstNonEmpty(pgEnv["POSTGRES_USER"], "dokploy")
	pgDB := firstNonEmpty(pgEnv["POSTGRES_DB"], "dokploy")

	var secrets []string
	for _, s := range []string{
		appEnv["ENCRYPTION_KEY"],
		appEnv["BETTER_AUTH_SECRET"],
	} {
		if s != "" {
			secrets = append(secrets, s)
		}
	}
	var rawKeys [][]byte
	if b, err := host.ReadFile(filepath.Join(baseDir, "encryption.key")); err == nil {
		rawKeys = ParseDokployKeyFile(string(b))
	}

	query := `SELECT json_build_object(
		'orgs', COALESCE((SELECT json_agg(json_build_object('id', id, 'name', name)) FROM organization), '[]'::json),
		'projects', COALESCE((SELECT json_agg(json_build_object(
			'projectId', "projectId",
			'name', name,
			'description', description,
			'organizationId', "organizationId",
			'env', env
		)) FROM project), '[]'::json),
		'environments', COALESCE((SELECT json_agg(json_build_object(
			'environmentId', "environmentId",
			'name', name,
			'projectId', "projectId",
			'env', env
		)) FROM environment), '[]'::json),
		'applications', COALESCE((SELECT json_agg(json_build_object(
			'applicationId', "applicationId",
			'name', name,
			'appName', "appName",
			'description', description,
			'env', env,
			'sourceType', "sourceType",
			'buildType', "buildType",
			'repository', repository,
			'owner', owner,
			'branch', branch,
			'buildPath', "buildPath",
			'gitlabRepository', "gitlabRepository",
			'gitlabOwner', "gitlabOwner",
			'gitlabBranch', "gitlabBranch",
			'gitlabBuildPath', "gitlabBuildPath",
			'bitbucketRepository', "bitbucketRepository",
			'bitbucketOwner', "bitbucketOwner",
			'bitbucketBranch', "bitbucketBranch",
			'bitbucketBuildPath', "bitbucketBuildPath",
			'giteaRepository', "giteaRepository",
			'giteaOwner', "giteaOwner",
			'giteaBranch', "giteaBranch",
			'giteaBuildPath', "giteaBuildPath",
			'customGitUrl', "customGitUrl",
			'customGitBranch', "customGitBranch",
			'customGitBuildPath', "customGitBuildPath",
			'dockerImage', "dockerImage",
			'dockerfile', dockerfile,
			'publishDirectory', "publishDirectory",
			'command', command,
			'autoDeploy', "autoDeploy",
			'environmentId', "environmentId"
		)) FROM application), '[]'::json),
		'composes', COALESCE((SELECT json_agg(json_build_object(
			'composeId', "composeId",
			'name', name,
			'appName', "appName",
			'description', description,
			'env', env,
			'sourceType', "sourceType",
			'repository', repository,
			'owner', owner,
			'branch', branch,
			'customGitUrl', "customGitUrl",
			'customGitBranch', "customGitBranch",
			'autoDeploy', "autoDeploy",
			'environmentId', "environmentId"
		)) FROM compose), '[]'::json),
		'postgres', COALESCE((SELECT json_agg(json_build_object(
			'id', "postgresId", 'name', name, 'appName', "appName", 'databaseName', "databaseName",
			'databaseUser', "databaseUser", 'databasePassword', "databasePassword",
			'dockerImage', "dockerImage", 'externalPort', "externalPort", 'environmentId', "environmentId"
		)) FROM postgres), '[]'::json),
		'mysql', COALESCE((SELECT json_agg(json_build_object(
			'id', "mysqlId", 'name', name, 'appName', "appName", 'databaseName', "databaseName",
			'databaseUser', "databaseUser", 'databasePassword', "databasePassword",
			'dockerImage', "dockerImage", 'externalPort', "externalPort", 'environmentId', "environmentId"
		)) FROM mysql), '[]'::json),
		'mariadb', COALESCE((SELECT json_agg(json_build_object(
			'id', "mariadbId", 'name', name, 'appName', "appName", 'databaseName', "databaseName",
			'databaseUser', "databaseUser", 'databasePassword', "databasePassword",
			'dockerImage', "dockerImage", 'externalPort', "externalPort", 'environmentId', "environmentId"
		)) FROM mariadb), '[]'::json),
		'mongo', COALESCE((SELECT json_agg(json_build_object(
			'id', "mongoId", 'name', name, 'appName', "appName",
			'databaseUser', "databaseUser", 'databasePassword', "databasePassword",
			'dockerImage', "dockerImage", 'externalPort', "externalPort", 'environmentId', "environmentId"
		)) FROM mongo), '[]'::json),
		'redis', COALESCE((SELECT json_agg(json_build_object(
			'id', "redisId", 'name', name, 'appName', "appName",
			'databasePassword', password,
			'dockerImage', "dockerImage", 'externalPort', "externalPort", 'environmentId', "environmentId"
		)) FROM redis), '[]'::json),
		'domains', COALESCE((SELECT json_agg(json_build_object(
			'host', host, 'https', https, 'port', port, 'path', path,
			'stripPath', "stripPath", 'applicationId', "applicationId", 'composeId', "composeId"
		)) FROM domain WHERE enabled = true), '[]'::json),
		'mounts', COALESCE((SELECT json_agg(json_build_object(
			'type', type, 'hostPath', "hostPath", 'volumeName', "volumeName", 'mountPath', "mountPath",
			'applicationId', "applicationId", 'composeId', "composeId",
			'postgresId', "postgresId", 'mysqlId', "mysqlId", 'mariadbId', "mariadbId",
			'mongoId', "mongoId", 'redisId', "redisId"
		)) FROM mount), '[]'::json),
		'deployments', COALESCE((SELECT json_agg(d) FROM (
			SELECT "deploymentId", title, status, "logPath", "applicationId", "composeId", "createdAt", "finishedAt",
			       ROW_NUMBER() OVER (PARTITION BY COALESCE("applicationId", "composeId") ORDER BY "createdAt" DESC) AS rn
			FROM deployment
			WHERE "applicationId" IS NOT NULL OR "composeId" IS NOT NULL
		) d WHERE d.rn <= 10), '[]'::json)
	)::text;`

	rawJSON, err := queryPostgresJSON(ctx, host, pgContainer, pgUser, pgDB, query)
	if err != nil {
		res.Status = "error"
		res.Error = err.Error()
		res.Summary = "Dokploy detected, but querying its PostgreSQL database failed."
		return res
	}

	var dump struct {
		Orgs []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"orgs"`
		Projects []struct {
			ProjectID      string `json:"projectId"`
			Name           string `json:"name"`
			Description    string `json:"description"`
			OrganizationID string `json:"organizationId"`
			Env            string `json:"env"`
		} `json:"projects"`
		Environments []struct {
			EnvironmentID string `json:"environmentId"`
			Name          string `json:"name"`
			ProjectID     string `json:"projectId"`
			Env           string `json:"env"`
		} `json:"environments"`
		Applications []struct {
			ApplicationID       string `json:"applicationId"`
			Name                string `json:"name"`
			AppName             string `json:"appName"`
			Description         string `json:"description"`
			Env                 string `json:"env"`
			SourceType          string `json:"sourceType"`
			BuildType           string `json:"buildType"`
			Repository          string `json:"repository"`
			Owner               string `json:"owner"`
			Branch              string `json:"branch"`
			BuildPath           string `json:"buildPath"`
			GitlabRepository    string `json:"gitlabRepository"`
			GitlabOwner         string `json:"gitlabOwner"`
			GitlabBranch        string `json:"gitlabBranch"`
			GitlabBuildPath     string `json:"gitlabBuildPath"`
			BitbucketRepository string `json:"bitbucketRepository"`
			BitbucketOwner      string `json:"bitbucketOwner"`
			BitbucketBranch     string `json:"bitbucketBranch"`
			BitbucketBuildPath  string `json:"bitbucketBuildPath"`
			GiteaRepository     string `json:"giteaRepository"`
			GiteaOwner          string `json:"giteaOwner"`
			GiteaBranch         string `json:"giteaBranch"`
			GiteaBuildPath      string `json:"giteaBuildPath"`
			CustomGitURL        string `json:"customGitUrl"`
			CustomGitBranch     string `json:"customGitBranch"`
			CustomGitBuildPath  string `json:"customGitBuildPath"`
			DockerImage         string `json:"dockerImage"`
			Dockerfile          string `json:"dockerfile"`
			PublishDirectory    string `json:"publishDirectory"`
			Command             string `json:"command"`
			AutoDeploy          bool   `json:"autoDeploy"`
			EnvironmentID       string `json:"environmentId"`
		} `json:"applications"`
		Composes []struct {
			ComposeID       string `json:"composeId"`
			Name            string `json:"name"`
			AppName         string `json:"appName"`
			Description     string `json:"description"`
			Env             string `json:"env"`
			SourceType      string `json:"sourceType"`
			Repository      string `json:"repository"`
			Owner           string `json:"owner"`
			Branch          string `json:"branch"`
			CustomGitURL    string `json:"customGitUrl"`
			CustomGitBranch string `json:"customGitBranch"`
			AutoDeploy      bool   `json:"autoDeploy"`
			EnvironmentID   string `json:"environmentId"`
		} `json:"composes"`
		Postgres []dokployDBRow `json:"postgres"`
		Mysql    []dokployDBRow `json:"mysql"`
		Mariadb  []dokployDBRow `json:"mariadb"`
		Mongo    []dokployDBRow `json:"mongo"`
		Redis    []dokployDBRow `json:"redis"`
		Domains  []struct {
			Host          string `json:"host"`
			HTTPS         bool   `json:"https"`
			Port          int    `json:"port"`
			Path          string `json:"path"`
			StripPath     bool   `json:"stripPath"`
			ApplicationID string `json:"applicationId"`
			ComposeID     string `json:"composeId"`
		} `json:"domains"`
		Mounts []struct {
			Type          string `json:"type"`
			HostPath      string `json:"hostPath"`
			VolumeName    string `json:"volumeName"`
			MountPath     string `json:"mountPath"`
			ApplicationID string `json:"applicationId"`
			ComposeID     string `json:"composeId"`
			PostgresID    string `json:"postgresId"`
			MysqlID       string `json:"mysqlId"`
			MariadbID     string `json:"mariadbId"`
			MongoID       string `json:"mongoId"`
			RedisID       string `json:"redisId"`
		} `json:"mounts"`
		Deployments []struct {
			DeploymentID  string `json:"deploymentId"`
			Title         string `json:"title"`
			Status        string `json:"status"`
			LogPath       string `json:"logPath"`
			ApplicationID string `json:"applicationId"`
			ComposeID     string `json:"composeId"`
			CreatedAt     string `json:"createdAt"`
			FinishedAt    string `json:"finishedAt"`
		} `json:"deployments"`
	}

	if err := json.Unmarshal([]byte(rawJSON), &dump); err != nil {
		res.Status = "error"
		res.Error = fmt.Sprintf("decode dokploy database snapshot: %v", err)
		res.Summary = "Dokploy database returned unexpected data."
		return res
	}

	orgNames := map[string]string{}
	for _, o := range dump.Orgs {
		orgNames[o.ID] = o.Name
	}

	type projMeta struct {
		Name string
		Org  string
		Env  map[string]string
	}
	projByID := map[string]projMeta{}
	for _, p := range dump.Projects {
		envStr, _ := DecryptDokploy(p.Env, secrets, rawKeys)
		projByID[p.ProjectID] = projMeta{
			Name: p.Name,
			Org:  orgNames[p.OrganizationID],
			Env:  ParseDotEnv(envStr),
		}
	}

	type envMeta struct {
		Name      string
		ProjectID string
		Env       map[string]string
	}
	envByID := map[string]envMeta{}
	for _, e := range dump.Environments {
		envStr, _ := DecryptDokploy(e.Env, secrets, rawKeys)
		envByID[e.EnvironmentID] = envMeta{
			Name:      e.Name,
			ProjectID: e.ProjectID,
			Env:       ParseDotEnv(envStr),
		}
	}

	domainsByOwner := map[string][]DiscoveredDomain{}
	for _, d := range dump.Domains {
		if d.Host == "" {
			continue
		}
		ownerID := firstNonEmpty(d.ApplicationID, d.ComposeID)
		if ownerID == "" {
			continue
		}
		domainsByOwner[ownerID] = append(domainsByOwner[ownerID], DiscoveredDomain{
			Host:        d.Host,
			PathPrefix:  firstNonEmpty(d.Path, "/"),
			StripPrefix: d.StripPath,
			HTTPS:       d.HTTPS,
			Port:        d.Port,
		})
	}

	mountsByOwner := map[string][]DiscoveredVolume{}
	for _, m := range dump.Mounts {
		if m.MountPath == "" || m.Type == "file" {
			continue
		}
		ownerID := firstNonEmpty(m.ApplicationID, m.ComposeID, m.PostgresID, m.MysqlID, m.MariadbID, m.MongoID, m.RedisID)
		if ownerID == "" {
			continue
		}
		srcName := m.VolumeName
		srcType := "volume"
		if m.Type == "bind" {
			srcName = m.HostPath
			srcType = "bind"
		}
		if srcName == "" {
			continue
		}
		mountsByOwner[ownerID] = append(mountsByOwner[ownerID], DiscoveredVolume{
			Name:       filepath.Base(m.MountPath),
			SourceType: srcType,
			SourceName: srcName,
			MountPath:  m.MountPath,
		})
	}

	logsByOwner := map[string][]DiscoveredLog{}
	for _, dep := range dump.Deployments {
		ownerID := firstNonEmpty(dep.ApplicationID, dep.ComposeID)
		if ownerID == "" {
			continue
		}
		content := ""
		if dep.LogPath != "" {
			if b, err := host.ReadFile(dep.LogPath); err == nil {
				content = string(b)
				const maxLogBytes = 64 * 1024
				if len(content) > maxLogBytes {
					content = content[len(content)-maxLogBytes:]
				}
			}
		}
		if content == "" {
			content = fmt.Sprintf("Imported Dokploy deployment %s (%s)", dep.DeploymentID, dep.Status)
		}
		logsByOwner[ownerID] = append(logsByOwner[ownerID], DiscoveredLog{
			Title:      dep.Title,
			Status:     dep.Status,
			CreatedAt:  parseTimeFlexible(dep.CreatedAt),
			FinishedAt: parseTimeFlexible(dep.FinishedAt),
			Content:    content,
		})
	}

	// Collect managed databases/services first so we can link them to apps in the same environment.
	servicesByEnv := map[string][]string{}
	addDBServices := func(engine string, rows []dokployDBRow) {
		for _, row := range rows {
			em := envByID[row.EnvironmentID]
			pm := projByID[em.ProjectID]
			cfg := map[string]string{}
			if row.DatabaseUser != "" {
				cfg["user"] = row.DatabaseUser
			}
			if row.DatabasePassword != "" {
				cfg["password"] = row.DatabasePassword
			}
			if row.DatabaseName != "" {
				cfg["database"] = row.DatabaseName
			}
			vols := append([]DiscoveredVolume{}, mountsByOwner[row.ID]...)
			container := findMatchingContainer(containers, row.AppName, row.ID)
			if container != "" {
				vols = append(vols, inspectContainerMounts(ctx, host, container)...)
			}
			vols = dedupVolumes(vols)

			ds := DiscoveredService{
				ID:            row.ID,
				Platform:      PlatformDokploy,
				SourceOrg:     pm.Org,
				SourceProject: pm.Name,
				Environment:   em.Name,
				Name:          row.Name,
				Engine:        engine,
				RawEngine:     firstNonEmpty(row.DockerImage, engine),
				Image:         row.DockerImage,
				Version:       ParseImageTag(row.DockerImage),
				ExternalPort:  row.ExternalPort,
				Config:        cfg,
				ConfigKeys:    sortedMapKeys(cfg),
				Volumes:       vols,
				ContainerName: container,
				Compatible:    true,
			}
			res.Services = append(res.Services, ds)
			servicesByEnv[row.EnvironmentID] = append(servicesByEnv[row.EnvironmentID], row.ID)
		}
	}
	addDBServices("postgresql", dump.Postgres)
	addDBServices("mysql", dump.Mysql)
	addDBServices("mariadb", dump.Mariadb)
	addDBServices("mongodb", dump.Mongo)
	addDBServices("redis", dump.Redis)

	for _, app := range dump.Applications {
		em := envByID[app.EnvironmentID]
		pm := projByID[em.ProjectID]

		var warnings []string
		mergedEnv := map[string]string{}
		for k, v := range pm.Env {
			mergedEnv[k] = v
		}
		for k, v := range em.Env {
			mergedEnv[k] = v
		}
		if app.Env != "" {
			if dec, err := DecryptDokploy(app.Env, secrets, rawKeys); err == nil {
				for k, v := range ParseDotEnv(dec) {
					mergedEnv[k] = v
				}
			} else {
				warnings = append(warnings, "Encrypted environment variables could not be decrypted; check ENCRYPTION_KEY / BETTER_AUTH_SECRET.")
			}
		}

		gitURL, branch, rootDir := resolveDokployGit(app.SourceType, app.Owner, app.Repository, app.Branch, app.BuildPath,
			app.GitlabOwner, app.GitlabRepository, app.GitlabBranch, app.GitlabBuildPath,
			app.BitbucketOwner, app.BitbucketRepository, app.BitbucketBranch, app.BitbucketBuildPath,
			app.GiteaOwner, app.GiteaRepository, app.GiteaBranch, app.GiteaBuildPath,
			app.CustomGitURL, app.CustomGitBranch, app.CustomGitBuildPath)

		sourceKind := "git"
		if app.SourceType == "docker" {
			sourceKind = "docker"
			if gitURL == "" {
				warnings = append(warnings, "Docker-image-only application: configure a Git source or Dockerfile in Startship to redeploy.")
			}
		} else if app.SourceType == "drop" {
			sourceKind = "local"
			warnings = append(warnings, "Uploaded archive (drop) application: configure a Git source in Startship to redeploy.")
		}

		doms := domainsByOwner[app.ApplicationID]
		port := 0
		for _, d := range doms {
			if d.Port > 0 {
				port = d.Port
				break
			}
		}

		vols := append([]DiscoveredVolume{}, mountsByOwner[app.ApplicationID]...)
		container := findMatchingContainer(containers, app.AppName, app.ApplicationID)
		if container != "" {
			vols = append(vols, inspectContainerMounts(ctx, host, container)...)
		}
		vols = dedupVolumes(vols)

		logs := append([]DiscoveredLog{}, logsByOwner[app.ApplicationID]...)
		if liveLogs := captureContainerLogs(ctx, host, container, 200); liveLogs != "" {
			logs = append(logs, DiscoveredLog{
				Title:   "Container runtime logs snapshot",
				Status:  "running",
				Content: liveLogs,
			})
		}

		res.Projects = append(res.Projects, DiscoveredProject{
			ID:             app.ApplicationID,
			Platform:       PlatformDokploy,
			SourceOrg:      pm.Org,
			SourceProject:  pm.Name,
			Environment:    em.Name,
			Name:           app.Name,
			Description:    app.Description,
			SourceKind:     sourceKind,
			GitURL:         gitURL,
			Branch:         firstNonEmpty(branch, "main"),
			RootDir:        cleanRootDir(rootDir),
			BuildType:      app.BuildType,
			Dockerfile:     app.Dockerfile,
			DockerImage:    app.DockerImage,
			StartCommand:   app.Command,
			OutputDir:      app.PublishDirectory,
			Port:           port,
			AutoDeploy:     app.AutoDeploy,
			Env:            mergedEnv,
			EnvKeys:        sortedMapKeys(mergedEnv),
			EnvCount:       len(mergedEnv),
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

	for _, comp := range dump.Composes {
		em := envByID[comp.EnvironmentID]
		pm := projByID[em.ProjectID]
		var warnings []string
		mergedEnv := map[string]string{}
		for k, v := range pm.Env {
			mergedEnv[k] = v
		}
		for k, v := range em.Env {
			mergedEnv[k] = v
		}
		if comp.Env != "" {
			if dec, err := DecryptDokploy(comp.Env, secrets, rawKeys); err == nil {
				for k, v := range ParseDotEnv(dec) {
					mergedEnv[k] = v
				}
			} else {
				warnings = append(warnings, "Encrypted compose environment could not be decrypted.")
			}
		}

		gitURL := comp.CustomGitURL
		branch := comp.CustomGitBranch
		if gitURL == "" && comp.Owner != "" && comp.Repository != "" {
			gitURL = fmt.Sprintf("https://github.com/%s/%s.git", comp.Owner, comp.Repository)
			branch = comp.Branch
		}
		if comp.SourceType == "raw" || gitURL == "" {
			warnings = append(warnings, "Raw Docker Compose stack: review build settings after import.")
		}

		vols := append([]DiscoveredVolume{}, mountsByOwner[comp.ComposeID]...)
		container := findMatchingContainer(containers, comp.AppName, comp.ComposeID)
		if container != "" {
			vols = append(vols, inspectContainerMounts(ctx, host, container)...)
		}
		vols = dedupVolumes(vols)

		res.Projects = append(res.Projects, DiscoveredProject{
			ID:             comp.ComposeID,
			Platform:       PlatformDokploy,
			SourceOrg:      pm.Org,
			SourceProject:  pm.Name,
			Environment:    em.Name,
			Name:           comp.Name,
			Description:    comp.Description,
			SourceKind:     "compose",
			GitURL:         gitURL,
			Branch:         firstNonEmpty(branch, "main"),
			RootDir:        ".",
			BuildType:      "auto",
			AutoDeploy:     comp.AutoDeploy,
			Env:            mergedEnv,
			EnvKeys:        sortedMapKeys(mergedEnv),
			EnvCount:       len(mergedEnv),
			Domains:        domainsByOwner[comp.ComposeID],
			Volumes:        vols,
			Logs:           logsByOwner[comp.ComposeID],
			LogCount:       len(logsByOwner[comp.ComposeID]),
			ContainerName:  container,
			LinkedServices: servicesByEnv[comp.EnvironmentID],
			Compatible:     true,
			Warnings:       warnings,
		})
	}

	res.Summary = fmt.Sprintf("Found %d project(s) and %d service(s).", len(res.Projects), len(res.Services))
	return res
}

type dokployDBRow struct {
	ID               string `json:"id"`
	Name             string `json:"name"`
	AppName          string `json:"appName"`
	DatabaseName     string `json:"databaseName"`
	DatabaseUser     string `json:"databaseUser"`
	DatabasePassword string `json:"databasePassword"`
	DockerImage      string `json:"dockerImage"`
	ExternalPort     int    `json:"externalPort"`
	EnvironmentID    string `json:"environmentId"`
}

func resolveDokployGit(
	sourceType, ghOwner, ghRepo, ghBranch, ghPath,
	glOwner, glRepo, glBranch, glPath,
	bbOwner, bbRepo, bbBranch, bbPath,
	gtOwner, gtRepo, gtBranch, gtPath,
	customURL, customBranch, customPath string,
) (string, string, string) {
	switch strings.ToLower(sourceType) {
	case "github":
		if ghOwner != "" && ghRepo != "" {
			return fmt.Sprintf("https://github.com/%s/%s.git", ghOwner, ghRepo), ghBranch, ghPath
		}
	case "gitlab":
		if glOwner != "" && glRepo != "" {
			return fmt.Sprintf("https://gitlab.com/%s/%s.git", glOwner, glRepo), glBranch, glPath
		}
	case "bitbucket":
		if bbOwner != "" && bbRepo != "" {
			return fmt.Sprintf("https://bitbucket.org/%s/%s.git", bbOwner, bbRepo), bbBranch, bbPath
		}
	case "gitea":
		if gtOwner != "" && gtRepo != "" {
			return fmt.Sprintf("%s/%s", gtOwner, gtRepo), gtBranch, gtPath
		}
	case "git":
		return customURL, customBranch, customPath
	}
	if customURL != "" {
		return customURL, customBranch, customPath
	}
	if ghOwner != "" && ghRepo != "" {
		return fmt.Sprintf("https://github.com/%s/%s.git", ghOwner, ghRepo), ghBranch, ghPath
	}
	return "", "", ""
}
