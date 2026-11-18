# Startship live updates (WebSocket) — contract

## Endpoint and auth
- `GET /api/live`, WebSocket upgrade on the dashboard listener (`ws(s)://<dashboard host>/api/live`).
- Auth: the existing `startship_session` cookie (the browser sends it). No session -> HTTP 401 before upgrade; account with a pending password change -> 403; `Origin` not equal to the dashboard host -> 403.
- Session expired/revoked (logout, password change) -> server closes with code `4401`; client should go to login.
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
| `workspace` | `workspace_id` | workspace visible | `{projects[]}` = GET `projects?workspace_id=` | `upsert`/`delete` kind project | projects list refetch |
| `certificates` | `workspace_id` | `workspace.domains` | `{certificates[]}` = GET `workspaces/:id/certificates` (no private keys) | a full `snapshot` on certificate create/status/renew/delete; access rechecked | workspace certificate list |
| `release_log` | `release_id` (release or legacy deployment id) | `logs.view` | the record with `log`, = GET `releases/:id` | `log` chunks as the build writes them; `upsert` kind release on status/finished change | LogPanel release 2s |
| `container_log` | `container_id` | `logs.view` | `{"log": "..."}` = GET `containers/:id/logs` (last 200 lines or the same "not deployed"/"static" text) | `log` chunks streamed from the running container; `log` with `reset:true` when the container's active deployment changes | LogPanel container 2s |
| `traffic` | `project_id`, `window` 300/900/3600 | `traffic.view` | = GET `projects/:id/traffic?window=` | a new `snapshot` pushed every 5s | TrafficView 5s |
| `system_update` | none | signed in (same as GET) | = GET `system/update` | a new `snapshot` whenever it changes (update requested, status file, release check) | SystemView 2s/60s |

Mutations stay on the REST API; their effects arrive over the socket.

## Services and volumes
- Topics `services` and `volumes` take `workspace_id` and require workspace visibility.
- Snapshots are `{services: Service[]}` and `{volumes: Volume[]}`, matching the workspace REST list routes. Every mutation/status change sends a replacement `snapshot`; access is rechecked.
- Service config password fields and lifecycle errors are omitted without workspace `services.manage`. Project connection password and URL values are masked without that permission.
- Attachment and project-volume mutations also notify the `project` topic. Subscribe to the workspace topics for Connections/Volumes views. Connection environment and volume mounts apply on the next deploy (redeploy to apply).
- Volume sizes are refreshed on list/subscribe at most every 30 seconds, without a background polling timer.
