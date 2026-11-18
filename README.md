# Startship

Self-hosted deployment platform, single Go binary `startshipd`. A dark, responsive dashboard is embedded with Go embed.FS; no Node toolchain or external asset service is required. The same dashboard server also exposes a JSON API.

## Install on a server

On Linux amd64/arm64 with Docker Engine, Git, curl and systemd installed:

```sh
curl -fsSL https://raw.githubusercontent.com/sammwyy/startship/main/install.sh | sudo sh
journalctl -u startshipd
sudo startship whoami
sudo startship workspaces list
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

## Updates

The daemon checks GitHub releases at start and every 12 hours, logs when a newer
version exists, and reports it at `GET /api/system/update` (`?refresh=1` checks
now). Set `STARTSHIP_UPDATE_CHECK=off` to disable the background check.
Unauthenticated GitHub API calls share a limit of 60 per hour per IP, so behind a
shared or NAT'd address the check falls back to the `releases/latest` page
redirect (version only, no notes). Set `STARTSHIP_GITHUB_TOKEN` (any token; no
scopes needed for this public repo) in `/etc/startship/startshipd.env` to
authenticate the API instead. Installing a release never uses the API.

```sh
startship update check          # CLI, daemon and latest release versions
sudo startship update           # install the latest CLI + daemon and restart the service
sudo startship update v1.2.3    # install a specific release (also downgrades)
startship update daemon         # ask the connected (local or remote) daemon to update itself
```

`startship update` replaces `startship` and the `startshipd` beside it after
verifying `checksums.txt`, and restarts `startshipd.service` when it is running.
The daemon runs unprivileged and cannot replace its own binaries, so
`POST /api/system/update` (Platform Admins; optional `{"version":"vX.Y.Z"}`,
default latest) writes `$STARTSHIP_DATA/update/request.json`; the installer's
root-only `startship-update.path` unit sees it and runs `startship update
apply-request`, which installs that release, writes `update/status.json`
(`state` running, succeeded or failed) and restarts the daemon. Installs from
before this feature get the updater by re-running the installer once.

| Endpoint | Access | Response |
| --- | --- | --- |
| `GET /api/system/version` | signed in | `version`, `commit` |
| `GET /api/system/update` | signed in | `current`, `latest`, `update_available`, `development`, `release_url`, `notes`, `checked_at`, `check_error`, `self_update`, `pending`, `update` |
| `POST /api/system/update` | Platform Admin | 202 with the queued `version`; 409 when already on it, an update is running or self-update is not installed |

## CLI

On the daemon's machine, root or members of the `startship` group can use the
CLI **without login** via `/run/startship/startship.sock`. Add a trusted user with
`sudo usermod -aG startship USER`, then log in again. Socket access grants local
Platform Admin access, acting as the longest-serving holder of the fixed Platform Admin role; protect it
like root access. Local access works even while dashboard first-run setup is pending.
Login, logout and account/password updates on the socket are not session operations;
use an HTTP login for account changes. `startship logout` without a saved server
simply clears the local configuration.

Transport selection is `STARTSHIP_SERVER`, then the server saved by `login`, then
an accessible local socket, then `http://localhost:8080`. `STARTSHIP_SOCKET` overrides
the CLI socket path; an explicit server can also be `unix:///absolute/path.sock`.
The daemon uses `STARTSHIP_SOCKET`, otherwise `$RUNTIME_DIRECTORY/startship.sock`
(set by the installed systemd service), otherwise `$STARTSHIP_DATA/startship.sock`.
For a manually run daemon, point `STARTSHIP_SOCKET` at that same file in the CLI.
The socket is mode 0660, owned by the daemon's user/group, and removed on shutdown.
Remote servers continue to use HTTP(S) and `startship login URL` with credentials.

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
### Users, roles and workspace members from the CLI

These commands use the daemon's permission checks; local socket access remains
trusted platform administration. Use `--json` for the full API response.

```sh
startship permissions
startship users create bob                         # prompts for temporary password
startship users list
startship roles create "My Role" --clone-of member --color '#336699' --icon star
startship roles edit "My Role" --name "Support" --perm project.view,logs.view
startship grant bob Contributor --workspace "Team"
startship members "Team"                           # or: workspaces members Team
startship assignments list --user bob --workspace "Team"
startship overrides set workspace "Team" --user bob --deny deploy
startship overrides list workspace "Team"
startship effective bob --workspace "Team"
startship revoke bob Contributor --workspace "Team"
startship audit --workspace "Team"
```

`grant` and `revoke` default to platform scope; use **one** of `--workspace` or
`--project` for a narrower assignment. Revoke matches the user, role and exact
scope, not inherited roles. `assignments list` without filters lists all visible
assignments. `effective` without a user checks the current account. Users accept
username or ID, roles accept name or ID, and scope targets accept name or ID;
ambiguous names require IDs. `roles get ROLE` shows full role details.

`--perm` accepts comma-separated permission keys and can be repeated. On edit,
it replaces the complete permission list; `--perm ''` clears it. Other role
fields remain unchanged unless supplied. `--clone-of` is create-only. Use
`permissions` to discover keys and supported icons. Built-in roles cannot be
edited or deleted; clone one instead.

Overrides require `workspace TARGET` or `project TARGET` and exactly one subject
(`--role` or `--user`). `--allow` and `--deny` are comma-separated lists; each set
replaces both lists, with omitted lists empty. Setting neither clears the
override.

`users rename USER NAME`, `users reset-password USER`, `users delete USER`, and
`roles delete ROLE` administer existing accounts and custom roles. Create and
reset-password prompt without terminal echo for a required temporary password
(8–72 bytes); new/reset users must change it on login. Passwords are never command
arguments. Keep deletions and platform role grants restricted to trusted admins;
the server enforces last-admin and self-deletion safeguards.

### Workspace domains

`domains` manages the workspace's allowed hostnames; use `routes` for project
ingress routes (the former `domains` alias for `routes` is replaced):

```sh
startship domains add Team '*.example.com'
startship domains add Team test.example.com
startship domains add Team services.example.net --subdomains
startship domains list Team
startship workspaces domains list Team --json
startship domains rm Team '*.example.com'
```

Every command accepts a workspace name or ID. Removal accepts the domain's name
or ID (`rm`, `remove` and `delete` are aliases). Quote wildcards so the shell does
not expand them. An exact name allows only that host; `*.example.com` allows
subdomains at any depth but not `example.com` itself. `--subdomains` on an exact
name allows both the apex and subdomains. Tables show `APEX`, `SUBDOMAINS`, route
counts and whether the caller can manage domains; `--json` preserves the API
response. `workspaces domains ...` is an alias for all these commands.

The API supports list/add/remove, not editing a domain. Remove and re-add to
change its policy; removing a domain still required by routes is rejected.
Workspace administrators with `workspace.domains` can add/remove domains;
members can list them. Cross-workspace overlapping claims are rejected. Project
route hosts must be allowed by their workspace; Platform Admins bypass this host
restriction. Use `routes add`/`routes update` to configure actual project routing.

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

Without Docker Compose, build the binary ([Go 1.26+](https://go.dev/dl/), Node.js 22.12+, npm, Git and Docker Engine required; no C toolchain). Node is only needed at build time, not on servers running the released binaries:

```sh
make build
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

The router always listens on HTTP (`STARTSHIP_HTTP_ADDR`, default `:80`) and
HTTPS (`STARTSHIP_HTTPS_ADDR`, default `:443`). HTTPS requires a matching stored
certificate or the legacy automatic certificate configuration below; otherwise
the TLS handshake fails. Both ports must be available even without ACME enabled.

Workspace certificates (requires `workspace.domains` for reads and writes):

- `GET /api/workspaces/:id/certificates` returns `{certificates: [...]}`.
- `POST` there accepts `{source:"upload", cert_pem:"...", key_pem:"..."}` or
  `{source:"acme", domains:["app.example.com"], auto_renew:true}`; returns a
  certificate with HTTP 201. ACME issuance runs asynchronously.
- `GET /api/workspaces/:id/certificates/:certificateId` returns its metadata
  and public certificate chain; private keys are never returned.
- `DELETE` there removes an unbound certificate (409 while issuing/bound).
- `POST /api/workspaces/:id/certificates/:certificateId/renew` queues ACME
  renewal (202; 409 if already issuing). Uploaded certificates cannot renew.
- Platform Admin `GET`/`PUT /api/system/acme` reads/sets
  `{email:"admin@example.com", directory_url:"https://.../directory"}` in the
  database. Defaults come from `STARTSHIP_ACME_EMAIL` and
  `STARTSHIP_ACME_DIRECTORY` (Let's Encrypt production by default).

Certificate fields: `id`, `workspace_id`, `domains`, `source`, `cert_pem`,
`issuer`, `not_before`, `not_after`, `status`, `last_error`, `auto_renew`,
`created`, `updated`, `created_by`. Status is pending/issuing/active/failed/expired.
Names must be permitted by workspace domain claims unless created by a Platform
Admin. Uploads support single-label wildcard certificates; HTTP-01 does not.
Certificate private keys and ACME account keys are stored in the private SQLite
data directory: protect database files and backups.

Routes accept `certificate_id` (same workspace, covering the hostname) and
`force_https` (301 redirect, preserving path/query and the configured HTTPS
port). SNI prefers an explicitly bound certificate, then any usable certificate
covering the exact hostname or a single-label wildcard, then legacy autocert.
SNI cannot distinguish paths: use one explicit certificate per hostname.
Public route URLs use HTTPS when a usable stored certificate covers the host.
Renewal runs at startup and hourly, starting 30 days before expiry; failed
attempts retry on the next tick. Uploaded certificates are never auto-renewed.
HTTP-01 challenges are served automatically before route matching/redirects;
the public HTTP port must reach this node. Configure the account email first.

Legacy automatic Let's Encrypt configuration remains available:

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

## Users, roles and permissions

Permissions are a fixed list defined in code (`GET /api/permissions`): platform ones (`workspaces.create`, `users.manage`, `roles.manage`, `audit.view`), workspace ones (`workspace.manage`, `projects.create`, `members.manage`) and project ones (`project.view`, `logs.view`, `traffic.view`, `deploy`, `project.settings`, `project.environment`, `project.source`, `domains.manage`, `project.delete`).

A role has a name, a color, an icon and a set of permissions. Guest, Member, Contributor and Administrator are built in and read-only but can be cloned (`POST /api/roles {"clone_of":"member"}`); custom roles are edited in the dashboard under **Roles** or with `PATCH /api/roles/{id}`. **Platform Admin** is fixed: it holds every permission everywhere, cannot be edited, deleted or overridden, and the last holder cannot lose it. The first account gets it automatically.

Roles are assigned to a user at a scope with `POST /api/assignments {"user_id","role_id","scope":"platform|workspace|project","scope_id"}`. Any role at a workspace makes the user a member; workspace roles apply to every project in it, and a project can add its own. A Guest is a member who sees no project until given a role on one. A plain user without roles can only edit their own account.

Overrides refine roles Discord-style: `PUT /api/overrides {"scope","scope_id","subject_type":"role|user","subject_id","allow":[...],"deny":[...]}`. Workspace overrides apply before project overrides and role overrides before user overrides, so the most specific rule wins. Nobody can grant a permission they do not hold. `GET /api/effective?workspace_id=&project_id=` returns the caller's permissions at a scope, `GET /api/workspaces/{id}/members` lists members, and every change to users, roles, assignments and overrides is recorded with its actor in `GET /api/audit`.

## Workspace domains

A workspace owns the domains its projects may route. Members with **Manage domains** (`workspace.domains`, part of Administrator) add them under Workspace settings or with `POST /api/workspaces/{id}/domains {"name":"example.com","subdomains":false}`. `example.com` allows exactly that host, `*.example.com` allows any subdomain at any depth, and `"subdomains":true` on `example.com` allows both. A domain belongs to one workspace: overlapping claims across workspaces (`*.example.com` here, `shop.example.com` there) and the dashboard domain are rejected.

Connecting or changing a route's host (`domains.manage`) requires a host under one of the project's workspace domains; Platform Admins may use any host. `GET /api/workspaces/{id}/domains` lists them with how many routes each serves, and `DELETE /api/workspaces/{id}/domains/{domainID}` refuses while a route still depends on the domain. Existing routes keep working when a domain is added or removed elsewhere. Domain changes are recorded in the audit log.

## API

All bodies and responses are JSON. Login using `POST /api/login {"username":"admin","password":"..."}`, keep the `startship_session` cookie. `POST /api/logout`, `GET /api/me`. Sessions expire after seven days.

`PATCH /api/me {"username":"new-name","current_password":"...","new_password":"..."}` changes the signed-in account. Supply a username, a new password, or both, plus the current password. Usernames must be unique and 1–100 bytes after trimming; new passwords must be 8–72 bytes (bcrypt's limit). Success returns `{id, username}`, invalidates all previous sessions, and sets a fresh session cookie so the caller stays signed in. This is not a password-reset flow: you must know the current password.

Ownership-scoped CRUD: `workspaces`, `projects`, and `routes`. Names are required except for routes. POST returns the new object including ID.

- workspace: `name`
- project: `workspace_id, name, git_url, git_token, branch, root_dir, build_type, port, env, test_command, auto_deploy`
- route: `project_id, container_id, host, path_prefix, strip_prefix, port`

The Domains view maps a public hostname and path to a detected container and its internal port. With one container, the destination is selected automatically. With several, choose the destination for each route. The optional route `port` overrides the container's detected port without changing its configuration or redeploying; omit it or set it to `0` to follow the detected port. Explicit ports must be between `1` and `65535`. Existing routes continue to use the detected port.

Project defaults: branch `main`, root `.`, build strategy `auto`, port `0` (detect), automatic push deployment off. Env is a string map applied to all detected containers. Git tokens require HTTPS and are excluded from responses. Project responses include their signed webhook details.

Read-only runtime views: `GET /api/containers?project_id=...`, `GET /api/containers/{id}`, and `GET /api/containers/{id}/logs` (last 200 runtime lines; static containers have no serving process). Grouped history: `GET /api/releases?project_id=...&logs=false` for metadata and `GET /api/releases/{id}` for combined logs and `container_releases` artifact IDs. Internal per-container build records remain readable under `deployments`. Legacy `apps` endpoints and `app_id` route fields remain for compatibility, not as dashboard configuration.

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
npm ci --prefix web
npm run build --prefix web # creates the assets embedded by Go; required before Go checks
npm run format:check --prefix web
npm test --prefix web
go vet ./... && go test ./...
./scripts/e2e.sh
```

E2E requires Python 3, curl, Git and real Docker. It also covers a two-HTTP-container project release, shared config, container domain targets, grouped failure preservation, artifact redeploy, and restart recovery. Uses localhost ports 18080/18081, creates a local Dockerfile + Python + Rust + static monorepo, auto-detects and deploys all four through the API, tests route URLs, prefix proxying and restart recovery, and removes its containers, images, and data afterward. Docker network startship is retained.

## Project layout

- `cmd/startshipd`, `cmd/startship`: daemon and CLI entrypoints.
- `internal/server`: HTTP API, authorization, schema migrations, deployment orchestration,
  Docker/buildx isolation, proxy serving, traffic collection and local socket.
- `internal/cli`: CLI commands, configuration and API transport.
- `internal/store`: SQLite records, persistence and migration registration.
- `internal/detect`: build-root detection and generated Dockerfiles.
- `internal/routing`: host/path normalization and route matching.
- `internal/config`: daemon environment and data-directory defaults.
- `internal/version`: build metadata shared by both binaries.
- `web/src/views`: route-level React views, grouped by project and access management.
- `web/src/components`, `web/src/layouts`: reusable UI and dashboard/project shells.
- `web/src/services`: typed API contracts, HTTP transport and query definitions.
- `web/src/providers`: session lifecycle, dialogs and notifications.
- `web/dist`: generated Vite production bundle, embedded into the daemon; not committed.
- `scripts`: deployment integration tests.

Use `make build` to build the frontend and both commands, or `go generate ./web`
followed by `go build ./cmd/...`. A clean checkout must build the frontend before
building or testing Go packages that import `web`. CI, Docker and release builds
do this automatically. The CLI alone still builds with just Go.

For frontend development, run the daemon on port 8080, then `npm run dev --prefix web`.
Vite proxies HTTP and WebSocket `/api` requests to that daemon; authentication stays same-origin. Hash routes
preserve existing dashboard bookmarks and need no server-side fallback. Views
load separately, and React owns all rendered markup. The project overview combines
general configuration, recent deployments and project information.

The authenticated dashboard shares one `/api/live` WebSocket. Mounted views
subscribe to project/workspace state, build/runtime logs, traffic and update
status; pushed snapshots and changes update the query cache without HTTP polling.
Disconnects reconnect with backoff and fresh snapshots. REST handles initial
loading, mutations and manual checks. `npm test --prefix web` runs component/service regressions.

Release builds set
`github.com/sammwyy/startship/internal/version.Version` and
`github.com/sammwyy/startship/internal/version.Commit` using `-ldflags -X`.
Docker builds accept matching `VERSION` and `COMMIT` build arguments.
Run `startshipd --version` or `startship version` to print the version.
