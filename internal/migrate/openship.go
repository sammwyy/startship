package migrate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// openshipHTTPClient is an optional interface a test Host can implement to mock
// loopback HTTP requests to a bare/PGlite Openship API.
type openshipHTTPClient interface {
	DoHTTP(req *http.Request) (*http.Response, error)
}

type openshipDump struct {
	Orgs        []openshipOrgRow        `json:"orgs"`
	Projects    []openshipProjectRow    `json:"projects"`
	EnvVars     []openshipEnvVarRow     `json:"env_vars"`
	Services    []openshipServiceRow    `json:"services"`
	Domains     []openshipDomainRow     `json:"domains"`
	Deployments []openshipDeploymentRow `json:"deployments"`
}

type openshipOrgRow struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type openshipProjectRow struct {
	ID                  string          `json:"id"`
	OrganizationID      string          `json:"organization_id"`
	Name                string          `json:"name"`
	Slug                string          `json:"slug"`
	EnvironmentName     string          `json:"environment_name"`
	GitProvider         string          `json:"git_provider"`
	GitOwner            string          `json:"git_owner"`
	GitRepo             string          `json:"git_repo"`
	GitBranch           string          `json:"git_branch"`
	GitURL              string          `json:"git_url"`
	LocalPath           string          `json:"local_path"`
	CloneTokenEncrypted string          `json:"clone_token_encrypted"`
	Framework           string          `json:"framework"`
	InstallCommand      string          `json:"install_command"`
	BuildCommand        string          `json:"build_command"`
	OutputDirectory     string          `json:"output_directory"`
	RootDirectory       string          `json:"root_directory"`
	StartCommand        string          `json:"start_command"`
	Port                int             `json:"port"`
	HasServer           *bool           `json:"has_server"`
	Volumes             json.RawMessage `json:"volumes"`
	DeletedAt           string          `json:"deleted_at"`
}

type openshipEnvVarRow struct {
	ProjectID   string `json:"project_id"`
	ServiceID   string `json:"service_id"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Environment string `json:"environment"`
	Plaintext   bool   `json:"-"` // true when fetched already-decrypted from Openship API
}

type openshipServiceRow struct {
	ID           string          `json:"id"`
	ProjectID    string          `json:"project_id"`
	Kind         string          `json:"kind"`
	Name         string          `json:"name"`
	Image        string          `json:"image"`
	Ports        json.RawMessage `json:"ports"`
	Environment  json.RawMessage `json:"environment"`
	Volumes      json.RawMessage `json:"volumes"`
	Domain       string          `json:"domain"`
	CustomDomain string          `json:"custom_domain"`
	ArchivedAt   string          `json:"archived_at"`
}

type openshipDomainRow struct {
	OwnerType  string `json:"owner_type"`
	ProjectID  string `json:"project_id"`
	ServiceID  string `json:"service_id"`
	Hostname   string `json:"hostname"`
	TargetPort int    `json:"target_port"`
	TargetPath string `json:"target_path"`
}

type openshipDeploymentRow struct {
	ID            string `json:"id"`
	ProjectID     string `json:"project_id"`
	Branch        string `json:"branch"`
	CommitSHA     string `json:"commit_sha"`
	CommitMessage string `json:"commit_message"`
	Status        string `json:"status"`
	ErrorMessage  string `json:"error_message"`
	CreatedAt     string `json:"created_at"`
}

func DetectOpenship(ctx context.Context, host Host) DetectedPlatform {
	res := DetectedPlatform{
		Platform: PlatformOpenship,
		Name:     PlatformLabel(PlatformOpenship),
		Status:   "not_found",
		Summary:  "No Openship installation detected on this host.",
		Projects: []DiscoveredProject{},
		Services: []DiscoveredService{},
	}

	containers, dockerErr := listDockerContainers(ctx, host)

	// Pass 1: identify any Openship container and collect its compose project(s)
	// and working dir(s). This handles installs whose compose project name is
	// "compose", "docker", "openship-dev", or a custom COMPOSE_PROJECT_NAME
	// where the postgres container uses stock postgres:16-alpine.
	openshipProjects := map[string]bool{}
	var composeWorkingDirs []string
	var pgContainer, apiContainer string

	for _, c := range containers {
		labels := parseLabels(c.Labels)
		nameLower := strings.ToLower(c.Names)
		imageLower := strings.ToLower(c.Image)
		proj := labels["com.docker.compose.project"]
		projLower := strings.ToLower(proj)
		workDir := labels["com.docker.compose.project.working_dir"]
		workDirLower := strings.ToLower(workDir)

		if strings.Contains(projLower, "openship") ||
			strings.Contains(workDirLower, "openship") ||
			strings.Contains(imageLower, "openship") ||
			strings.Contains(nameLower, "openship") {
			if proj != "" {
				openshipProjects[proj] = true
			}
			if workDir != "" {
				composeWorkingDirs = append(composeWorkingDirs, workDir)
			}
		}
	}

	// Pass 2: match postgres and api containers in discovered Openship compose
	// projects or directly named/imaged as Openship.
	for _, c := range containers {
		labels := parseLabels(c.Labels)
		nameLower := strings.ToLower(c.Names)
		imageLower := strings.ToLower(c.Image)
		proj := labels["com.docker.compose.project"]
		inProject := proj != "" && openshipProjects[proj]
		directMatch := strings.Contains(nameLower, "openship") ||
			strings.Contains(imageLower, "openship") ||
			strings.Contains(strings.ToLower(labels["com.docker.compose.project.working_dir"]), "openship")
		if !inProject && !directMatch {
			continue
		}

		svc := strings.ToLower(labels["com.docker.compose.service"])
		if svc == "postgres" || strings.Contains(nameLower, "postgres") || (inProject && strings.Contains(imageLower, "postgres")) {
			if pgContainer == "" || strings.EqualFold(c.State, "running") {
				pgContainer = c.Names
			}
		}
		if svc == "api" || strings.Contains(imageLower, "openship-api") || (strings.Contains(nameLower, "api") && (inProject || strings.Contains(nameLower, "openship"))) {
			if apiContainer == "" || strings.EqualFold(c.State, "running") {
				apiContainer = c.Names
			}
		}
	}

	// Discover .env, auth-secret, internal-token, ports.json, and bare data dir
	// across container working dirs, OPENSHIP_HOME, host.HomeDir(), /root, /home/*,
	// /etc/openship, and /opt/openship.
	envFromFile, bareInfo := discoverOpenshipFiles(host, composeWorkingDirs)
	if bareInfo.dataDir != "" {
		res.DataDir = bareInfo.dataDir
	}

	hasBareInstall := bareInfo.hasDataDir || bareInfo.internalToken != "" || bareInfo.authSecret != "" || bareInfo.apiPort > 0
	if pgContainer == "" && apiContainer == "" && res.DataDir == "" && !hasBareInstall {
		if dockerErr != nil {
			res.Status = "error"
			res.Error = dockerErr.Error()
			res.Summary = "Unable to inspect Docker containers."
		}
		return res
	}

	res.Detected = true
	res.Container = pgContainer
	res.Status = "detected"

	var pgEnv, apiEnv map[string]string
	if pgContainer != "" {
		pgEnv = inspectContainerEnv(ctx, host, pgContainer)
	}
	if apiContainer != "" {
		apiEnv = inspectContainerEnv(ctx, host, apiContainer)
	}

	secrets := collectOpenshipSecrets(apiEnv, envFromFile, bareInfo.authSecret)
	internalTokens := collectOpenshipInternalTokens(apiEnv, envFromFile, bareInfo.internalToken)
	apiPort := resolveOpenshipAPIPort(apiEnv, envFromFile, bareInfo.apiPort)

	var dump openshipDump
	var queryErr error
	loadedFromDB := false

	if pgContainer != "" {
		dbURLUser, dbURLPass, dbURLName := parsePostgresURL(firstNonEmpty(apiEnv["DATABASE_URL"], envFromFile["DATABASE_URL"]))
		pgUser := firstNonEmpty(pgEnv["POSTGRES_USER"], dbURLUser, envFromFile["POSTGRES_USER"], "openship")
		pgDB := firstNonEmpty(pgEnv["POSTGRES_DB"], dbURLName, envFromFile["POSTGRES_DB"], "openship")
		pgPass := firstNonEmpty(pgEnv["POSTGRES_PASSWORD"], dbURLPass, envFromFile["POSTGRES_PASSWORD"])

		dump, queryErr = queryOpenshipPostgres(ctx, host, pgContainer, pgUser, pgDB, pgPass, &res)
		if queryErr == nil {
			loadedFromDB = true
		}
	}

	// Fallback to local Openship HTTP API (supports bare/PGlite installs and
	// compose installs where direct psql fails).
	if !loadedFromDB && len(internalTokens) > 0 {
		apiDump, apiErr := queryOpenshipLocalAPI(ctx, host, apiPort, internalTokens)
		if apiErr == nil {
			dump = apiDump
			loadedFromDB = true
			if pgContainer == "" {
				res.Container = fmt.Sprintf("127.0.0.1:%d", apiPort)
			}
			if queryErr != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("PostgreSQL query failed (%v); loaded data from Openship local API instead.", queryErr))
			}
		} else if pgContainer == "" {
			if hasBareInstall {
				res.Warnings = append(res.Warnings, fmt.Sprintf("Openship bare/PGlite installation detected at %s, but its local API on 127.0.0.1:%d was not reachable (%v). Start Openship (`openship up`) so Startship can read its database.", res.DataDir, apiPort, apiErr))
				res.Summary = "Openship bare installation detected, but its local API is not reachable."
			} else {
				res.Warnings = append(res.Warnings, "Openship config directory found, but no Openship PostgreSQL container is running.")
				res.Summary = "Openship files detected, but its PostgreSQL container was not found."
			}
			return res
		}
	}

	if !loadedFromDB {
		if pgContainer == "" {
			if hasBareInstall {
				res.Warnings = append(res.Warnings, fmt.Sprintf("Openship bare/PGlite installation detected at %s, but no internal token was readable to query its local API.", res.DataDir))
				res.Summary = "Openship bare installation detected, but could not authenticate to its local API."
			} else {
				res.Warnings = append(res.Warnings, "Openship config directory found, but no Openship PostgreSQL container is running.")
				res.Summary = "Openship files detected, but its PostgreSQL container was not found."
			}
			return res
		}
		res.Status = "error"
		res.Error = queryErr.Error()
		res.Summary = "Openship detected, but querying its PostgreSQL database failed."
		return res
	}

	populateOpenshipDiscovery(ctx, host, containers, dump, secrets, &res)
	return res
}

type openshipLocalFiles struct {
	dataDir       string
	hasDataDir    bool
	authSecret    string
	internalToken string
	apiPort       int
}

func discoverOpenshipFiles(host Host, composeWorkingDirs []string) (map[string]string, openshipLocalFiles) {
	var info openshipLocalFiles
	envFromFile := map[string]string{}

	var dirs []string
	seenDir := map[string]bool{}
	addDir := func(d string) {
		d = strings.TrimSpace(d)
		if d == "" {
			return
		}
		d = filepath.Clean(d)
		if !seenDir[d] {
			seenDir[d] = true
			dirs = append(dirs, d)
		}
	}

	for _, wd := range composeWorkingDirs {
		addDir(wd)
		if filepath.Base(wd) == "compose" {
			addDir(filepath.Dir(wd))
		}
	}
	if _, isOS := host.(OSHost); isOS {
		if oh := strings.TrimSpace(os.Getenv("OPENSHIP_HOME")); oh != "" {
			addDir(oh)
		}
	}
	if home := strings.TrimSpace(host.HomeDir()); home != "" {
		addDir(filepath.Join(home, ".openship"))
	}
	addDir("/root/.openship")
	if _, isOS := host.(OSHost); isOS {
		if entries, err := os.ReadDir("/home"); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					addDir(filepath.Join("/home", e.Name(), ".openship"))
				}
			}
		}
	}
	addDir("/etc/openship")
	addDir("/opt/openship")
	addDir("/opt/openship/docker")

	// 1. Look for compose/.env or .env first.
	for _, dir := range dirs {
		candidates := []string{
			filepath.Join(dir, "compose", ".env"),
			filepath.Join(dir, ".env"),
		}
		for _, p := range candidates {
			if b, err := host.ReadFile(p); err == nil {
				if info.dataDir == "" {
					info.dataDir = filepath.Dir(p)
				}
				parsed := ParseDotEnv(string(b))
				for k, v := range parsed {
					if _, exists := envFromFile[k]; !exists && v != "" {
						envFromFile[k] = v
					}
				}
				break
			}
		}
		// 2. Check bare/CLI state files in dir.
		if info.authSecret == "" {
			if b, err := host.ReadFile(filepath.Join(dir, "auth-secret")); err == nil {
				if s := strings.TrimSpace(string(b)); s != "" {
					info.authSecret = s
					if info.dataDir == "" {
						info.dataDir = dir
					}
				}
			}
		}
		if info.internalToken == "" {
			if b, err := host.ReadFile(filepath.Join(dir, "internal-token")); err == nil {
				if s := strings.TrimSpace(string(b)); s != "" {
					info.internalToken = s
					if info.dataDir == "" {
						info.dataDir = dir
					}
				}
			}
		}
		if info.apiPort == 0 {
			if b, err := host.ReadFile(filepath.Join(dir, "ports.json")); err == nil {
				var pf struct {
					APIPort int `json:"apiPort"`
					API     int `json:"api"`
					Port    int `json:"port"`
				}
				if json.Unmarshal(b, &pf) == nil {
					if pf.APIPort > 0 {
						info.apiPort = pf.APIPort
					} else if pf.API > 0 {
						info.apiPort = pf.API
					} else if pf.Port > 0 {
						info.apiPort = pf.Port
					}
					if info.apiPort > 0 && info.dataDir == "" {
						info.dataDir = dir
					}
				}
			}
		}
		if !info.hasDataDir {
			if st, err := host.Stat(filepath.Join(dir, "data")); err == nil && st.IsDir() {
				info.hasDataDir = true
				if info.dataDir == "" {
					info.dataDir = dir
				}
			}
		}
	}

	return envFromFile, info
}

func collectOpenshipSecrets(apiEnv, envFromFile map[string]string, bareAuthSecret string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range []string{
		apiEnv["BETTER_AUTH_SECRET"],
		envFromFile["BETTER_AUTH_SECRET"],
		bareAuthSecret,
		"change-me-in-production",
	} {
		s = strings.TrimSpace(s)
		if s != "" && !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

func collectOpenshipInternalTokens(apiEnv, envFromFile map[string]string, bareToken string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range []string{
		apiEnv["INTERNAL_TOKEN"],
		envFromFile["INTERNAL_TOKEN"],
		bareToken,
	} {
		t = strings.TrimSpace(t)
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

func resolveOpenshipAPIPort(apiEnv, envFromFile map[string]string, portsFilePort int) int {
	for _, raw := range []string{apiEnv["PORT"], apiEnv["API_PORT"], envFromFile["API_PORT"]} {
		if n, err := strconv.Atoi(strings.TrimSpace(raw)); err == nil && n > 0 {
			return n
		}
	}
	if portsFilePort > 0 {
		return portsFilePort
	}
	return 4000
}

func parsePostgresURL(raw string) (user, password, db string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", ""
	}
	if u.User != nil {
		user = u.User.Username()
		password, _ = u.User.Password()
	}
	db = strings.TrimPrefix(u.Path, "/")
	return user, password, db
}

func queryOpenshipPostgres(ctx context.Context, host Host, pgContainer, pgUser, pgDB, pgPass string, res *DetectedPlatform) (openshipDump, error) {
	// Use to_jsonb(t) so column additions/removals across Openship versions
	// (e.g. project.volumes, domain.owner_type, project.deleted_at) never fail
	// the query with "column ... does not exist".
	combinedQuery := `SELECT json_build_object(
		'orgs', COALESCE((SELECT json_agg(to_jsonb(o)) FROM organization o), '[]'::json),
		'projects', COALESCE((SELECT json_agg(to_jsonb(p)) FROM project p), '[]'::json),
		'env_vars', COALESCE((SELECT json_agg(to_jsonb(e)) FROM env_var e), '[]'::json),
		'services', COALESCE((SELECT json_agg(to_jsonb(s)) FROM service s), '[]'::json),
		'domains', COALESCE((SELECT json_agg(to_jsonb(d)) FROM domain d), '[]'::json),
		'deployments', COALESCE((SELECT json_agg(to_jsonb(d)) FROM (
			SELECT d.*, ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY created_at DESC) AS _rn
			FROM deployment d
		) d WHERE d._rn <= 10), '[]'::json)
	)::text;`

	rawJSON, err := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass, combinedQuery)
	if err == nil {
		var dump openshipDump
		if decErr := json.Unmarshal([]byte(rawJSON), &dump); decErr != nil {
			return openshipDump{}, fmt.Errorf("decode openship database snapshot: %w", decErr)
		}
		return dump, nil
	}

	// Fallback: query each table individually so a missing optional table
	// (e.g. older schema without service/domain/deployment) does not break discovery.
	var dump openshipDump
	projRaw, projErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(p)), '[]'::json)::text FROM project p;`)
	if projErr != nil {
		return openshipDump{}, err
	}
	if decErr := json.Unmarshal([]byte(projRaw), &dump.Projects); decErr != nil {
		return openshipDump{}, fmt.Errorf("decode openship projects: %w", decErr)
	}

	if raw, qErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(o)), '[]'::json)::text FROM organization o;`); qErr == nil {
		_ = json.Unmarshal([]byte(raw), &dump.Orgs)
	}
	if raw, qErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(e)), '[]'::json)::text FROM env_var e;`); qErr == nil {
		_ = json.Unmarshal([]byte(raw), &dump.EnvVars)
	}
	if raw, qErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(s)), '[]'::json)::text FROM service s;`); qErr == nil {
		_ = json.Unmarshal([]byte(raw), &dump.Services)
	}
	if raw, qErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(d)), '[]'::json)::text FROM domain d;`); qErr == nil {
		_ = json.Unmarshal([]byte(raw), &dump.Domains)
	}
	if raw, qErr := queryPostgresJSONWithPassword(ctx, host, pgContainer, pgUser, pgDB, pgPass,
		`SELECT COALESCE(json_agg(to_jsonb(d)), '[]'::json)::text FROM (SELECT d.*, ROW_NUMBER() OVER (PARTITION BY project_id ORDER BY created_at DESC) AS _rn FROM deployment d) d WHERE d._rn <= 10;`); qErr == nil {
		_ = json.Unmarshal([]byte(raw), &dump.Deployments)
	}
	res.Warnings = append(res.Warnings, "Some Openship database tables were unavailable; imported all available records.")
	return dump, nil
}

func doOpenshipHTTP(host Host, req *http.Request) (*http.Response, error) {
	if hc, ok := host.(openshipHTTPClient); ok {
		return hc.DoHTTP(req)
	}
	client := &http.Client{Timeout: 8 * time.Second}
	return client.Do(req)
}

func queryOpenshipLocalAPI(ctx context.Context, host Host, apiPort int, internalTokens []string) (openshipDump, error) {
	baseURL := fmt.Sprintf("http://127.0.0.1:%d", apiPort)

	var sessionToken string
	var lastErr error
	for _, tok := range internalTokens {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/api/system/cli-session", bytes.NewReader([]byte("{}")))
		if err != nil {
			lastErr = err
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Internal-Token", tok)
		resp, err := doOpenshipHTTP(host, req)
		if err != nil {
			lastErr = err
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			var parsed struct {
				Token string `json:"token"`
			}
			if json.Unmarshal(body, &parsed) == nil && parsed.Token != "" {
				sessionToken = parsed.Token
				break
			}
		}
		lastErr = fmt.Errorf("cli-session returned HTTP %d", resp.StatusCode)
	}
	if sessionToken == "" {
		if lastErr == nil {
			lastErr = fmt.Errorf("no valid internal token")
		}
		return openshipDump{}, lastErr
	}

	defer func() {
		req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, baseURL+"/api/auth/sign-out", bytes.NewReader([]byte("{}")))
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+sessionToken)
			req.Header.Set("Cookie", "better-auth.session_token="+sessionToken)
			if resp, err := doOpenshipHTTP(host, req); err == nil {
				resp.Body.Close()
			}
		}
	}()

	apiGet := func(path string) ([]byte, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+path, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Authorization", "Bearer "+sessionToken)
		req.Header.Set("Cookie", "better-auth.session_token="+sessionToken)
		resp, err := doOpenshipHTTP(host, req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("GET %s returned HTTP %d", path, resp.StatusCode)
		}
		return body, nil
	}

	projBody, err := apiGet("/api/projects?perPage=200")
	if err != nil {
		return openshipDump{}, err
	}

	var projResp struct {
		Data []json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(projBody, &projResp); err != nil {
		return openshipDump{}, fmt.Errorf("decode openship /api/projects: %w", err)
	}

	var dump openshipDump
	for _, raw := range projResp.Data {
		p := decodeOpenshipAPIProject(raw)
		if p.ID == "" {
			continue
		}
		dump.Projects = append(dump.Projects, p)

		// Fetch env vars (revealed plaintext from Openship API when available).
		if envBody, err := apiGet("/api/projects/" + url.PathEscape(p.ID) + "/env?environment=production"); err == nil {
			var envResp struct {
				Data []struct {
					Key         string `json:"key"`
					Value       string `json:"value"`
					ServiceID   string `json:"serviceId"`
					ServiceIDSn string `json:"service_id"`
					Environment string `json:"environment"`
				} `json:"data"`
			}
			if json.Unmarshal(envBody, &envResp) == nil {
				for _, ev := range envResp.Data {
					if ev.Key == "" {
						continue
					}
					dump.EnvVars = append(dump.EnvVars, openshipEnvVarRow{
						ProjectID:   p.ID,
						ServiceID:   firstNonEmpty(ev.ServiceID, ev.ServiceIDSn),
						Key:         ev.Key,
						Value:       ev.Value,
						Environment: firstNonEmpty(ev.Environment, "production"),
						Plaintext:   true,
					})
				}
			}
		}

		// Fetch services.
		if svcBody, err := apiGet("/api/projects/" + url.PathEscape(p.ID) + "/services"); err == nil {
			var svcResp struct {
				Data []json.RawMessage `json:"data"`
			}
			if json.Unmarshal(svcBody, &svcResp) == nil {
				for _, sraw := range svcResp.Data {
					s := decodeOpenshipAPIService(p.ID, sraw)
					if s.ID != "" {
						dump.Services = append(dump.Services, s)
					}
				}
			}
		}
	}

	return dump, nil
}

func decodeOpenshipAPIProject(raw json.RawMessage) openshipProjectRow {
	var p openshipProjectRow
	_ = json.Unmarshal(raw, &p)
	// Openship HTTP API uses camelCase; backfill from camelCase fields if snake_case was empty.
	var camel struct {
		OrganizationID  string          `json:"organizationId"`
		EnvironmentName string          `json:"environmentName"`
		GitProvider     string          `json:"gitProvider"`
		GitOwner        string          `json:"gitOwner"`
		GitRepo         string          `json:"gitRepo"`
		GitBranch       string          `json:"gitBranch"`
		GitURL          string          `json:"gitUrl"`
		LocalPath       string          `json:"localPath"`
		InstallCommand  string          `json:"installCommand"`
		BuildCommand    string          `json:"buildCommand"`
		OutputDirectory string          `json:"outputDirectory"`
		RootDirectory   string          `json:"rootDirectory"`
		StartCommand    string          `json:"startCommand"`
		HasServer       *bool           `json:"hasServer"`
		DeletedAt       string          `json:"deletedAt"`
		Volumes         json.RawMessage `json:"volumes"`
	}
	if json.Unmarshal(raw, &camel) == nil {
		p.OrganizationID = firstNonEmpty(p.OrganizationID, camel.OrganizationID)
		p.EnvironmentName = firstNonEmpty(p.EnvironmentName, camel.EnvironmentName)
		p.GitProvider = firstNonEmpty(p.GitProvider, camel.GitProvider)
		p.GitOwner = firstNonEmpty(p.GitOwner, camel.GitOwner)
		p.GitRepo = firstNonEmpty(p.GitRepo, camel.GitRepo)
		p.GitBranch = firstNonEmpty(p.GitBranch, camel.GitBranch)
		p.GitURL = firstNonEmpty(p.GitURL, camel.GitURL)
		p.LocalPath = firstNonEmpty(p.LocalPath, camel.LocalPath)
		p.InstallCommand = firstNonEmpty(p.InstallCommand, camel.InstallCommand)
		p.BuildCommand = firstNonEmpty(p.BuildCommand, camel.BuildCommand)
		p.OutputDirectory = firstNonEmpty(p.OutputDirectory, camel.OutputDirectory)
		p.RootDirectory = firstNonEmpty(p.RootDirectory, camel.RootDirectory)
		p.StartCommand = firstNonEmpty(p.StartCommand, camel.StartCommand)
		if p.HasServer == nil {
			p.HasServer = camel.HasServer
		}
		p.DeletedAt = firstNonEmpty(p.DeletedAt, camel.DeletedAt)
		if len(p.Volumes) == 0 {
			p.Volumes = camel.Volumes
		}
	}
	return p
}

func decodeOpenshipAPIService(projectID string, raw json.RawMessage) openshipServiceRow {
	var s openshipServiceRow
	_ = json.Unmarshal(raw, &s)
	var camel struct {
		ProjectID    string `json:"projectId"`
		CustomDomain string `json:"customDomain"`
		ArchivedAt   string `json:"archivedAt"`
	}
	if json.Unmarshal(raw, &camel) == nil {
		s.ProjectID = firstNonEmpty(s.ProjectID, camel.ProjectID, projectID)
		s.CustomDomain = firstNonEmpty(s.CustomDomain, camel.CustomDomain)
		s.ArchivedAt = firstNonEmpty(s.ArchivedAt, camel.ArchivedAt)
	}
	return s
}

func populateOpenshipDiscovery(ctx context.Context, host Host, containers []dockerContainerInfo, dump openshipDump, secrets []string, res *DetectedPlatform) {
	orgNames := map[string]string{}
	for _, o := range dump.Orgs {
		orgNames[o.ID] = o.Name
	}

	projectEnv := map[string]map[string]string{}
	serviceEnv := map[string]map[string]string{}
	envDecryptWarnings := map[string]int{}
	for _, ev := range dump.EnvVars {
		if ev.Key == "" {
			continue
		}
		val := ev.Value
		if !ev.Plaintext && ev.Value != "" {
			if dec, err := DecryptOpenship(ev.Value, secrets); err == nil {
				val = dec
			} else {
				envDecryptWarnings[ev.ProjectID]++
			}
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
	seenDomain := map[string]bool{}
	addDomain := func(projectID string, d DiscoveredDomain) {
		if projectID == "" || d.Host == "" {
			return
		}
		key := projectID + "|" + strings.ToLower(d.Host) + "|" + d.PathPrefix
		if seenDomain[key] {
			return
		}
		seenDomain[key] = true
		domainsByProject[projectID] = append(domainsByProject[projectID], d)
	}
	for _, d := range dump.Domains {
		if d.Hostname == "" || d.ProjectID == "" {
			continue
		}
		if d.OwnerType != "" && !strings.EqualFold(d.OwnerType, "project") {
			continue
		}
		prefix := d.TargetPath
		if prefix == "" {
			prefix = "/"
		}
		addDomain(d.ProjectID, DiscoveredDomain{
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
		if dep.ErrorMessage != "" {
			content += "\nError: " + dep.ErrorMessage
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
	var activeProjects []openshipProjectRow
	for _, p := range dump.Projects {
		if strings.TrimSpace(p.DeletedAt) != "" {
			continue
		}
		activeProjects = append(activeProjects, p)
		projectNames[p.ID] = p.Name
		projectOrgs[p.ID] = orgNames[p.OrganizationID]
	}

	for _, svc := range dump.Services {
		if strings.TrimSpace(svc.ArchivedAt) != "" {
			continue
		}
		// Capture any service-level custom domain onto the parent project.
		for _, hostName := range []string{svc.CustomDomain, svc.Domain} {
			hostName = strings.TrimSpace(hostName)
			if hostName != "" && svc.ProjectID != "" {
				addDomain(svc.ProjectID, DiscoveredDomain{
					Host:       hostName,
					PathPrefix: "/",
					HTTPS:      true,
				})
			}
		}

		engine := NormalizeEngine("", svc.Image)
		if engine == "" {
			continue
		}
		envMap := parseFlexibleStringMap(svc.Environment)
		for k, v := range serviceEnv[svc.ID] {
			envMap[k] = v
		}
		cfg := extractServiceConfig(engine, envMap)
		vols := parseFlexibleVolumes(svc.Volumes)
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

	for _, p := range activeProjects {
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

		vols := parseFlexibleVolumes(p.Volumes)
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
}

func parseFlexibleStringMap(raw json.RawMessage) map[string]string {
	out := map[string]string{}
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return out
	}
	var generic map[string]any
	if err := json.Unmarshal(trimmed, &generic); err != nil {
		return out
	}
	for k, v := range generic {
		if v == nil {
			continue
		}
		switch tv := v.(type) {
		case string:
			out[k] = tv
		case float64:
			out[k] = strconv.FormatFloat(tv, 'f', -1, 64)
		case bool:
			out[k] = strconv.FormatBool(tv)
		default:
			if b, err := json.Marshal(tv); err == nil {
				out[k] = string(b)
			}
		}
	}
	return out
}

func parseFlexibleVolumes(raw json.RawMessage) []DiscoveredVolume {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")) {
		return nil
	}
	var strs []string
	if err := json.Unmarshal(trimmed, &strs); err == nil {
		return parseVolumeStrings(strs)
	}
	var items []json.RawMessage
	if err := json.Unmarshal(trimmed, &items); err != nil {
		return nil
	}
	var out []DiscoveredVolume
	for _, item := range items {
		var s string
		if json.Unmarshal(item, &s) == nil {
			out = append(out, parseVolumeStrings([]string{s})...)
			continue
		}
		var obj struct {
			Source string `json:"source"`
			Target string `json:"target"`
			Name   string `json:"name"`
			Path   string `json:"path"`
			Type   string `json:"type"`
		}
		if json.Unmarshal(item, &obj) == nil {
			src := firstNonEmpty(obj.Source, obj.Name)
			dst := firstNonEmpty(obj.Target, obj.Path)
			if src == "" || dst == "" || !strings.HasPrefix(dst, "/") {
				continue
			}
			srcType := obj.Type
			if srcType == "" {
				if strings.HasPrefix(src, "/") || strings.HasPrefix(src, ".") {
					srcType = "bind"
				} else {
					srcType = "volume"
				}
			}
			out = append(out, DiscoveredVolume{
				Name:       filepath.Base(dst),
				SourceType: srcType,
				SourceName: src,
				MountPath:  dst,
			})
		}
	}
	return out
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
			if strings.Contains(strings.ToLower(c.Names), id) ||
				strings.Contains(strings.ToLower(c.Labels), id) {
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
