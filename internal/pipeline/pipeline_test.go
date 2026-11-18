package pipeline

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
func TestDetectTemplates(t *testing.T) {
	for _, tc := range []struct {
		id    string
		files map[string]string
	}{
		{"dockerfile", map[string]string{"Dockerfile": "FROM scratch", "package.json": `{"dependencies":{"next":"1"}}`}},
		{"node", map[string]string{"package.json": "{}"}}, {"bun", map[string]string{"bun.lock": ""}},
		{"nextjs", map[string]string{"package.json": `{"dependencies":{"next":"1","vite":"1"}}`}},
		{"nuxt", map[string]string{"package.json": `{"devDependencies":{"nuxt":"1"}}`}},
		{"sveltekit", map[string]string{"package.json": `{"devDependencies":{"@sveltejs/kit":"1"}}`}},
		{"astro", map[string]string{"package.json": `{"dependencies":{"astro":"1"}}`}},
		{"vite", map[string]string{"package.json": `{"devDependencies":{"vite":"1"}}`}},
		{"nextjs", map[string]string{"next.config.mjs": ""}},
		{"nuxt", map[string]string{"nuxt.config.ts": ""}},
		{"sveltekit", map[string]string{"svelte.config.js": ""}},
		{"astro", map[string]string{"astro.config.mjs": ""}},
		{"vite", map[string]string{"vite.config.ts": ""}},
		{"django", map[string]string{"manage.py": ""}}, {"fastapi", map[string]string{"requirements.txt": "fastapi==1"}}, {"flask", map[string]string{"pyproject.toml": "dependencies = ['flask']"}},
		{"python", map[string]string{"main.py": ""}}, {"python", map[string]string{"Procfile": "web: gunicorn app:app"}}, {"node", map[string]string{"Procfile": "web: node server.js"}},
		{"go", map[string]string{"go.mod": ""}}, {"rust", map[string]string{"Cargo.toml": ""}}, {"static", map[string]string{"index.html": ""}}, {"", nil},
	} {
		t.Run(tc.id, func(t *testing.T) {
			if got := DetectTemplate(fixture(t, tc.files)); got != tc.id {
				t.Fatalf("got %q want %q", got, tc.id)
			}
		})
	}
}
func TestResolve(t *testing.T) {
	for _, tc := range []struct{ lock, install, run string }{
		{"", "npm install", "npm run"}, {"package-lock.json", "npm ci", "npm run"}, {"pnpm-lock.yaml", "corepack enable && pnpm install --frozen-lockfile", "pnpm run"}, {"yarn.lock", "corepack enable && yarn install --frozen-lockfile", "yarn run"}, {"bun.lock", "bun install --frozen-lockfile", "bun run"},
	} {
		t.Run(tc.lock, func(t *testing.T) {
			files := map[string]string{"package.json": `{"engines":{"node":"20"},"scripts":{"build":"x"}}`}
			if tc.lock != "" {
				files[tc.lock] = ""
			}
			p, e := Resolve("node", fixture(t, files))
			if e != nil || len(p.Build.Commands) != 2 || p.Build.Commands[0] != tc.install || p.Build.Commands[1] != tc.run+" build" {
				t.Fatal(p, e)
			}
			if tc.lock != "bun.lock" && p.Build.Image != "node:20-alpine" {
				t.Fatal(p)
			}
		})
	}
	p, e := Resolve("node", fixture(t, map[string]string{"package.json": "{}", "Procfile": "web: node server.js"}))
	if e != nil || len(p.Build.Commands) != 1 || p.Deploy.Command != "node server.js" {
		t.Fatal(p, e)
	}
	p, e = Resolve("python", fixture(t, map[string]string{"app.py": "", "pyproject.toml": ""}))
	if e != nil || p.Deploy.Command != "python app.py" || p.Build.Commands[0] != "pip install --no-cache-dir ." {
		t.Fatal(p, e)
	}
	if _, e = Resolve("python", t.TempDir()); e == nil || !strings.Contains(e.Error(), "no Python entrypoint found") {
		t.Fatal(e)
	}
	if _, e = Resolve("missing", t.TempDir()); e == nil {
		t.Fatal("unknown template accepted")
	}
	p, e = Resolve("dockerfile", fixture(t, map[string]string{"Dockerfile": "FROM alpine\nEXPOSE 7070"}))
	if e != nil || p.Deploy.Port != 7070 {
		t.Fatal(p, e)
	}
	p, e = Resolve("python", fixture(t, map[string]string{"manage.py": ""}))
	if e != nil || !strings.Contains(p.Deploy.Command, "runserver") {
		t.Fatal(p, e)
	}
	for _, template := range Templates() {
		if e := template.Pipeline.Validate(); e != nil {
			t.Fatal(template.ID, e)
		}
	}
}
func TestValidateAndDockerfile(t *testing.T) {
	p := Pipeline{Build: Build{Image: "node:22-alpine", Commands: []string{`printf '%s' '"quoted"'`}}, Test: Test{Image: "alpine:3.23"}, Deploy: Deploy{Image: "alpine:3.23", Artifact: "dist", Port: 8080, Command: `echo "hello"`}}
	content, e := p.Dockerfile()
	if e != nil {
		t.Fatal(e)
	}
	for _, want := range []string{"AS build", "AS test", "AS final", "COPY --from=build /app /app", `COPY --from=build ["/app/dist","/app"]`, `CMD ["sh","-c","exec echo \"hello\""]`, `RUN ["sh","-c",`} {
		if !strings.Contains(content, want) {
			t.Fatalf("missing %s in %s", want, content)
		}
	}
	p.Test.Image = ""
	p.Deploy.Image = ""
	content, e = p.Dockerfile()
	if e != nil || !strings.Contains(content, "FROM build AS test") || !strings.Contains(content, "FROM build AS final") {
		t.Fatal(content, e)
	}
	for _, mutate := range []func(*Pipeline){func(p *Pipeline) { p.Build.Image = "bad\nRUN evil" }, func(p *Pipeline) { p.Deploy.Port = 65536 }, func(p *Pipeline) { p.Deploy.Port = -1 }, func(p *Pipeline) { p.Deploy.Artifact = "../secret" }, func(p *Pipeline) { p.Build.Commands = []string{" "} }, func(p *Pipeline) { p.Test.Commands = []string{""} }, func(p *Pipeline) { p.Build.Dockerfile = "/etc/passwd" }, func(p *Pipeline) { p.Deploy.ExpectStatus = 999 }, func(p *Pipeline) { p.Deploy.HealthPath = "http://elsewhere" }} {
		q := p
		mutate(&q)
		if e := q.Validate(); e == nil {
			t.Fatal("invalid pipeline accepted", q)
		}
	}
	if e := (Pipeline{}).Validate(); e == nil {
		t.Fatal("empty pipeline accepted")
	}
}
