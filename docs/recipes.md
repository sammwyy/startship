# App recipes

The **Apps** workspace section and `startship apps` use the same recipe registry.
The daemon bundles [registry/recipes.json](../registry/recipes.json), and by
default fetches the latest copy from the repository's `main` branch. Set
`STARTSHIP_RECIPES_URL` on the daemon and restart it to use a custom index
with one JSON object containing a `recipes` array. A valid remote index
replaces the **entire** catalog. See [catalog refresh](catalogs.md) for cache
behavior, permissions, and the commands to refresh manually.
Installs retain a snapshot of their recipe, so editing the index does not
silently alter a running app. **Update images** pulls the installed recipe's
image references again.

```json
{
  "recipes": [{
    "id": "example",
    "name": "Example",
    "icon": "box",
    "category": "dev-tools",
    "description": "A small HTTP server",
    "version": "1",
    "volumes": ["data"],
    "containers": [{
      "id": "web",
      "image": "busybox:1.37",
      "command": ["httpd", "-f", "-p", "8080", "-h", "/www"],
      "healthcheck": "wget -qO- http://127.0.0.1:8080/settings.json",
      "mounts": {"data": "/www"}
    }],
    "http": [{"id": "web", "container": "web", "port": 8080}],
    "ports": [{"id": "direct", "container": "web", "port": 8080, "protocol": "tcp", "host_port": 0}],
    "config_schema": {"type": "object", "properties": {}},
    "files": [{
      "id": "settings",
      "volume": "data",
      "path": "settings.json",
      "format": "json",
      "schema": {
        "type": "object",
        "properties": {"title": {"type": "string", "default": "My app"}}
      }
    }]
  }]
}
```

- Container IDs are DNS aliases inside the app. The recipe declares named
  volumes and each container's absolute mount path. Container `env` values and
  individual `command` arguments may substitute `${field}` from validated
  install settings. Use argument arrays rather than interpolating settings into
  shell scripts. `healthcheck`, if supplied, runs inside that container.
- `http` endpoints can be bound to hostnames through Startship's HTTP/HTTPS
  router. `ports` deliberately publish TCP or UDP on the host independently of
  the router. Set `host_port` to zero to leave the port private by default.
- Schemas are a subset of JSON Schema: flat objects with string, boolean,
  integer and number properties; title, description, default, enum,
  minimum/maximum, minLength/maxLength, and required fields are supported.
  Unlisted settings are rejected. Config files use JSON or flat `key=value`
  properties/INI, not nested or sectioned INI. For flat files, unlisted keys
  and comments are preserved.
- Files are addressed by their declared ID, not by a client-supplied path.
  A file path is relative to one declared volume. Defaults (schema defaults
  plus optional file-level `defaults`) seed files only when absent. File edits
  can optionally restart a running app; install setting edits recreate running
  containers to apply environment or port changes.
- Retained Docker volumes use `startship-app-<appID>-vol-<volumeID>`. Keep the
  app ID if you plan to recover the data manually outside Startship.

The REST API uses `/api/recipes` and `/api/workspaces/{workspace}/apps`.
The existing `/api/apps` resource continues to represent project containers.
