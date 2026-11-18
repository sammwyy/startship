#!/bin/sh
set -eu

die() { printf '%s\n' "startship installer: $*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || die 'run as root (sudo sh install.sh)'
data=${STARTSHIP_DATA:-/var/lib/startship}
case "$data" in /*) ;; *) die 'STARTSHIP_DATA must be absolute' ;; esac
case "$data" in *[!a-zA-Z0-9_./-]*) die 'STARTSHIP_DATA contains unsupported characters' ;; esac
if [ "${1:-}" = --uninstall ]; then
    if [ -d /run/systemd/system ]; then systemctl disable --now startship-update.path startshipd.service || true; fi
    rm -f /etc/systemd/system/startshipd.service /etc/systemd/system/startship-update.path /etc/systemd/system/startship-update.service /usr/local/bin/startship /usr/local/bin/startshipd
    if [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
    printf '%s\n' 'Uninstalled binaries and service; data, configuration and service user preserved.'
    exit 0
fi
[ "$#" = 0 ] || die 'usage: install.sh [--uninstall]'
[ "$(uname -s)" = Linux ] || die 'only Linux is supported'
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die 'supported architectures: amd64, arm64' ;; esac
for tool in curl sha256sum tar docker git useradd usermod getent; do command -v "$tool" >/dev/null 2>&1 || die "install $tool first"; done
getent group docker >/dev/null || die 'Docker group missing; install Docker Engine first'
base=${STARTSHIP_DOWNLOAD_URL:-}
if [ -z "$base" ]; then
    if [ -n "${STARTSHIP_VERSION:-}" ]; then base="https://github.com/sammwyy/startship/releases/download/$STARTSHIP_VERSION"
    else base=https://github.com/sammwyy/startship/releases/latest/download; fi
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
fetch() { curl --fail --location --silent --show-error "$base/$1" -o "$tmp/$1"; }
fetch checksums.txt
for bin in startship startshipd; do
    asset="${bin}_linux_${arch}"
    fetch "$asset"
    expected=$(awk -v name="$asset" '$2 == name {print $1}' "$tmp/checksums.txt")
    [ "${#expected}" = 64 ] || die "missing or invalid checksum for $asset"
    printf '%s  %s\n' "$expected" "$tmp/$asset" | sha256sum -c - || die "checksum mismatch for $asset"
done
case "$arch" in
    amd64) nixarch=x86_64; nixsum=0f55de7874507b9cf7502113120bd96f2ab6979f78d10eaf2eb2ade9207b3af6 ;;
    arm64) nixarch=aarch64; nixsum=912bd02dd2bb6f9c3a9ed965fe8a68b4aa318dc7a2546e2eca6f2806a894ba39 ;;
esac
curl --fail --location --silent --show-error "https://github.com/railwayapp/nixpacks/releases/download/v1.41.0/nixpacks-v1.41.0-$nixarch-unknown-linux-musl.tar.gz" -o "$tmp/nixpacks.tar.gz"
printf '%s  %s\n' "$nixsum" "$tmp/nixpacks.tar.gz" | sha256sum -c - || die 'Nixpacks checksum mismatch'
tar -xzf "$tmp/nixpacks.tar.gz" -C "$tmp" nixpacks
if ! id startship >/dev/null 2>&1; then useradd --system --home-dir "$data" --shell /usr/sbin/nologin startship; fi
usermod -a -G docker startship
install -d -m 0755 /usr/local/bin /etc/startship
install -m 0755 "$tmp/nixpacks" /usr/local/bin/nixpacks
install -d -o startship -g startship -m 0700 "$data"
install -d -o startship -g startship -m 0750 "$data/update"
for bin in startship startshipd; do
    install -m 0755 "$tmp/${bin}_linux_${arch}" "/usr/local/bin/.$bin.new"
    mv -f "/usr/local/bin/.$bin.new" "/usr/local/bin/$bin"
done
if [ ! -f /etc/startship/startshipd.env ]; then
    printf 'STARTSHIP_DATA=%s\nSTARTSHIP_DASHBOARD_ADDR=:8080\nSTARTSHIP_HTTP_ADDR=:80\nSTARTSHIP_HTTPS_ADDR=:443\n' "$data" > /etc/startship/startshipd.env
    chmod 0600 /etc/startship/startshipd.env
fi
if [ ! -f /etc/startship/startship.toml ]; then
    printf '%s\n' '# Startship daemon settings; flags > environment > this file > defaults.' '# Existing /etc/startship/startshipd.env values override these settings.' '# Remove corresponding *_ADDR entries from that env file to use file listeners.' '# dashboard_listen = ["127.0.0.1:8080"]' '# dashboard_listen = ["tailscale0:8080", "127.0.0.1:8080"]' '# http_listen = [":80"]' '# https_listen = [":443"]' "# data_dir = \"$data\"" '# dashboard_domain = "deploy.example.com" # exposes the panel via the proxy too' '# acme_email = "admin@example.com"' '# acme_directory = "https://acme-v02.api.letsencrypt.org/directory"' '# socket_path = "/run/startship/startship.sock"' > /etc/startship/startship.toml
    chmod 0644 /etc/startship/startship.toml
fi
install -d -m 0755 /etc/systemd/system
printf '%s\n' '[Unit]' 'Description=Startship deployment daemon' 'After=network-online.target docker.service' 'Wants=network-online.target' 'Requires=docker.service' '' '[Service]' 'Type=simple' 'User=startship' 'Group=startship' 'RuntimeDirectory=startship' 'RuntimeDirectoryMode=0750' 'SupplementaryGroups=docker' 'EnvironmentFile=/etc/startship/startshipd.env' 'WorkingDirectory=/var/lib/startship' 'Environment=STARTSHIP_UPDATE_REQUEST=/var/lib/startship/update/request.json' 'Environment=STARTSHIP_UPDATE_STATUS=/var/lib/startship/update/status.json' 'ExecStart=/usr/local/bin/startshipd' 'AmbientCapabilities=CAP_NET_BIND_SERVICE' 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' 'Restart=on-failure' 'RestartSec=5' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/startshipd.service
# The daemon cannot replace root-owned binaries; it queues a request that this root-only updater applies.
printf '%s\n' '[Unit]' 'Description=Startship update request watcher' '' '[Path]' 'PathExists=/var/lib/startship/update/request.json' 'Unit=startship-update.service' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/startship-update.path
printf '%s\n' '[Unit]' 'Description=Install the Startship release requested from the dashboard or CLI' 'After=network-online.target' 'Wants=network-online.target' '' '[Service]' 'Type=oneshot' 'Environment=STARTSHIP_UPDATE_REQUEST=/var/lib/startship/update/request.json' 'Environment=STARTSHIP_UPDATE_STATUS=/var/lib/startship/update/status.json' 'ExecStart=/usr/local/bin/startship update apply-request' > /etc/systemd/system/startship-update.service
# The data directory may be overridden on first install.
sed -i "s|/var/lib/startship|$data|g" /etc/systemd/system/startshipd.service /etc/systemd/system/startship-update.path /etc/systemd/system/startship-update.service
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable --now startshipd.service startship-update.path
    systemctl restart startshipd.service
else
    printf '%s\n' 'systemd is not running; service installed but not started. On boot: systemctl enable --now startshipd'
fi
printf '%s\n' 'Installed. Local CLI works without login as root or for members of the startship group.' 'Add a local user: usermod -aG startship USER (then log in again).' 'Use startship login URL only for remote servers.' 'Dashboard first-run administrator credentials: journalctl -u startshipd' 'Treat credentials logs as sensitive. Complete dashboard setup by signing in and changing the password.'
