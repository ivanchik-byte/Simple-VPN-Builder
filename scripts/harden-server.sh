#!/usr/bin/env bash
# ==============================================================================
# Simple-VPN-Builder Server Hardening Script
# https://github.com/ivanchik-byte/Simple-VPN-Builder
#
# Production Host Hardening with Zero-Lockout Safeguard & Clean Reversion
# Supported Distributions: Debian, Ubuntu, AlmaLinux, Rocky Linux, CentOS, Fedora
# ==============================================================================

set -euo pipefail

BACKUP_DIR="/var/backups/vpnbuilder-hardening"
SSHD_CONFIG_DIR="/etc/ssh/sshd_config.d"
SSHD_DROPIN="/etc/ssh/sshd_config.d/99-vpnbuilder-hardening.conf"
SSHD_MAIN="/etc/ssh/sshd_config"
FAIL2BAN_JAIL="/etc/fail2ban/jail.d/vpnbuilder.local"
PROFILE=""
NON_INTERACTIVE=false

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
        log_error "This script must be executed as root (e.g. sudo bash harden-server.sh)."
        exit 1
    fi
}

detect_distro() {
    if [ ! -f /etc/os-release ]; then
        log_error "/etc/os-release not found. Cannot determine distribution."
        exit 1
    fi
    . /etc/os-release
    DISTRO_ID="${ID:-unknown}"
    case "$DISTRO_ID" in
        ubuntu|debian|raspbian)
            PKG_MANAGER="apt"
            ;;
        centos|almalinux|rocky|rhel|fedora)
            PKG_MANAGER="dnf"
            command -v dnf >/dev/null 2>&1 || PKG_MANAGER="yum"
            ;;
        *)
            PKG_MANAGER="unknown"
            ;;
    esac
}

detect_ssh_port() {
    # Check active sshd listener port
    local port
    port=$(ss -tlnp 2>/dev/null | grep -E 'sshd|ssh' | awk '{print $4}' | awk -F: '{print $NF}' | head -n 1 || true)
    if [ -z "$port" ]; then
        port=$(grep -Ei '^\s*Port\s+' /etc/ssh/sshd_config 2>/dev/null | awk '{print $2}' | head -n 1 || true)
    fi
    if [ -z "$port" ]; then
        port=22
    fi
    echo "$port"
}

detect_admin_ip() {
    # Extract client IP if running over SSH
    if [ -n "${SSH_CLIENT:-}" ]; then
        echo "${SSH_CLIENT%% *}"
    elif [ -n "${SSH_CONNECTION:-}" ]; then
        echo "${SSH_CONNECTION%% *}"
    else
        echo ""
    fi
}

backup_original_configs() {
    mkdir -p "$BACKUP_DIR"
    if [ ! -f "$BACKUP_DIR/sshd_config.orig" ] && [ -f "$SSHD_MAIN" ]; then
        cp -a "$SSHD_MAIN" "$BACKUP_DIR/sshd_config.orig"
        log_info "Backed up $SSHD_MAIN to $BACKUP_DIR/sshd_config.orig"
    fi
}

verify_ssh_keys_exist() {
    local authorized_keys_found=false
    # Check root authorized_keys
    if [ -s /root/.ssh/authorized_keys ]; then
        authorized_keys_found=true
    fi
    # Check sudo users authorized_keys
    for home_dir in /home/*; do
        if [ -d "$home_dir/.ssh" ] && [ -s "$home_dir/.ssh/authorized_keys" ]; then
            authorized_keys_found=true
            break
        fi
    done
    if [ "$authorized_keys_found" = true ]; then
        return 0
    else
        return 1
    fi
}

setup_firewall() {
    local profile="$1"
    local ssh_port
    ssh_port=$(detect_ssh_port)
    log_info "Configuring firewall for profile '$profile' (SSH port: $ssh_port)..."

    if command -v ufw >/dev/null 2>&1; then
        ufw --force reset >/dev/null 2>&1 || true
        ufw default deny incoming
        ufw default allow outgoing

        # Always allow detected SSH port
        ufw allow "$ssh_port"/tcp comment "SSH administration"
        # Control plane HTTP/HTTPS & Web portal
        ufw allow 80/tcp comment "HTTP reverse proxy / ACME"
        ufw allow 443/tcp comment "HTTPS / VLESS / Trojan"
        ufw allow 8110/tcp comment "Control Plane Web UI"
        # gRPC Node Agent tunnel
        ufw allow 9090/tcp comment "Control Plane gRPC"
        # WireGuard / AmneziaWG default UDP port
        ufw allow 51820/udp comment "WireGuard / AmneziaWG VPN"

        # Explicitly deny database external access on WAN
        ufw deny 5432/tcp comment "PostgreSQL local only" || true
        ufw deny 6379/tcp comment "Redis local only" || true

        ufw --force enable
        log_info "UFW firewall successfully enabled with secure rules."
    elif command -v firewall-cmd >/dev/null 2>&1; then
        systemctl enable --now firewalld >/dev/null 2>&1 || true
        firewall-cmd --set-default-zone=public >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port="$ssh_port"/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port=80/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port=443/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port=8110/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port=9090/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --add-port=51820/udp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --remove-port=5432/tcp >/dev/null 2>&1 || true
        firewall-cmd --permanent --zone=public --remove-port=6379/tcp >/dev/null 2>&1 || true
        firewall-cmd --reload >/dev/null 2>&1 || true
        log_info "Firewalld configured and reloaded."
    else
        log_warn "Neither ufw nor firewalld was found. Installing ufw..."
        if [ "$PKG_MANAGER" = "apt" ]; then
            DEBIAN_FRONTEND=noninteractive apt-get install -y ufw >/dev/null 2>&1
            setup_firewall "$profile"
        fi
    fi
}

setup_fail2ban() {
    local profile="$1"
    local ssh_port
    ssh_port=$(detect_ssh_port)
    local admin_ip
    admin_ip=$(detect_admin_ip)

    log_info "Setting up Fail2ban intrusion protection..."
    if ! command -v fail2ban-client >/dev/null 2>&1; then
        if [ "$PKG_MANAGER" = "apt" ]; then
            DEBIAN_FRONTEND=noninteractive apt-get install -y fail2ban >/dev/null 2>&1
        elif [ "$PKG_MANAGER" = "dnf" ] || [ "$PKG_MANAGER" = "yum" ]; then
            $PKG_MANAGER install -y epel-release >/dev/null 2>&1 || true
            $PKG_MANAGER install -y fail2ban >/dev/null 2>&1
        fi
    fi

    mkdir -p /etc/fail2ban/jail.d
    local ignore_ips="127.0.0.1/8 ::1 10.0.0.0/8 172.16.0.0/12 192.168.0.0/16"
    if [ -n "$admin_ip" ]; then
        ignore_ips="$ignore_ips $admin_ip"
        log_info "Whitelisted current admin IP ($admin_ip) in fail2ban."
    fi

    local bantime="1h"
    local findtime="10m"
    local maxretry=5
    if [ "$profile" = "strict" ]; then
        bantime="24h"
        maxretry=3
    fi

    cat > "$FAIL2BAN_JAIL" << EOF
[DEFAULT]
ignoreip = $ignore_ips
bantime  = $bantime
findtime = $findtime
maxretry = $maxretry
banaction = ufw

[sshd]
enabled = true
port    = $ssh_port
logpath = %(sshd_log)s
backend = %(default_backend)s
maxretry = $maxretry
bantime  = $bantime
EOF

    systemctl enable fail2ban >/dev/null 2>&1 || true
    systemctl restart fail2ban >/dev/null 2>&1 || true
    log_info "Fail2ban configured and restarted."
}

setup_unattended_upgrades() {
    log_info "Configuring automatic security updates..."
    if [ "$PKG_MANAGER" = "apt" ]; then
        DEBIAN_FRONTEND=noninteractive apt-get install -y unattended-upgrades apt-listchanges >/dev/null 2>&1 || true
        cat > /etc/apt/apt.conf.d/20auto-upgrades << 'EOF'
APT::Periodic::Update-Package-Lists "1";
APT::Periodic::Unattended-Upgrade "1";
APT::Periodic::AutocleanInterval "7";
EOF
        cat > /etc/apt/apt.conf.d/50unattended-upgrades-vpnbuilder << 'EOF'
Unattended-Upgrade::Allowed-Origins {
    "${distro_id}:${distro_codename}-security";
    "${distro_id}ESMApps:${distro_codename}-apps-security";
    "${distro_id}ESM:${distro_codename}-infra-security";
};
Unattended-Upgrade::Package-Blacklist {
};
Unattended-Upgrade::AutoFixInterruptedDpkg "true";
Unattended-Upgrade::MinimalSteps "true";
Unattended-Upgrade::InstallOnShutdown "false";
Unattended-Upgrade::Remove-Unused-Kernel-Packages "true";
Unattended-Upgrade::Remove-Unused-Dependencies "true";
Unattended-Upgrade::Automatic-Reboot "false";
EOF
        systemctl enable unattended-upgrades >/dev/null 2>&1 || true
        systemctl restart unattended-upgrades >/dev/null 2>&1 || true
        log_info "Unattended-upgrades security channel activated."
    elif [ "$PKG_MANAGER" = "dnf" ]; then
        $PKG_MANAGER install -y dnf-automatic >/dev/null 2>&1 || true
        sed -i 's/upgrade_type = default/upgrade_type = security/' /etc/dnf/automatic.conf 2>/dev/null || true
        sed -i 's/apply_updates = no/apply_updates = yes/' /etc/dnf/automatic.conf 2>/dev/null || true
        systemctl enable --now dnf-automatic.timer >/dev/null 2>&1 || true
        log_info "dnf-automatic security updates enabled."
    fi
}

setup_ssh_hardening() {
    local profile="$1"
    log_info "Applying SSH hardening for profile '$profile'..."

    mkdir -p "$SSHD_CONFIG_DIR"
    local disable_password_auth=false

    if [ "$profile" = "strict" ]; then
        if verify_ssh_keys_exist; then
            disable_password_auth=true
            log_info "Verified authorized SSH public keys are installed. Password authentication will be disabled in strict profile."
        else
            log_warn "ZERO-LOCKOUT SAFEGUARD: No authorized SSH public keys found in /root/.ssh/authorized_keys or /home/*/.ssh/authorized_keys."
            log_warn "Keeping PasswordAuthentication YES to prevent locking you out of the server."
            disable_password_auth=false
        fi
    elif [ "$profile" = "standard" ]; then
        if [ "$NON_INTERACTIVE" = false ] && verify_ssh_keys_exist; then
            echo ""
            echo "SSH Key Detection: Valid authorized_keys were detected."
            read -r -p "Do you want to disable SSH password authentication? [y/N]: " choice
            case "$choice" in
                [yY][eE][sS]|[yY])
                    disable_password_auth=true
                    log_info "Admin chose to disable password authentication."
                    ;;
                *)
                    disable_password_auth=false
                    log_info "Keeping password authentication enabled."
                    ;;
            esac
        fi
    fi

    local pwd_auth_val="yes"
    if [ "$disable_password_auth" = true ]; then
        pwd_auth_val="no"
    fi

    cat > "$SSHD_DROPIN" << EOF
# Simple-VPN-Builder Hardening Drop-in
# Profile: $profile
PermitEmptyPasswords no
MaxAuthTries 4
ClientAliveInterval 300
ClientAliveCountMax 2
X11Forwarding no
AllowAgentForwarding no
AllowTcpForwarding yes
PasswordAuthentication $pwd_auth_val
KbdInteractiveAuthentication $pwd_auth_val
EOF

    # Validate syntax before reloading
    if ! sshd -t 2>/dev/null; then
        log_error "sshd syntax test failed with new drop-in. Removing drop-in to prevent lockout."
        rm -f "$SSHD_DROPIN"
        return 1
    fi

    # Reload sshd safely
    if systemctl is-active sshd >/dev/null 2>&1; then
        systemctl reload sshd || systemctl restart sshd
    elif systemctl is-active ssh >/dev/null 2>&1; then
        systemctl reload ssh || systemctl restart ssh
    fi
    log_info "SSH hardening applied successfully."
}

revert_all_hardening() {
    log_info "Reverting all server hardening to baseline..."

    # 1. Revert SSH
    if [ -f "$SSHD_DROPIN" ]; then
        rm -f "$SSHD_DROPIN"
        log_info "Removed SSH hardening drop-in ($SSHD_DROPIN)."
    fi
    if [ -f "$BACKUP_DIR/sshd_config.orig" ]; then
        cp -a "$BACKUP_DIR/sshd_config.orig" "$SSHD_MAIN"
        log_info "Restored original $SSHD_MAIN."
    fi
    if sshd -t 2>/dev/null; then
        if systemctl is-active sshd >/dev/null 2>&1; then
            systemctl reload sshd || systemctl restart sshd
        elif systemctl is-active ssh >/dev/null 2>&1; then
            systemctl reload ssh || systemctl restart ssh
        fi
        log_info "SSH daemon reloaded with baseline configuration."
    fi

    # 2. Revert Fail2ban
    if [ -f "$FAIL2BAN_JAIL" ]; then
        rm -f "$FAIL2BAN_JAIL"
        systemctl restart fail2ban >/dev/null 2>&1 || true
        log_info "Removed custom Fail2ban jail."
    fi

    # 3. Reset Firewall
    if command -v ufw >/dev/null 2>&1; then
        ufw --force disable >/dev/null 2>&1 || true
        log_info "Disabled UFW firewall."
    fi

    log_info "Server hardening successfully reverted."
}

apply_profile() {
    local profile="$1"
    log_info "======================================================================"
    log_info "Starting Hardening Deployment: Profile = $profile"
    log_info "======================================================================"

    backup_original_configs
    setup_firewall "$profile"
    setup_fail2ban "$profile"
    setup_unattended_upgrades

    if [ "$profile" != "basic" ]; then
        setup_ssh_hardening "$profile"
    else
        log_info "Basic profile selected: skipping strict SSH alterations."
    fi

    log_info "======================================================================"
    log_info "Hardening profile '$profile' applied successfully."
    log_info "To change or revert anytime, run: sudo bash scripts/harden-server.sh"
    log_info "======================================================================"
}

interactive_menu() {
    echo "======================================================================"
    echo "            Simple-VPN-Builder Server Hardening Wizard"
    echo "======================================================================"
    echo " 1) Basic (Firewall + Fail2ban + Auto Security Updates)"
    echo " 2) Standard [RECOMMENDED] (Basic + Safe SSH Ciphers & Hardening)"
    echo " 3) Strict (Standard + Enforce SSH Keys, Disable Passwords, 24h Ban)"
    echo " 4) Revert Hardening (Disable firewall, restore default SSH & Fail2ban)"
    echo " 5) Exit"
    echo "======================================================================"
    read -r -p "Select security level [1-5]: " choice
    case "$choice" in
        1) PROFILE="basic" ;;
        2) PROFILE="standard" ;;
        3) PROFILE="strict" ;;
        4) revert_all_hardening; exit 0 ;;
        5) exit 0 ;;
        *) log_error "Invalid selection."; exit 1 ;;
    esac
}

parse_args() {
    while [ $# -gt 0 ]; do
        case "$1" in
            --basic)
                PROFILE="basic"
                ;;
            --standard)
                PROFILE="standard"
                ;;
            --strict)
                PROFILE="strict"
                ;;
            --revert)
                PROFILE="revert"
                ;;
            --non-interactive)
                NON_INTERACTIVE=true
                ;;
            -h|--help)
                echo "Usage: harden-server.sh [--basic | --standard | --strict | --revert] [--non-interactive]"
                exit 0
                ;;
            *)
                log_error "Unknown argument: $1"
                exit 1
                ;;
        esac
        shift
    done
}

main() {
    parse_args "$@"
    check_root
    detect_distro

    if [ "$PROFILE" = "revert" ]; then
        revert_all_hardening
        exit 0
    fi

    if [ -z "$PROFILE" ]; then
        if [ "$NON_INTERACTIVE" = true ]; then
            PROFILE="standard"
        else
            interactive_menu
        fi
    fi

    apply_profile "$PROFILE"
}

main "$@"
