# Project pipelines

Every project uses one pipeline for all detected containers. `build_type` defaults
to `auto`; it can also be a template ID or `custom` (which requires `pipeline`).
`GET /api/pipeline/templates` returns an array of
`{id, name, description, icon, category, pipeline}` for signed-in users.
`POST /api/detect` includes `template` and the resolved `pipeline` on each candidate.

PATCH a project with, for example:

```json
{
  "build_type": "custom",
  "pipeline": {
    "build": {"image": "node:22-alpine", "commands": ["npm ci", "npm run build"]},
    "test": {"commands": ["npm test"], "expect_exit_code": 0, "expect_output": "passed"},
    "deploy": {"command": "npm start", "port": 3000, "health_path": "/health", "expect_status": 200, "health_timeout": 90}
  }
}
```

Build supports `image`, `commands`, or `dockerfile` (a repository-relative path
which replaces generated builds). Test supports an optional separate `image`,
`commands`, `expect_exit_code` (default 0), and `expect_output` (required substring).
Dockerfile pipelines run tests in the Dockerfile's final image.
Deploy supports `image` (empty reuses build), `artifact` (safe relative path,
default `.`), `static`, `command`, `port`, `health_path`, `expect_status`, and `health_timeout`.
Separate runtime images receive the build artifact at `/app`. Static artifacts
are served directly. Health checks use TCP unless a path is supplied; HTTP then
expects status 200 unless overridden.

Auto detects a template per container, overlaying only the project's test stage,
nonzero deploy port and HTTP health settings. Explicit templates/custom use one
container at `root_dir`. Without a pipeline, legacy `test_command` and `port`
still apply. Releases snapshot configuration and retain resolved pipelines for
redeploy health checks.

## Health checks

`deploy.health_timeout` sets the readiness wait in seconds (0–600). Zero or omitted
keeps the 60-second default. An empty `health_path` checks TCP on the deployment
port; otherwise Startship sends HTTP GET requests until `expect_status` (default
200) is returned. Static artifacts do not use container health checks.

## Additional engines

The registry includes Spring Boot (Maven or Gradle, preferring repository wrappers),
PHP, Laravel, Ruby/Rack, Rails, ASP.NET Core, Phoenix, Deno, Angular, Remix/React
Router, NestJS and Hugo. Framework dependencies take priority over generic language
manifests; `deno.json`/`deno.jsonc` take priority over Node, while Dockerfile remains
the highest-priority explicit container definition. Review generated commands in
the editor for application-specific assets, database migrations or runtime secrets.
