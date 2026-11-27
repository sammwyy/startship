# Startship CLI

Use `startship projects sleep ID` and `startship projects wake ID` to stop/start all containers in a project's active release. Enable idle sleep with `startship projects update ID '{"sleep_after":10}'` (minutes, 0 disables, maximum 10080). Sleep and wake require deploy permission; configuring idle sleep requires project settings permission.

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

Login prompts for the username and password. Session settings are stored in `$XDG_CONFIG_HOME/startship/config.json` (or `~/.config/startship/config.json`). `STARTSHIP_SERVER` and `STARTSHIP_TOKEN` override saved server and credentials; `STARTSHIP_TOKEN` also accepts a personal access token (`sst_…`), sent as a Bearer token. Without a saved or explicit server, the CLI uses the local socket if accessible and otherwise falls back to `http://localhost:9000`. Run `startship logout` to end a saved session.

The CLI uses a saved refresh credential to renew its five-minute access credential. HTTP sessions last up to seven days unless revoked sooner. If the saved session is no longer valid, sign in again.

## Account and activity

```sh
startship account update --display-name "Jane Doe" --email jane@example.com
startship passwd
startship account sessions
startship account revoke SESSION_ID
startship account activity
startship account tokens
startship account token create ci --workspaces prod,staging --expires 30 --no-mcp
startship account token create agent --all --admin --expires 0
startship account token revoke TOKEN_ID
startship activity workspace Team --follow
startship activity project website
```

Account activity includes the session that performed each action when known. Use `--follow` on any activity feed to receive new actions over the WebSocket, or add `--json` before the command for machine-readable output. Theme preference is local to the dashboard browser.

### Uptime and public status

`startship uptime PROJECT` shows the current state, 24-hour/7-day/30-day uptime, latest probe latency, and incidents. Add `enable` or `disable` to change the project opt-in. Uptime is the percentage of successful checks; sleeping and static projects are not probed. Two consecutive failures start an incident dated from the first failure. The next success resolves it.

`startship status-page show WORKSPACE` shows the workspace's publishing settings. For example:

```sh
startship status-page set "Team" --slug team-status --title "Team status" --projects "API,Website" --enable
```

Visitors can open `/status/team-status` on the dashboard origin without signing in. They see only the selected projects' public summaries as of page load. To unpublish, use `startship status-page set "Team" --disable`.

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
