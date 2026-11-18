package pipeline

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestEngineDockerBuilds(t *testing.T) {
	if os.Getenv("STARTSHIP_PIPELINE_BUILDS") != "1" {
		t.Skip("set STARTSHIP_PIPELINE_BUILDS=1 to build real engine fixtures")
	}
	for _, tc := range []struct {
		id    string
		files map[string]string
	}{
		{"spring", map[string]string{
			"pom.xml":                        `<project xmlns="http://maven.apache.org/POM/4.0.0"><modelVersion>4.0.0</modelVersion><parent><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-parent</artifactId><version>3.5.3</version></parent><groupId>example</groupId><artifactId>app</artifactId><version>1</version><properties><java.version>21</java.version></properties><dependencies><dependency><groupId>org.springframework.boot</groupId><artifactId>spring-boot-starter-web</artifactId></dependency></dependencies><build><plugins><plugin><groupId>org.springframework.boot</groupId><artifactId>spring-boot-maven-plugin</artifactId></plugin></plugins></build></project>`,
			"src/main/java/example/App.java": `package example; @org.springframework.boot.autoconfigure.SpringBootApplication public class App { public static void main(String[] args) { org.springframework.boot.SpringApplication.run(App.class,args); } }`,
		}},
		{"php", map[string]string{"index.php": `<?php echo "fixture ready";`}},
		{"ruby", map[string]string{"config.ru": `run ->(env) { [200, {"content-type" => "text/plain"}, ["fixture ready"]] }`}},
		{"dotnet", map[string]string{"App.csproj": `<Project Sdk="Microsoft.NET.Sdk.Web"><PropertyGroup><TargetFramework>net10.0</TargetFramework><ImplicitUsings>enable</ImplicitUsings></PropertyGroup></Project>`, "Program.cs": `var app = Microsoft.AspNetCore.Builder.WebApplication.Create(args); app.MapGet("/", () => "fixture ready"); app.Run();`}},
		{"deno", map[string]string{"deno.json": `{"tasks":{"start":"deno run --allow-net --allow-env main.ts"}}`, "main.ts": `Deno.serve({port: Number(Deno.env.get("PORT")) || 8080}, () => new Response("fixture ready"));`}},
		{"hugo", map[string]string{"hugo.toml": `baseURL = 'https://example.com/'`, "layouts/index.html": `<!doctype html><h1>fixture ready</h1>`}},
	} {
		t.Run(tc.id, func(t *testing.T) {
			dir := t.TempDir()
			for name, body := range tc.files {
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.id == "spring" && os.Getenv("STARTSHIP_MAVEN_MIRROR") != "" {
				if err := os.MkdirAll(filepath.Join(dir, ".mvn"), 0700); err != nil {
					t.Fatal(err)
				}
				settings := `<settings><mirrors><mirror><id>fixture-mirror</id><mirrorOf>central</mirrorOf><url>` + os.Getenv("STARTSHIP_MAVEN_MIRROR") + `</url></mirror></mirrors></settings>`
				if err := os.WriteFile(filepath.Join(dir, ".mvn/settings.xml"), []byte(settings), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, ".mvn/maven.config"), []byte("-s\n.mvn/settings.xml\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			p, err := Resolve(tc.id, dir)
			if err != nil {
				t.Fatal(err)
			}
			dockerfile, err := p.Dockerfile()
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(dockerfile), 0600); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("docker", "buildx", "build", "--load", "--progress=plain", "-t", "startship-fixture-"+tc.id, dir)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("%s\n%v", output, err)
			}
			t.Logf("%s generated Dockerfile: BUILD PASS", tc.id)
		})
	}
}
