# Startship

Self-hosted deployment platform, single Go binary `startshipd`. A dark, responsive dashboard is embedded with Go embed.FS; no Node toolchain or external asset service is required. The same dashboard server also exposes a JSON API.

## Run

Requires [Docker Engine](https://docs.docker.com/engine/install/) with Compose and [Git](https://git-scm.com/downloads); your user must be able to access the Docker daemon. Ports 80, 443 and 8080 must be free on the host.

```sh
git clone https://github.com/sammwyy/startship.git
cd startship
docker compose up --build -d
```

Open `http://localhost:8080` (or `http://<server-ip>:8080`). The first visit asks you to create the administrator account (password of at least 8 characters); you are then signed in and land on your workspaces. Every user starts with `<username>'s Workspace`. There is no public registration after setup, and no password reset, so use a strong, unique password and keep it safe.

From there: open a workspace (or create one with **+ New workspace**; the top-bar selector switches between them) and create a project connected to **one Git repository**. The project owns the repository, branch, credentials, and GitHub webhook. Choose a single application to build the repository root, or a monorepo to define frontend, backend, or daemon apps in folders of that same repository. Apps only configure their root directory, build, port, variables, and deployment behavior; they cannot select another source. Deploy an app and connect a domain to expose it.

Projects open directly into their application's controls. Multi-app projects add an application switcher; single-app projects skip that extra navigation. Both use the same deployments, domains, app settings, and **Project Git** tabs, with live build logs and the project's copyable webhook credentials. **Project source** edits the source shared by every app. Environment variables use one KEY=VALUE per line. Sign out next to your avatar in the top-right corner. The theme button remembers your light/dark choice.

Use **Open app** or a public endpoint in the application's Deployments or Domains tab to visit the deployed app, not the
dashboard or internal container port. URLs use the configured router port and
HTTPS when ACME is enabled; if a gateway maps different external ports, use your
gateway's public URL instead.

Without Docker Compose, build the binary ([Go 1.26+](https://go.dev/dl/), Git and Docker Engine required; no C toolchain):

```sh
go build -o startshipd .
STARTSHIP_HTTP_ADDR=:8081 ./startshipd
```

The proxy defaults to `:80`, which requires root or `CAP_NET_BIND_SERVICE` on Linux;
the command above uses unprivileged port **8081** instead, and the dashboard stays
on **8080**. Route `localhost` plus a path such as `/site/` to an app, then visit
`http://localhost:8081/site/`. For a remote server, use its hostname and point DNS
at it. The app's internal port is not published. Check `docker info` works as the
user running Startship.

To use standard ports without running the daemon as root, grant the binary the
Linux bind capability (or use Docker Compose):

```sh
sudo setcap 'cap_net_bind_service=+ep' ./startshipd
./startshipd
```

Reapply the capability after rebuilding or replacing the binary. Alternatively,
keep `STARTSHIP_HTTP_ADDR=:8081` (and, when using ACME,
`STARTSHIP_HTTPS_ADDR=:8443`) and forward public ports 80/443 at your gateway.
ACME validation still needs public port 80. HTTPS on default `:443` also needs
privileged-port access.

To skip the first-run screen, set `STARTSHIP_ADMIN_USER` and `STARTSHIP_ADMIN_PASSWORD` (uncomment them in `compose.yaml`); they only create the account when no user exists yet. Over the API, `GET /api/setup` returns `{"required":true}` until the first account exists, created with `POST /api/setup {"username":"admin","password":"at-least-eight-characters"}`.

When `STARTSHIP_DATA` is unset, existing `./data` is preserved; otherwise new installations default to `./_data`. The underscore makes Go tooling skip the data directory even when unreadable.

## Container isolation and limits

Each project uses a private bridge network named `startship-<projectID>`. App
and test containers join only that network, with a lowercase, dash-separated
app-name alias (use distinct names within a project). Siblings can connect by
alias and internal port; other projects cannot connect directly. Internet egress
is retained. Deployments persist their network; older deployments retain the
legacy `startship` network until redeployed. A containerized daemon joins project
networks on deploy and restart (`STARTSHIP_SELF_CONTAINER` overrides auto-detection).

Set these environment variables on **startshipd** (add them to Compose's
`environment` section when using Compose):

| Variable | Default | Purpose |
| --- | --- | --- |
| `STARTSHIP_APP_MEMORY` | `512m` | App and test memory limit |
| `STARTSHIP_APP_CPUS` | `1` | App and test CPU quota |
| `STARTSHIP_APP_PIDS` | `512` | App and test process limit |
| `STARTSHIP_BUILD_MEMORY` | `1g` | BuildKit worker memory limit |
| `STARTSHIP_BUILD_CPUS` | `1` | BuildKit worker CPU quota |
| `STARTSHIP_BUILD_CACHE` | `5g` | Best-effort retained build cache per project |

Builds require the Docker buildx plugin (included in `docker:29-cli`). Each project
reuses a persistent `docker-container` BuildKit builder with memory and CPU
driver options and `--load`; ordinary `docker build --memory` is not used.
Builds in one project are serialized, including builder creation and cache
pruning; different projects can build concurrently. Changing build memory/CPU
limits replaces the project's builder and its cache on the next build. After
each build, best-effort pruning keeps cache near `STARTSHIP_BUILD_CACHE` (not a
hard disk quota). Project deletion removes its builder and network. Invalid
limits fail the deployment. Apps and tests drop all capabilities except CHOWN, SETUID, SETGID,
and NET_BIND_SERVICE and enable no-new-privileges. Their root filesystem remains
writable; no host mounts or privileged mode are granted to them.

This is defense in depth, **not a sandbox for hostile tenants**. The daemon's
Docker socket grants host-level control; BuildKit workers use Docker's privileged
build infrastructure. Apps can still reach the host gateway and exposed daemon
proxy/dashboard, including publicly routed apps of other projects. Keep the
dashboard authenticated and firewalled. Containers share the host kernel;
legacy releases remain on the shared network until redeployed.

### Deploy, rebuild, or redeploy

**Deploy now** builds the project's current branch with cached layers.
**Rebuild without cache** rebuilds without reusing executable build layers (base
images and metadata-only steps may still be cached by BuildKit). **Redeploy** starts the active release without cloning, building,
or running tests, using the app's current environment and port. The release
inspector's **Redeploy this release** can roll back to an older successful release
while its image or static directory still exists. All container starts retain the
same limits, capabilities, project network, and health check. A failed start keeps
the old active release. Static redeploys share the existing static files without
deleting them when the previous release is superseded.

API equivalents:

```text
POST /api/apps/<id>/deploy     {}                   # cached build
POST /api/apps/<id>/deploy     {"no_cache": true}   # uncached build
POST /api/apps/<id>/redeploy   {}                   # active release
POST /api/apps/<id>/redeploy   {"deployment_id": "<release-id>"}
```

Redeploy returns HTTP 409 if there is no active release, the selected release was
not successful, or its image/static directory is gone; rebuild instead. The new
deployment records `redeploy_of` and copies the source commit, and still activates
atomically. Deployments are queued per app as before.

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
| STARTSHIP_DATA | ./_data |
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
- project: `workspace_id, name, git_url, git_token, branch`
- app: `project_id, name, root_dir, build_type, port, env, test_command, auto_deploy`
- route: `project_id, app_id, host, path_prefix, strip_prefix`

Projects accept `git_url, git_token, branch` in addition to their name and workspace. Apps inherit that source; setting source fields on an app is rejected. Defaults: project branch `main`, app root `.`, build type `auto`, port 8080, path prefix `/`. Env is a JSON object of strings. Tokens are used only with HTTPS and excluded from API responses. API project responses include the shared webhook secret.

On upgrade, an existing source is promoted from apps to their project only when all populated app sources agree. Conflicting legacy repositories/branches/credentials are flagged for an explicit choice in **Project source**, without deleting apps or interrupting active deployments. Builds are blocked until that choice is made. Existing app-specific signed webhook URLs remain compatible but use the project's branch and source.

`POST /api/apps/{id}/deploy {}` queues a build. `GET /api/deployments?app_id=...` and `GET /api/deployments/{id}` include persisted incremental logs. Lists also support `project_id` and `workspace_id` filters.

`POST /api/detect {"project_id":"..."}` clones the authenticated project's shared source and scans its root and immediate child directories. It returns `root_dir, build_type, port` candidates; create apps from these via the apps endpoint without repeating source fields.

GitHub webhook: `POST /api/webhooks/github/{projectID}`; set the project's webhook secret in GitHub and select push events. HMAC SHA-256 signatures are required. Pushes to the project's branch enqueue builds for its apps with `auto_deploy: true`. One project webhook covers all app folders in that repository.

## Builds and routing

Auto detection order: Dockerfile, Node package.json, Python requirements.txt/pyproject.toml, Go go.mod, Rust Cargo.toml, static index.html. Each app builds serially; different apps build concurrently. Repositories are cloned shallowly at the configured branch. Roots cannot escape the checkout. For complex build workflows supply a Dockerfile.

Generated Node images select the highest matching major from 18, 20, 22, 24 and 26 using `engines.node` major ranges (including `||`, `>=`, `^` and exact majors); absent or unsupported ranges fall back to 22. They install dependencies (`npm ci` with a lockfile, otherwise `npm install`), run a build script if present, and use npm start. A Procfile `web:` command overrides the launch command for Node and Python. Python installs requirements or pyproject.toml when present, then selects main.py, app.py, or manage.py (Django runserver on 0.0.0.0:$PORT), in that order; without an entrypoint it requests a Procfile instead of guessing. Standalone Python entrypoint files are also detected. Go builds the root main package; Rust expects one executable target. All generated images set PORT; your app must bind on 0.0.0.0. Test commands execute inside a disposable image container with app env. Static apps copy their root without .git (symlinks rejected).

Static test commands run in a disposable Alpine image containing the site; no container runs to serve the site. Healthy means the application container accepts TCP connections within 60 seconds. After success, active deployment switches transactionally and the old container is removed. Failed builds preserve the previous deployment. Startup re-inspects running containers; interrupted builds are marked failed rather than replayed. Build checkouts are removed after completion. Deployment metadata, images, logs, and static files are retained; manage disk usage accordingly.

Routing matches case-insensitive host without port, then the longest segment-aware path prefix. `/api` matches `/api` and `/api/...`, but not `/apiary`. `/api/` matches paths beginning with `/api/`. On save, missing leading slashes are added and a trailing `*` is removed: enter an `example.com/api/*` style rule as hostname `example.com` and path prefix `/api/*` in **Connect domain**; it is saved as `/api/`. Do not put the hostname in the path field.

For a monorepo, connect `api.example.com` with prefix `/` to the backend, `example.com` with prefix `/` to the frontend, and `example.com` with prefix `/api/*` to the backend. Leave **Strip prefix before forwarding** unchecked if the backend handles `/api/health`. Enable it if the backend expects `/health` instead. `strip_prefix` removes the matched prefix and ensures a leading slash; stripping exact `/api` forwards `/`. Proxy supports WebSockets and supplies forwarded headers. Static sites support index.html fallback for client-side routes.

## Checks

```sh
go vet ./... && go test ./...
./scripts/e2e.sh
```

E2E requires Python 3, curl, Git and real Docker. Uses localhost ports 18080/18081, creates a local Dockerfile + Python + Rust + static monorepo, auto-detects and deploys all four through the API, tests route URLs, prefix proxying and restart recovery, and removes its containers, images, and data afterward. Docker network startship is retained.
