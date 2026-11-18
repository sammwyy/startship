# Access tokens and MCP

Personal access tokens let the API, the CLI and MCP clients act as you without your password. Create them under **Account settings › Tokens** or with `startship account token create`.

## Tokens

Each token has:

- **Workspaces**: one or more chosen workspaces, or all workspaces, which includes workspaces created later.
- **Access**: *Operate* keeps viewing, creating, configuring and deploying projects (`project.view`, `logs.view`, `traffic.view`, `activity.view`, `firewall.view`, `projects.create`, `deploy`, `project.settings`, `project.environment`, `project.source`, `domains.manage`). *Administer* keeps every workspace and project permission, including members, workspace settings, services, firewall and deletions; with all workspaces it can also create workspaces.
- **Use with**: MCP clients, the API and CLI, or both.
- **Expiry**: 30, 90 or 365 days, or never.

A token only narrows your own access: roles, overrides and later permission changes still apply, and deleting the user deletes its tokens. Tokens cannot manage your account, sessions or tokens, and cannot open the dashboard live connection. Startship stores only a SHA-256 hash; the secret (`sst_…`) is shown once at creation.

Every request records the token's last-used time, IP address and user agent, shown in the token list. Actions taken with a token appear in activity logs with the source `token`; your account activity names the token. Creating and revoking tokens is recorded too. Revoking takes effect on the next request.

API: `GET /api/me/tokens`, `POST /api/me/tokens` (`name`, `workspaces`, `all_workspaces`, `access`: `operate`|`admin`, `api`, `mcp`, `expires_in_days`; the response includes `token` once), `DELETE /api/me/tokens/ID`. Send a token as `Authorization: Bearer sst_…`.

## MCP server

The endpoint is `https://YOUR-STARTSHIP/api/mcp`, using the MCP Streamable HTTP transport in stateless mode (protocol `2025-06-18`, also `2025-03-26` and `2024-11-05`). Authenticate with a token that has MCP enabled:

```json
{
  "mcpServers": {
    "startship": {
      "type": "http",
      "url": "https://YOUR-STARTSHIP/api/mcp",
      "headers": { "Authorization": "Bearer sst_…" }
    }
  }
}
```

For example, with Claude Code: `claude mcp add --transport http startship https://YOUR-STARTSHIP/api/mcp --header "Authorization: Bearer sst_…"`.

Tools run as the token's owner through the regular API, so they obey the same permissions and appear in the same activity logs. Workspaces, projects and containers can be named by ID or unique name.

| Tool | Does |
| --- | --- |
| `whoami` | account and token in use |
| `list_workspaces`, `create_workspace`, `rename_workspace`, `delete_workspace` | workspaces |
| `list_members`, `workspace_activity` | workspace members and audit activity |
| `list_projects`, `get_project`, `create_project`, `update_project`, `delete_project` | projects and their single source |
| `deploy_project`, `redeploy_project`, `list_deployments`, `get_deployment` | deployments and build logs |
| `list_containers`, `container_logs`, `list_routes`, `project_activity` | deployment outputs, routes and activity |
| `api_request` | any other API call, such as services, volumes, backups or firewall |
