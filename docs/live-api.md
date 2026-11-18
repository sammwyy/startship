# Startship live updates (WebSocket) — contract

## Endpoint and auth
- `GET /api/live`, WebSocket upgrade on the dashboard listener (`ws(s)://<dashboard host>/api/live`).
- Auth: the five-minute access cookie, `startship_session`. Missing or expired access -> HTTP 401 before upgrade; account with a pending password change -> 403; mismatched `Origin` -> 403. The trusted Unix socket does not require cookies.
- Access expired or replaced -> close code `4408`. Call `POST /api/refresh` with the HTTP-only `startship_refresh` cookie, reconnect with the new access cookie and resubscribe. If the session has expired or been revoked, the server closes with `4401` or rejects renewal with HTTP 401; sign in again.
- Server sends WebSocket ping frames (browsers answer automatically). All payloads are JSON text frames.
- After connect the server sends `{"type":"ready","user_id":"...","version":"...","commit":"..."}`. After a daemon restart/update the client reconnects (backoff 0.5s -> 10s) and re-sends its subscriptions; every subscribe starts with a full snapshot, so reconnects self-heal.

## Client -> server
- `{"type":"subscribe","id":"<client-chosen sub id>","topic":"<topic>", ...topic params}`
- `{"type":"unsubscribe","id":"<sub id>"}`
Re-subscribing with an existing id replaces that subscription.

## Server -> client (every message carries the sub `id` and `topic`)
- `snapshot`: `{"type":"snapshot","id","topic","data":...}` full state; replace local state.
- `upsert`: `{"type":"upsert","id","topic","kind":"project|container|release|route","record":{...}}` insert or replace by `record.id`.
- `delete`: `{"type":"delete","id","topic","kind","record_id":"..."}`.
- `log`: `{"type":"log","id","topic","data":"<text chunk>","reset":false}` append; `reset:true` means replace the whole log with `data`.
- `error`: `{"type":"error","id","topic","error":"not found|forbidden|invalid subscription"}`; the subscription is dropped.

Record shapes are exactly those of the REST API (`publicRecord` / container records); release/deployment records in `upsert` never include `log`.

## Topics (replace each polling query)
| Topic | Params | Permission | Snapshot `data` | Then | Replaces |
|---|---|---|---|---|---|
| `project` | `project_id` | project visible | `{project, containers[], releases[] (no log), routes[]}` = GET `projects/:id`, `containers?project_id=`, `releases?project_id=&logs=false`, `routes?project_id=` | `upsert`/`delete` with kind project, container, release, route | ProjectLayout 2s queries, DashboardLayout project 2s |
| `workspaces` | none | signed in | = GET `workspaces` | replacement snapshot when a workspace is created, renamed or deleted, or an audited change may alter which workspaces you see | sidebar workspace list |
| `workspace` | `workspace_id` | workspace visible | `{projects[]}` = GET `projects?workspace_id=` | `upsert`/`delete` kind project | projects list refetch |
| `certificates` | `workspace_id` | `workspace.domains` | `{certificates[]}` = GET `workspaces/:id/certificates` (no private keys) | a full `snapshot` on certificate create/status/renew/delete; access rechecked | workspace certificate list |
| `firewall` | `workspace_id` | workspace visible + `firewall.view` | `{policy, lists, events, stats, can_manage}` | replacement snapshots on policy/list changes and at most once per second during telemetry activity; access rechecked | workspace firewall data |
| `release_log` | `release_id` (release or legacy deployment id) | `logs.view` | the record with `log`, = GET `releases/:id` | `log` chunks as the build writes them; `upsert` kind release on status/finished change | LogPanel release 2s |
| `container_log` | `container_id` | `logs.view` | `{"log": "..."}` = GET `containers/:id/logs` (last 200 lines or the same "not deployed"/"static" text) | `log` chunks streamed from the running container; `log` with `reset:true` when the container's active deployment changes | LogPanel container 2s |
| `traffic` | `project_id`, `window` 300/900/3600 | `traffic.view` | = GET `projects/:id/traffic?window=` | a new `snapshot` pushed every 5s | TrafficView 5s |
| `system_update` | none | signed in (same as GET) | = GET `system/update` | a new `snapshot` whenever it changes (update requested, status file, release check) | SystemView 2s/60s |
| `account_sessions` | none | signed in | = GET `me/sessions` | replacement snapshot when session data changes | account session list |
| `account_activity` | none | signed in | = GET `me/activity` | replacement snapshot when the actor records an action | account activity |
| `workspace_activity` | `workspace_id` | workspace visible + `activity.view` at workspace level | = GET `workspaces/:id/activity` | replacement snapshot when the workspace or one of its projects records an action | workspace activity |
| `project_activity` | `project_id` | project visible + `activity.view` at project level | = GET `projects/:id/activity` | replacement snapshot when that project records an action | project activity |
| `audit_activity` | none | platform `audit.view` | = GET `audit` | replacement snapshot when an action is recorded | platform audit |

Mutations stay on the REST API; their effects arrive over the socket.

## Firewall
- Subscribe with `{"type":"subscribe","id":"fw","topic":"firewall","workspace_id":"..."}`.
- `policy` and `lists` match the workspace firewall REST responses. `events` contains the latest 100 of the workspace's 500 buffered match events, newest first. `can_manage` reflects `firewall.manage` for the current user.
- `stats` is `{total, blocked, by_action, minutes, top_rules, top_ips, top_countries}` over the last 60 minute buckets. Each minute is `{at, total, blocked, by_action}` (`at` = Unix seconds, oldest first); top arrays contain up to five `{value, count}` items ordered by descending count. Allowed requests = total minus blocked. Tops count match events, not distinct requests.
- Policy/list mutations send replacement snapshots promptly; events **and observations of allowed requests** are coalesced to at most one snapshot per second. No configuration polling occurs while idle. Access is checked for each snapshot and on the normal session timer; lost visibility/`firewall.view` yields `error` and drops the subscription. Administrator can edit; Contributor can view; Member cannot subscribe by default.
- See [firewall.md](firewall.md) for the complete rule language, event/stat semantics, storage and router contract. Events and stats are memory-only and reset on restart; policies/lists/bans persist.

## Services and volumes
- Topics `services` and `volumes` take `workspace_id` and require workspace visibility.
- Snapshots are `{services: Service[]}` and `{volumes: Volume[]}`, matching the workspace REST list routes. Every mutation/status change sends a replacement `snapshot`; access is rechecked.
- Topic `backups` takes `workspace_id` and requires workspace visibility. Snapshots are `{backups: Backup[], volume: Volume | null}`, matching `GET /api/workspaces/:id/backups`; each backup start, finish, failure, restore or deletion sends a replacement `snapshot`.
- Service config password fields and lifecycle errors are omitted without workspace `services.manage`. Project connection password and URL values are masked without that permission.
- Attachment and project-volume mutations also notify the `project` topic. Subscribe to the workspace topics for Connections/Volumes views. Connection environment and volume mounts apply on the next deploy (redeploy to apply).
- Volume sizes are refreshed on list/subscribe at most every 30 seconds, without a background polling timer.
