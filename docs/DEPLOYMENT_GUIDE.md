# Simple-VPN-Builder Production Deployment Guide

This guide covers production deployment scenarios for Simple-VPN-Builder:
1. Universal One-Line Installer (Recommended for standard Linux VPS)
2. Bare-Metal Systemd Deployment
3. Docker Compose Deployment
4. 1-Click Cloud-Init Exit Node Provisioning (Hetzner, DigitalOcean, Vultr, AWS)
5. Reverse Proxy & TLS Setup (Caddy / Nginx)

---

## 1. Universal One-Line Installer

The fastest method to deploy Control Plane and Node Agent on Debian, Ubuntu, CentOS, AlmaLinux, Rocky, Alpine, or Arch:

### Single-Node Setup (Control Plane + Node Agent together)
```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- --all
```

### Control Plane Only
```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- --cp
```

### Remote Exit Node Agent
```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- \
  --agent \
  --cp-url cp.vpn.example.com:9090
```

---

## 2. Bare-Metal Systemd Deployment

### 2.1 Prerequisites
- Linux Server with Kernel 5.6+ (WireGuard in-tree)
- PostgreSQL 16
- Redis 7
- `wireguard-tools` and `nftables`

### 2.2 System User and Directory Structure
```bash
sudo groupadd --system vpnbuilder
sudo useradd --system -g vpnbuilder -d /var/lib/vpnbuilder -s /usr/sbin/nologin vpnbuilder

sudo mkdir -p /etc/vpnbuilder /var/lib/vpnbuilder
sudo chown root:vpnbuilder /etc/vpnbuilder && sudo chmod 0750 /etc/vpnbuilder
sudo chown vpnbuilder:vpnbuilder /var/lib/vpnbuilder && sudo chmod 0700 /var/lib/vpnbuilder
```

### 2.3 Installing Control Plane
1. Place binary at `/usr/local/bin/vpnbuilder-cp` (`chmod 0755`).
2. Copy configuration to `/etc/vpnbuilder/control-plane.yaml`:
   ```yaml
   server:
     http_addr: ":8110"
     grpc_addr: ":9090"
   database:
     dsn: "postgres://vpnbuilder:PASSWORD@localhost:5432/vpnbuilder?sslmode=disable"
   redis:
     addr: "localhost:6379"
   auth:
     jwt_secret: "GENERATE_SECURE_32_CHAR_SECRET_KEY"
   log:
     level: "info"
     format: "json"
   ```
3. Install systemd unit from `packaging/systemd/vpnbuilder-cp.service`:
   ```bash
   sudo cp packaging/systemd/vpnbuilder-cp.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now vpnbuilder-cp
   ```

### 2.4 Installing Node Agent
1. Place binary at `/usr/local/bin/vpnbuilder-agent` (`chmod 0755`).
2. Copy configuration to `/etc/vpnbuilder/agent.yaml`:
   ```yaml
   agent:
     node_name: "node-frankfurt-01"
     control_plane: "cp.vpn.example.com:9090"
     sync_interval: "30s"
     metrics_interval: "30s"
     wireguard:
       interface_prefix: "wg"
   log:
     level: "info"
     format: "json"
   ```
3. Enable Kernel Forwarding and BBR:
   ```bash
   sudo tee /etc/sysctl.d/99-vpnbuilder.conf << EOF
   net.ipv4.ip_forward = 1
   net.ipv6.conf.all.forwarding = 1
   net.core.default_qdisc = fq
   net.ipv4.tcp_congestion_control = bbr
   EOF
   sudo sysctl --system
   ```
4. Install systemd unit:
   ```bash
   sudo cp packaging/systemd/vpnbuilder-agent.service /etc/systemd/system/
   sudo systemctl daemon-reload
   sudo systemctl enable --now vpnbuilder-agent
   ```
5. Run Node Diagnostics:
   ```bash
   vpnbuilder-agent doctor
   ```

---

## 3. Docker Compose Deployment

A complete production stack including PostgreSQL 16, Redis 7, Control Plane, and Node Agent is available in `docker/docker-compose.yml`.

```bash
cd docker/
docker compose up -d
```

Verify running containers:
```bash
docker compose ps
curl http://localhost:8110/healthz
```

Never copy the dev defaults from `docker/docker-compose.yml` (database password, JWT secret, API key) into production. Generate fresh secrets per environment:

```bash
openssl rand -hex 32  # VPNBUILDER_AUTH_JWT_SECRET
openssl rand -hex 32  # VPNBUILDER_METRICS_TOKEN (agent :8081/metrics bearer)
openssl rand -hex 24  # CONTROL_PLANE_API_KEY
```

---

## 4. 1-Click Cloud-Init Exit Node Provisioning

For automated exit node deployment on Hetzner Cloud, DigitalOcean, Vultr, or AWS EC2:

1. Locate the Cloud-Init template at `deploy/cloud-init/agent-cloud-init.yaml`.
2. Edit `control_plane` address to point to your public Control Plane domain:
   ```yaml
   control_plane: "cp.vpn.example.com:9090"
   ```
3. Pass the user-data file when creating the server:
   - **Hetzner**: Paste into "Cloud Config / User Data" box.
   - **DigitalOcean**: Check "User Data" checkbox and paste YAML.
   - **AWS EC2**: Paste into "Advanced Details -> User data".
4. The instance boots, configures sysctls, fetches the agent binary, and registers with the Control Plane automatically.

---

## 5. Reverse Proxy & TLS Setup

For public access to the REST API, Web Admin UI, and Subscription Portal on ports 80/443:

### Caddyfile (Recommended)
```caddy
vpn.example.com {
    # Admin UI & REST API
    reverse_proxy localhost:8110

    # Compression
    encode gzip zstd

    # Logging
    log {
        output file /var/log/caddy/vpn_access.log
    }
}
```

### Nginx Configuration
```nginx
server {
    listen 80;
    server_name vpn.example.com;
    return 301 https://$host$request_uri;
}

server {
    listen 443 ssl http2;
    server_name vpn.example.com;

    ssl_certificate /etc/letsencrypt/live/vpn.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/vpn.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:8110;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

---

## 6. Admin Panel Hardening & Zero Trust Access

In production, exposing the web management interface (`/admin/*`) directly to the public internet attracts automated port scanners and brute-force bots. Choose one of the following battle-tested isolation patterns:

### Option A: Cloudflare Zero Trust Tunnel (Zero Open Ports)
The most stealthy and secure approach. Ports `8110` and `9090` are bound to `127.0.0.1` or internal Docker networks only, with the VPS public firewall blocking all raw inbound traffic.

1. Create a Cloudflare Tunnel in the Cloudflare Zero Trust Dashboard (`Access -> Tunnels`).
2. Add the `cloudflared` agent to your `docker/docker-compose.yml`:
   ```yaml
     cloudflared:
       image: cloudflare/cloudflared:latest
       container_name: vpnbuilder-tunnel
       restart: unless-stopped
       command: tunnel run
       environment:
         - TUNNEL_TOKEN=YOUR_CLOUDFLARE_TUNNEL_TOKEN
       networks:
         - vpnbuilder-net
   ```
3. Route `admin.yourdomain.com` in Cloudflare Dashboard to `http://control-plane:8110`.
4. Enable Cloudflare Access Policies (require Corporate Email OTP, Google OAuth, or Hardware Security Keys before any user can even view the login page).

### Option B: Tailscale / WireGuard Admin Overlay (Internal Network Only)
Bind the Control Plane HTTP listener exclusively to your private administrative interface:

1. In `/etc/vpnbuilder/control-plane.yaml` or Docker port mapping:
   ```yaml
   server:
     http_addr: "100.64.0.5:8110" # Tailscale IP or private WireGuard interface
   ```
2. Only workstations connected to your internal management VPN mesh can reach the Admin UI at `http://100.64.0.5:8110/admin`.
3. Client subscription endpoints (`/sub/*`) and payment webhooks (`/api/v1/billing/webhooks/*`) can be selectively routed through Caddy/Nginx on the public IP.

### Option C: Reverse Proxy IP Whitelist & Firewall (UFW)
If you manage servers with static IPs, restrict administrative paths at the Nginx or Caddy layer:

```nginx
# Restrict admin dashboard to authorized operator IPs
location /admin/ {
    allow 203.0.113.50; # Authorized operator IP
    deny all;
    proxy_pass http://127.0.0.1:8110;
}

# Keep subscription links open, webhooks reachable
location /sub/ {
    proxy_pass http://127.0.0.1:8110;
}
```

