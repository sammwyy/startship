package main

import (
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
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

func buildImage(root string, out io.Writer, image, dockerfile string) error {
	cpus, err := strconv.ParseFloat(limitEnv("STARTSHIP_BUILD_CPUS", "1"), 64)
	if err != nil || math.IsNaN(cpus) || cpus < 0.01 || cpus > 1024 {
		return fmt.Errorf("STARTSHIP_BUILD_CPUS must be between 0.01 and 1024")
	}
	builder := "startship-build-" + randomID()
	err = command("", out, "docker", "buildx", "create", "--name", builder, "--driver", "docker-container",
		"--driver-opt", "memory="+limitEnv("STARTSHIP_BUILD_MEMORY", "1g"),
		"--driver-opt", "cpu-period=100000", "--driver-opt", fmt.Sprintf("cpu-quota=%d", int64(cpus*100000)))
	if err != nil {
		return err
	}
	defer command("", out, "docker", "buildx", "rm", "--force", builder)
	return command(root, out, "docker", "buildx", "build", "--builder", builder, "--load", "--progress", "plain", "-t", image, "-f", dockerfile, ".")
}
