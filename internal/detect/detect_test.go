package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNodeEngines(t *testing.T) {
	for _, tc := range []struct {
		engine string
		want   int
	}{
		{"", 22}, {"22.x || 24.x", 24}, {">=20", 26}, {"^18", 18}, {"20", 20}, {"~20.0.0", 20}, {">=20 <24", 22}, {">= 20", 26}, {"*", 26}, {"invalid", 22}, {">=99", 22}, {"22.x || 24.x || 26.x", 26},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			if got := NodeMajor(tc.engine); got != tc.want {
				t.Fatalf("got %d, want %d", got, tc.want)
			}
		})
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"engines":{"node":"24.x"},"scripts":{"start":"node server.js"}}`), 0600)
	got, err := Dockerfile("node", 8080, dir)
	if err != nil || !strings.HasPrefix(got, "FROM node:24-alpine\n") || !strings.Contains(got, "RUN npm install\n") {
		t.Fatal(got, err)
	}
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{invalid`), 0600)
	got, err = Dockerfile("node", 8080, dir)
	if err != nil || !strings.HasPrefix(got, "FROM node:22-alpine\n") {
		t.Fatal(got, err)
	}
}

func TestDetection(t *testing.T) {
	for _, v := range []struct{ file, kind string }{{"Dockerfile", "dockerfile"}, {"package.json", "node"}, {"requirements.txt", "python"}, {"pyproject.toml", "python"}, {"go.mod", "go"}, {"Cargo.toml", "rust"}, {"index.html", "static"}} {
		t.Run(v.kind+v.file, func(t *testing.T) {
			dir := t.TempDir()
			if got := Detect(dir); got != "" {
				t.Fatal(got)
			}
			os.WriteFile(filepath.Join(dir, v.file), []byte("{}"), 0600)
			if got := Detect(dir); got != v.kind {
				t.Fatal(got)
			}
		})
	}
	dir := t.TempDir()
	os.Mkdir(filepath.Join(dir, "api"), 0700)
	os.WriteFile(filepath.Join(dir, "api", "go.mod"), nil, 0600)
	if c := Candidates(dir); len(c) != 1 || c[0].RootDir != "api" {
		t.Fatal(c)
	}
}
func TestDockerfiles(t *testing.T) {
	for _, kind := range []string{"node", "python", "go", "rust"} {
		dir := t.TempDir()
		if kind == "python" {
			os.WriteFile(filepath.Join(dir, "main.py"), nil, 0600)
		}
		f, e := Dockerfile(kind, 8123, dir)
		if e != nil || !strings.Contains(f, "ENV PORT=8123") || !strings.Contains(f, "CMD ") {
			t.Fatal(kind, f, e)
		}
	}
	if _, e := Dockerfile("", 0, ""); e == nil {
		t.Fatal("expected unsupported type")
	}
}
func TestEntrypoints(t *testing.T) {
	for _, file := range []string{"main.py", "app.py", "manage.py"} {
		dir := t.TempDir()
		os.WriteFile(filepath.Join(dir, file), nil, 0600)
		if Detect(dir) != "python" {
			t.Fatal(file)
		}
		got, err := Dockerfile("python", 8080, dir)
		if err != nil || !strings.Contains(got, file) {
			t.Fatal(got, err)
		}
		if file == "manage.py" && !strings.Contains(got, "0.0.0.0:$PORT") {
			t.Fatal(got)
		}
	}
	for _, kind := range []string{"node", "python"} {
		dir := t.TempDir()
		cmd := "node server.js"
		if kind == "python" {
			cmd = "gunicorn main:app --bind 0.0.0.0:$PORT"
		}
		os.WriteFile(filepath.Join(dir, "Procfile"), []byte("worker: ignore\nweb: "+cmd+"\n"), 0600)
		got, err := Dockerfile(kind, 9000, dir)
		if err != nil || !strings.Contains(got, cmd) || Detect(dir) != kind {
			t.Fatal(got, err)
		}
	}
	if _, err := Dockerfile("python", 8080, t.TempDir()); err == nil {
		t.Fatal("missing entrypoint accepted")
	}
}
