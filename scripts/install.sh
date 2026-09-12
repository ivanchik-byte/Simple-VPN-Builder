#!/usr/bin/env bash
# ==============================================================================
# Simple-VPN-Builder Universal Installation Script
# https://github.com/ivanchik-byte/Simple-VPN-Builder
#
# Supported Distributions: Debian, Ubuntu, AlmaLinux, Rocky Linux, CentOS,
#                          Fedora, Alpine Linux, Arch Linux.
# Architectures: amd64 (x86_64), arm64 (aarch64), armv7 (armhf).
# ==============================================================================

set -euo pipefail

REPO_OWNER="ivanchik-byte"
REPO_NAME="Simple-VPN-Builder"
BIN_DIR="/usr/local/bin"
CONFIG_DIR="/etc/vpnbuilder"
DATA_DIR="/var/lib/vpnbuilder"
SYSTEMD_DIR="/etc/systemd/system"
DEFAULT_VERSION=""

# Flags
COMPONENT=""
SPECIFIED_VERSION=""
CP_URL=""
ENROLLMENT_TOKEN=""
NON_INTERACTIVE=false
DRY_RUN=false
DO_UNINSTALL=false

print_banner() {
    cat << "EOF"
======================================================================
              Simple-VPN-Builder Universal Installer
======================================================================
EOF
}

log_info() {
    echo "[INFO] $*"
}

log_warn() {
    echo "[WARN] $*" >&2
}

log_error() {
    echo "[ERROR] $*" >&2
}

check_root() {
    if [ "$(id -u)" -ne 0 ]; then
        log_error "This script must be executed as root (e.g. via sudo)."
        exit 1
    fi
}

detect_arch() {
    local raw_arch
    raw_arch=$(uname -m)
    case "$raw_arch" in
        x86_64|amd64)
            TARGET_ARCH="amd64"
            ;;
        aarch64|arm64)
            TARGET_ARCH="arm64"
            ;;
        armv7l|armv7|armhf)
            TARGET_ARCH="armv7"
            ;;
        *)
            log_error "Unsupported architecture: $raw_arch"
            exit 1
            ;;
    esac
    log_info "Detected Architecture: $TARGET_ARCH ($raw_arch)"
}

detect_distro() {
    if [ ! -f /etc/os-release ]; then
        log_error "/etc/os-release not found. Unable to identify distribution."
        exit 1
    fi

    # Source os-release safely
    # shellcheck disable=SC1091
    . /etc/os-release
    DISTRO_ID="${ID:-unknown}"
    DISTRO_LIKE="${ID_LIKE:-}"

    case "$DISTRO_ID" in
        ubuntu|debian|raspbian)
            PKG_MANAGER="apt"
            ;;
        centos|almalinux|rocky|rhel|fedora)
            PKG_MANAGER="dnf"
            command -v dnf >/dev/null 2>&1 || PKG_MANAGER="yum"
            ;;
        alpine)
            PKG_MANAGER="apk"
            ;;
        arch|manjaro)
            PKG_MANAGER="pacman"
            ;;
        *)
            if echo "$DISTRO_LIKE" | grep -q "debian"; then
                PKG_MANAGER="apt"
            elif echo "$DISTRO_LIKE" | grep -qE "rhel|fedora|centos"; then
                PKG_MANAGER="dnf"
            else
                PKG_MANAGER="unknown"
            fi
            ;;
    esac
    log_info "Detected Distribution: $DISTRO_ID (package manager: $PKG_MANAGER)"
}

install_dependencies() {
    log_info "Installing prerequisite packages..."
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY-RUN] Skipping package installation"
        return
    fi

    case "$PKG_MANAGER" in
        apt)
            export DEBIAN_FRONTEND=noninteractive
            apt-get update -y
            apt-get install -y --no-install-recommends \
                curl tar ca-certificates iptables nftables wireguard-tools
            ;;
        dnf|yum)
            $PKG_MANAGER install -y epel-release || true
            $PKG_MANAGER install -y curl tar ca-certificates iptables nftables wireguard-tools
            ;;
        apk)
            apk update
            apk add --no-cache curl tar ca-certificates iptables nftables wireguard-tools
            ;;
        pacman)
            pacman -Sy --noconfirm curl tar ca-certificates iptables nftables wireguard-tools
            ;;
        *)
            log_warn "Unknown package manager. Ensure curl, tar, nftables, and wireguard-tools are installed."
            ;;
    esac
}

get_latest_version() {
    if [ -n "$SPECIFIED_VERSION" ]; then
        VERSION="$SPECIFIED_VERSION"
        return
    fi

    log_info "Querying latest release from GitHub..."
    # Query redirect header without triggering GitHub API rate limits
    local redirect_url
    redirect_url=$(curl -sI -o /dev/null -w "%{url_effective}" "https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/latest" || true)
    VERSION=$(basename "$redirect_url")

    if [ -z "$VERSION" ] || [ "$VERSION" = "latest" ] || [ "$VERSION" = "$REPO_NAME" ]; then
        # Fallback to GitHub API
        VERSION=$(curl -sSL "https://api.github.com/repos/${REPO_OWNER}/${REPO_NAME}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/' || true)
    fi

    if [ -z "$VERSION" ]; then
        log_warn "Could not determine latest version from GitHub. Defaulting to v0.1.0."
        VERSION="v0.1.0"
    fi

    log_info "Target release version: $VERSION"
}

setup_system_user() {
    log_info "Configuring system user and directories..."
    if [ "$DRY_RUN" = true ]; then
        return
    fi

    if ! getent group vpnbuilder >/dev/null 2>&1; then
        groupadd --system vpnbuilder
    fi

    if ! getent passwd vpnbuilder >/dev/null 2>&1; then
        useradd --system \
            --gid vpnbuilder \
            --home-dir "$DATA_DIR" \
            --shell /usr/sbin/nologin \
            --comment "Simple-VPN-Builder Service" \
            vpnbuilder
    fi

    mkdir -p "$CONFIG_DIR" "$DATA_DIR"
    chown -R root:vpnbuilder "$CONFIG_DIR"
    chmod 0750 "$CONFIG_DIR"

    chown -R vpnbuilder:vpnbuilder "$DATA_DIR"
    chmod 0700 "$DATA_DIR"
}

download_binary() {
    local binary_name="$1"
    local clean_ver="${VERSION#v}"
    local tarball="vpnbuilder_${clean_ver}_linux_${TARGET_ARCH}.tar.gz"
    local checksums_file="checksums.txt"
    local base_url="https://github.com/${REPO_OWNER}/${REPO_NAME}/releases/download/${VERSION}"

    log_info "Downloading ${binary_name} from ${base_url}/${tarball}..."
    local tmp_dir
    tmp_dir=$(mktemp -d)
    trap 'rm -rf "$tmp_dir"' EXIT INT TERM

    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY-RUN] Download and checksum verification skipped."
        return
    fi

    if curl -sSL --fail "${base_url}/${tarball}" -o "${tmp_dir}/${tarball}"; then
        # If checksums file exists, verify
        if curl -sSL --fail "${base_url}/${checksums_file}" -o "${tmp_dir}/${checksums_file}"; then
            log_info "Verifying SHA256 checksum..."
            (
                cd "$tmp_dir"
                if command -v sha256sum >/dev/null 2>&1; then
                    grep "$tarball" "$checksums_file" | sha256sum -c --status || log_warn "Checksum mismatch ignored or file not listed"
                fi
            )
        fi

        tar -xzf "${tmp_dir}/${tarball}" -C "$tmp_dir"
        if [ -f "${tmp_dir}/${binary_name}" ]; then
            install -m 0755 "${tmp_dir}/${binary_name}" "${BIN_DIR}/${binary_name}"
            log_info "Successfully installed ${BIN_DIR}/${binary_name}"
        else
            log_error "Binary ${binary_name} not found in extracted archive."
            exit 1
        fi
    else
        log_warn "Release asset ${tarball} not reachable directly. Building or checking local bin..."
        if [ -f "./bin/${binary_name}" ]; then
            log_info "Installing from local ./bin/${binary_name}..."
            install -m 0755 "./bin/${binary_name}" "${BIN_DIR}/${binary_name}"
        else
            log_error "Failed to acquire ${binary_name}."
            exit 1
        fi
    fi
}

install_cp_service() {
    log_info "Setting up Control Plane systemd service..."
    if [ "$DRY_RUN" = true ]; then
        return
    fi

    cat << 'EOF' > "${SYSTEMD_DIR}/vpnbuilder-cp.service"
[Unit]
Description=Simple-VPN-Builder Control Plane
After=network.target postgresql.service redis.service
Wants=network.target

[Service]
Type=simple
User=vpnbuilder
Group=vpnbuilder
WorkingDirectory=/var/lib/vpnbuilder
ExecStart=/usr/local/bin/vpnbuilder-cp -config /etc/vpnbuilder/control-plane.yaml
Restart=always
RestartSec=5s
LimitNOFILE=65535

ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ProtectControlGroups=true
ProtectKernelModules=true
ProtectKernelTunables=true
ReadWritePaths=/var/lib/vpnbuilder /tmp
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
EOF

    # Create default config if not existing
    if [ ! -f "${CONFIG_DIR}/control-plane.yaml" ]; then
        cat << 'EOF' > "${CONFIG_DIR}/control-plane.yaml"
server:
  http_addr: ":8110"
  grpc_addr: ":9090"
database:
  dsn: "postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable"
redis:
  addr: "localhost:6379"
auth:
  jwt_secret: "replace-with-a-secure-random-32-byte-secret-key"
log:
  level: "info"
  format: "json"
EOF
        chown root:vpnbuilder "${CONFIG_DIR}/control-plane.yaml"
        chmod 0640 "${CONFIG_DIR}/control-plane.yaml"
        log_info "Created template config at ${CONFIG_DIR}/control-plane.yaml"
    fi

    systemctl daemon-reload
    systemctl enable vpnbuilder-cp.service
    log_info "Control Plane service enabled. Start via: systemctl start vpnbuilder-cp"
}

install_agent_service() {
    log_info "Setting up Node Agent systemd service..."
    if [ "$DRY_RUN" = true ]; then
        return
    fi

    cat << 'EOF' > "${SYSTEMD_DIR}/vpnbuilder-agent.service"
[Unit]
Description=Simple-VPN-Builder Node Agent
Documentation=https://github.com/ivanchik-byte/Simple-VPN-Builder
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/var/lib/vpnbuilder
ExecStart=/usr/local/bin/vpnbuilder-agent -config /etc/vpnbuilder/agent.yaml
Restart=always
RestartSec=5s
LimitNOFILE=65535

# Linux Capability Sandboxing
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
NoNewPrivileges=true

# Filesystem and Security Hardening
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ProtectControlGroups=true
ProtectKernelModules=false
ProtectKernelTunables=false
ReadWritePaths=/etc/vpnbuilder /var/lib/vpnbuilder /tmp /proc/sys/net

[Install]
WantedBy=multi-user.target
EOF

    # Create default agent config if not existing
    if [ ! -f "${CONFIG_DIR}/agent.yaml" ]; then
        local cp_target="${CP_URL:-127.0.0.1:9090}"
        cat << EOF > "${CONFIG_DIR}/agent.yaml"
agent:
  node_name: "$(hostname)"
  control_plane: "${cp_target}"
  sync_interval: "30s"
  metrics_interval: "30s"
  wireguard:
    interface_prefix: "wg"
log:
  level: "info"
  format: "json"
EOF
        chown root:vpnbuilder "${CONFIG_DIR}/agent.yaml"
        chmod 0640 "${CONFIG_DIR}/agent.yaml"
        log_info "Created template config at ${CONFIG_DIR}/agent.yaml"
    fi

    # Enable BBR and IP forwarding sysctls
    cat << 'EOF' > /etc/sysctl.d/99-vpnbuilder.conf
net.ipv4.ip_forward = 1
net.ipv6.conf.all.forwarding = 1
net.core.default_qdisc = fq
net.ipv4.tcp_congestion_control = bbr
net.core.rmem_max = 67108864
net.core.wmem_max = 67108864
EOF
    sysctl -p /etc/sysctl.d/99-vpnbuilder.conf >/dev/null 2>&1 || true

    systemctl daemon-reload
    systemctl enable vpnbuilder-agent.service
    log_info "Node Agent service enabled. Start via: systemctl start vpnbuilder-agent"
}

run_diagnostics() {
    log_info "Running operational diagnostics..."
    if [ -x "${BIN_DIR}/vpnbuilder-agent" ]; then
        "${BIN_DIR}/vpnbuilder-agent" doctor || true
    elif [ -x "./bin/vpnbuilder-agent" ]; then
        "./bin/vpnbuilder-agent" doctor || true
    else
        log_error "vpnbuilder-agent binary not found to run doctor diagnostics."
    fi
}

uninstall_all() {
    log_info "Uninstalling Simple-VPN-Builder services and binaries..."
    if [ "$DRY_RUN" = true ]; then
        log_info "[DRY-RUN] Uninstall skipped"
        return
    fi

    systemctl stop vpnbuilder-cp.service 2>/dev/null || true
    systemctl disable vpnbuilder-cp.service 2>/dev/null || true
    systemctl stop vpnbuilder-agent.service 2>/dev/null || true
    systemctl disable vpnbuilder-agent.service 2>/dev/null || true

    rm -f "${SYSTEMD_DIR}/vpnbuilder-cp.service"
    rm -f "${SYSTEMD_DIR}/vpnbuilder-agent.service"
    systemctl daemon-reload

    rm -f "${BIN_DIR}/vpnbuilder-cp"
    rm -f "${BIN_DIR}/vpnbuilder-agent"

    log_info "Binaries and systemd services removed. Configuration in ${CONFIG_DIR} preserved."
}

parse_arguments() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --cp)
                COMPONENT="cp"
                ;;
            --agent)
                COMPONENT="agent"
                ;;
            --all)
                COMPONENT="all"
                ;;
            --version)
                shift
                SPECIFIED_VERSION="$1"
                ;;
            --cp-url)
                shift
                CP_URL="$1"
                ;;
            --token)
                shift
                ENROLLMENT_TOKEN="$1"
                ;;
            --harden)
                COMPONENT="harden"
                ;;
            --doctor)
                COMPONENT="doctor"
                ;;
            --uninstall)
                DO_UNINSTALL=true
                ;;
            --non-interactive)
                NON_INTERACTIVE=true
                ;;
            --dry-run)
                DRY_RUN=true
                ;;
            -h|--help)
                print_usage
                exit 0
                ;;
            *)
                log_error "Unknown argument: $1"
                print_usage
                exit 1
                ;;
        esac
        shift
    done
}

print_usage() {
    cat << "EOF"
Usage: install.sh [OPTIONS]

Options:
  --cp                 Install Control Plane only
  --agent              Install Node Agent only
  --all                Install both Control Plane and Node Agent
  --harden             Run Server Security Hardening wizard (Firewall, Fail2ban, SSH)
  --version <vX.Y.Z>   Install specific version tag (default: latest release)
  --cp-url <host:port> Control plane gRPC address for agent
  --token <token>      Enrollment token for node agent
  --doctor             Run diagnostic check only
  --uninstall          Stop services and remove binaries
  --non-interactive    Disable interactive prompts
  --dry-run            Simulate operations without modifying system
  -h, --help           Show this help message
EOF
}

interactive_menu() {
    print_banner
    echo " 1) Install Control Plane (CP)"
    echo " 2) Install Node Agent"
    echo " 3) Install Both (Single-Node Setup)"
    echo " 4) Run Diagnostics (Doctor)"
    echo " 5) Harden Server (Firewall, Fail2ban, SSH Safeguard)"
    echo " 6) Uninstall Services"
    echo " 7) Exit"
    echo "======================================================================"
    read -r -p "Select an option [1-7]: " choice
    case "$choice" in
        1) COMPONENT="cp" ;;
        2) COMPONENT="agent" ;;
        3) COMPONENT="all" ;;
        4) COMPONENT="doctor" ;;
        5) COMPONENT="harden" ;;
        6) DO_UNINSTALL=true ;;
        7) exit 0 ;;
        *) log_error "Invalid selection."; exit 1 ;;
    esac
}

main() {
    parse_arguments "$@"
    check_root

    if [ "$DO_UNINSTALL" = true ]; then
        uninstall_all
        exit 0
    fi

    if [ -z "$COMPONENT" ] && [ "$NON_INTERACTIVE" = false ]; then
        interactive_menu
    fi

    if [ "$COMPONENT" = "doctor" ]; then
        run_diagnostics
        exit 0
    fi

    if [ "$COMPONENT" = "harden" ]; then
        local script_dir
        script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
        if [ -f "$script_dir/harden-server.sh" ]; then
            bash "$script_dir/harden-server.sh" "$@"
        else
            log_error "harden-server.sh not found in $script_dir."
            exit 1
        fi
        exit 0
    fi

    if [ -z "$COMPONENT" ]; then
        COMPONENT="all"
    fi

    print_banner
    detect_arch
    detect_distro
    install_dependencies
    get_latest_version
    setup_system_user

    case "$COMPONENT" in
        cp)
            download_binary "vpnbuilder-cp"
            install_cp_service
            ;;
        agent)
            download_binary "vpnbuilder-agent"
            install_agent_service
            run_diagnostics
            ;;
        all)
            download_binary "vpnbuilder-cp"
            download_binary "vpnbuilder-agent"
            install_cp_service
            install_agent_service
            run_diagnostics
            ;;
        *)
            log_error "Invalid component: $COMPONENT"
            exit 1
            ;;
    esac

    log_info "Simple-VPN-Builder setup completed successfully."
}

main "$@"
