package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/tarball"
	"github.com/sammwyy/startship/internal/pipeline"
)

const nixpacksVersion = "1.41.0"
const cnbBuilder = "mirror.gcr.io/paketobuildpacks/builder-jammy-base@sha256:5cd813b834965d9b9daf4d94b35d22bd76f743961dd5cfa7f141804bc4cef815"
const cnbRunImage = "mirror.gcr.io/paketobuildpacks/run-jammy-base@sha256:1c2aa9aac21b5e77230c4e1721ffe1c764c62c4c6f6948e2668a7e28bb73add4"
const cnbRunRef = "mirror.gcr.io/paketobuildpacks/run-jammy-base:latest"
const cnbRunLayout = "out/mirror.gcr.io/paketobuildpacks/run-jammy-base/latest"
const cnbAppLayout = "out/index.docker.io/library/startship-cnb/latest"

func externalEngine(p *pipeline.Pipeline) bool {
	return p != nil && (p.Build.Engine == "nixpacks" || p.Build.Engine == "buildpacks")
}

func engineAppDir(p *pipeline.Pipeline) string {
	if p.Build.Engine == "buildpacks" {
		return "/workspace"
	}
	return "/app"
}

func buildEngine(root string, out io.Writer, projectID, image string, p *pipeline.Pipeline, noCache bool) error {
	stage, err := os.MkdirTemp(filepath.Dir(root), ".startship-engine-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if p.Build.Engine == "nixpacks" {
		return buildNixpacks(root, stage, out, projectID, image, noCache)
	}
	if runtime.GOOS != "linux" || runtime.GOARCH != "amd64" {
		return fmt.Errorf("the pinned Buildpacks builder supports linux/amd64 only")
	}
	if err = copyBuildContext(root, filepath.Join(stage, "app")); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Fprintln(out, "Staging pinned Buildpacks run image")
	ref, err := name.ParseReference(cnbRunImage)
	if err != nil {
		return err
	}
	run, err := remote.Image(ref, remote.WithContext(ctx), remote.WithPlatform(v1.Platform{OS: "linux", Architecture: "amd64"}), remote.WithAuthFromKeychain(authn.DefaultKeychain))
	if err != nil {
		return fmt.Errorf("fetch Buildpacks run image: %w", err)
	}
	if err = writeImageLayout(filepath.Join(stage, cnbRunLayout), run); err != nil {
		return err
	}
	content := cnbDockerfile(projectID, noCache)
	if err = os.WriteFile(filepath.Join(stage, "Dockerfile"), []byte(content), 0600); err != nil {
		return err
	}
	dest := filepath.Join(stage, "result")
	if err = buildImage(stage, out, projectID, image, imageBuild{Dockerfile: "Dockerfile", NoCache: noCache, Output: "type=local,dest=" + dest}); err != nil {
		return err
	}
	if err = copyBuildContext(filepath.Join(stage, cnbRunLayout, "blobs"), filepath.Join(dest, "blobs")); err != nil {
		return err
	}
	appLayout, err := layout.FromPath(dest)
	if err != nil {
		return err
	}
	index, err := appLayout.ImageIndex()
	if err != nil {
		return err
	}
	manifest, err := index.IndexManifest()
	if err != nil {
		return err
	}
	if len(manifest.Manifests) != 1 {
		return fmt.Errorf("expected one Buildpacks app image")
	}
	appImage, err := index.Image(manifest.Manifests[0].Digest)
	if err != nil {
		return err
	}
	tag, err := name.NewTag(image)
	if err != nil {
		return err
	}
	archive := filepath.Join(stage, "app.tar")
	if err = tarball.WriteToFile(archive, tag, appImage); err != nil {
		return err
	}
	return command("", out, "docker", "load", "-i", archive)
}

func writeImageLayout(dir string, image v1.Image) error {
	p, err := layout.Write(dir, empty.Index)
	if err != nil {
		return err
	}
	return p.AppendImage(image)
}

func cnbDockerfile(projectID string, noCache bool) string {
	key := sha256.Sum256([]byte(projectID))
	mount := fmt.Sprintf("--mount=type=cache,id=startship-cnb-%x,target=/cache,uid=1001,gid=1000 ", key[:12])
	restore := ""
	if noCache {
		mount, restore = "", "-skip-restore "
	}
	return fmt.Sprintf(`FROM %s AS build
USER root
ENV CNB_PLATFORM_API=0.12 CNB_EXPERIMENTAL_MODE=warn
WORKDIR /workspace
COPY --chown=1001:1000 app/ ./
RUN mkdir -p /layers /cache /platform /out && chown -R 1001:1000 /layers /cache /platform /out /workspace
COPY %s/ /%s/
RUN %s/cnb/lifecycle/creator -app /workspace -layers /layers -platform /platform -order /cnb/order.toml -cache-dir /cache %s-layout -layout-dir /out -run-image %s -process-type web startship-cnb:latest
FROM scratch
COPY --from=build /%s/ /
`, cnbBuilder, cnbRunLayout, cnbRunLayout, mount, restore, cnbRunRef, cnbAppLayout)
}

var buildVariable = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func nixpacksVariables(plan []byte) (map[string]string, error) {
	var p struct {
		Variables map[string]string `json:"variables"`
	}
	if err := json.Unmarshal(plan, &p); err != nil {
		return nil, fmt.Errorf("invalid Nixpacks plan: %w", err)
	}
	for key, value := range p.Variables {
		if !buildVariable.MatchString(key) || strings.ContainsRune(value, 0) {
			return nil, fmt.Errorf("invalid Nixpacks build variable %q", key)
		}
	}
	return p.Variables, nil
}

func nixpacksCommand(root string, out io.Writer, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c := exec.CommandContext(ctx, limitEnv("STARTSHIP_NIXPACKS", "nixpacks"), args...)
	c.Dir = root
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if out != nil {
		c.Stderr = io.MultiWriter(out, &stderr)
	}
	if err := c.Run(); err != nil {
		return nil, fmt.Errorf("nixpacks %s: %w (%v): %s", args[0], err, ctx.Err(), stderr.String())
	}
	return stdout.Bytes(), nil
}

func buildNixpacks(root, stage string, out io.Writer, projectID, image string, noCache bool) error {
	version, err := nixpacksCommand(root, out, "--version")
	if err != nil {
		return fmt.Errorf("install nixpacks %s (see docs/pipelines.md): %w", nixpacksVersion, err)
	}
	if strings.TrimSpace(string(version)) != "nixpacks "+nixpacksVersion {
		return fmt.Errorf("nixpacks %s is required, got %q", nixpacksVersion, strings.TrimSpace(string(version)))
	}
	plan, err := nixpacksCommand(root, out, "plan", root)
	if err != nil {
		return err
	}
	variables, err := nixpacksVariables(plan)
	if err != nil {
		return err
	}
	assets := filepath.Join(stage, "assets")
	args := []string{"build", root, "--out", assets, "--json-plan", string(plan), "--cache-key", projectID}
	if noCache {
		args = append(args, "--no-cache")
	}
	output, err := nixpacksCommand(root, out, args...)
	fmt.Fprint(out, string(output))
	if err != nil {
		return err
	}
	buildRoot := filepath.Join(stage, "context")
	if err = copyBuildContext(root, buildRoot); err != nil {
		return err
	}
	if err = os.RemoveAll(filepath.Join(buildRoot, ".nixpacks")); err != nil {
		return err
	}
	if err = copyBuildContext(filepath.Join(assets, ".nixpacks"), filepath.Join(buildRoot, ".nixpacks")); err != nil {
		return err
	}
	ignore, err := os.ReadFile(filepath.Join(root, ".dockerignore"))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	ignore = append(ignore, []byte("\n!.nixpacks\n!.nixpacks/**\n")...)
	if err = os.WriteFile(filepath.Join(buildRoot, ".nixpacks", "Dockerfile.dockerignore"), ignore, 0600); err != nil {
		return err
	}
	return buildImage(buildRoot, out, projectID, image, imageBuild{Dockerfile: ".nixpacks/Dockerfile", NoCache: noCache, Args: variables})
}

func copyBuildContext(src, dest string) error {
	return filepath.WalkDir(src, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if entry.Name() == ".git" {
				return filepath.SkipDir
			}
			return os.MkdirAll(filepath.Join(dest, rel), 0755)
		}
		if entry.Type()&os.ModeSymlink != 0 {
			target, err := filepath.EvalSymlinks(path)
			if err != nil {
				return err
			}
			within, err := filepath.Rel(src, target)
			if err != nil || within == ".." || strings.HasPrefix(within, ".."+string(os.PathSeparator)) {
				return fmt.Errorf("build symlink escapes source: %s", rel)
			}
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if filepath.IsAbs(link) {
				return fmt.Errorf("absolute build symlink: %s", rel)
			}
			return os.Symlink(link, filepath.Join(dest, rel))
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("unsupported build file: %s", rel)
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		defer in.Close()
		mode := os.FileMode(0644)
		if info.Mode().Perm()&0111 != 0 {
			mode = 0755
		}
		out, err := os.OpenFile(filepath.Join(dest, rel), os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	})
}

func engineStageImage(root string, out io.Writer, projectID, built, image, base, artifact string, p *pipeline.Pipeline) error {
	stage, err := os.MkdirTemp(filepath.Dir(root), ".startship-image-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err = os.Mkdir(filepath.Join(stage, "app"), 0755); err != nil {
		return err
	}
	id, err := exec.Command("docker", "create", "--entrypoint", "/bin/sh", built).Output()
	if err != nil {
		return err
	}
	container := strings.TrimSpace(string(id))
	defer command("", nil, "docker", "rm", "-f", container)
	source := filepath.Join(engineAppDir(p), artifact)
	if err = command("", out, "docker", "cp", container+":"+source+"/.", filepath.Join(stage, "app")); err != nil {
		return err
	}
	content := engineStageDockerfile(base)
	if err = os.WriteFile(filepath.Join(stage, "Dockerfile"), []byte(content), 0600); err != nil {
		return err
	}
	return buildImage(stage, out, projectID, image, imageBuild{Dockerfile: "Dockerfile"})
}

func engineStageDockerfile(base string) string {
	return fmt.Sprintf("FROM %s\nWORKDIR /app\nCOPY app/ /app/\n", base)
}

func engineCommand(p *pipeline.Pipeline, image, command string, customImage bool) []string {
	if p.Build.Engine == "buildpacks" && !customImage {
		return []string{"--entrypoint", "/cnb/lifecycle/launcher", image, "--", "/bin/sh", "-c", command}
	}
	return []string{"--entrypoint", "/bin/sh", image, "-c", command}
}
