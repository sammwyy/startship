# Startship

Self-hosted deployment platform, single Go binary `startshipd`. A dark, responsive dashboard is embedded with Go embed.FS; no Node toolchain or external asset service is required. The same dashboard server also exposes a JSON API.

## Run

Requires Go 1.26+, Docker Engine and Git. No C toolchain required.

```sh
go build -o startshipd .
# Native quick start: use an unprivileged router port (dashboard stays on 8080).
STARTSHIP_HTTP_ADDR=:8081 ./startshipd
```

Open `http://localhost:8080` and create your administrator account. The dashboard
is on port **8080**; deployed apps are served by the separate router on port
**8081** in this quick start. Route `localhost` plus a path such as `/site/` to
an app, then visit `http://localhost:8081/site/`. For a remote server, use its
hostname instead and point DNS at it. The app's internal port is not published.

On Linux, the default router ports 80/443 require permission to bind privileged
ports. To use those defaults without running the daemon as root:

```sh
sudo setcap 'cap_net_bind_service=+ep' ./startshipd
./startshipd
```

Reapply the capability after rebuilding or replacing the binary. Alternatively,
keep `STARTSHIP_HTTP_ADDR=:8081` (and, when using ACME,
`STARTSHIP_HTTPS_ADDR=:8443`) and forward public ports 80/443 at your gateway.
ACME validation still needs public port 80. Docker socket access is also required;
check `docker info` works as the user running Startship.

Or use Docker Compose (no host Go installation required):

```sh
docker compose up --build -d
```

Visit `GET /api/setup` on port 8080. If no seed user exists, `POST /api/setup` with `{"username":"admin","password":"at-least-eight-characters"}`. Setup closes once a user exists. Every user gets `<username>'s Workspace`. No public registration after setup.

In the dashboard, first-run setup and login guide you into your workspaces. Create a project, add a Git application (or detect monorepo apps), deploy it, and connect a domain. Open the route's URL to visit the app through the router, not the dashboard or internal container port. URLs use the configured router port and HTTPS when ACME is enabled; if a gateway maps different external ports, use your gateway's public URL instead. Application pages include live build logs, editable settings, and copyable GitHub webhook credentials. Environment variables use one KEY=VALUE per line.

**Security:** Startship controls the Docker socket and can execute arbitrary repository code. Treat every dashboard user and repository as a server administrator, not as an untrusted tenant. Do not expose initial setup publicly. Terminate dashboard TLS at a trusted gateway or use `STARTSHIP_DASHBOARD_DOMAIN` and ACME. Restrict direct access to dashboard port 8080. Cookies are HttpOnly/SameSite=Lax and Secure on HTTPS. Build logs may include secrets printed by application commands: do not print credentials. File URLs are allowed intentionally for local deployment.

## Configuration

| Environment | Default |
|---|---|
| STARTSHIP_DATA | ./data |
| STARTSHIP_DASHBOARD_ADDR | :8080 |
| STARTSHIP_HTTP_ADDR | :80 |
| STARTSHIP_HTTPS_ADDR | :443 |
| STARTSHIP_ACME_EMAIL | empty (HTTPS disabled) |
| STARTSHIP_ADMIN_USER / STARTSHIP_ADMIN_PASSWORD | empty (first-run setup) |
| STARTSHIP_DASHBOARD_DOMAIN | empty |
| STARTSHIP_SELF_CONTAINER | detected hostname inside Docker |

Point DNS at the server; allow inbound 80/443. ACME only authorizes hosts configured in routes. With `STARTSHIP_DASHBOARD_DOMAIN`, matching proxy requests go to the API; add that hostname as a route as well if using ACME. The daemon creates Docker network `startship`. Containerized startshipd must join that same network; compose configures it, and startup also attempts connection using `STARTSHIP_SELF_CONTAINER`. No application ports are published.

## API

All bodies and responses are JSON. Login using `POST /api/login {"username":"admin","password":"..."}`, keep the `startship_session` cookie. `POST /api/logout`, `GET /api/me`. Sessions expire after seven days.

Ownership-scoped `GET/POST /api/{workspaces,projects,apps,routes}` and `GET/PUT/PATCH/DELETE /api/{collection}/{id}`. Delete children before parents. POST returns the new object including ID. Fields:

- workspace: `name`
- project: `workspace_id, name`
- app: `project_id, name, git_url, git_token, branch, root_dir, build_type, port, env, test_command, auto_deploy`
- route: `project_id, app_id, host, path_prefix, strip_prefix`

Defaults: branch `main`, root `.`, build type `auto`, port 8080, path prefix `/`. Env is a JSON object of strings. Git tokens are accepted only with HTTPS and excluded from API responses. API app responses include a random webhook secret.

`POST /api/apps/{id}/deploy {}` queues a build. `GET /api/deployments?app_id=...` and `GET /api/deployments/{id}` include persisted incremental logs. Lists also support `project_id` and `workspace_id` filters.

`POST /api/detect {"git_url":"https://github.com/owner/repo","branch":"main","git_token":"optional"}` clones and scans root and immediate child directories. It returns `root_dir, build_type, port` candidates; create apps from these via the apps endpoint.

GitHub webhook: `POST /api/webhooks/github/{appID}`; set the app's webhook secret in GitHub, select push events. HMAC SHA-256 signatures are required. Only pushes to the configured branch with `auto_deploy: true` enqueue builds.

## Builds and routing

Auto detection order: Dockerfile, Node package.json, Python requirements.txt/pyproject.toml, Go go.mod, Rust Cargo.toml, static index.html. Each app builds serially; different apps build concurrently. Repositories are cloned shallowly at the configured branch. Roots cannot escape the checkout. For complex build workflows supply a Dockerfile.

Generated Node images install dependencies (npm ci with lockfile), run a build script if present, and use npm start. A Procfile `web:` command overrides the launch command for Node and Python. Python installs requirements or pyproject.toml when present, then selects main.py, app.py, or manage.py (Django runserver on 0.0.0.0:$PORT), in that order; without an entrypoint it requests a Procfile instead of guessing. Standalone Python entrypoint files are also detected. Go builds the root main package; Rust expects one executable target. All generated images set PORT; your app must bind on 0.0.0.0. Test commands execute inside a disposable image container with app env. Static apps copy their root without .git (symlinks rejected).

Static test commands run in a disposable Alpine image containing the site; no container runs to serve the site. Healthy means the application container accepts TCP connections within 60 seconds. After success, active deployment switches transactionally and the old container is removed. Failed builds preserve the previous deployment. Startup re-inspects running containers; interrupted builds are marked failed rather than replayed. Build checkouts are removed after completion. Deployment metadata, images, logs, and static files are retained; manage disk usage accordingly.

Routing matches case-insensitive host without port, then longest literal path prefix. `/api/` avoids accidentally matching `/apiculture`; use `/api` if that literal behavior is desired. `strip_prefix` removes the matched prefix. Proxy supports WebSockets and supplies forwarded headers. Static sites support index.html fallback for client-side routes.

## Checks

```sh
go vet ./... && go test ./...
./scripts/e2e.sh
```

E2E requires Python 3, curl, Git and real Docker. Uses localhost ports 18080/18081, creates a local Dockerfile + Python + Rust + static monorepo, auto-detects and deploys all four through the API, tests route URLs, prefix proxying and restart recovery, and removes its containers, images, and data afterward. Docker network startship is retained.
