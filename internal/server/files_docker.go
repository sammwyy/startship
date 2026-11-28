package server

import (
	"archive/tar"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"path"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	errFilePath    = errors.New("invalid or unsupported file path")
	errFileMissing = errors.New("file not found")
)

func filePath(p string) (string, error) {
	if p == "" || p == "/" {
		return "/", nil
	}
	if !strings.HasPrefix(p, "/") {
		return "", errFilePath
	}
	if len(p) > 4096 || !utf8.ValidString(p) || strings.ContainsAny(p, "\\\x00\r\n") {
		return "", errFilePath
	}
	for _, part := range strings.Split(p[1:], "/") {
		if part == "" || part == "." || part == ".." || len(part) > 255 {
			return "", errFilePath
		}
	}
	return p, nil
}

type fileTarget struct {
	container, volume, workspace, project string
}

type fileDocker struct {
	id, base    string
	write, move bool
	reason      string
}

func fileDockerCommand(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, "docker", args...)
}

func (t fileTarget) open(ctx context.Context, write bool) (fileDocker, func(), error) {
	fs := fileDocker{id: t.container, base: "/", write: write}
	noop := func() {}
	if t.volume != "" {
		if err := fileDockerCommand(ctx, "volume", "inspect", t.volume).Run(); err != nil {
			return fs, noop, errFileMissing
		}
		name := "startship-files-" + randomID()
		close := func() {
			cleanup, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = fileDockerCommand(cleanup, "rm", "-f", name).Run()
		}
		mount := "type=volume,source=" + t.volume + ",destination=/data,volume-nocopy"
		if !write {
			mount += ",readonly"
		}
		if err := fileDockerCommand(ctx, "run", "-d", "--name", name, "--network", "none", "--read-only",
			"--security-opt", "no-new-privileges", "--mount", mount, "busybox:1.37", "sleep", "infinity").Run(); err != nil {
			close()
			return fs, noop, errors.New("could not open volume")
		}
		fs.id, fs.base, fs.move = name, "/data", write
		return fs, close, nil
	}
	b, err := fileDockerCommand(ctx, "container", "inspect", t.container).Output()
	if err != nil {
		return fs, noop, errFileMissing
	}
	var result []struct {
		ID         string
		State      struct{ Running bool }
		HostConfig struct{ ReadonlyRootfs bool }
		Mounts     []struct{ Type string }
	}
	if json.Unmarshal(b, &result) != nil || len(result) != 1 || result[0].ID == "" {
		return fs, noop, errors.New("could not inspect container")
	}
	c := result[0]
	for _, m := range c.Mounts {
		if m.Type != "volume" && m.Type != "tmpfs" {
			return fs, noop, errors.New("file access is unavailable on containers with host mounts")
		}
	}
	fs.id = c.ID
	switch {
	case !write:
		fs.reason = "You have read-only file access."
	case c.HostConfig.ReadonlyRootfs:
		fs.write = false
		fs.reason = "The container filesystem is read-only."
	case !c.State.Running:
		fs.reason = "The container is stopped."
	default:
		probe, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		fs.move = fileDockerCommand(probe, "exec", fs.id, "/bin/sh", "-c", "command -v mv >/dev/null && command -v rm >/dev/null").Run() == nil
		if !fs.move {
			fs.reason = "The container does not have a usable shell and file utilities."
		}
	}
	return fs, noop, nil
}

func (fs fileDocker) dockerPath(p string) string {
	return path.Join(fs.base, p)
}

func (fs fileDocker) read(ctx context.Context, p string, use func(*tar.Reader) error) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	source := fs.dockerPath(p)
	if source == "/" {
		source = "/."
	}
	cmd := fileDockerCommand(ctx, "cp", fs.id+":"+source, "-")
	var stderr strings.Builder
	cmd.Stderr = &stderr
	r, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	err = use(tar.NewReader(r))
	if err != nil {
		if err == io.EOF {
			_, _ = io.Copy(io.Discard, r)
		} else {
			cancel()
		}
	} else {
		_, err = io.Copy(io.Discard, r)
	}
	wait := cmd.Wait()
	if err == nil && wait == nil {
		return nil
	}
	if err == io.EOF && strings.Contains(stderr.String(), "Could not find the file") {
		return errFileMissing
	}
	if err != nil {
		return err
	}
	return errors.New("could not read container files")
}

var errFileHeader = errors.New("file header read")

func (fs fileDocker) stat(ctx context.Context, p string) (*tar.Header, error) {
	if p == "/" {
		return &tar.Header{Typeflag: tar.TypeDir}, nil
	}
	var h *tar.Header
	err := fs.read(ctx, p, func(t *tar.Reader) error {
		var err error
		h, err = t.Next()
		if err != nil {
			return err
		}
		return errFileHeader
	})
	if err == errFileHeader {
		return h, nil
	}
	return nil, err
}

func (fs fileDocker) check(ctx context.Context, p string) (*tar.Header, error) {
	return fs.checkPath(ctx, p, false)
}

func (fs fileDocker) checkPath(ctx context.Context, p string, createDirs bool) (*tar.Header, error) {
	if fs.base == "/" && (p == "/proc" || p == "/sys" || p == "/dev" ||
		strings.HasPrefix(p, "/proc/") || strings.HasPrefix(p, "/sys/") || strings.HasPrefix(p, "/dev/")) {
		return nil, errFilePath
	}
	if p != "/" {
		for prefix := ""; ; {
			part := strings.SplitN(p[len(prefix)+1:], "/", 2)
			prefix += "/" + part[0]
			h, err := fs.stat(ctx, prefix)
			if createDirs && errors.Is(err, errFileMissing) {
				h = &tar.Header{Typeflag: tar.TypeDir, Mode: 0755, ModTime: time.Now()}
				err = fs.put(ctx, prefix, h, nil)
			}
			if err != nil {
				return nil, err
			}
			if h.Typeflag != tar.TypeDir && (createDirs || prefix != p) {
				return nil, errFilePath
			}
			if prefix == p {
				return h, nil
			}
		}
	}
	return fs.stat(ctx, p)
}

func (fs fileDocker) writeTar(ctx context.Context, dir string, content io.Reader) error {
	cmd := fileDockerCommand(ctx, "cp", "--archive", "-", fs.id+":"+fs.dockerPath(dir))
	cmd.Stdin = content
	if err := cmd.Run(); err != nil {
		return errors.New("could not write files to target")
	}
	return nil
}

func (fs fileDocker) mutate(ctx context.Context, op, p, dst string) error {
	var args []string
	if op == "delete" {
		args = []string{"rm", "-rf", "--", fs.dockerPath(p)}
	} else {
		args = []string{"mv", "-T", "--", fs.dockerPath(p), fs.dockerPath(dst)}
	}
	if err := fileDockerCommand(ctx, append([]string{"exec", "--user", "0", fs.id}, args...)...).Run(); err != nil {
		return fmt.Errorf("could not %s file", op)
	}
	return nil
}
