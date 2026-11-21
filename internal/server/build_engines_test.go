package server

import (
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/layout"
	"github.com/sammwyy/startship/internal/pipeline"
)

func TestBuildArgsVariablesAndOutputs(t *testing.T) {
	args := buildArgs("bounded", "app", imageBuild{Dockerfile: "generated", Target: "app", NoCache: true, Args: map[string]string{"NODE_ENV": "production", "A": "a b=$hello"}, Output: "type=local,dest=/result"})
	want := []string{"buildx", "build", "--builder", "bounded", "--output", "type=local,dest=/result", "--progress", "plain", "-t", "app", "-f", "generated", "--no-cache", "--target", "app", "--build-arg", "A=a b=$hello", "--build-arg", "NODE_ENV=production", "."}
	if !reflect.DeepEqual(args, want) {
		t.Fatal(args)
	}
	for _, output := range []string{"", "type=oci,dest=/result.tar"} {
		args = buildArgs("bounded", "app", imageBuild{Dockerfile: "generated", Output: output})
		if strings.Contains(strings.Join(args, " "), "--load") != (output == "") {
			t.Fatal(args)
		}
	}
}

func TestEngineAvailability(t *testing.T) {
	old := engineHost
	t.Cleanup(func() { engineHost = old })
	for _, host := range []struct{ OS, Arch string }{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"windows", "arm64"}} {
		engineHost = host
		for _, engine := range []string{"", "docker", "nixpacks", "buildpacks"} {
			reason := engineUnavailable(engine)
			unsupported := engine == "buildpacks" && host != (struct{ OS, Arch string }{"linux", "amd64"})
			if (reason != "") != unsupported {
				t.Fatalf("%v %s: %q", host, engine, reason)
			}
			if unsupported {
				want := "Buildpacks needs a linux/amd64 server; this one is " + host.OS + "/" + host.Arch + ". Use Dockerfile or Nixpacks."
				if reason != want {
					t.Fatal(reason)
				}
				p := &pipeline.Pipeline{Build: pipeline.Build{Engine: engine}}
				if err := buildEngine("missing", io.Discard, "project", "app", p, false); err == nil || err.Error() != reason {
					t.Fatal(err)
				}
			}
		}
	}
}

func TestCNBDockerfile(t *testing.T) {
	for _, noCache := range []bool{false, true} {
		dockerfile := cnbDockerfile("project", noCache)
		for _, part := range []string{cnbBuilder, "CNB_PLATFORM_API=0.12", "CNB_EXPERIMENTAL_MODE=warn", "COPY --chown=1001:1000 app/", cnbRunLayout, "/cnb/lifecycle/creator", "-layout -layout-dir /out", cnbRunRef, cnbAppLayout, "FROM scratch"} {
			if !strings.Contains(dockerfile, part) {
				t.Fatal(part, dockerfile)
			}
		}
		if strings.Contains(dockerfile, "type=cache") == noCache || strings.Contains(dockerfile, "-skip-restore") != noCache {
			t.Fatal(dockerfile)
		}
		for _, forbidden := range []string{"docker.sock", "--privileged", "-daemon"} {
			if strings.Contains(dockerfile, forbidden) {
				t.Fatal(forbidden)
			}
		}
	}
	if cnbDockerfile("a", false) == cnbDockerfile("b", false) {
		t.Fatal("shared cache keys")
	}
}

func TestNixpacksVariables(t *testing.T) {
	for _, input := range []string{`{}`, `{"variables":{}}`, `{"variables":{"CI":"true","VALUE":"a b=$hello"}}`} {
		if _, err := nixpacksVariables([]byte(input)); err != nil {
			t.Fatal(err)
		}
	}
	for _, input := range []string{`{`, `{"variables":{"A B":"value"}}`, `{"variables":{"A":"\u0000"}}`} {
		if _, err := nixpacksVariables([]byte(input)); err == nil {
			t.Fatal("accepted", input)
		}
	}
	t.Setenv("STARTSHIP_NIXPACKS", filepath.Join(t.TempDir(), "missing"))
	if err := buildNixpacks(t.TempDir(), t.TempDir(), io.Discard, "project", "app", false); err == nil || !strings.Contains(err.Error(), "install nixpacks 1.41.0") {
		t.Fatal(err)
	}
}

func TestEngineImageCommands(t *testing.T) {
	stage := engineStageDockerfile("node:22-alpine")
	if !strings.Contains(stage, "FROM node:22-alpine") || !strings.Contains(stage, "COPY app/ /app/") || strings.Contains(stage, "ENTRYPOINT") || strings.Contains(stage, "CMD") {
		t.Fatal("stage image must retain its base start command", stage)
	}
	for _, engine := range []string{"", "docker", "nixpacks", "buildpacks"} {
		p := &pipeline.Pipeline{Build: pipeline.Build{Engine: engine}}
		for _, custom := range []bool{false, true} {
			for _, command := range []string{"", "set -e\nnode -e \"console.log('engine-test ' + process.version)\"", "set -e\necho engine-test\nexit 3", "exec node -e \"require('./server.js')\" -- --override"} {
				want := []string{"--entrypoint", "/bin/sh", "app", "-c", command}
				if engine == "buildpacks" && !custom {
					want = []string{"--entrypoint", "/cnb/lifecycle/launcher", "app", "--", "/bin/sh", "-c", command}
				}
				if args := engineCommand(p, "app", command, custom); !reflect.DeepEqual(args, want) {
					t.Fatalf("engine %q custom %v: got %#v, want %#v", engine, custom, args, want)
				}
			}
		}
		if externalEngine(p) != (engine == "nixpacks" || engine == "buildpacks") {
			t.Fatal(engine)
		}
	}
	if externalEngine(nil) || externalEngine(&pipeline.Pipeline{}) {
		t.Fatal("default is not docker")
	}
}

func TestCopyBuildContext(t *testing.T) {
	src, dest := t.TempDir(), filepath.Join(t.TempDir(), "context")
	os.WriteFile(filepath.Join(src, "run"), []byte("hello"), 0700)
	os.Mkdir(filepath.Join(src, ".git"), 0700)
	os.WriteFile(filepath.Join(src, ".git", "token"), []byte("ignored"), 0600)
	os.Symlink("run", filepath.Join(src, "link"))
	if err := copyBuildContext(src, dest); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dest, "run"))
	if err != nil || info.Mode().Perm() != 0755 {
		t.Fatal(info, err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".git")); !os.IsNotExist(err) {
		t.Fatal("copied .git")
	}
	if data, err := os.ReadFile(filepath.Join(dest, "link")); err != nil || string(data) != "hello" {
		t.Fatal(err)
	}
	os.Symlink("../escape", filepath.Join(src, "bad"))
	if err := copyBuildContext(src, filepath.Join(t.TempDir(), "context")); err == nil {
		t.Fatal("accepted escaping link")
	}
	if err := copyBuildContext(filepath.Join(src, "missing"), t.TempDir()); err == nil {
		t.Fatal("accepted missing context")
	}
}

func TestWriteRunImageLayout(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "run")
	if err := writeImageLayout(dir, empty.Image); err != nil {
		t.Fatal(err)
	}
	p, err := layout.FromPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	index, err := p.ImageIndex()
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := index.IndexManifest()
	if err != nil || len(manifest.Manifests) != 1 {
		t.Fatal(manifest, err)
	}
}
