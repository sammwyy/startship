<h1 align="center">Startship</h1>

<p align="center"><strong>Deploy from Git. Run it on your own server.</strong></p>

<p align="center">
  <a href="https://github.com/sammwyy/startship/releases/latest"><img src="https://img.shields.io/github/v/release/sammwyy/startship" alt="Latest release"></a>
  <a href="https://github.com/sammwyy/startship/actions/workflows/ci.yml"><img src="https://github.com/sammwyy/startship/actions/workflows/ci.yml/badge.svg?branch=main" alt="CI status"></a>
  <a href="LICENSE"><img src="https://img.shields.io/badge/License-Apache%202.0-blue.svg" alt="License: Apache 2.0"></a>
</p>

Startship is a self-hosted deployment platform for web apps, APIs, and static sites. Connect a Git repository, build and deploy it on your own Docker host, and serve it behind a domain with HTTPS. Manage deployments, data, and access from the dashboard or CLI.

**On this page:** [Quick start](#quick-start) · [Features](#what-you-can-do) · [CLI](#cli) · [Self-hosted apps](#self-hosted-apps) · [Architecture](#how-it-fits-together) · [Documentation](#documentation)

<img src="docs/assets/dashboard.png" alt="Startship workspace dashboard split between light and dark themes, showing projects, running containers, and recent deployment activity">

## Quick start

Use the systemd installer on a Linux amd64 or arm64 server with Git, curl, and a running Docker Engine with Buildx. The daemon has Docker access and runs code from repositories you deploy, so use a server you control and give access only to trusted users.

```sh
curl -fsSL https://raw.githubusercontent.com/sammwyy/startship/main/install.sh -o install.sh
# Review install.sh before running it as root.
sudo sh install.sh --dashboard-listen 127.0.0.1:9000
sudo journalctl -u startshipd
```

1. Find the temporary administrator credentials in the daemon logs. Open `http://localhost:9000` on the host or through a private tunnel, sign in, and change the temporary password. Keep the dashboard listener private.
2. In a workspace, open **Projects → New project**, enter your Git repository and branch, and review the detected pipeline.
3. Open **Deployments → Deploy** to start a release and follow its build output. Once it's healthy, connect a hostname and path under **Domains** to serve it through the application router, not the dashboard listener.

See the [installation guide](INSTALL.md) for [Docker Compose](INSTALL.md#docker-compose), [source builds](INSTALL.md#build-from-source), and remote access. The [first deployment guide](docs/getting-started.md) walks through routing, certificates, and services.

## What you can do

### Deploy and serve

| Feature | What it does |
| --- | --- |
| **Build from Git** | Auto-detect application roots, pick a pipeline template, or customize the build, test, and deploy stages. Use Docker (including a repository Dockerfile), Nixpacks, or Buildpacks (Linux amd64). Serve static artifacts directly or run an application in Docker. |
| **Release with health checks** | Keep the previous release serving if a new build or health check fails. Redeploy a successful release from retained artifacts without rebuilding. |
| **Automate deploys and previews** | A signed GitHub webhook can deploy the configured branch and build previews for pushes to other branches when enabled. Previews use separate releases and are removed on branch deletion or pull request closure. |
| **Route traffic with HTTPS** | Route by hostname and path. Issue Let's Encrypt certificates or upload your own, and optionally redirect HTTP to HTTPS. |
| **Sleep idle projects** | Stop runtime containers after a configurable idle period and wake them on the next allowed request. You can also sleep and wake projects manually. |
| **Organize projects** | Group projects in workspaces and move or copy projects between workspaces. |

### Run services and apps

| Feature | What it does |
| --- | --- |
| **Managed services** | Run engines such as PostgreSQL, Redis, and S3-compatible SeaweedFS. Link projects, apps and services through **Connections** (one private network per link, internal DNS and env vars), or group them in workspace **Networks**. |
| **Volumes and backups** | Mount persistent volumes, schedule backups for volume-backed services, and download or restore backups from the dashboard or CLI. |
| **App recipes** | Install [self-hosted apps](#self-hosted-apps) such as Jellyfin, Gitea, Minecraft, or TeamSpeak. Each installation has its own Docker network, volumes, and configuration. Use built-in or remote catalogs for recipes and service engines. |
| **Migration** | Platform Admins can import selected projects and services from Openship, Dokploy, or Coolify installations on the same host without modifying the source installations. |

### Monitor, secure, and operate

| Feature | What it does |
| --- | --- |
| **Live dashboard** | Follow activity, build and runtime logs, and routed traffic. View relationships between projects, services, and volumes on the workspace Map, and set your preferred theme and accent color. |
| **Uptime and alerts** | Opt projects into uptime checks, publish selected projects on a workspace status page, and send deployment or uptime notifications to Slack, Discord, or an HTTP webhook. |
| **Traffic policies** | Monitor or block requests with workspace Firewall and WAF rules. Native Linux deployments can opt in to the host-wide network firewall. |
| **Listener interfaces** | Platform Admins can change the interface or port from the dashboard or CLI. Saved listener addresses take precedence over startup defaults. |
| **Roles and audit history** | Assign built-in or custom roles at workspace and project scope and review actions in activity and audit logs. |
| **Automation and MCP** | Use scoped personal access tokens with the API, CLI, or built-in [MCP server](docs/mcp.md). MCP tools use the same permissions and audit trail as the API. |
| **Updates** | Systemd installs with the update helper can update the daemon from the dashboard or CLI. |

## CLI

The installer includes `startship`. On the server, use the Unix socket as root or as a trusted member of the `startship` group. Socket access grants Platform Admin privileges.

```sh
sudo startship workspaces list
```

From another machine, use your configured dashboard HTTPS URL. Substitute your workspace name for `Team` and your project name for `website`:

```sh
startship login https://deploy.example.com
startship projects list Team
startship deploy website --wait
startship logs website
```

Run `startship --help` or see the [CLI guide](docs/cli.md) for more commands.

## File explorer

Select **Files** on a workspace or project volume, a deployed project container, or an installed App's container or persistent volume. Use the breadcrumbs to browse, drag in files to upload them, and open UTF-8 text files up to 5 MiB in the syntax-aware editor. Use **Format** for supported languages such as JSON and YAML and **Ctrl+S** (or **⌘S**) to save. Download files directly or folders as ZIP or TAR.GZ. Select files to compress them on the target, or extract ZIP, TAR, and TAR.GZ archives into a directory. Extraction does not replace existing top-level entries.

**Read files** and **Write files** are separate permissions. Project containers and attached project volumes use project permissions; standalone workspace volumes and installed Apps use workspace permissions. Stopped and shell-less containers support browsing and copy-based changes, but not deletion or moving files. Changes to a container's writable layer disappear when it is replaced; put persistent configuration in a volume. Startship-managed service backup volumes use the dedicated Backups screen.

The same operations are available over the CLI's usual HTTPS or Unix-socket connection. Replace the sample identifiers with Startship IDs:

```sh
startship files ls volume:VOLUME_ID /
startship files put volume:VOLUME_ID ./settings.json /settings.json
startship files edit volume:VOLUME_ID /settings.json  # uses $EDITOR
startship files get container:CONTAINER_ID /config ./config.tar.gz
startship files ls app:WORKSPACE_ID/APP_ID/web /
startship files ls app-volume:WORKSPACE_ID/APP_ID/data /
startship files zip volume:VOLUME_ID /config.zip /settings.json
startship files unzip volume:VOLUME_ID /config.zip /restored
```

## Container terminal

Select **Terminal** beside **Files** on a running project or App container, or on a Service's detail page. Startship opens an interactive shell with Bash when available and falls back to sh. The **Open container terminals** permission (`containers.exec`) can be granted at project scope for project containers or at workspace scope for the workspace's containers. Grant it only to users you trust to run commands with the container's configured user and access, including its environment and mounted data.

The CLI uses the same terminal over HTTPS or the local admin socket. Use Startship IDs and pass a command after `--` if you don't want a shell:

```sh
startship exec container:CONTAINER_ID
startship exec app:WORKSPACE_ID/APP_ID/web
startship exec service:SERVICE_ID -- redis-cli --help
```

## Self-hosted apps

Open **Apps** in a workspace and select **Install app** to choose a recipe. App
management requires the workspace **Manage apps** permission. Each app's
containers can communicate by service name on their own bridge and are not
attached to projects, managed services or other apps' private networks unless
you add a Connection or a workspace Network. Apps
can still reach public endpoints, including ports deliberately published to
the host.

Connect a workspace domain to an app's HTTP endpoint. Non-HTTP endpoints such
as Minecraft and TeamSpeak use host ports instead; set a host port to `0` to
leave it unpublished. **Configure** edits install settings and any files
declared by the recipe. Updating re-pulls the recipe's images without replacing
volumes. Deleting keeps the Docker volumes unless **Purge** is selected.

```sh
startship apps recipes list
startship apps install WORKSPACE gitea git --config '{"root_url":"https://git.example.com/"}'
startship apps domains add WORKSPACE git web git.example.com
startship apps ls WORKSPACE
```

See [the recipe format](docs/recipes.md) and [catalog refresh and remote registry setup](docs/catalogs.md).

## How it fits together

- **Control plane:** The Go daemon (`startshipd`) embeds the React dashboard, serves the API and live WebSocket updates, and stores platform state in SQLite. No separate web server or database is required for the control plane.
- **Builds and runtime:** The daemon uses Docker and Buildx to build and run workloads. A project has one Git source and release history; automatic detection can deploy multiple runnable components together.
- **Application router:** The daemon routes application traffic by hostname and path. By default, the dashboard and API listen on port `9000`, separate from the application router on ports `80` and `443`.
- **Management clients:** The dashboard, `startship` CLI, and MCP clients use the daemon's API. The CLI does not need direct access to Docker or SQLite.

## Documentation

| I want to… | Read |
| --- | --- |
| Install, upgrade, or uninstall Startship | [Installation](INSTALL.md) |
| Connect a repository, deploy, and add a domain | [First deployment](docs/getting-started.md) |
| Configure listeners, storage, and certificates | [Daemon configuration](docs/configuration.md) |
| Manage Startship from a terminal | [CLI guide](docs/cli.md) |
| Connect AI assistants or automation with tokens | [Access tokens and MCP](docs/mcp.md) |
| Customize build, test, or deploy stages | [Project pipelines](docs/pipelines.md) |
| Write an app recipe or configure external catalogs | [App recipes](docs/recipes.md) · [Catalogs](docs/catalogs.md) |
| Configure WAF rules or the optional network firewall | [Firewall and WAF](docs/firewall.md) |
| Integrate with dashboard live updates | [Live API](docs/live-api.md) |
| Build and test a change locally | [Development](docs/development.md) |

## License

Startship is licensed under the [Apache License 2.0](LICENSE).
