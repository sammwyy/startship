package server

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
)

func limitEnv(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

var aliasSeparators = regexp.MustCompile(`[^a-z0-9]+`)

func appAlias(name string) string {
	alias := strings.Trim(aliasSeparators.ReplaceAllString(strings.ToLower(name), "-"), "-")
	if len(alias) > 63 {
		alias = strings.TrimRight(alias[:63], "-")
	}
	if alias == "" {
		return "app"
	}
	return alias
}

func appRunArgs(app Record, network string, test bool) []string {
	args := []string{"run", "--network", network, "--network-alias", appAlias(app.Name),
		"--memory", limitEnv("STARTSHIP_APP_MEMORY", "512m"),
		"--cpus", limitEnv("STARTSHIP_APP_CPUS", "1"),
		"--pids-limit", limitEnv("STARTSHIP_APP_PIDS", "512"),
		"--cap-drop", "ALL", "--cap-add", "CHOWN", "--cap-add", "SETUID",
		"--cap-add", "SETGID", "--cap-add", "NET_BIND_SERVICE",
		"--security-opt", "no-new-privileges", "-e", fmt.Sprintf("PORT=%d", app.Port)}
	if test {
		args = append(args, "--rm", "--label", "startship.test="+app.ID)
	} else {
		args = append(args, "-d", "--restart", "unless-stopped", "--label", "startship.managed=true", "--label", "startship.app="+app.ID)
	}
	for k, v := range app.Env {
		args = append(args, "-e", k+"="+v)
	}
	return args
}

func selfContainer() string {
	if self := os.Getenv("STARTSHIP_SELF_CONTAINER"); self != "" {
		return self
	}
	if _, err := os.Stat("/.dockerenv"); err == nil {
		self, _ := os.Hostname()
		return self
	}
	return ""
}

func connectSelf(network string) error {
	if self := selfContainer(); self != "" {
		if _, err := containerIP(self, network); err != nil {
			return command("", os.Stderr, "docker", "network", "connect", network, self)
		}
	}
	return nil
}

func ensureNetwork(network string, out io.Writer) error {
	if exec.Command("docker", "network", "inspect", network).Run() != nil {
		if err := command("", out, "docker", "network", "create", "--driver", "bridge", network); err != nil {
			if exec.Command("docker", "network", "inspect", network).Run() != nil {
				return err
			}
		}
	}
	return connectSelf(network)
}

var projectBuildLocks sync.Map

func projectBuildLock(projectID string) *sync.Mutex {
	lock, _ := projectBuildLocks.LoadOrStore(projectID, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

func buildConfig(projectID string) (string, []string, error) {
	cpus, err := strconv.ParseFloat(limitEnv("STARTSHIP_BUILD_CPUS", "1"), 64)
	if err != nil || math.IsNaN(cpus) || cpus < 0.01 || cpus > 1024 {
		return "", nil, fmt.Errorf("STARTSHIP_BUILD_CPUS must be between 0.01 and 1024")
	}
	opts := []string{"--driver-opt", "memory=" + limitEnv("STARTSHIP_BUILD_MEMORY", "1g"),
		"--driver-opt", "cpu-period=100000", "--driver-opt", fmt.Sprintf("cpu-quota=%d", int64(cpus*100000))}
	hash := sha256.Sum256([]byte(strings.Join(opts, "\x00")))
	return fmt.Sprintf("startship-build-%s-%x", projectID, hash[:6]), opts, nil
}

func removeProjectBuilders(projectID, keep string, out io.Writer) {
	names := map[string]bool{}
	data, err := exec.Command("docker", "buildx", "ls", "--format", "json").Output()
	if err == nil {
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		for {
			var builder struct{ Name string }
			if decoder.Decode(&builder) != nil {
				break
			}
			names[builder.Name] = true
		}
	}
	// Docker workers survive replacement of the daemon's CLI configuration.
	workers, _ := exec.Command("docker", "ps", "-a", "--filter", "name=buildx_buildkit_startship-build-"+projectID+"-", "--format", "{{.Names}}").Output()
	for _, worker := range strings.Fields(string(workers)) {
		names[strings.TrimSuffix(strings.TrimPrefix(worker, "buildx_buildkit_"), "0")] = true
	}
	for name := range names {
		if !strings.HasPrefix(name, "startship-build-"+projectID+"-") || name == keep {
			continue
		}
		command("", out, "docker", "buildx", "rm", "--force", name)
		worker := "buildx_buildkit_" + name + "0"
		if exec.Command("docker", "container", "inspect", worker).Run() == nil {
			command("", out, "docker", "rm", "-f", worker)
			command("", out, "docker", "volume", "rm", worker+"_state")
		}
	}
}

func buildArgs(builder, image, dockerfile string, noCache bool) []string {
	args := []string{"buildx", "build", "--builder", builder, "--load", "--progress", "plain", "-t", image, "-f", dockerfile}
	if noCache {
		args = append(args, "--no-cache")
	}
	return append(args, ".")
}

func buildImage(root string, out io.Writer, projectID, image, dockerfile string, noCache bool) error {
	builder, opts, err := buildConfig(projectID)
	if err != nil {
		return err
	}
	lock := projectBuildLock(projectID)
	lock.Lock()
	defer lock.Unlock()
	removeProjectBuilders(projectID, builder, out)
	if exec.Command("docker", "buildx", "inspect", builder).Run() != nil {
		args := append([]string{"buildx", "create", "--name", builder, "--driver", "docker-container"}, opts...)
		if err := command("", out, "docker", args...); err != nil {
			if exec.Command("docker", "buildx", "inspect", builder).Run() != nil {
				return err
			}
		}
	}
	defer command("", out, "docker", "buildx", "prune", "--builder", builder, "--max-used-space", limitEnv("STARTSHIP_BUILD_CACHE", "5g"), "--reserved-space", "0", "-f")
	return command(root, out, "docker", buildArgs(builder, image, dockerfile, noCache)...)
}
