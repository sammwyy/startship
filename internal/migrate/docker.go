package migrate

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type dockerContainerInfo struct {
	ID     string `json:"ID"`
	Names  string `json:"Names"`
	Image  string `json:"Image"`
	State  string `json:"State"`
	Status string `json:"Status"`
	Labels string `json:"Labels"`
}

type dockerMountInfo struct {
	Type        string `json:"Type"`
	Name        string `json:"Name"`
	Source      string `json:"Source"`
	Destination string `json:"Destination"`
}

func listDockerContainers(ctx context.Context, host Host) ([]dockerContainerInfo, error) {
	out, err := host.RunDocker(ctx, "ps", "-a", "--format", "{{json .}}")
	if err != nil {
		return nil, fmt.Errorf("docker ps: %w", err)
	}
	var list []dockerContainerInfo
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var info dockerContainerInfo
		if json.Unmarshal([]byte(line), &info) == nil && info.Names != "" {
			list = append(list, info)
		}
	}
	return list, nil
}

func parseLabels(raw string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(raw, ",") {
		if k, v, ok := strings.Cut(part, "="); ok {
			out[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return out
}

func inspectContainerEnv(ctx context.Context, host Host, container string) map[string]string {
	out, err := host.RunDocker(ctx, "inspect", "--format", "{{json .Config.Env}}", container)
	if err != nil {
		return map[string]string{}
	}
	var entries []string
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &entries) != nil {
		return map[string]string{}
	}
	res := map[string]string{}
	for _, entry := range entries {
		if k, v, ok := strings.Cut(entry, "="); ok {
			res[k] = v
		}
	}
	return res
}

func inspectContainerMounts(ctx context.Context, host Host, container string) []DiscoveredVolume {
	out, err := host.RunDocker(ctx, "inspect", "--format", "{{json .Mounts}}", container)
	if err != nil {
		return nil
	}
	var mounts []dockerMountInfo
	if json.Unmarshal([]byte(strings.TrimSpace(out)), &mounts) != nil {
		return nil
	}
	var vols []DiscoveredVolume
	for _, m := range mounts {
		if m.Destination == "" || m.Destination == "/var/run/docker.sock" {
			continue
		}
		switch m.Type {
		case "volume":
			if m.Name != "" {
				vols = append(vols, DiscoveredVolume{
					Name:       m.Name,
					SourceType: "volume",
					SourceName: m.Name,
					MountPath:  m.Destination,
				})
			}
		case "bind":
			if m.Source != "" {
				vols = append(vols, DiscoveredVolume{
					Name:       m.Destination,
					SourceType: "bind",
					SourceName: m.Source,
					MountPath:  m.Destination,
				})
			}
		}
	}
	return vols
}

func captureContainerLogs(ctx context.Context, host Host, container string, tail int) string {
	if container == "" {
		return ""
	}
	if tail <= 0 {
		tail = 200
	}
	out, err := host.RunDocker(ctx, "logs", "--tail", fmt.Sprintf("%d", tail), container)
	if err != nil {
		return ""
	}
	const maxLogBytes = 64 * 1024
	if len(out) > maxLogBytes {
		out = out[len(out)-maxLogBytes:]
	}
	return strings.TrimSpace(out)
}

func queryPostgresJSON(ctx context.Context, host Host, container, user, db, query string) (string, error) {
	if user == "" {
		user = "postgres"
	}
	if db == "" {
		db = "postgres"
	}
	out, err := host.RunDocker(ctx, "exec", "-i", container, "psql", "-U", user, "-d", db, "-t", "-A", "-v", "ON_ERROR_STOP=1", "-c", query)
	if err != nil {
		return "", fmt.Errorf("psql query on %s: %w (%s)", container, err, strings.TrimSpace(out))
	}
	return strings.TrimSpace(out), nil
}

// CopyVolumeData safely copies data from a source Docker volume or host bind path
// into a newly created Startship Docker volume without modifying the source.
func CopyVolumeData(ctx context.Context, host Host, srcType, srcName, dstVolume string) error {
	if srcName == "" || dstVolume == "" {
		return fmt.Errorf("source and destination volume are required")
	}
	if _, err := host.RunDocker(ctx, "volume", "create", dstVolume); err != nil {
		return fmt.Errorf("create volume %s: %w", dstVolume, err)
	}
	mountSrc := srcName + ":/from:ro"
	mountDst := dstVolume + ":/to"
	images := []string{"alpine:3.23", "alpine:latest", "busybox:latest"}
	var lastErr error
	for _, img := range images {
		out, err := host.RunDocker(ctx, "run", "--rm",
			"-v", mountSrc,
			"-v", mountDst,
			img, "sh", "-c", "cp -a /from/. /to/ 2>/dev/null || true",
		)
		if err == nil {
			return nil
		}
		lastErr = fmt.Errorf("copy with %s: %w (%s)", img, err, strings.TrimSpace(out))
	}
	return lastErr
}

func sortedMapKeys(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func dedupVolumes(vols []DiscoveredVolume) []DiscoveredVolume {
	seen := map[string]bool{}
	var out []DiscoveredVolume
	for _, v := range vols {
		if v.MountPath == "" || v.SourceName == "" {
			continue
		}
		key := v.SourceType + "|" + v.SourceName + "|" + v.MountPath
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, v)
	}
	return out
}

func parseTimeFlexible(raw string) (t time.Time) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return t
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999-07",
		"2006-01-02 15:04:05.999999+00",
		"2006-01-02 15:04:05-07",
		"2006-01-02 15:04:05+00",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05.999Z",
	}
	for _, layout := range layouts {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return t
}
