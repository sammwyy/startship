#!/bin/sh
set -eu

die() { printf '%s\n' "startship installer: $*" >&2; exit 1; }
[ "$(id -u)" = 0 ] || die 'run as root (sudo sh install.sh)'
data=${STARTSHIP_DATA:-/var/lib/startship}
case "$data" in /*) ;; *) die 'STARTSHIP_DATA must be absolute' ;; esac
case "$data" in *[!a-zA-Z0-9_./-]*) die 'STARTSHIP_DATA contains unsupported characters' ;; esac
if [ "${1:-}" = --uninstall ]; then
    if [ -d /run/systemd/system ]; then systemctl disable --now startshipd.service; fi
    rm -f /etc/systemd/system/startshipd.service /usr/local/bin/startship /usr/local/bin/startshipd
    if [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
    printf '%s\n' 'Uninstalled binaries and service; data, configuration and service user preserved.'
    exit 0
fi
[ "$#" = 0 ] || die 'usage: install.sh [--uninstall]'
[ "$(uname -s)" = Linux ] || die 'only Linux is supported'
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die 'supported architectures: amd64, arm64' ;; esac
for tool in curl sha256sum docker git useradd usermod getent; do command -v "$tool" >/dev/null 2>&1 || die "install $tool first"; done
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
if ! id startship >/dev/null 2>&1; then useradd --system --home-dir "$data" --shell /usr/sbin/nologin startship; fi
usermod -a -G docker startship
install -d -m 0755 /usr/local/bin /etc/startship
install -d -o startship -g startship -m 0700 "$data"
for bin in startship startshipd; do
    install -m 0755 "$tmp/${bin}_linux_${arch}" "/usr/local/bin/.$bin.new"
    mv -f "/usr/local/bin/.$bin.new" "/usr/local/bin/$bin"
done
if [ ! -f /etc/startship/startshipd.env ]; then
    printf 'STARTSHIP_DATA=%s\nSTARTSHIP_DASHBOARD_ADDR=:8080\nSTARTSHIP_HTTP_ADDR=:80\nSTARTSHIP_HTTPS_ADDR=:443\n' "$data" > /etc/startship/startshipd.env
    chmod 0600 /etc/startship/startshipd.env
fi
install -d -m 0755 /etc/systemd/system
printf '%s\n' '[Unit]' 'Description=Startship deployment daemon' 'After=network-online.target docker.service' 'Wants=network-online.target' 'Requires=docker.service' '' '[Service]' 'Type=simple' 'User=startship' 'Group=startship' 'SupplementaryGroups=docker' 'EnvironmentFile=/etc/startship/startshipd.env' 'WorkingDirectory=/var/lib/startship' 'ExecStart=/usr/local/bin/startshipd' 'AmbientCapabilities=CAP_NET_BIND_SERVICE' 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' 'Restart=on-failure' 'RestartSec=5' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/startshipd.service
# The data directory may be overridden on first install.
sed -i "s|WorkingDirectory=/var/lib/startship|WorkingDirectory=$data|" /etc/systemd/system/startshipd.service
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable --now startshipd.service
    systemctl restart startshipd.service
else
    printf '%s\n' 'systemd is not running; service installed but not started. On boot: systemctl enable --now startshipd'
fi
printf '%s\n' 'Installed. First-run administrator credentials: journalctl -u startshipd' 'Treat credentials logs as sensitive. Then: startship login http://localhost:8080' 'Complete first-run setup with: startship passwd'
