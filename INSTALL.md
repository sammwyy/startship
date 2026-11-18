# Install Startship

Use the **systemd installer** for a Linux server, **Docker Compose** to run the daemon in a container, or **build from source** to try a checkout. Use a host you control: Startship has Docker access and executes code from the repositories you deploy. Only give trusted users access; project isolation is not a sandbox for untrusted tenants.

## Linux with systemd

Use Linux amd64 or arm64 with systemd, Docker Engine and the Buildx plugin, Git, curl, and standard Linux account and archive utilities. Check that Docker is running, `docker buildx version` works, and ports 80, 443, and 8080 are free. Docker Engine must provide the `docker` group. Restrict inbound TCP 8080 to trusted clients before installing: the daemon starts immediately and the default dashboard listener binds on all interfaces.

```sh
curl -fsSL https://raw.githubusercontent.com/sammwyy/startship/main/install.sh -o install.sh
# Review install.sh before running it as root.
sudo sh install.sh
sudo journalctl -u startshipd
```

The installer verifies release checksums, installs `startship` and `startshipd` in `/usr/local/bin`, installs Nixpacks, and starts the daemon. It keeps existing settings in `/etc/startship/startshipd.env` and `/etc/startship/startship.toml`. By default, state lives in `/var/lib/startship`, including the SQLite database `startship.db`. The service runs as `startship` with Docker access. Without a running systemd, the script writes the units but cannot start them.

To install a specific release instead of the latest, use the installer from that release and pass the same version through `sudo`:

```sh
curl -fsSL https://raw.githubusercontent.com/sammwyy/startship/v0.8.0/install.sh -o install.sh
sudo env STARTSHIP_VERSION=v0.8.0 sh install.sh
```

## Docker Compose

Install Docker Engine with the Compose plugin and Git. The checkout builds the daemon image; no host Go or Node.js installation is needed. The provided Compose file mounts the host Docker socket and persists daemon state in the `startship-data` volume.

```sh
git clone https://github.com/sammwyy/startship.git
cd startship
docker compose up --build -d
docker compose logs startship
```

Before running on a remote host, change the dashboard mapping in [compose.yaml](compose.yaml) from `8080:8080` to `127.0.0.1:8080:8080` (or otherwise restrict access to port 8080). Keep the 80 and 443 mappings if Startship should serve public projects directly. Compose builds the daemon, not the CLI; use a separately built or installed CLI if needed.

## Build from source

Install Go 1.26+, Node.js 22.12+ with npm, Make, Git, Docker Engine, and Buildx. The user running the daemon needs Docker access. Node.js is used to build the embedded dashboard, not to run the compiled binary.

```sh
git clone https://github.com/sammwyy/startship.git
cd startship
make build
STARTSHIP_DASHBOARD_ADDR=127.0.0.1:8080 STARTSHIP_HTTP_ADDR=127.0.0.1:8081 STARTSHIP_HTTPS_ADDR=127.0.0.1:8443 ./startshipd
```

The example keeps all three listeners local and avoids privileged ports. For this run, the application HTTP router is on port 8081. Native source builds do not install Nixpacks; see [Project pipelines](docs/pipelines.md#build-engines) if you plan to use it. For build and test commands, see [Development](docs/development.md).

## Sign in and secure access

1. Find the temporary administrator credentials in the daemon logs. For a foreground source build, check the terminal you started it in. Treat these logs as sensitive.
2. Open `http://localhost:8080` from the host (or through a private tunnel), sign in, and change the temporary password. Restarting before setup is complete issues a new temporary password; use the most recent one.
3. Continue to [your first deployment](docs/getting-started.md).

| Default TCP port | Purpose | Exposure |
| --- | --- | --- |
| 8080 | Dashboard and API | Limit to localhost, a trusted private network, or a secured gateway. Do not expose first-run setup. |
| 80 | Application HTTP and ACME HTTP-01 validation | Public if serving public HTTP routes or issuing Let's Encrypt certificates. |
| 443 | Application HTTPS | Public if serving public HTTPS routes. A certificate must match the requested host. |

Both application router ports need to be available on the configured bind addresses even if you are not using HTTPS yet. See [Daemon configuration](docs/configuration.md) for private listeners and TLS. Keep SSH or local console access before enabling the optional [network firewall](docs/firewall.md#network-firewall-l4).

## Upgrade

Back up the daemon data and any service or application volumes you rely on before upgrading. A service backup alone does not cover the daemon database or unrelated volumes.

- **Systemd install:** Run `startship update check` to check versions and `sudo startship update` to verify and install the latest release. The updater restarts the daemon. To select a release, run `sudo startship update v0.8.0` (replace the tag with the version you want); selecting an older release is a downgrade.
- **Dashboard or remote CLI:** A Platform Admin can use **Updates** or `startship update daemon` on a systemd install with the installer's updater running.
- **Compose:** Update the checkout to the revision you want to run and rerun `docker compose up --build -d`. Keep the named data volume.
- **Source:** Update the checkout, rerun `make build`, and restart using the same settings and data directory.

## Uninstall

For a systemd install, run `sudo sh install.sh --uninstall` using the installer script. It removes the Startship CLI, daemon, and systemd units, but preserves the data, configuration, service account, and Nixpacks binary. It does not remove deployed workloads.

For Compose, run `docker compose down` from the checkout to stop and remove the daemon container while keeping its data volume. Do not add `--volumes` unless you intend to remove Compose-managed data. For a source build, stop the daemon and remove the binaries; keep the data directory if you want to reuse it.
*** End Patch
