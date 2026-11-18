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

Build supports `engine` (`docker`, `nixpacks`, or `buildpacks`; empty means `docker`),
`image`, `commands`, or `dockerfile` (a repository-relative path
which replaces generated builds). Test supports an optional separate `image`,
`commands`, `expect_exit_code` (default 0), and `expect_output` (required substring).
Dockerfile pipelines run tests in the Dockerfile's final image.
Deploy supports `image` (empty reuses build), `artifact` (safe relative path,
default `.`), `static`, `command`, `port`, `health_path`, `expect_status`, and `health_timeout`.
Separate runtime images receive the build artifact at `/app`. Static artifacts
are served directly. Health checks use TCP unless a path is supplied; HTTP then
expects status 200 unless overridden.

Auto detects a template per container, overlaying only the project's test stage,
build engine, nonzero deploy port and HTTP health settings. Explicit templates/custom use one
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

## Build engines

The engine is project-owned and snapshotted with the release. Auto remains the
default strategy; changing the engine does not change strategy or create per-app
configuration. Nixpacks and Buildpacks ignore Build image, commands and Dockerfile.
Test and Deploy settings still apply. The generated engine image retains its start
command unless `deploy.command` overrides it. Build fields are retained so switching
back to Docker restores them.

In Auto with a non-Docker engine, detection supplies container roots and the
default port, not Docker-template runtime images/commands. Project Test and
Deploy overrides still apply; otherwise the engine supplies the runtime.

Nixpacks **1.41.0** is installed on the daemon host by `install.sh`, and bundled
in the daemon Docker image (checksum-verified musl binaries for amd64/arm64).
For a manually built daemon, download
`https://github.com/railwayapp/nixpacks/releases/download/v1.41.0/nixpacks-v1.41.0-x86_64-unknown-linux-musl.tar.gz`,
verify SHA256 `0f55de7874507b9cf7502113120bd96f2ab6979f78d10eaf2eb2ade9207b3af6`,
and extract `nixpacks` onto the daemon's PATH. For arm64 substitute `aarch64` and
SHA256 `912bd02dd2bb6f9c3a9ed965fe8a68b4aa318dc7a2546e2eca6f2806a894ba39`.
`STARTSHIP_NIXPACKS` can select an absolute binary path; the version is checked.
Plan and asset generation each have a two-minute timeout. Generated assets are
staged with a copy of the source; plan variables become build arguments, with a
stable project cache key. Project runtime secrets are not passed to the planner
or baked into the image. This is not a mechanism for build-time secrets.

Buildpacks requires no host `pack`, crane or registry service. The daemon stages
the run image as OCI data, then executes the CNB lifecycle inside the same bounded
buildx worker as Docker/Nixpacks. No Docker socket is exposed to the lifecycle.
The final app OCI layout is completed with run-image blobs, imported and tagged;
CNB launcher/metadata are preserved. The tested pins are:

- Builder: `mirror.gcr.io/paketobuildpacks/builder-jammy-base@sha256:5cd813b834965d9b9daf4d94b35d22bd76f743961dd5cfa7f141804bc4cef815`
- Run image: `mirror.gcr.io/paketobuildpacks/run-jammy-base@sha256:1c2aa9aac21b5e77230c4e1721ffe1c764c62c4c6f6948e2668a7e28bb73add4`
- Embedded lifecycle: **0.21.29**, Platform API **0.12**.

This builder is linux/amd64 only. OCI layout export is experimental in the pinned
lifecycle; pins must be updated/tested together. Its run-image reference in layout
metadata uses `:latest`, but fetched bytes are always pinned to the digest above.
Buildpack caches are project-specific BuildKit cache mounts. No-cache builds skip
restoration and bypass that mount. All engine builds retain project locking,
CPU/memory limits and BuildKit pruning. Plan/OCI staging is outside the worker;
image downloads and Docker imports, like ordinary `--load`, are not compilation.

Tests run in the engine image, with inherited entrypoints replaced (CNB's launcher
still sets up layer environments). A separate Test image receives application
files at `/app` and must supply its own tools. A separate Deploy image similarly
receives the artifact at `/app` and must supply its own runtime/start command.
Artifacts are relative to `/app` for Nixpacks or `/workspace` for Buildpacks.
Static extraction, when explicitly configured, uses that same application path.

`STARTSHIP_E2E_ENGINES=1 ./scripts/e2e.sh` additionally exercises real Node engine
deployments, tests, command override, snapshots, worker limits and proxy responses.
This is opt-in: pinned host tooling and large CNB/Nix downloads are unsuitable for
the default 20-minute CI job. Unit/API tests always cover engine configuration.
To mirror Docker Hub for Docker/Nixpacks stages, configure the daemon user's
`~/.docker/buildx/buildkitd.default.toml`:

```toml
[registry."docker.io"]
  mirrors = ["mirror.gcr.io"]
```
