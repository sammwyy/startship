package pipeline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

func additionalTemplates() []Template {
	specs := []struct {
		id, name, description, icon, category, image, command string
		commands                                              []string
	}{
		{"spring", "Spring Boot", "Package a Java Spring Boot server with Maven or Gradle", "springboot", "Backend", "maven:3-eclipse-temurin-21", "java -jar out/app.jar", []string{"mvn -B -DskipTests package", "mkdir -p out && cp target/*.jar out/app.jar"}},
		{"php", "PHP", "Serve PHP applications with the built-in HTTP server", "php", "Backend", "php:8.4-cli-bookworm", "php -S 0.0.0.0:$PORT -t .", nil},
		{"laravel", "Laravel", "Install Composer dependencies and serve a Laravel application", "laravel", "Backend", "php:8.4-cli-bookworm", "php artisan serve --host=0.0.0.0 --port=$PORT", nil},
		{"ruby", "Ruby / Rack", "Install gems and serve a Rack application", "ruby", "Backend", "ruby:3.4-slim", "bundle exec rackup -o 0.0.0.0 -p $PORT", []string{"bundle install"}},
		{"rails", "Rails", "Install gems and serve a Ruby on Rails application", "rubyonrails", "Backend", "ruby:3.4-slim", "bundle exec rails server -b 0.0.0.0 -p $PORT", []string{"apt-get update && apt-get install -y --no-install-recommends build-essential libyaml-dev libsqlite3-dev libpq-dev && rm -rf /var/lib/apt/lists/*", "bundle install"}},
		{"dotnet", ".NET", "Publish an ASP.NET Core application for the .NET runtime", "dotnet", "Backend", "mcr.microsoft.com/dotnet/sdk:10.0", "dotnet app.dll --urls http://0.0.0.0:$PORT", []string{"dotnet publish -c Release -o /app/out -p:AssemblyName=app"}},
		{"phoenix", "Phoenix", "Compile and serve an Elixir Phoenix application", "phoenixframework", "Backend", "elixir:1.18-otp-27-slim", "mix phx.server", []string{"apt-get update && apt-get install -y --no-install-recommends git build-essential && rm -rf /var/lib/apt/lists/*", "mix local.hex --force && mix local.rebar --force", "mix deps.get && mix compile"}},
		{"deno", "Deno", "Run a Deno application with its configured start task", "deno", "Backend", "denoland/deno:debian-2.6.8", "deno task start", nil},
		{"angular", "Angular", "Build Angular and serve the generated browser files", "angular", "Frontend", "node:22-alpine", "", []string{"npm ci", "npm run build -- --output-path=out", angularBrowserOutput}},
		{"remix", "Remix / React Router", "Build a React Router or Remix server application", "reactrouter", "Frontend", "node:22-alpine", "npm run start", []string{"npm ci", "npm run build"}},
		{"nestjs", "NestJS", "Build and run a NestJS Node.js API", "nestjs", "Backend", "node:22-alpine", "node dist/main.js", []string{"npm ci", "npm run build"}},
		{"hugo", "Hugo", "Generate a Hugo site and serve its static output", "hugo", "Frontend", "ghcr.io/gohugoio/hugo:v0.153.0", "", []string{"hugo --minify"}},
	}
	out := make([]Template, 0, len(specs))
	for _, s := range specs {
		p := Pipeline{Build: Build{Image: s.image, Commands: s.commands}, Deploy: Deploy{Command: s.command, Port: 8080}}
		switch s.id {
		case "spring":
			p.Deploy.Image = "eclipse-temurin:21-jre-jammy"
			p.Deploy.Artifact = "out"
			p.Deploy.Command = "java -jar app.jar --server.port=$PORT"
		case "dotnet":
			p.Deploy.Image = "mcr.microsoft.com/dotnet/aspnet:10.0"
			p.Deploy.Artifact = "out"
		case "angular":
			p.Deploy.Static = true
			p.Deploy.Artifact = "out"
		case "hugo":
			p.Deploy.Static = true
			p.Deploy.Artifact = "public"
		}
		out = append(out, Template{s.id, s.name, s.description, s.icon, s.category, p})
	}
	return out
}

func detectAdditional(dir string) string {
	if exists(dir, "deno.json") || exists(dir, "deno.jsonc") {
		return "deno"
	}
	b, _ := os.ReadFile(filepath.Join(dir, "package.json"))
	var pkg struct{ Dependencies, DevDependencies map[string]json.RawMessage }
	_ = json.Unmarshal(b, &pkg)
	for _, pair := range [][2]string{{"@nestjs/core", "nestjs"}, {"@react-router/dev", "remix"}, {"@remix-run/react", "remix"}, {"@angular/core", "angular"}} {
		if pkg.Dependencies[pair[0]] != nil || pkg.DevDependencies[pair[0]] != nil {
			return pair[1]
		}
	}
	b, _ = os.ReadFile(filepath.Join(dir, "composer.json"))
	if exists(dir, "artisan") || strings.Contains(string(b), "laravel/framework") {
		return "laravel"
	}
	if exists(dir, "composer.json") || exists(dir, "index.php") {
		return "php"
	}
	b, _ = os.ReadFile(filepath.Join(dir, "Gemfile"))
	if strings.Contains(string(b), "rails") || exists(dir, "bin/rails") {
		return "rails"
	}
	if exists(dir, "Gemfile") || exists(dir, "config.ru") {
		return "ruby"
	}
	for _, pattern := range []string{"*.csproj", "*.fsproj"} {
		matches, _ := filepath.Glob(filepath.Join(dir, pattern))
		if len(matches) > 0 {
			return "dotnet"
		}
	}
	if exists(dir, "pom.xml") || exists(dir, "build.gradle") || exists(dir, "build.gradle.kts") {
		return "spring"
	}
	b, _ = os.ReadFile(filepath.Join(dir, "mix.exs"))
	if strings.Contains(string(b), ":phoenix") {
		return "phoenix"
	}
	for _, file := range []string{"hugo.toml", "hugo.yaml", "hugo.json"} {
		if exists(dir, file) {
			return "hugo"
		}
	}
	return ""
}

func resolveAdditional(id, dir string, p Pipeline) Pipeline {
	switch id {
	case "spring":
		if exists(dir, "gradlew") || exists(dir, "build.gradle") || exists(dir, "build.gradle.kts") {
			p.Build.Image = "gradle:8-jdk21"
			command := "gradle --no-daemon bootJar"
			if exists(dir, "gradlew") {
				command = "chmod +x gradlew && ./gradlew --no-daemon bootJar"
			}
			p.Build.Commands = []string{command, "mkdir -p out && find build/libs -maxdepth 1 -name '*.jar' ! -name '*-plain.jar' -exec cp {} out/app.jar \\;"}
		} else if exists(dir, "mvnw") {
			p.Build.Commands[0] = "chmod +x mvnw && ./mvnw -B -DskipTests package"
		}
	case "php", "laravel":
		if exists(dir, "composer.json") {
			p.Build.Commands = []string{"apt-get update && apt-get install -y --no-install-recommends git unzip libzip-dev && docker-php-ext-install zip pdo_mysql && rm -rf /var/lib/apt/lists/*", "php -r \"copy('https://getcomposer.org/installer', '/tmp/composer-setup.php');\" && php /tmp/composer-setup.php --install-dir=/usr/local/bin --filename=composer", "COMPOSER_ALLOW_SUPERUSER=1 composer install --no-dev --prefer-dist --no-interaction"}
		}
		if id == "php" {
			if info, err := os.Stat(filepath.Join(dir, "public")); err == nil && info.IsDir() {
				p.Deploy.Command = "php -S 0.0.0.0:$PORT -t public"
			}
		}
	case "ruby":
		if !exists(dir, "Gemfile") {
			p.Build.Commands = []string{"gem install rack rackup webrick --no-document"}
			p.Deploy.Command = "rackup -s webrick -o 0.0.0.0 -p $PORT"
		} else {
			p.Build.Commands = append([]string{"apt-get update && apt-get install -y --no-install-recommends build-essential libyaml-dev && rm -rf /var/lib/apt/lists/*"}, p.Build.Commands...)
		}
	case "angular":
		p.Deploy.Artifact = "out"
		if len(p.Build.Commands) > 1 {
			separator := " "
			if strings.HasPrefix(p.Build.Commands[1], "npm ") {
				separator = " -- "
			}
			p.Build.Commands[1] += separator + "--output-path=out"
			p.Build.Commands = append(p.Build.Commands, angularBrowserOutput)
		}
	}
	return p
}

const angularBrowserOutput = "if [ -d out/browser ]; then cp -a out/browser/. out/ && rm -rf out/browser; fi"
