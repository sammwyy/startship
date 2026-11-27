#!/bin/sh
set -eu

die() { printf '\nStartship: %s\n' "$*" >&2; exit 1; }
section() { printf '\n%s\n' "$1"; }
detail() { printf '  %s\n' "$@"; }
usage() {
    cat <<'EOF'
Startship installer

Usage: sudo sh install.sh [options]
       sudo sh install.sh --uninstall

Install options:
  --version TAG             install release TAG instead of the latest (STARTSHIP_VERSION)
  --download-url URL        fetch release assets and checksums.txt from URL (STARTSHIP_DOWNLOAD_URL)
  --data-dir DIR            data directory, chosen on first install (STARTSHIP_DATA; default /var/lib/startship)
  --dashboard-listen ADDRS  dashboard and API listeners, comma-separated (default :9000)
  --http-listen ADDRS       application router HTTP listeners, comma-separated (default :80)
  --https-listen ADDRS      application router HTTPS listeners, comma-separated (default :443)
  --no-router               disable the application router so ports 80 and 443 stay free
  --dashboard-domain HOST   also serve the dashboard through the router at HOST
  --acme-email EMAIL        account email for Let's Encrypt certificates
  --network-firewall        enable the platform network firewall (STARTSHIP_NETWORK_FIREWALL=1)
  --no-nixpacks             do not install Nixpacks
  --no-start                install the services without enabling or starting them
  -h, --help                show this help

Values accept --option VALUE or --option=VALUE. Settings are written to
/etc/startship/config/startship.toml; on a reinstall, omitted options keep the
current settings.
EOF
}
version=${STARTSHIP_VERSION:-} base=${STARTSHIP_DOWNLOAD_URL:-} data=${STARTSHIP_DATA:-}
firewall=${STARTSHIP_NETWORK_FIREWALL:-0}
uninstall=0 nixpacks=1 start=1 no_router=0 options=0
dashboard_listen='' http_listen='' https_listen='' dashboard_domain='' acme_email=''
while [ "$#" -gt 0 ]; do
    opt=$1 val='' inline=0
    case "$opt" in --*=*) val=${opt#*=} opt=${opt%%=*} inline=1 ;; esac
    shift
    options=$((options + 1))
    case "$opt" in
        --version|--download-url|--data-dir|--dashboard-listen|--http-listen|--https-listen|--dashboard-domain|--acme-email)
            if [ "$inline" = 0 ]; then
                [ "$#" -gt 0 ] || die "$opt requires a value"
                val=$1
                shift
            fi
            [ -n "$val" ] || die "$opt requires a value" ;;
        *) [ "$inline" = 0 ] || die "$opt does not take a value" ;;
    esac
    case "$opt" in
        -h|--help) usage; exit 0 ;;
        --uninstall) uninstall=1 ;;
        --version) version=$val ;;
        --download-url) base=$val ;;
        --data-dir) data=$val ;;
        --dashboard-listen) dashboard_listen=$val ;;
        --http-listen) http_listen=$val ;;
        --https-listen) https_listen=$val ;;
        --no-router) no_router=1 ;;
        --dashboard-domain) dashboard_domain=$val ;;
        --acme-email) acme_email=$val ;;
        --network-firewall) firewall=1 ;;
        --no-nixpacks) nixpacks=0 ;;
        --no-start) start=0 ;;
        *) die "unknown option $opt (see sh install.sh --help)" ;;
    esac
done
[ "$(id -u)" = 0 ] || die 'run as root (sudo sh install.sh)'
config=/etc/startship/config/startship.toml
env_file=/etc/startship/startshipd.env
if [ "$uninstall" = 1 ]; then
    [ "$options" = 1 ] || die '--uninstall takes no other options'
    section 'Removing Startship'
    if [ -d /run/systemd/system ]; then systemctl disable --now startship-update.path startshipd.service || true; fi
    rm -f /etc/systemd/system/startshipd.service /etc/systemd/system/startship-update.path /etc/systemd/system/startship-update.service /usr/local/bin/startship /usr/local/bin/startshipd
    if [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
    detail 'Binaries and systemd units removed.' 'Data, configuration, and the startship user were kept.'
    detail "Configuration: $config" "Environment:   $env_file"
    exit 0
fi
installed_data=''
if [ -f "$env_file" ]; then installed_data=$(sed -n 's/^STARTSHIP_DATA=//p' "$env_file" | tail -n 1); fi
data=${data:-${installed_data:-/var/lib/startship}}
case "$data" in /*) ;; *) die 'the data directory must be absolute' ;; esac
case "$data" in *[!a-zA-Z0-9_./-]*) die 'the data directory contains unsupported characters' ;; esac
data=${data%/}
[ -n "$data" ] || die 'the data directory cannot be /'
if [ -n "$installed_data" ] && [ "$data" != "${installed_data%/}" ]; then
    die "this host already uses data directory $installed_data ($env_file); the data directory can only be chosen on first install"
fi
[ "$(uname -s)" = Linux ] || die 'only Linux is supported'
case "$(uname -m)" in x86_64|amd64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) die 'supported architectures: amd64, arm64' ;; esac
for tool in curl sha256sum tar docker git useradd usermod getent runuser; do command -v "$tool" >/dev/null 2>&1 || die "install $tool first"; done
caps=CAP_NET_BIND_SERVICE
if [ "$firewall" = 1 ]; then
    for tool in ipset iptables ip6tables; do command -v "$tool" >/dev/null 2>&1 || die "install $tool first"; done
    caps="$caps CAP_NET_ADMIN CAP_NET_RAW"
fi
getent group docker >/dev/null || die 'Docker group missing; install Docker Engine first'
if [ -z "$base" ]; then
    if [ -n "$version" ]; then base="https://github.com/sammwyy/startship/releases/download/$version"
    else base=https://github.com/sammwyy/startship/releases/latest/download; fi
fi
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT HUP INT TERM
fetch() { curl --fail --location --silent --show-error "$base/$1" -o "$tmp/$1"; }
section 'Downloading Startship'
detail "Platform: Linux $arch"
if [ -n "$version" ]; then detail "Release: $version"; fi
fetch checksums.txt
for bin in startship startshipd; do
    asset="${bin}_linux_${arch}"
    fetch "$asset"
    expected=$(awk -v name="$asset" '$2 == name {print $1}' "$tmp/checksums.txt")
    [ "${#expected}" = 64 ] || die "missing or invalid checksum for $asset"
    printf '%s  %s\n' "$expected" "$tmp/$asset" | sha256sum -c --status - || die "checksum mismatch for $asset"
done
detail 'CLI and daemon checksums verified.'
if [ "$nixpacks" = 1 ]; then
    detail 'Downloading and verifying Nixpacks...'
    case "$arch" in
        amd64) nixarch=x86_64; nixsum=0f55de7874507b9cf7502113120bd96f2ab6979f78d10eaf2eb2ade9207b3af6 ;;
        arm64) nixarch=aarch64; nixsum=912bd02dd2bb6f9c3a9ed965fe8a68b4aa318dc7a2546e2eca6f2806a894ba39 ;;
    esac
    curl --fail --location --silent --show-error "https://github.com/railwayapp/nixpacks/releases/download/v1.41.0/nixpacks-v1.41.0-$nixarch-unknown-linux-musl.tar.gz" -o "$tmp/nixpacks.tar.gz"
    printf '%s  %s\n' "$nixsum" "$tmp/nixpacks.tar.gz" | sha256sum -c --status - || die 'Nixpacks checksum mismatch'
    tar -xzf "$tmp/nixpacks.tar.gz" -C "$tmp" nixpacks
fi
section 'Installing Startship'
detail 'Setting up binaries, configuration, and systemd units...'
if ! id startship >/dev/null 2>&1; then useradd --system --home-dir "$data" --shell /usr/sbin/nologin startship; fi
usermod -a -G docker startship
install -d -m 0755 /usr/local/bin
install -d -o root -g root -m 0755 /etc/startship
install -d -o startship -g startship -m 0750 /etc/startship/config
if [ "$nixpacks" = 1 ]; then install -m 0755 "$tmp/nixpacks" /usr/local/bin/nixpacks; fi
install -d -o startship -g startship -m 0700 "$data"
install -d -o startship -g startship -m 0750 "$data/update"
for bin in startship startshipd; do
    install -m 0755 "$tmp/${bin}_linux_${arch}" "/usr/local/bin/.$bin.new"
    mv -f "/usr/local/bin/.$bin.new" "/usr/local/bin/$bin"
done
# Settings given as options are checked on a copy first, then written to the TOML.
# A matching variable in the environment file would override them, so it is removed.
configure() { # TOML EDIT_ENV [RUNNER...]
    file=$1 edit_env=$2
    shift 2
    for setting in router_disabled http_listen https_listen dashboard_listen dashboard_domain acme_email network_firewall; do
        case "$setting" in
            router_disabled) value=$no_router; [ "$value" = 1 ] && value=true || value='' ;;
            http_listen) value=$http_listen ;;
            https_listen) value=$https_listen ;;
            dashboard_listen) value=$dashboard_listen ;;
            dashboard_domain) value=$dashboard_domain ;;
            acme_email) value=$acme_email ;;
            network_firewall) value=$firewall; [ "$value" = 1 ] && value=true || value='' ;;
        esac
        [ -n "$value" ] || continue
        "$@" /usr/local/bin/startship config --config "$file" set "$setting" "$value" >/dev/null || return 1
        if [ "$edit_env" = 1 ] && [ -f "$env_file" ]; then
            variable=$(printf 'STARTSHIP_%s' "$setting" | tr a-z A-Z)
            case "$setting" in *_listen) variable=STARTSHIP_$(printf '%s' "${setting%_listen}" | tr a-z A-Z)_ADDR ;; esac
            sed -i "/^$variable=/d" "$env_file"
        fi
    done
}
if [ -f "$config" ]; then cp "$config" "$tmp/check.toml"; fi
configure "$tmp/check.toml" 0 || die 'invalid settings; nothing was configured'
dashboard_summary='Dashboard: see configuration and environment below'
if [ -n "$dashboard_listen" ]; then
    dashboard_summary="Dashboard listeners: $dashboard_listen"
elif [ ! -f "$env_file" ]; then
    dashboard_summary='Dashboard:     http://localhost:9000'
fi
if [ ! -f "$env_file" ]; then
    printf 'STARTSHIP_DATA=%s\n' "$data" > "$env_file"
    if [ -z "$dashboard_listen" ]; then printf '%s\n' 'STARTSHIP_DASHBOARD_ADDR=:9000' >> "$env_file"; fi
    chmod 0600 "$env_file"
fi
if [ ! -f "$config" ]; then
    printf '%s\n' '# Startship daemon settings; flags > environment > this file > defaults.' '# Existing /etc/startship/startshipd.env values override these settings.' '# Remove corresponding *_ADDR entries from that env file to use file listeners.' '# dashboard_listen = ["127.0.0.1:9000"]' '# dashboard_listen = ["tailscale0:9000", "127.0.0.1:9000"]' '# http_listen = [":80"]' '# https_listen = [":443"]' '# router_disabled = false' "# data_dir = \"$data\"" '# dashboard_domain = "deploy.example.com" # exposes the panel via the proxy too' '# acme_email = "admin@example.com"' '# acme_directory = "https://acme-v02.api.letsencrypt.org/directory"' '# socket_path = "/run/startship/startship.sock"' |
        runuser -u startship -- sh -c 'umask 027; cat > "$1"' sh "$config"
fi
configure "$config" 1 runuser -u startship -- || die "could not write settings to $config"
install -d -m 0755 /etc/systemd/system
printf '%s\n' '[Unit]' 'Description=Startship deployment daemon' 'After=network-online.target docker.service' 'Wants=network-online.target' 'Requires=docker.service' '' '[Service]' 'Type=simple' 'User=startship' 'Group=startship' 'RuntimeDirectory=startship' 'RuntimeDirectoryMode=0750' 'SupplementaryGroups=docker' 'EnvironmentFile=/etc/startship/startshipd.env' 'WorkingDirectory=/var/lib/startship' 'Environment=STARTSHIP_UPDATE_REQUEST=/var/lib/startship/update/request.json' 'Environment=STARTSHIP_UPDATE_STATUS=/var/lib/startship/update/status.json' 'ExecStart=/usr/local/bin/startshipd' 'AmbientCapabilities=CAP_NET_BIND_SERVICE' 'CapabilityBoundingSet=CAP_NET_BIND_SERVICE' 'Restart=on-failure' 'RestartSec=5' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/startshipd.service
# The daemon cannot replace root-owned binaries; it queues a request that this root-only updater applies.
sed -i "s/^AmbientCapabilities=.*/AmbientCapabilities=$caps/; s/^CapabilityBoundingSet=.*/CapabilityBoundingSet=$caps/" /etc/systemd/system/startshipd.service
printf '%s\n' '[Unit]' 'Description=Startship update request watcher' '' '[Path]' 'PathExists=/var/lib/startship/update/request.json' 'Unit=startship-update.service' '' '[Install]' 'WantedBy=multi-user.target' > /etc/systemd/system/startship-update.path
printf '%s\n' '[Unit]' 'Description=Install the Startship release requested from the dashboard or CLI' 'After=network-online.target' 'Wants=network-online.target' '' '[Service]' 'Type=oneshot' 'Environment=STARTSHIP_UPDATE_REQUEST=/var/lib/startship/update/request.json' 'Environment=STARTSHIP_UPDATE_STATUS=/var/lib/startship/update/status.json' 'ExecStart=/usr/local/bin/startship update apply-request' > /etc/systemd/system/startship-update.service
# The data directory may be overridden on first install.
sed -i "s|/var/lib/startship|$data|g" /etc/systemd/system/startshipd.service /etc/systemd/system/startship-update.path /etc/systemd/system/startship-update.service
if [ "$start" = 0 ]; then
    if [ -d /run/systemd/system ]; then systemctl daemon-reload; fi
    status='Services were not started (--no-start).'
elif [ -d /run/systemd/system ]; then
    detail 'Starting systemd services...'
    systemctl daemon-reload
    systemctl enable --now startshipd.service startship-update.path
    systemctl restart startshipd.service
    status=''
else
    status='Services were not started because systemd is not running.'
fi
section 'Startship installed'
detail "$dashboard_summary" "Data:          $data" "Configuration: $config" "Environment:   $env_file"
if [ -n "$status" ]; then
    section 'Start the services'
    detail "$status" 'Run when systemd is available:' '  sudo systemctl enable --now startshipd.service startship-update.path'
fi
section 'First sign-in'
detail 'After the daemon starts, retrieve the temporary admin credentials:' \
    '  sudo journalctl -u startshipd' \
    'Keep the logs private and change the password when you sign in.'
section 'CLI access'
detail 'Local: root and members of the startship group do not need to log in.' \
    '  sudo usermod -aG startship USER  (then sign out and back in)' \
    'Remote: startship login URL'
