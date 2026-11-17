# Startship

Self-hosted deployment platform, single Go binary `startshipd`. A dark, responsive dashboard is embedded with Go embed.FS; no Node toolchain or external asset service is required. The same dashboard server also exposes a JSON API.

## Install on a server

On Linux amd64/arm64 with Docker Engine, Git, curl and systemd installed:

```sh
curl -fsSL https://raw.githubusercontent.com/sammwyy/startship/main/install.sh | sudo sh
journalctl -u startshipd
startship login http://localhost:8080
startship passwd
```

The installer verifies release SHA-256 checksums, installs `startship` and
`startshipd` in `/usr/local/bin`, and enables the daemon at boot. Re-run to upgrade;
set `STARTSHIP_VERSION=vX.Y.Z` to pin a release. Existing
`/etc/startship/startshipd.env` is preserved. Data is embedded **SQLite**, stored at
`$STARTSHIP_DATA/startship.db` (installer default `/var/lib/startship`); no external
database is needed. The service runs as `startship` with Docker access (equivalent
to root privileges) and permission to bind ports 80/443. Keep dashboard access
restricted to trusted networks or use HTTPS for remote login.

Download and inspect the script before executing it if desired. `sudo sh
install.sh --uninstall` removes the service and binaries but preserves data,
configuration and the service user. Without running systemd the installer writes
the unit but reports that it cannot start it. `STARTSHIP_DOWNLOAD_URL` can override
the release asset base URL for testing.

## CLI

Build locally with `go build -o startship ./cmd/startship`. The CLI talks to the
daemon's dashboard JSON API; it does not open the database or require Docker on
the client. `startship help` lists every command. For example:

```sh
startship login https://deploy.example.com
startship whoami
startship workspaces list
startship projects create website "admin's Workspace"
startship projects source website '{"git_url":"https://github.com/owner/repo","branch":"main"}'
startship projects env set website NODE_ENV=production
startship deploy website --wait
startship deployments list website --json
startship logs website
startship containers list website
startship routes add website CONTAINER_ID example.com /
startship traffic website --json
```

Project deployments map to API **releases** (combined build logs); container runtime
logs use `startship logs --container ID`. `projects update ID JSON` exposes project
settings, and `detect JSON` exposes source detection. Environment `set` replaces
the entire environment map (no pairs clears it). Use IDs when names are ambiguous.
`--json` returns API JSON; errors exit non-zero. Login and account updates store
the rotated session cookie in `$XDG_CONFIG_HOME/startship/config.json` (fallback
`~/.config/startship/config.json`), mode 0600. `STARTSHIP_SERVER` and
`STARTSHIP_TOKEN` override saved settings. Passwords are prompted, not passed as
command arguments; terminal echo is disabled when `stty` is available. Use
`passwd` to complete first-run setup, and `account update NAME` to rename yourself.
Roles, workspace membership and workspace domain policies are not added here.

## Run

Requires [Docker Engine](https://docs.docker.com/engine/install/) with Compose and [Git](https://git-scm.com/downloads); your user must be able to access the Docker daemon. Ports 80, 443 and 8080 must be free on the host.

```sh
git clone https://github.com/sammwyy/startship.git
cd startship
docker compose up --build -d
```

Run `docker compose logs startship` to find the prominent first-run credentials banner. Open `http://localhost:8080` (or `http://<server-ip>:8080`), sign in as `admin` with the printed random password, and set your own password (8–72 bytes). Setup is then marked complete and you land on workspaces. Restarting before this step rotates the temporary password and prints it again; use the latest banner. Treat these logs as sensitive. Every user starts with `<username>'s Workspace`. There is no public registration after setup, and no password reset, so keep your new password safe.

From there, use the sidebar's workspace menu to switch or create workspaces, then connect a project to **one Git repository**. Configure the **project**, not separate apps: source, build defaults, environment, tests, and push deployment are shared. Startship detects the runnable containers in that repository and manages them as **one project deployment**. Most projects have one container; a monorepo can have several HTTP servers.

Every project uses the same direct sidebar sections: **Deployments, Logs, Containers, Domains, Source, Environment, Settings**. There is no app selector, app creation step, per-app configuration, or per-container deploy action. Containers are a read-only runtime view with links to logs. Deployments show one release history and combined build output; Logs can show project build output or a container's runtime output. Connect a domain after the first deployment; a single container is selected automatically, while multiple containers offer an explicit target.

**All projects** returns to the selected workspace in one click. On narrow screens the navigation button opens a drawer. The bottom account menu includes username/password editing, the remembered light/dark theme choice, and sign out. Account changes require your current password, rotate this session, and invalidate other sessions. Environment variables use one KEY=VALUE per line.

Use a public link in **Domains** to visit a deployed project, not the dashboard or an internal container port. URLs include the configured router port and use HTTPS when ACME is enabled; behind a gateway, use its public URL.

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

To seed your own credentials instead, set `STARTSHIP_ADMIN_USER` and `STARTSHIP_ADMIN_PASSWORD` (uncomment them in `compose.yaml`); they only create the account when no user exists yet and mark setup complete without forcing a password change. Without them, first-run credentials are printed in the server logs (`docker compose logs startship`). `GET /api/setup` reports `completed` and the legacy `required` (no users) flag. `POST /api/setup` remains supported only when there are no users; normal daemon startup now creates the administrator automatically.

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

**Deploy project** checks out the configured branch once and detects its containers. Containers build and pass health checks before all active pointers and routes switch transactionally. If any container fails, staged runtimes are removed and the whole previous project deployment stays active. Deployments are queued per project.

**Build without cache** disables reusable executable build layers (base images may remain cached). **Redeploy selected** restarts all containers from a successful project release without cloning, building, or running tests, using the project's current shared environment and optional port override. Static releases share existing files. Artifact availability is checked for the entire selected release.

```text
POST /api/projects/<id>/deploy     {}
POST /api/projects/<id>/deploy     {"no_cache": true}
POST /api/projects/<id>/redeploy   {}
POST /api/projects/<id>/redeploy   {"deployment_id": "<project-release-id>"}
```

Redeploy requires a successful project release whose images/static directories still exist. Legacy individually deployed containers remain online during upgrade; the first project deployment establishes the grouped release history.

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
| STARTSHIP_ADMIN_USER / STARTSHIP_ADMIN_PASSWORD | empty (random admin credentials printed in logs; change on first sign-in) |
| STARTSHIP_DASHBOARD_DOMAIN | empty (dashboard only on port 8080) |
| STARTSHIP_SELF_CONTAINER | detected hostname inside Docker |

With `STARTSHIP_DASHBOARD_DOMAIN`, proxy requests for that hostname go to the dashboard and API. The daemon creates Docker network `startship`. Containerized startshipd must join that same network; compose configures it, and startup also attempts connection using `STARTSHIP_SELF_CONTAINER`. No application ports are published.

## API

All bodies and responses are JSON. Login using `POST /api/login {"username":"admin","password":"..."}`, keep the `startship_session` cookie. `POST /api/logout`, `GET /api/me`. Sessions expire after seven days.

`PATCH /api/me {"username":"new-name","current_password":"...","new_password":"..."}` changes the signed-in account. Supply a username, a new password, or both, plus the current password. Usernames must be unique and 1–100 bytes after trimming; new passwords must be 8–72 bytes (bcrypt's limit). Success returns `{id, username}`, invalidates all previous sessions, and sets a fresh session cookie so the caller stays signed in. This is not a password-reset flow: you must know the current password.

Ownership-scoped CRUD: `workspaces`, `projects`, and `routes`. Names are required except for routes. POST returns the new object including ID.

- workspace: `name`
- project: `workspace_id, name, git_url, git_token, branch, root_dir, build_type, port, env, test_command, auto_deploy`
- route: `project_id, container_id, host, path_prefix, strip_prefix`

Project defaults: branch `main`, root `.`, build strategy `auto`, port `0` (detect), automatic push deployment off. Env is a string map applied to all detected containers. Git tokens require HTTPS and are excluded from responses. Project responses include their signed webhook details.

Read-only runtime views: `GET /api/containers?project_id=...`, `GET /api/containers/{id}`, and `GET /api/containers/{id}/logs` (last 200 runtime lines; static containers have no serving process). Grouped history: `GET /api/releases?project_id=...&logs=false` for lightweight polling and `GET /api/releases/{id}` for combined logs and `container_releases` artifact IDs. Internal per-container build records remain readable under `deployments`. Legacy `apps` endpoints and `app_id` route fields remain for compatibility, not as dashboard configuration.

Upgrade promotes compatible legacy source/settings to the project without interrupting traffic. Conflicting sources require choosing one source. Different legacy environments/test commands set a configuration warning and block project deployment until the shared settings and environment have been reviewed and explicitly confirmed with `config_confirmed: true`. Existing container records retain their old values for reference until a healthy project deployment replaces them.

GitHub webhook: `POST /api/webhooks/github/{projectID}`, with HMAC SHA-256 signature and push events. A matching branch queues **one project release** when the project's `auto_deploy` is enabled; legacy signed container URLs also resolve to the project. No container-specific push switches are used by project deployments.

## Builds and routing

Auto detection checks the selected root first. A runnable root is one container; otherwise it descends up to three folder levels to discover server roots, ignoring dependency, hidden, and build directories. Workspace-only Node manifests are not servers. Detection recognizes Dockerfiles (including numeric EXPOSE ports), runnable Node projects, Python entrypoints, Go modules, Rust crates, and static index.html. Each detected root is an independent build context; complex shared-workspace builds need an appropriate Dockerfile. A manual project build strategy treats its selected root as a single container. Port defaults to 3000 for Node or 8080 otherwise, unless detected from EXPOSE or overridden project-wide. Builds within a project are serialized; separate projects can deploy concurrently.

Generated Node images select the highest matching major from 18, 20, 22, 24 and 26 using `engines.node` major ranges (including `||`, `>=`, `^` and exact majors); absent or unsupported ranges fall back to 22. They install dependencies (`npm ci` with a lockfile, otherwise `npm install`), run a build script if present, and use npm start. A Procfile `web:` command overrides the launch command for Node and Python. Python installs requirements or pyproject.toml when present, then selects main.py, app.py, or manage.py (Django runserver on 0.0.0.0:$PORT), in that order; without an entrypoint it requests a Procfile instead of guessing. Standalone Python entrypoint files are also detected. Go builds the root main package; Rust expects one executable target. All generated images set PORT; your app must bind on 0.0.0.0. Test commands execute inside a disposable image container with app env. Static apps copy their root without .git (symlinks rejected).

Static test commands run in a disposable Alpine image containing the site; no container runs to serve the site. Healthy means the application container accepts TCP connections within 60 seconds. After success, active deployment switches transactionally and the old container is removed. Failed builds preserve the previous deployment. Startup re-inspects running containers; interrupted builds are marked failed rather than replayed. Build checkouts are removed after completion. Deployment metadata, images, logs, and static files are retained; manage disk usage accordingly.

Routing matches case-insensitive host without port, then the longest segment-aware path prefix. `/api` matches `/api` and `/api/...`, but not `/apiary`. `/api/` matches paths beginning with `/api/`. On save, missing leading slashes are added and a trailing `*` is removed: enter an `example.com/api/*` style rule as hostname `example.com` and path prefix `/api/*` in **Connect domain**; it is saved as `/api/`. Do not put the hostname in the path field.

For a monorepo, connect `api.example.com` with prefix `/` to the backend, `example.com` with prefix `/` to the frontend, and `example.com` with prefix `/api/*` to the backend. Leave **Strip prefix before forwarding** unchecked if the backend handles `/api/health`. Enable it if the backend expects `/health` instead. `strip_prefix` removes the matched prefix and ensures a leading slash; stripping exact `/api` forwards `/`. Proxy supports WebSockets and supplies forwarded headers. Static sites support index.html fallback for client-side routes.

## Checks

```sh
go vet ./... && go test ./...
node --test scripts/navigation.test.cjs # optional frontend regression checks, Node 18+
./scripts/e2e.sh
```

E2E requires Python 3, curl, Git and real Docker. It also covers a two-HTTP-container project release, shared config, container domain targets, grouped failure preservation, artifact redeploy, and restart recovery. Uses localhost ports 18080/18081, creates a local Dockerfile + Python + Rust + static monorepo, auto-detects and deploys all four through the API, tests route URLs, prefix proxying and restart recovery, and removes its containers, images, and data afterward. Docker network startship is retained.
