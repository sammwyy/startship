# Startship

Self-hosted deployment platform, single Go binary `startshipd`. A dark, responsive dashboard is embedded with Go embed.FS; no Node toolchain or external asset service is required. The same dashboard server also exposes a JSON API.

## Run

Requires Docker Engine with Compose. Ports 80, 443 and 8080 must be free on the host.

```sh
git clone https://github.com/sammwyy/startship && cd startship
docker compose up --build -d
```

Open `http://localhost:8080` (or `http://<server-ip>:8080`). The first visit asks you to create the administrator account (password of at least 8 characters); you are then signed in and land on your workspaces. Every user starts with `<username>'s Workspace`. There is no public registration after setup, and no password reset, so keep the password safe.

From there: open a workspace (or create one with **+ New workspace**; the sidebar selector switches between them), create a project, add a Git application (or detect monorepo apps), deploy it, and connect a domain. Application pages include live build logs, editable settings, and copyable GitHub webhook credentials. Environment variables use one KEY=VALUE per line. Sign out with the button next to your username at the bottom of the sidebar.

Without Docker Compose, build the binary (Go 1.26+, Git and Docker Engine required; no C toolchain):

```sh
go build -o startshipd .
./startshipd   # listens on :8080 (dashboard), :80 and :443; port 80 usually needs root
```

To skip the first-run screen, set `STARTSHIP_ADMIN_USER` and `STARTSHIP_ADMIN_PASSWORD` (uncomment them in `compose.yaml`); they only create the account when no user exists yet. Over the API, `GET /api/setup` returns `{"required":true}` until the first account exists, created with `POST /api/setup {"username":"admin","password":"at-least-eight-characters"}`.

## HTTPS

Startship gets Let's Encrypt certificates itself:

1. Point DNS A records for the dashboard domain and every app host at the server; allow inbound 80 and 443.
2. In `compose.yaml`, uncomment `STARTSHIP_ACME_EMAIL` and `STARTSHIP_DASHBOARD_DOMAIN`, then run `docker compose up -d`.
3. Open `https://<dashboard domain>`. Plain HTTP requests to that domain are redirected to HTTPS.

Certificates are only issued for the dashboard domain and hosts configured in routes; app hosts keep answering on plain HTTP too. Certificates are cached in the data volume.

Alternatively, terminate TLS at a trusted gateway in front of port 8080 (or 80 with `STARTSHIP_DASHBOARD_DOMAIN`) and leave `STARTSHIP_ACME_EMAIL` empty; the gateway must forward `X-Forwarded-Proto: https` so session cookies are marked Secure.

**Security:** Startship controls the Docker socket and can execute arbitrary repository code. Treat every dashboard user and repository as a server administrator, not as an untrusted tenant. Do not expose initial setup publicly: finish it right after starting, or seed the admin account through the environment. Once HTTPS is configured, restrict direct access to dashboard port 8080 (for example with a firewall, or by changing the compose mapping to `127.0.0.1:8080:8080`). Cookies are HttpOnly/SameSite=Lax and Secure on HTTPS. Build logs may include secrets printed by application commands: do not print credentials. File URLs are allowed intentionally for local deployment.

## Configuration

| Environment | Default |
|---|---|
| STARTSHIP_DATA | ./data |
| STARTSHIP_DASHBOARD_ADDR | :8080 |
| STARTSHIP_HTTP_ADDR | :80 |
| STARTSHIP_HTTPS_ADDR | :443 |
| STARTSHIP_ACME_EMAIL | empty (HTTPS disabled) |
| STARTSHIP_ADMIN_USER / STARTSHIP_ADMIN_PASSWORD | empty (first-run setup) |
| STARTSHIP_DASHBOARD_DOMAIN | empty (dashboard only on port 8080) |
| STARTSHIP_SELF_CONTAINER | detected hostname inside Docker |

With `STARTSHIP_DASHBOARD_DOMAIN`, proxy requests for that hostname go to the dashboard and API. The daemon creates Docker network `startship`. Containerized startshipd must join that same network; compose configures it, and startup also attempts connection using `STARTSHIP_SELF_CONTAINER`. No application ports are published.

## API

All bodies and responses are JSON. Login using `POST /api/login {"username":"admin","password":"..."}`, keep the `startship_session` cookie. `POST /api/logout`, `GET /api/me`. Sessions expire after seven days.

Names are required and trimmed. Ownership-scoped `GET/POST /api/{workspaces,projects,apps,routes}` and `GET/PUT/PATCH/DELETE /api/{collection}/{id}`. Delete children before parents. POST returns the new object including ID. Fields:

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

E2E requires Python 3, curl, Git and real Docker. Uses localhost ports 18080/18081, creates a local Python + static monorepo, deploys both through the API, tests longest-prefix proxying and restart recovery, and removes its containers, images, and data afterward. Docker network startship is retained.
