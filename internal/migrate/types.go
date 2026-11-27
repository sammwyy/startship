package migrate

import (
	"context"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/sammwyy/startship/internal/pipeline"
)

type Platform string

const (
	PlatformOpenship Platform = "openship"
	PlatformDokploy  Platform = "dokploy"
	PlatformCoolify  Platform = "coolify"
)

var AllPlatforms = []Platform{PlatformOpenship, PlatformDokploy, PlatformCoolify}

func ParsePlatform(raw string) (Platform, bool) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case string(PlatformOpenship):
		return PlatformOpenship, true
	case string(PlatformDokploy):
		return PlatformDokploy, true
	case string(PlatformCoolify):
		return PlatformCoolify, true
	default:
		return "", false
	}
}

func PlatformLabel(p Platform) string {
	switch p {
	case PlatformOpenship:
		return "Openship"
	case PlatformDokploy:
		return "Dokploy"
	case PlatformCoolify:
		return "Coolify"
	default:
		return string(p)
	}
}

type DetectedPlatform struct {
	Platform  Platform            `json:"platform"`
	Name      string              `json:"name"`
	Detected  bool                `json:"detected"`
	Status    string              `json:"status"` // "detected", "not_found", "error"
	Summary   string              `json:"summary"`
	Container string              `json:"container,omitempty"`
	DataDir   string              `json:"data_dir,omitempty"`
	Error     string              `json:"error,omitempty"`
	Projects  []DiscoveredProject `json:"projects"`
	Services  []DiscoveredService `json:"services"`
	Warnings  []string            `json:"warnings,omitempty"`
}

type DiscoveredDomain struct {
	Host        string `json:"host"`
	PathPrefix  string `json:"path_prefix,omitempty"`
	StripPrefix bool   `json:"strip_prefix,omitempty"`
	HTTPS       bool   `json:"https,omitempty"`
	Port        int    `json:"port,omitempty"`
}

type DiscoveredVolume struct {
	Name       string `json:"name"`
	SourceType string `json:"source_type"` // "volume" or "bind"
	SourceName string `json:"source_name"` // Docker volume name or host bind path
	MountPath  string `json:"mount_path"`
}

type DiscoveredLog struct {
	Title      string    `json:"title,omitempty"`
	Status     string    `json:"status,omitempty"`
	Commit     string    `json:"commit,omitempty"`
	CreatedAt  time.Time `json:"created_at,omitzero"`
	FinishedAt time.Time `json:"finished_at,omitzero"`
	Content    string    `json:"content,omitempty"`
}

type DiscoveredProject struct {
	ID             string             `json:"id"`
	Platform       Platform           `json:"platform"`
	SourceOrg      string             `json:"source_org,omitempty"`
	SourceProject  string             `json:"source_project,omitempty"`
	Environment    string             `json:"environment,omitempty"`
	Name           string             `json:"name"`
	Description    string             `json:"description,omitempty"`
	SourceKind     string             `json:"source_kind"` // "git", "docker", "compose", "static", "local"
	GitURL         string             `json:"git_url,omitempty"`
	GitToken       string             `json:"git_token,omitempty"`
	Branch         string             `json:"branch,omitempty"`
	RootDir        string             `json:"root_dir,omitempty"`
	BuildType      string             `json:"build_type,omitempty"`
	Dockerfile     string             `json:"dockerfile,omitempty"`
	DockerImage    string             `json:"docker_image,omitempty"`
	InstallCommand string             `json:"install_command,omitempty"`
	BuildCommand   string             `json:"build_command,omitempty"`
	StartCommand   string             `json:"start_command,omitempty"`
	OutputDir      string             `json:"output_dir,omitempty"`
	Port           int                `json:"port,omitempty"`
	HealthPath     string             `json:"health_path,omitempty"`
	AutoDeploy     bool               `json:"auto_deploy,omitempty"`
	Env            map[string]string  `json:"env,omitempty"`
	EnvKeys        []string           `json:"env_keys,omitempty"`
	EnvCount       int                `json:"env_count"`
	Domains        []DiscoveredDomain `json:"domains,omitempty"`
	Volumes        []DiscoveredVolume `json:"volumes,omitempty"`
	Logs           []DiscoveredLog    `json:"logs,omitempty"`
	LogCount       int                `json:"log_count"`
	ContainerName  string             `json:"container_name,omitempty"`
	LinkedServices []string           `json:"linked_services,omitempty"`
	Compatible     bool               `json:"compatible"`
	Warnings       []string           `json:"warnings,omitempty"`
}

type DiscoveredService struct {
	ID            string             `json:"id"`
	Platform      Platform           `json:"platform"`
	SourceOrg     string             `json:"source_org,omitempty"`
	SourceProject string             `json:"source_project,omitempty"`
	Environment   string             `json:"environment,omitempty"`
	Name          string             `json:"name"`
	Engine        string             `json:"engine,omitempty"`     // Startship catalog engine id
	RawEngine     string             `json:"raw_engine,omitempty"` // Original engine/image
	Image         string             `json:"image,omitempty"`
	Version       string             `json:"version,omitempty"`
	ExternalPort  int                `json:"external_port,omitempty"`
	Config        map[string]string  `json:"config,omitempty"`
	ConfigKeys    []string           `json:"config_keys,omitempty"`
	Volumes       []DiscoveredVolume `json:"volumes,omitempty"`
	ContainerName string             `json:"container_name,omitempty"`
	Compatible    bool               `json:"compatible"`
	Warnings      []string           `json:"warnings,omitempty"`
}

type ImportRequest struct {
	Platform            Platform           `json:"platform"`
	Mode                string             `json:"mode"` // "all" or "cherry_pick"
	TargetWorkspaceID   string             `json:"target_workspace_id,omitempty"`
	CreateWorkspaceName string             `json:"create_workspace_name,omitempty"`
	CopyVolumes         *bool              `json:"copy_volumes,omitempty"`
	ImportDomains       *bool              `json:"import_domains,omitempty"`
	ImportEnv           *bool              `json:"import_env,omitempty"`
	ImportLogs          *bool              `json:"import_logs,omitempty"`
	StartServices       *bool              `json:"start_services,omitempty"`
	DeployProjects      *bool              `json:"deploy_projects,omitempty"`
	Projects            []ProjectSelection `json:"projects,omitempty"`
	Services            []ServiceSelection `json:"services,omitempty"`
}

type ProjectSelection struct {
	SourceID          string `json:"source_id"`
	TargetWorkspaceID string `json:"target_workspace_id,omitempty"`
	TargetName        string `json:"target_name,omitempty"`
	CopyVolumes       *bool  `json:"copy_volumes,omitempty"`
	ImportDomains     *bool  `json:"import_domains,omitempty"`
	ImportEnv         *bool  `json:"import_env,omitempty"`
	ImportLogs        *bool  `json:"import_logs,omitempty"`
	Deploy            *bool  `json:"deploy,omitempty"`
}

type ServiceSelection struct {
	SourceID          string   `json:"source_id"`
	TargetWorkspaceID string   `json:"target_workspace_id,omitempty"`
	TargetName        string   `json:"target_name,omitempty"`
	CopyVolumes       *bool    `json:"copy_volumes,omitempty"`
	Start             *bool    `json:"start,omitempty"`
	AttachProjects    []string `json:"attach_projects,omitempty"`
}

type MigrationJob struct {
	ID                  string                  `json:"id"`
	Platform            Platform                `json:"platform"`
	Mode                string                  `json:"mode"`
	Status              string                  `json:"status"` // "running", "completed", "completed_with_warnings", "failed"
	TargetWorkspaceID   string                  `json:"target_workspace_id,omitempty"`
	TargetWorkspaceName string                  `json:"target_workspace_name,omitempty"`
	CreatedBy           string                  `json:"created_by,omitempty"`
	StartedAt           time.Time               `json:"started_at"`
	FinishedAt          *time.Time              `json:"finished_at,omitempty"`
	Projects            []MigratedProjectResult `json:"projects"`
	Services            []MigratedServiceResult `json:"services"`
	Logs                []string                `json:"logs"`
	Warnings            []string                `json:"warnings,omitempty"`
	Error               string                  `json:"error,omitempty"`
}

type MigratedProjectResult struct {
	SourceID        string   `json:"source_id"`
	SourceName      string   `json:"source_name"`
	ProjectID       string   `json:"project_id,omitempty"`
	ProjectName     string   `json:"project_name,omitempty"`
	WorkspaceID     string   `json:"workspace_id,omitempty"`
	VolumesCopied   int      `json:"volumes_copied"`
	DomainsImported int      `json:"domains_imported"`
	EnvCount        int      `json:"env_count"`
	LogsImported    int      `json:"logs_imported"`
	DeploymentID    string   `json:"deployment_id,omitempty"`
	Status          string   `json:"status"` // "ok", "warning", "failed", "skipped"
	Warnings        []string `json:"warnings,omitempty"`
	Error           string   `json:"error,omitempty"`
}

type MigratedServiceResult struct {
	SourceID         string   `json:"source_id"`
	SourceName       string   `json:"source_name"`
	ServiceID        string   `json:"service_id,omitempty"`
	ServiceName      string   `json:"service_name,omitempty"`
	WorkspaceID      string   `json:"workspace_id,omitempty"`
	Engine           string   `json:"engine,omitempty"`
	VolumesCopied    int      `json:"volumes_copied"`
	AttachedProjects int      `json:"attached_projects"`
	Status           string   `json:"status"` // "ok", "warning", "failed", "skipped"
	Warnings         []string `json:"warnings,omitempty"`
	Error            string   `json:"error,omitempty"`
}

// Host abstracts Docker CLI and host filesystem access for testability.
type Host interface {
	RunDocker(ctx context.Context, args ...string) (string, error)
	ReadFile(path string) ([]byte, error)
	Stat(path string) (fs.FileInfo, error)
	HomeDir() string
}

type OSHost struct{}

func (OSHost) RunDocker(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (OSHost) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func (OSHost) Stat(path string) (fs.FileInfo, error) {
	return os.Stat(path)
}

func (OSHost) HomeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "/root"
	}
	return h
}

var nonServiceChars = regexp.MustCompile(`[^a-z0-9_-]+`)

// SanitizeServiceName converts an arbitrary service name into a valid Startship
// service identifier matching ^[a-z][a-z0-9_-]{0,62}$.
func SanitizeServiceName(raw string) string {
	s := strings.ToLower(strings.TrimSpace(raw))
	s = strings.ReplaceAll(s, " ", "-")
	s = nonServiceChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if s == "" {
		return "service"
	}
	if s[0] < 'a' || s[0] > 'z' {
		s = "svc-" + s
	}
	if len(s) > 63 {
		s = s[:63]
	}
	return s
}

// NormalizeEngine maps a competitor database/service type or Docker image name
// to a Startship catalog engine ID.
func NormalizeEngine(rawType, image string) string {
	candidates := []string{
		strings.ToLower(strings.TrimSpace(rawType)),
		strings.ToLower(strings.TrimSpace(image)),
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		// Strip registry prefix and tag.
		base := c
		if idx := strings.LastIndex(base, "/"); idx >= 0 {
			base = base[idx+1:]
		}
		if idx := strings.Index(base, ":"); idx >= 0 {
			base = base[:idx]
		}
		switch {
		case base == "postgres" || base == "postgresql" || strings.HasPrefix(base, "postgres") || strings.Contains(c, "pgvector") || strings.Contains(c, "postgis") || strings.Contains(c, "timescaledb"):
			return "postgresql"
		case base == "mysql" || strings.HasPrefix(base, "mysql"):
			return "mysql"
		case base == "mariadb" || strings.HasPrefix(base, "mariadb"):
			return "mariadb"
		case base == "mongo" || base == "mongodb" || strings.HasPrefix(base, "mongo"):
			return "mongodb"
		case base == "redis" || strings.HasPrefix(base, "redis") || base == "keydb" || base == "dragonfly":
			return "redis"
		case base == "valkey" || strings.HasPrefix(base, "valkey"):
			return "valkey"
		case base == "memcached" || strings.HasPrefix(base, "memcached"):
			return "memcached"
		case base == "rabbitmq" || strings.HasPrefix(base, "rabbitmq"):
			return "rabbitmq"
		case base == "kafka" || strings.Contains(c, "kafka"):
			return "kafka"
		case base == "nats" || strings.HasPrefix(base, "nats"):
			return "nats"
		case base == "clickhouse" || strings.Contains(c, "clickhouse"):
			return "clickhouse"
		case base == "seaweedfs" || strings.Contains(c, "seaweedfs"):
			return "seaweedfs"
		case base == "opensearch" || strings.Contains(c, "opensearch"):
			return "opensearch"
		}
	}
	return ""
}

// ParseImageTag extracts the version tag from a Docker image reference (e.g. "postgres:16-alpine" -> "16").
func ParseImageTag(image string) string {
	image = strings.TrimSpace(image)
	if image == "" {
		return ""
	}
	if at := strings.Index(image, "@"); at >= 0 {
		image = image[:at]
	}
	lastSlash := strings.LastIndex(image, "/")
	colon := strings.LastIndex(image, ":")
	if colon <= lastSlash {
		return ""
	}
	tag := image[colon+1:]
	if dash := strings.Index(tag, "-"); dash > 0 {
		tag = tag[:dash]
	}
	return tag
}

// BuildPipelineFromDiscovered maps a discovered project's build/deploy config
// into Startship's BuildType and *pipeline.Pipeline.
func BuildPipelineFromDiscovered(p DiscoveredProject) (string, *pipeline.Pipeline) {
	bt := strings.ToLower(strings.TrimSpace(p.BuildType))
	port := p.Port
	if port <= 0 {
		port = 3000
	}
	switch bt {
	case "dockerfile", "docker":
		df := strings.TrimSpace(p.Dockerfile)
		if df == "" {
			df = "Dockerfile"
		}
		return "dockerfile", &pipeline.Pipeline{
			Build: pipeline.Build{Dockerfile: df},
			Deploy: pipeline.Deploy{
				Port:       port,
				HealthPath: p.HealthPath,
			},
		}
	case "nixpacks", "railpack":
		return "auto", &pipeline.Pipeline{
			Build: pipeline.Build{Engine: "nixpacks"},
			Deploy: pipeline.Deploy{
				Command:    p.StartCommand,
				Port:       port,
				HealthPath: p.HealthPath,
			},
		}
	case "heroku_buildpacks", "paketo_buildpacks", "buildpacks":
		return "auto", &pipeline.Pipeline{
			Build: pipeline.Build{Engine: "buildpacks"},
			Deploy: pipeline.Deploy{
				Command:    p.StartCommand,
				Port:       port,
				HealthPath: p.HealthPath,
			},
		}
	case "static":
		var cmds []string
		if p.InstallCommand != "" {
			cmds = append(cmds, p.InstallCommand)
		}
		if p.BuildCommand != "" {
			cmds = append(cmds, p.BuildCommand)
		}
		artifact := strings.TrimPrefix(strings.TrimSpace(p.OutputDir), "/")
		return "static", &pipeline.Pipeline{
			Build: pipeline.Build{
				Image:    "node:22-alpine",
				Commands: cmds,
			},
			Deploy: pipeline.Deploy{
				Static:   true,
				Artifact: artifact,
			},
		}
	default:
		// If custom commands or a known framework were configured, preserve them.
		var cmds []string
		if p.InstallCommand != "" {
			cmds = append(cmds, p.InstallCommand)
		}
		if p.BuildCommand != "" {
			cmds = append(cmds, p.BuildCommand)
		}
		if len(cmds) > 0 || p.StartCommand != "" || p.HealthPath != "" {
			return "auto", &pipeline.Pipeline{
				Build: pipeline.Build{Commands: cmds},
				Deploy: pipeline.Deploy{
					Command:    p.StartCommand,
					Port:       port,
					HealthPath: p.HealthPath,
				},
			}
		}
		return "auto", nil
	}
}

func cleanRootDir(raw string) string {
	s := strings.TrimSpace(raw)
	if s == "" || s == "/" || s == "." {
		return "."
	}
	s = strings.TrimPrefix(s, "/")
	s = filepath.Clean(s)
	if s == "" || s == "." || strings.HasPrefix(s, "..") {
		return "."
	}
	return s
}

func BoolDefault(b *bool, fallback bool) bool {
	if b == nil {
		return fallback
	}
	return *b
}
