package pipeline

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/sammwyy/startship/internal/detect"
)

type Build struct {
	Engine     string   `json:"engine,omitempty"`
	Image      string   `json:"image,omitempty"`
	Commands   []string `json:"commands,omitempty"`
	Dockerfile string   `json:"dockerfile,omitempty"`
}
type Test struct {
	Image          string   `json:"image,omitempty"`
	Commands       []string `json:"commands,omitempty"`
	ExpectExitCode int      `json:"expect_exit_code"`
	ExpectOutput   string   `json:"expect_output,omitempty"`
}
type Deploy struct {
	Image          string `json:"image,omitempty"`
	Artifact       string `json:"artifact,omitempty"`
	Static         bool   `json:"static,omitempty"`
	Command        string `json:"command,omitempty"`
	Port           int    `json:"port,omitempty"`
	HealthPath     string `json:"health_path,omitempty"`
	ExpectStatus   int    `json:"expect_status,omitempty"`
	HealthTimeout  int    `json:"health_timeout,omitempty"`
	HealthDisabled bool   `json:"health_disabled,omitempty"`
}
type Pipeline struct {
	Build  Build  `json:"build"`
	Test   Test   `json:"test"`
	Deploy Deploy `json:"deploy"`
}
type Template struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Icon        string   `json:"icon"`
	Category    string   `json:"category"`
	Pipeline    Pipeline `json:"pipeline"`
}

func Templates() []Template {
	descriptions := map[string]string{
		"custom":     "Define your own build, test and runtime stages",
		"dockerfile": "Build and run your repository's Dockerfile",
		"static":     "Serve HTML, CSS and assets without a container runtime",
		"node":       "Install dependencies and run a Node.js server",
		"bun":        "Build and run JavaScript with the Bun toolkit",
		"nextjs":     "Build a Next.js application and run its Node.js server",
		"vite":       "Vite SPA built with Node and served as static files",
		"astro":      "Build an Astro site and serve its static output",
		"nuxt":       "Build Nuxt and run its Nitro server with Node.js",
		"sveltekit":  "Build SvelteKit and run its adapter-node server",
		"python":     "Install Python dependencies and run your entrypoint",
		"django":     "Install dependencies and serve a Django application",
		"fastapi":    "Serve a Python ASGI application with Uvicorn",
		"flask":      "Serve a Flask application with Gunicorn",
		"go":         "Compile a Go binary and run it in a minimal Alpine image",
		"rust":       "Compile a release binary and run it in a slim Debian image",
	}
	specs := []struct{ id, name, icon, category, image, command string }{
		{"custom", "Custom", "custom", "Custom", "alpine:3.23", ""}, {"dockerfile", "Dockerfile", "docker", "Container", "", ""}, {"static", "Static", "html5", "Frontend", "alpine:3.23", ""},
		{"node", "Node.js", "nodedotjs", "Backend", "node:22-alpine", "npm start"}, {"bun", "Bun", "bun", "Backend", "oven/bun:1", "bun run start"}, {"nextjs", "Next.js", "nextdotjs", "Frontend", "node:22-alpine", "npm start"}, {"vite", "Vite", "vite", "Frontend", "node:22-alpine", ""}, {"astro", "Astro", "astro", "Frontend", "node:22-alpine", ""}, {"nuxt", "Nuxt", "nuxt", "Frontend", "node:22-alpine", "node .output/server/index.mjs"}, {"sveltekit", "SvelteKit", "svelte", "Frontend", "node:22-alpine", "node build"},
		{"python", "Python", "python", "Backend", "python:3.13-slim", "python main.py"}, {"django", "Django", "django", "Backend", "python:3.13-slim", "python manage.py runserver 0.0.0.0:$PORT"}, {"fastapi", "FastAPI", "fastapi", "Backend", "python:3.13-slim", "uvicorn main:app --host 0.0.0.0 --port $PORT"}, {"flask", "Flask", "flask", "Backend", "python:3.13-slim", "gunicorn --bind 0.0.0.0:$PORT app:app"}, {"go", "Go", "go", "Backend", "golang:1.26", "./app"}, {"rust", "Rust", "rust", "Backend", "rust:1-slim", "./app"},
	}
	out := []Template{}
	for _, s := range specs {
		p := Pipeline{Build: Build{Image: s.image}, Deploy: Deploy{Command: s.command, Port: 8080}}
		switch s.id {
		case "dockerfile":
			p.Build.Dockerfile = "Dockerfile"
		case "static":
			p.Deploy.Static = true
		case "node", "nextjs", "vite", "astro", "nuxt", "sveltekit":
			p.Build.Commands = []string{"npm ci", "npm run build"}
			p.Deploy.Port = 3000
		case "bun":
			p.Build.Commands = []string{"bun install", "bun run build"}
			p.Deploy.Port = 3000
		case "python", "django", "fastapi", "flask":
			p.Build.Commands = []string{"pip install --no-cache-dir -r requirements.txt"}
		case "go":
			p.Build.Commands = []string{"CGO_ENABLED=0 go build -o /app/out/app ."}
			p.Deploy.Image = "alpine:3.23"
			p.Deploy.Artifact = "out"
		case "rust":
			p.Build.Commands = []string{"cargo build --release && mkdir -p /app/out && find target/release -maxdepth 1 -type f -executable -exec cp {} /app/out/app \\;"}
			p.Deploy.Image = "debian:trixie-slim"
			p.Deploy.Artifact = "out"
		}
		if s.id == "vite" || s.id == "astro" {
			p.Deploy.Static = true
			p.Deploy.Artifact = "dist"
		}
		out = append(out, Template{s.id, s.name, descriptions[s.id], s.icon, s.category, p})
	}
	return append(out, additionalTemplates()...)
}
func exists(dir, name string) bool {
	s, e := os.Stat(filepath.Join(dir, name))
	return e == nil && !s.IsDir()
}
func DetectTemplate(dir string) string {
	if exists(dir, "Dockerfile") {
		return "dockerfile"
	}
	if id := detectAdditional(dir); id != "" {
		return id
	}
	if exists(dir, "package.json") {
		b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
		var p struct{ Dependencies, DevDependencies map[string]json.RawMessage }
		_ = json.Unmarshal(b, &p)
		for _, d := range [][2]string{{"next", "nextjs"}, {"nuxt", "nuxt"}, {"@sveltejs/kit", "sveltekit"}, {"astro", "astro"}, {"vite", "vite"}} {
			if p.Dependencies[d[0]] != nil || p.DevDependencies[d[0]] != nil {
				return d[1]
			}
		}
	}
	for _, framework := range []struct{ id, base string }{{"nextjs", "next.config"}, {"nuxt", "nuxt.config"}, {"sveltekit", "svelte.config"}, {"astro", "astro.config"}, {"vite", "vite.config"}} {
		for _, ext := range []string{".js", ".mjs", ".ts", ".cjs", ".mts"} {
			if exists(dir, framework.base+ext) {
				return framework.id
			}
		}
	}
	if exists(dir, "bun.lockb") || exists(dir, "bun.lock") {
		return "bun"
	}
	if exists(dir, "package.json") {
		return "node"
	}
	for _, f := range []string{"requirements.txt", "pyproject.toml"} {
		b, _ := os.ReadFile(filepath.Join(dir, f))
		s := strings.ToLower(string(b))
		for _, id := range []string{"django", "fastapi", "flask"} {
			if strings.Contains(s, id) {
				return id
			}
		}
	}
	if exists(dir, "manage.py") {
		return "django"
	}
	return detect.Detect(dir)
}
func Resolve(id, dir string) (Pipeline, error) {
	var p Pipeline
	found := false
	for _, t := range Templates() {
		if t.ID == id {
			p = t.Pipeline
			found = true
			break
		}
	}
	if !found {
		return p, fmt.Errorf("unknown pipeline template %q", id)
	}
	if id == "dockerfile" {
		b, _ := os.ReadFile(filepath.Join(dir, "Dockerfile"))
		if match := regexp.MustCompile(`(?im)^\s*EXPOSE\s+(\d+)`).FindSubmatch(b); len(match) > 1 {
			p.Deploy.Port, _ = strconv.Atoi(string(match[1]))
		}
	}
	if strings.HasPrefix(p.Build.Image, "node:") || id == "bun" {
		var pkg struct {
			Scripts map[string]string
			Engines struct{ Node string }
		}
		b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
		_ = json.Unmarshal(b, &pkg)
		if id != "bun" {
			p.Build.Image = fmt.Sprintf("node:%d-alpine", detect.NodeMajor(pkg.Engines.Node))
		}
		install, run := "npm install", "npm run"
		if exists(dir, "package-lock.json") {
			install = "npm ci"
		}
		if exists(dir, "pnpm-lock.yaml") {
			install = "corepack enable && pnpm install --frozen-lockfile"
			run = "pnpm run"
		} else if exists(dir, "yarn.lock") {
			install = "corepack enable && yarn install --frozen-lockfile"
			run = "yarn run"
		} else if id == "bun" || exists(dir, "bun.lock") || exists(dir, "bun.lockb") {
			install = "bun install --frozen-lockfile"
			run = "bun run"
			p.Build.Image = "oven/bun:1"
		}
		p.Build.Commands = []string{install}
		if pkg.Scripts["build"] != "" {
			p.Build.Commands = append(p.Build.Commands, run+" build")
		}
		if id == "node" || id == "bun" || id == "nextjs" {
			p.Deploy.Command = run + " start"
		}
	}
	if p.Build.Image == "python:3.13-slim" {
		p.Build.Commands = nil
		if exists(dir, "requirements.txt") {
			p.Build.Commands = []string{"pip install --no-cache-dir -r requirements.txt"}
		} else if exists(dir, "pyproject.toml") {
			p.Build.Commands = []string{"pip install --no-cache-dir ."}
		}
		if id == "python" {
			p.Deploy.Command = ""
			for _, f := range []string{"main.py", "app.py", "manage.py"} {
				if exists(dir, f) {
					p.Deploy.Command = "python " + f
					if f == "manage.py" {
						p.Deploy.Command += " runserver 0.0.0.0:$PORT"
					}
					break
				}
			}
			if p.Deploy.Command == "" && detect.ProcfileWeb(dir) == "" {
				return p, fmt.Errorf("no Python entrypoint found: add main.py, app.py, manage.py or a Procfile web command")
			}
		}
		if id == "fastapi" && !exists(dir, "main.py") && exists(dir, "app.py") {
			p.Deploy.Command = "uvicorn app:app --host 0.0.0.0 --port $PORT"
		}
		if id == "flask" && !exists(dir, "app.py") && exists(dir, "main.py") {
			p.Deploy.Command = "gunicorn --bind 0.0.0.0:$PORT main:app"
		}
	}
	if web := detect.ProcfileWeb(dir); web != "" {
		p.Deploy.Command = web
	}
	p = resolveAdditional(id, dir, p)
	return p, p.Validate()
}

var imageName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._/@:-]*$`)

func safePath(s string) bool {
	return !filepath.IsAbs(s) && s != ".." && !strings.HasPrefix(filepath.Clean(s), "../") && !strings.ContainsAny(s, "\r\n\x00\\")
}
func (p Pipeline) Validate() error {
	if p.Deploy.HealthTimeout < 0 || p.Deploy.HealthTimeout > 600 {
		return fmt.Errorf("health timeout must be between 0 and 600 seconds")
	}
	if p.Build.Engine != "" && p.Build.Engine != "docker" && p.Build.Engine != "nixpacks" && p.Build.Engine != "buildpacks" {
		return fmt.Errorf("invalid build engine %q", p.Build.Engine)
	}
	docker := p.Build.Engine == "" || p.Build.Engine == "docker"
	images := []string{p.Test.Image, p.Deploy.Image}
	if docker {
		images = append(images, p.Build.Image)
	}
	for _, s := range images {
		if s != "" && !imageName.MatchString(s) {
			return fmt.Errorf("invalid image %q", s)
		}
	}
	if docker && p.Build.Dockerfile == "" && p.Build.Image == "" {
		return fmt.Errorf("build image is required")
	}
	if (docker && !safePath(p.Build.Dockerfile)) || !safePath(p.Deploy.Artifact) {
		return fmt.Errorf("artifact and Dockerfile must be safe relative paths")
	}
	if p.Deploy.Port < 0 || p.Deploy.Port > 65535 {
		return fmt.Errorf("invalid deploy port")
	}
	if p.Deploy.ExpectStatus != 0 && (p.Deploy.ExpectStatus < 100 || p.Deploy.ExpectStatus > 599) {
		return fmt.Errorf("invalid expected HTTP status")
	}
	if p.Deploy.HealthPath != "" && (!strings.HasPrefix(p.Deploy.HealthPath, "/") || strings.ContainsAny(p.Deploy.HealthPath, "\r\n")) {
		return fmt.Errorf("invalid health path")
	}
	commandsToCheck := [][]string{p.Test.Commands}
	if docker {
		commandsToCheck = append(commandsToCheck, p.Build.Commands)
	}
	for _, commands := range commandsToCheck {
		for _, c := range commands {
			if strings.TrimSpace(c) == "" || strings.ContainsAny(c, "\x00\r\n") {
				return fmt.Errorf("commands must be non-empty single lines")
			}
		}
	}
	return nil
}
func (p Pipeline) Dockerfile() (string, error) {
	if e := p.Validate(); e != nil {
		return "", e
	}
	if p.Build.Engine != "" && p.Build.Engine != "docker" {
		return "", fmt.Errorf("build engine %s generates its own image", p.Build.Engine)
	}
	if p.Build.Dockerfile != "" {
		return "", fmt.Errorf("use repository Dockerfile")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "FROM %s AS build\nUSER root\nENTRYPOINT []\nWORKDIR /app\nCOPY . .\n", p.Build.Image)
	for _, c := range p.Build.Commands {
		v, _ := json.Marshal([]string{"sh", "-c", c})
		fmt.Fprintf(&b, "RUN %s\n", v)
	}
	base := p.Test.Image
	if base == "" {
		base = "build"
	}
	fmt.Fprintf(&b, "FROM %s AS test\nWORKDIR /app\n", base)
	if base != "build" {
		b.WriteString("COPY --from=build /app /app\n")
	}
	base = p.Deploy.Image
	if base == "" {
		base = "build"
	}
	fmt.Fprintf(&b, "FROM %s AS final\nWORKDIR /app\n", base)
	if base != "build" {
		artifact := p.Deploy.Artifact
		if artifact == "" {
			artifact = "."
		}
		v, _ := json.Marshal([]string{"/app/" + artifact, "/app"})
		fmt.Fprintf(&b, "COPY --from=build %s\n", v)
	}
	fmt.Fprintf(&b, "ENV PORT=%d\nEXPOSE %d\n", p.Deploy.Port, p.Deploy.Port)
	if p.Deploy.Command != "" {
		b.WriteString("ENTRYPOINT []\n")
		v, _ := json.Marshal([]string{"sh", "-c", "exec " + p.Deploy.Command})
		fmt.Fprintf(&b, "CMD %s\n", v)
	}
	return b.String(), nil
}
