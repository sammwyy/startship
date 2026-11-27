# Daemon configuration

Startship loads settings at startup. Precedence is **daemon flags → environment variables → TOML → defaults**, from highest to lowest. Select the TOML file with `startshipd --config PATH` or `STARTSHIP_CONFIG`; otherwise Startship uses `/etc/startship/config/startship.toml` if present. An explicitly selected file must exist and be valid.

| TOML key | Environment variable | Default |
| --- | --- | --- |
| `dashboard_listen` | `STARTSHIP_DASHBOARD_ADDR` | `:9000` |
| `http_listen` | `STARTSHIP_HTTP_ADDR` | `:80` |
| `https_listen` | `STARTSHIP_HTTPS_ADDR` | `:443` |
| `router_disabled` | `STARTSHIP_ROUTER_DISABLED` | `false` |
| `data_dir` | `STARTSHIP_DATA` | `./_data`; the systemd installer uses `/var/lib/startship` |
| `dashboard_domain` | `STARTSHIP_DASHBOARD_DOMAIN` | Unset; dashboard not served on the application router |
| `acme_email` | `STARTSHIP_ACME_EMAIL` | Unset |
| `acme_directory` | `STARTSHIP_ACME_DIRECTORY` | Let's Encrypt production |
| `socket_path` | `STARTSHIP_SOCKET` | `$RUNTIME_DIRECTORY/startship.sock`, or `startship.sock` in the data directory |
| `trusted_proxies` | `STARTSHIP_TRUSTED_PROXIES` | None |
| `geoip_database` | `STARTSHIP_GEOIP_DB` | Unset |
| `network_firewall` | `STARTSHIP_NETWORK_FIREWALL` | Off; see [Firewall and WAF](firewall.md#network-firewall-l4) before enabling |

Run `startshipd --help` for the corresponding flags. Listeners accept `host:port`, `[IPv6]:port`, `:port` for all interfaces, or a name such as `eth0:9000` to bind the interface's current addresses. Environment variables accept comma-separated entries and listener flags can be repeated.

## Bind the dashboard privately

For a native install, set `STARTSHIP_DASHBOARD_ADDR=127.0.0.1:9000` in the service environment or add this to the TOML file:

```toml
dashboard_listen = ["127.0.0.1:9000"]
```

On a systemd install, remove any corresponding `STARTSHIP_DASHBOARD_ADDR` setting from `/etc/startship/startshipd.env` if you want TOML to control the listener, then run `sudo systemctl restart startshipd`. With Compose, change the host port mapping instead; the host's network interfaces normally are not visible in the container. Restricting this listener does not restrict the application router: leave `dashboard_domain` unset if the dashboard should be reachable only through the private listener.

## Change or disable the application router

Use `startship router show` to see the active listeners, config path, and any settings overridden at startup. A Platform Admin can apply changes without restarting the daemon:

```sh
startship router set --http :8081 --https :8443
startship router disable
startship router enable
```

The application router is separate from the dashboard listener and admin socket. To start the dashboard on a host where another service already uses ports 80 or 443, set `router_disabled = true` in the daemon TOML before starting Startship. Compose publishes ports separately; remove or change its host port mappings if they conflict. Live changes are saved to the TOML and cannot modify a key overridden by an environment variable or daemon flag.

## HTTPS and certificates

The router listens on both HTTP and HTTPS. To serve TLS, the request's hostname must have a usable certificate; simply opening port 443 is not enough.

- **Application domains:** Add the hostname to the workspace and point its DNS at Startship. Under workspace **Certificates**, request a Let's Encrypt certificate or upload your own. Set an ACME account email before requesting certificates, and make sure public port 80 reaches Startship for HTTP-01 validation. HTTP-01 cannot issue wildcard certificates. In a project's **Domains**, choose a matching certificate or let the router select one, and enable HTTPS redirection if you want it.
- **Dashboard hostname:** Set `STARTSHIP_DASHBOARD_DOMAIN` and `STARTSHIP_ACME_EMAIL`, point the hostname at the server, and allow public TCP 80 and 443. These settings enable the automatic certificate flow and expose the panel through the application router. Keep raw dashboard port 9000 private.

Store the daemon's data directory and any copies of it securely: it contains the SQLite database, including private certificate material. Keep copies of service and application volumes separately.

## Other environment settings

| Environment variable | Purpose |
| --- | --- |
| `STARTSHIP_ADMIN_USER` and `STARTSHIP_ADMIN_PASSWORD` | Seed the initial administrator on an empty database without a forced password change. Otherwise, Startship generates a temporary password and writes it to the logs. |
| `STARTSHIP_SELF_CONTAINER` | Override how the daemon identifies its own container when joining project networks. The provided Compose file sets it. |
| `STARTSHIP_BACKUP_IMAGE` | Override the service backup helper image (default `busybox:1.37`). |
| `STARTSHIP_UPDATE_CHECK` | Set to `off` to disable the background release check. |
| `STARTSHIP_GITHUB_TOKEN` | Authenticate GitHub release checks if the shared unauthenticated rate limit is a problem. |

Builds use a persistent, project-specific Buildx builder; builds for a given project are serialized. These environment variables set daemon-wide resource defaults:

| Variable | Default | What it limits |
| --- | --- | --- |
| `STARTSHIP_APP_MEMORY` | `512m` | App and test container memory |
| `STARTSHIP_APP_CPUS` | `1` | App and test container CPU quota |
| `STARTSHIP_APP_PIDS` | `512` | App and test container processes |
| `STARTSHIP_BUILD_MEMORY` | `1g` | BuildKit worker memory |
| `STARTSHIP_BUILD_CPUS` | `1` | BuildKit worker CPU quota |
| `STARTSHIP_BUILD_CACHE` | `5g` | Best-effort build cache retention per project, not a disk quota |
