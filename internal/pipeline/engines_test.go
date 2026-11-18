package pipeline

import (
	"strings"
	"testing"
)

func TestAdditionalEngines(t *testing.T) {
	for _, tc := range []struct {
		id    string
		files map[string]string
		want  string
	}{
		{"spring", map[string]string{"pom.xml": "", "mvnw": ""}, "./mvnw"},
		{"spring", map[string]string{"build.gradle.kts": "", "gradlew": ""}, "./gradlew"},
		{"spring", map[string]string{"build.gradle": ""}, "gradle --no-daemon"},
		{"php", map[string]string{"index.php": ""}, "php -S"},
		{"laravel", map[string]string{"composer.json": `{"require":{"laravel/framework":"^12"}}`, "package.json": `{"dependencies":{"vite":"1"}}`}, "composer install"},
		{"ruby", map[string]string{"config.ru": ""}, "gem install"},
		{"ruby", map[string]string{"Gemfile": `gem "rack"`}, "bundle install"},
		{"rails", map[string]string{"Gemfile": `gem "rails"`}, "rails server"},
		{"dotnet", map[string]string{"App.csproj": ""}, "dotnet publish"},
		{"phoenix", map[string]string{"mix.exs": "{:phoenix, \"~> 1.8\"}"}, "mix deps.get"},
		{"deno", map[string]string{"deno.json": "{}", "package.json": "{}"}, "deno task start"},
		{"angular", map[string]string{"package.json": `{"dependencies":{"@angular/core":"1","vite":"1"},"scripts":{"build":"ng build"}}`}, "--output-path=out"},
		{"remix", map[string]string{"package.json": `{"devDependencies":{"@react-router/dev":"1","vite":"1"},"scripts":{"build":"react-router build"}}`}, "npm run build"},
		{"remix", map[string]string{"package.json": `{"dependencies":{"@remix-run/react":"1","vite":"1"}}`}, "npm run start"},
		{"nestjs", map[string]string{"package.json": `{"dependencies":{"@nestjs/core":"1","vite":"1"},"scripts":{"build":"nest build"}}`}, "node dist/main.js"},
		{"hugo", map[string]string{"hugo.toml": "baseURL='https://example.com'"}, "hugo --minify"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			dir := fixture(t, tc.files)
			if got := DetectTemplate(dir); got != tc.id {
				t.Fatalf("detected %q, want %q", got, tc.id)
			}
			p, err := Resolve(tc.id, dir)
			if err != nil {
				t.Fatal(err)
			}
			dockerfile, err := p.Dockerfile()
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(p.Build.Commands, "\n")+p.Deploy.Command, tc.want) {
				t.Fatalf("missing %q in %#v", tc.want, p)
			}
			for _, stage := range []string{"AS build", "AS test", "AS final"} {
				if !strings.Contains(dockerfile, stage) {
					t.Fatal(dockerfile)
				}
			}
		})
	}
}

func TestHealthTimeoutValidation(t *testing.T) {
	for _, timeout := range []int{-1, 0, 1, 600, 601} {
		p := Pipeline{Build: Build{Image: "alpine:3.23"}, Deploy: Deploy{HealthTimeout: timeout}}
		err := p.Validate()
		if (err != nil) != (timeout < 0 || timeout > 600) {
			t.Fatalf("timeout %d: %v", timeout, err)
		}
	}
}
