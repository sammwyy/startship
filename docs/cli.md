# Startship CLI

The CLI talks to the daemon's dashboard API. It doesn't need Docker or direct access to the database. The systemd installer includes it; to build just the CLI from the checkout, run `go build -o startship ./cmd/startship`. Run `startship --help` for the full command list.

## Connect to a daemon

**On a systemd host**, root and trusted members of the `startship` group can use the Unix socket without an HTTP login:

```sh
sudo startship whoami
sudo startship workspaces list
```

The installer socket is `/run/startship/startship.sock`. Socket access grants Platform Admin privileges; do not give it to untrusted users. For a daemon run outside systemd, set `STARTSHIP_SOCKET` in the CLI to the same socket path used by the daemon. Use an HTTP session for account or password changes.

**From another machine**, use the dashboard's HTTPS URL. This example assumes `deploy.example.com` has been configured as the dashboard hostname:

```sh
startship login https://deploy.example.com
startship whoami
startship workspaces list
```

Login prompts for the username and password. Session settings are stored in `$XDG_CONFIG_HOME/startship/config.json` (or `~/.config/startship/config.json`). `STARTSHIP_SERVER` and `STARTSHIP_TOKEN` override saved server and credentials. Without a saved or explicit server, the CLI uses the local socket if accessible and otherwise falls back to `http://localhost:8080`. Run `startship logout` to end a saved session.

## Deploy a project

Replace `Team` with your workspace and the example Git URL with your repository. These examples use an existing workspace and create a new project:

```sh
startship projects create website Team
startship projects source website '{"git_url":"https://github.com/owner/repo","branch":"main"}'
startship deploy website --wait
startship --json deployments list website
startship logs website
startship containers list website
```

`startship logs website` shows the build log; use `startship logs --container CONTAINER_ID` for runtime output. Use IDs when names are ambiguous. `startship --json` before a command requests the API response as JSON. `startship projects env set` replaces the whole environment, not just the named keys.

## Common operations

| Task | Command |
| --- | --- |
| Inspect project routes | `startship routes list website` |
| Inspect allowed workspace domains | `startship domains list Team` |
| Browse the service catalog | `startship catalog` |
| List services, volumes, and backups | `startship services list Team`, `startship volumes list Team`, `startship backups list Team` |
| View project connections | `startship services connections website` |
| Inspect workspace members and audit history | `startship members Team`, `startship audit --workspace Team` |
| Read the daemon's TOML configuration file | `startship config show` (does not include overrides from flags or environment) |
| Check release versions | `startship update check` |

See the [installation guide](../INSTALL.md#upgrade) for updates, and [Firewall and WAF](firewall.md#cli) for firewall commands.
