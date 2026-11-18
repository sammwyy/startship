# Workspace firewall / WAF

Each workspace has one L7 policy, applied to its projects' public routes. The
dashboard domain and ACME HTTP challenges bypass it so an incorrect rule cannot
lock you out of the control panel. New policies are **off**. Network/L4 rules
and a dashboard rule editor are separate features, not implemented by this
workspace API.

## Policy and rules

`GET /api/workspaces/:id/firewall` returns the complete policy. Replace it with
`PUT` (not a merge). `updated` and `updated_by` are set by the server. Example:

```json
{
  "mode": "enforce",
  "managed": {
    "sqli": {"enabled": true, "action": "block"},
    "xss": {"enabled": true, "action": "block"}
  },
  "limits": {
    "max_body_bytes": 1048576,
    "max_url_bytes": 8192,
    "max_header_bytes": 32768,
    "concurrent_per_ip": 20,
    "rate": null
  },
  "rules": [{
    "id": "protect-login",
    "name": "Protect login",
    "enabled": true,
    "match": "all",
    "conditions": [
      {"field": "method", "op": "eq", "value": "POST"},
      {"field": "path", "op": "eq", "value": "/login"}
    ],
    "action": "rate_limit",
    "rate": {"requests": 10, "window_seconds": 60, "by": "ip"},
    "ban_seconds": 900
  }]
}
```

Modes: `off` bypasses evaluation; `monitor` evaluates and records would-be
rejections but admits requests and never writes bans; `enforce` rejects matches.
Monitor follows the same first-terminal-match ordering, rather than reporting
every rule which might match.

A rule has `id`, `name`, `enabled`, `match` (`all` or `any`), 1–16 `conditions`,
and `action`. IDs must be nonempty, unique within a policy and cannot start
with `__` (reserved for implicit limits). Conditions have `field`, `op`, `value`
and optional `negate` (default false). `in` takes a nonempty string array; other
operators take a string. For `exists`, use `value: ""`.

### Fields and operators

Fetch `GET /api/firewall/catalog` rather than hardcoding an editor's options.
It returns `{fields: [{id, ops, values?}], actions: [...], managed_rules:
[{id, name, description, default_action, default_enabled}], geoip: bool}`.
`header.<name>` is a template: use e.g. `header.Authorization` in a condition.

| Field | Meaning | Operators |
|---|---|---|
| `ip` | resolved client IPv4/IPv6 | `in_cidr` (IP or CIDR), `in_list` (workspace list ID) |
| `country` | ISO-2 country | `eq`, `in` |
| `host` | request host | text operators |
| `path` | decoded URL path | text operators |
| `method` | HTTP method | text operators |
| `query` | raw query string | text operators |
| `header.<name>` | joined header values, case-insensitive header name | text operators |
| `user_agent` | raw User-Agent | text operators |
| `ua_class` | `browser`, `crawler`, `bot`, `cli`, `library`, `unknown`, `empty` | text operators |
| `device` | `desktop`, `mobile`, `tablet`, `bot`, `unknown` | text operators |
| `scheme` | `http`, `https` | text operators |

Text operators: `eq` (equal), `in` (equal to any value), `contains`, `prefix`,
`suffix`, `matches` (Go RE2, at most 512 characters, compiled once), `exists`
(field present/nonempty; a present empty header counts as existing).
Matching folds case for host, method, ua_class, device and country. Other values,
including paths and user agents, are case-sensitive. `negate` inverts a
condition, **except that an unknown country never matches**, even when negated.

### Actions

- `block`: reject with 403.
- `allow`: skip remaining custom rules, managed rules and limits; concurrency
  admission still applies. Block lists run earlier and cannot be overridden.
- `log`: emit an event and continue evaluating subsequent rules.
- `rate_limit`: requires `rate: {requests, window_seconds, by: "ip"|"ip_path"}`.
  Requests within the window continue evaluation; excess requests get 429. A
  positive `ban_seconds` also persists a temporary ban; zero means no ban for
  this action. Counters are scoped to workspace, rule ID, IP and optional path.
- `ban`: persist the client IP in `banned` and return 403. `ban_seconds: 0`
  means permanent; positive values expire. Subsequent banned-list matches
  return 403.

Rate windows and positive ban/entry TTLs are capped at 31,536,000 seconds (one
year). Rate counts/windows must be positive. Size/concurrency limits cannot be
negative. Invalid policies receive HTTP 400 with the exact
`firewall.Validate` field-path message, e.g.
`rules[2].conditions[0]: unknown field "foo"`.

### Managed rules

Defaults enable `sqli`, `xss`, `path_traversal`, `scanner_paths` with `block`.
Defaults disable `crawlers`, `bad_bots`, `unknown_user_agents` (also default
action `block`). A complete policy replacement uses exactly its `managed` map;
omitted IDs are disabled, not merged with defaults.

- `sqli`: conservative SQL injection signatures, including tautologies and
  UNION SELECT, not ordinary phrases like “select a plan” or O'Brien.
- `xss`: script tags, script URLs and event handlers.
- `path_traversal`: parent traversal, encoded traversal and null bytes.
- `scanner_paths`: sensitive/scanner targets such as `/.env`, `/.git/`,
  `/wp-login.php` and phpMyAdmin.
- `crawlers`: known search/AI crawlers, including Googlebot, bingbot and GPTBot.
- `bad_bots`: command-line clients/libraries/attack tools such as curl, wget,
  python-requests, Go-http-client and sqlmap. Enable with care for API services.
- `unknown_user_agents`: empty or unrecognized agents.

Managed actions support `block`, `allow`, `log`, `ban` (permanent). They cannot
use `rate_limit` because managed entries have no rate configuration. SQLi/XSS
scan decoded path/query values plus complete form/JSON bodies up to 64 KiB.
Larger bodies are not signature-scanned. Sampled bytes are restored for upstream;
sampling is not sanitization and the firewall is not a replacement for application
input validation.

### Limits and evaluation order

`limits` contains `max_body_bytes`, `max_url_bytes`, `max_header_bytes`,
`concurrent_per_ip` (each zero = unlimited), and nullable `rate`:
`{requests, window_seconds, action: "block"|"ban", ban_seconds}`. Workspace rate
is per client IP across all routes; exceeding it returns 429 and Retry-After.
`ban_seconds: 0` is permanent for workspace rate action `ban`.

1. Route lookup (dashboard and challenge hooks bypass admission).
2. Concurrency acquire/release around the entire in-flight request (429 when full).
3. Body sample, then engine: active bans/block lists, allow lists, custom rules
   in stored order, enabled managed rules in catalog order, body/URL/header
   limits, workspace rate. First terminal match wins; `log` continues.
4. Proxy/static response, recording one outcome per request plus match events.

Body sizes use Content-Length, sampled bytes and streaming `MaxBytesReader`
(413); URL limit is encoded request URI bytes (414); headers count name/value
bytes and framing (431). Explicit allows bypass these limits, never concurrency.
Streaming unknown-length bodies may reach upstream in part before overflow is
detected. Rejections include “Blocked by Startship firewall”, a request ID and
`X-Startship-Ray`; 429 also includes `Retry-After`.

## IP lists and bans

Lists are workspace scoped:
`{id, name, kind: "block"|"allow", entries: [{value, expires_at?, note, created_by}]}`.
Values are IPs or CIDRs, not hostnames/countries. Use a `country in [...]` rule
for countries. All block lists win over all allow lists. An allow-list match
bypasses custom/managed rules and limits except concurrency.

Every workspace has the implicit `banned` block list. It cannot be deleted or
changed to an allow list; its entries may be edited/cleared using the same list
endpoints. Ban actions, manual bans and workspace-rate bans persist here.
`expires_at` is RFC3339; omitted means permanent. Entry-add and ban requests take
`ttl_seconds` (zero/omitted = permanent) and the server computes expiration.
Expired entries are ignored immediately by the engine and purged lazily from
SQLite when configuration is next read/refreshed. Manual unban also clears the
old in-memory ban, without resetting rate/concurrency counters.

## REST and permissions

All paths below start with `/api/workspaces/:id/firewall`:

| Method | Suffix | Body / response |
|---|---|---|
| GET / PUT | (none) | policy / replaced policy |
| POST | `/rules` | rule (ID optional, generated if absent) / updated policy |
| PUT / DELETE | `/rules/:rid` | replacement rule / updated policy |
| POST | `/rules/order` | `{ids: [...]}` containing every ID exactly once / updated policy |
| GET / POST | `/lists` | list array / new list (ID optional) |
| PUT / DELETE | `/lists/:lid` | replacement list / `{ok: true}` on delete |
| POST | `/lists/:lid/entries` | `{value, ttl_seconds?, note?}` / updated list (upserts value) |
| DELETE | `/lists/:lid/entries/:value` | updated list |
| POST | `/ban` | `{ip, ttl_seconds?, note?}` / updated banned list |
| DELETE | `/ban/:ip` | updated banned list |
| GET | `/events` | latest 500 events, newest first |

`GET /api/firewall/catalog` is available to authenticated users. All successful
workspace operations return 200. Unsupported methods return 405; optional policy
PATCH is not implemented. URL-encode individual IDs and entry values (especially
CIDR slashes). Rule IDs in PUT bodies must match the URL if supplied. List
updates replace entries, not merge; list IDs are at most 128 characters without
URL delimiters, names are 1–200 characters. The server attributes entries to the
actor and writes policy update metadata. All mutations are audited as
`firewall.policy.update`, `firewall.rule.create|update|delete|order`,
`firewall.list.create|update|delete|expire`, `firewall.entry.create|delete`,
`firewall.ban.create|delete`; automatic bans/expiry use actor `firewall`.
Configuration and audit records commit in the same SQLite transaction.

Workspace visibility plus `firewall.view` is required to read policy/lists/events
or subscribe to live data. Mutations require `firewall.manage`. Administrator
has both; Contributor has view only; Member has neither. Platform Admin has all
keys, including the reserved platform permission `network.firewall`. These
endpoints also work over the existing local-admin Unix socket, using the same
handler and socket authorization as other APIs.

## Client identity / GeoIP

Daemon TOML `trusted_proxies = ["10.0.0.0/8", "2001:db8::/32"]` or
`STARTSHIP_TRUSTED_PROXIES` (comma-separated CIDRs) controls trusted forwarding
peers. Default: none. Without a trusted peer, client IP is RemoteAddr and all
forwarded identity headers are ignored. With a trusted peer, use the rightmost
untrusted X-Forwarded-For hop; without XFF, use CF-Connecting-IP then the peer.
Malformed XFF fails closed to the peer, without falling back to Cloudflare IP.
Only trust proxies you control; never blindly trust the entire internet.

`geoip_database = "/path/to/country.mmdb"` or `STARTSHIP_GEOIP_DB` opens a
MaxMind-format country database. No database means empty country, and catalog
`geoip: false`; a trusted peer's valid CF-IPCountry can still supply country and
takes precedence over MMDB. The MMDB is opened by the daemon and closed on
shutdown. Missing/invalid configured databases fail startup rather than silently
disabling geolocation. The router depends only on the nil-safe `firewall.GeoIP`
interface, not on a database implementation.

## Live data and telemetry

Subscribe to `/api/live` with
`{"type":"subscribe","id":"fw","topic":"firewall","workspace_id":"..."}`.
Every update is a replacement snapshot:

```json
{
  "policy": {}, "lists": [], "events": [], "can_manage": true,
  "stats": {
    "total": 0, "blocked": 0, "by_action": {},
    "minutes": [{"at": 1700000040, "total": 0, "blocked": 0, "by_action": {}}],
    "top_rules": [], "top_ips": [], "top_countries": []
  }
}
```

`policy`/`lists` have the shapes above. Events (latest 100, newest first) contain
`time`, `workspace_id`, `project_id`, `route_host`, `ip`, `country`, `method`,
`host`, `path`, `ua`, `ua_class`, `rule_id`, `rule_name`, `action`, `status`,
`monitored`. Times are RFC3339. Monitor events retain the would-be status;
requests remain admitted. Rings keep 500 per workspace and are memory-only.

Stats cover the current minute plus previous 59 minutes; `minutes` always has
60 oldest-first buckets, `at` is Unix seconds at minute start. Total counts
requests, blocked counts actual firewall rejections, allowed = total − blocked.
`by_action` counts each request's final decision, including monitor decisions.
Top arrays have at most five `{value, count}` entries, descending count, ties by
value. These aggregate **match events**, not distinct requests (multiple log
matches can count more than once); absent country is excluded. Stats/rings reset
at restart and are isolated by workspace.

Policy/list changes push promptly (coalesced on the live hub's flush); telemetry
events/observations push at most once per second and idle subscriptions do not
poll configuration. Access is rechecked before snapshots and on the usual live
session timer; revocation drops the subscription. See [live-api.md](live-api.md).

## CLI

Run `startship help` for the complete `firewall` command family. Workspace names
and IDs are accepted, using the existing saved login or local-admin socket.
`show`, `mode`, `managed`, `limits`, `rules`, `lists`, `ban`, `unban`, `events`,
`apply` and `export` use the same workspace REST endpoints and permissions.
`--json` works as for other commands; `export WS` always emits JSON suitable for
`apply WS FILE.json`. Policy edits read the current policy then PUT the changed
policy, preserving unrelated settings (concurrent edits are last-writer-wins).

Examples:

```sh
startship firewall mode Team enforce
startship firewall limits Team --rate 100/1m --rate-action ban --ban 15m --max-body 10MB
startship firewall rules add Team --name "Protect login" --when 'method eq POST' --when 'path eq /login' --action rate_limit --rate 10/1m --ban 15m
startship firewall lists add Team Office --kind allow
startship firewall lists entry add Team LIST_ID 203.0.113.0/24 --for 7d
startship firewall ban Team 192.0.2.1 --for 1h
startship --json firewall events Team --follow
```

List creation takes a name and optional `--kind block|allow` (default block);
the API generates the list ID. Subsequent list commands take that ID.
Rule commands take rule IDs; `rules move WS RULE POS` uses **1-based** positions.
`--when` repeats; `in` takes comma-separated values, `exists` can omit the value.
`--any` selects OR instead of AND. Rule rates default to per-IP scope.
Durations accept whole seconds via Go duration notation (`30s`, `15m`, `1h`)
plus days (`7d`), up to 365 days; zero makes a ban permanent. Sizes accept integer
bytes or decimal `KB`, `MB`, `GB` (512KB = 512000 bytes); zero removes a size or
concurrency limit.

`events --follow` subscribes to the live `firewall` topic, without REST polling.
It works over HTTP(S) with the saved session cookie **and over Unix sockets**
with the existing socket transport. Initial buffered events and subsequent new
events print oldest-first; replacement snapshots do not reprint old events.
With `--json`, each new event is one NDJSON line. Ctrl-C ends the stream.
The live API includes only the latest 100 events per snapshot, so more than 100
new events between snapshots can leave gaps; the CLI does not promise a durable
event log or automatically reconnect after an interrupted connection.

## Implementer contract: daemon and router

`internal/firewall` owns the JSON types, catalog, validation, compiled evaluation,
identity resolution and state. Daemon `internal/server` owns SQLite, authorization,
audits, GeoIP lifecycle, telemetry and live distribution. SQLite tables are
`firewall_policies(workspace_id PRIMARY KEY, body, updated, updated_by)` and
`firewall_lists(id, workspace_id, body, PRIMARY KEY(workspace_id,id))`; composite
list identity permits the same logical `banned` ID in every workspace.

`internal/router.Router` is an `http.Handler`, with immutable atomic snapshots:

```go
router.Snapshot{
    Targets: []router.Target{},
    RouteWorkspace: map[string]string{}, // route ID -> workspace ID
    Policies: map[string]*firewall.Engine{}, // workspace ID -> compiled engine
}
```

The daemon builds route targets and compiles policies at startup and after every
change, then calls `Router.Update`. One long-lived `firewall.State` per workspace
retains rule/workspace sliding windows and concurrency across recompiles. After
snapshot publication, transient bans are cleared in favor of the authoritative
persisted banned list; rates/concurrency are never reset by an unban. The router
copies snapshot routing slices/maps; compiled engines own their policy/lists.

`router.Hooks` supplies `Challenge` and `Dashboard` handlers (return true when
handled), `Traffic`, `Event`, `Observe`, and a synchronous `Ban` persistence hook.
Ban persistence failures fail closed with 503. `router.Options` supplies trusted
proxy prefixes, the GeoIP interface and HTTPS port. The router has no access to
SQLite, RBAC or API handlers beyond those hooks. Listeners remain owned by the
daemon; this is a package boundary, not a separate process.
