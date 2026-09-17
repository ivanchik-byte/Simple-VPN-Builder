# Deployment Guide

This guide covers how to deploy Simple VPN Builder in production, whether you want an automated script, a bare-metal systemd setup, or Docker Compose.

---

## One-line installer

If you are running on a clean Debian or Ubuntu VPS, the fastest way to get everything running is my automated installer:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

The script downloads the control plane binary, creates a dedicated `vpnbuilder` system user, installs a systemd unit, runs database migrations, and starts the service.

---

## Bare metal with systemd

If you prefer building from source and running with systemd (how I run my primary production instance):

**Requirements:** Go 1.25+, PostgreSQL 16, Redis 7, Linux kernel 5.6+

### 1. Build the binaries

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder
make build
# Creates: ./bin/vpnbuilder-cp, ./bin/vpnbuilder-agent, and ./bin/vpnbuilder-bot
```

### 2. Set up system users and folders

```bash
useradd --system --shell /usr/sbin/nologin vpnbuilder
mkdir -p /etc/vpnbuilder /var/lib/vpnbuilder
cp bin/vpnbuilder-cp /usr/local/bin/
chown root:vpnbuilder /etc/vpnbuilder && chmod 0750 /etc/vpnbuilder
chown vpnbuilder:vpnbuilder /var/lib/vpnbuilder && chmod 0700 /var/lib/vpnbuilder
```

### 3. Create your configuration file

Save this as `/etc/vpnbuilder/control-plane.yaml`:

```yaml
server:
  http_addr: ":8110"
  grpc_addr: ":9090"
database:
  dsn: "postgres://vpnbuilder:your_password@localhost:5432/vpnbuilder?sslmode=disable"
redis:
  addr: "localhost:6379"
auth:
  jwt_secret: "replace-with-a-secure-random-32-byte-secret-key"
log:
  level: "info"
  format: "json"
```

Lock down file permissions:

```bash
chown root:vpnbuilder /etc/vpnbuilder/control-plane.yaml
chmod 0640 /etc/vpnbuilder/control-plane.yaml
```

Database migrations run automatically whenever the control plane boots. If you want to apply them manually beforehand:

```bash
VPNBUILDER_DATABASE_DSN="postgres://vpnbuilder:your_password@localhost:5432/vpnbuilder?sslmode=disable" make migrate-up
```

### 4. Create the systemd unit

Save this file as `/etc/systemd/system/vpnbuilder-cp.service`:

```ini
[Unit]
Description=Simple VPN Builder Control Plane
After=network.target postgresql.service redis.service

[Service]
Type=simple
User=vpnbuilder
Group=vpnbuilder
WorkingDirectory=/var/lib/vpnbuilder
ExecStart=/usr/local/bin/vpnbuilder-cp -config /etc/vpnbuilder/control-plane.yaml
Restart=always
RestartSec=5s
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
PrivateTmp=true
ReadWritePaths=/var/lib/vpnbuilder /tmp

[Install]
WantedBy=multi-user.target
```

### 5. Start the service

```bash
systemctl daemon-reload
systemctl enable vpnbuilder-cp
systemctl start vpnbuilder-cp
systemctl status vpnbuilder-cp
```

---

## Docker Compose

If you want to run everything in containers:

```bash
cp .env.example .env
# Edit .env with your secrets

# Start PostgreSQL, Redis, control plane, and a local agent
make dev-up
```

To tail the logs:

```bash
docker compose -f docker/docker-compose.yml logs -f control-plane
```

---

## Important security step: change default Owner credentials

> [!WARNING]
> On first start, the database seeds an initial administrator account: `admin@vpnbuilder.local` with password `Admin1234!`.
> Before doing anything else on a live server:
> 1. Open the dashboard at `http://YOUR_SERVER_IP:8110/admin/dashboard-v2`.
> 2. Go to **Settings** (`/admin/settings-v2`) -> **Administrators**.
> 3. Click **Add Administrator**, enter your personal email, a strong password, and select the **Owner** role.
> 4. Log out and sign in using your new Owner account.
> 5. **Delete** `admin@vpnbuilder.local`.
> I added a check in the backend preventing you from deleting the only remaining Owner, so you can safely create your account first and then delete the default one.

---

## Deploying node agents on VPN servers

On each remote server that should act as a VPN exit node:

### 0. Pre-create the node in the panel

Unknown nodes are refused at gRPC Register. Create it first (name must equal the agent `node_name`, default: server hostname):

```bash
curl -s -X POST http://127.0.0.1:8110/api/v1/nodes \
  -H "Authorization: Bearer $ADMIN_JWT_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"name": "frankfurt-01"}' | jq .
```

Or use the one-line bootstrap (does steps 1-4 for you):

```bash
curl -fsSL https://YOUR_PANEL_IP:8110/bootstrap/node.sh | bash -s -- \
  --panel "https://YOUR_PANEL_IP:8110" \
  --grpc "YOUR_PANEL_IP:9090" \
  --node-name "frankfurt-01"
```

### 1. Copy the agent binary

```bash
scp bin/vpnbuilder-agent root@vpn-server:/usr/local/bin/vpnbuilder-agent
```

### 2. Create `/etc/vpnbuilder/agent.yaml`

```yaml
agent:
  node_name: "frankfurt-01"
  control_plane: "your-control-plane-domain:9090"
  sync_interval: "30s"
  metrics_interval: "30s"
  wireguard:
    interface_prefix: "wg"
log:
  level: "info"
  format: "json"
```

If your panel requires mTLS, place the certificates and reference them via environment (same process as the unit below):

```env
VPNBUILDER_AGENT_NODE_NAME=frankfurt-01
VPNBUILDER_AGENT_CONTROL_PLANE=your-control-plane-domain:9090
VPNBUILDER_AGENT_CA_CERT=/etc/vpnbuilder/certs/ca.pem
VPNBUILDER_AGENT_CERT_FILE=/etc/vpnbuilder/certs/agent.crt
VPNBUILDER_AGENT_KEY_FILE=/etc/vpnbuilder/certs/agent.key
```

### 3. Create the systemd service

Save as `/etc/systemd/system/vpnbuilder-agent.service` (ships in `packaging/systemd/vpnbuilder-agent.service`):

```ini
[Unit]
Description=Simple VPN Builder Node Agent
After=network.target network-online.target
Wants=network-online.target

[Service]
Type=simple
User=root
Group=root
WorkingDirectory=/var/lib/vpnbuilder
ExecStart=/usr/local/bin/vpnbuilder-agent -config /etc/vpnbuilder/agent.yaml
Restart=always
RestartSec=5
LimitNOFILE=65535
CapabilityBoundingSet=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
AmbientCapabilities=CAP_NET_ADMIN CAP_NET_RAW CAP_NET_BIND_SERVICE
NoNewPrivileges=true
ProtectSystem=full
ProtectHome=true
PrivateTmp=true
ProtectControlGroups=true
ProtectKernelModules=false
ProtectKernelTunables=false
ReadWritePaths=/etc/vpnbuilder /var/lib/vpnbuilder /var/run /tmp /proc/sys/net

[Install]
WantedBy=multi-user.target
```

### 4. Enable and start the agent

```bash
systemctl daemon-reload
systemctl enable vpnbuilder-agent
systemctl start vpnbuilder-agent
```

---

## Automated VPS setup via cloud-init

If you spin up nodes on Hetzner, DigitalOcean, or similar providers, you can drop this into your cloud-init user-data:

```yaml
#cloud-config
packages:
  - curl
  - wireguard

runcmd:
  - curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash -s -- --agent --cp-url cp.vpn.example.com:9090
```

---

## Reverse proxy setup

I strongly advise against exposing port 8110 directly to the internet. Put it behind Caddy or Nginx.

### Caddy (recommended)

Caddy handles automatic HTTPS certificates with Let's Encrypt:

```caddyfile
vpn.yourdomain.com {
    reverse_proxy localhost:8110
}
```

### Nginx

```nginx
server {
    listen 443 ssl http2;
    server_name vpn.yourdomain.com;

    ssl_certificate     /etc/letsencrypt/live/vpn.yourdomain.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/vpn.yourdomain.com/privkey.pem;

    location / {
        proxy_pass         http://localhost:8110;
        proxy_set_header   Host $host;
        proxy_set_header   X-Real-IP $remote_addr;
        proxy_set_header   X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header   X-Forwarded-Proto $scheme;
        
        # Turn off buffering so Server-Sent Events work for the AI Copilot:
        proxy_buffering    off;
        proxy_cache        off;
    }
}
```

---

## Restricting admin panel access

If you do not want your admin panel publicly reachable, here are three simple ways to lock it down:

### Cloudflare Tunnel

```bash
cloudflared tunnel login
cloudflared tunnel create vpn-builder

# In ~/.cloudflared/config.yml:
# tunnel: <your-tunnel-id>
# credentials-file: /root/.cloudflared/<your-tunnel-id>.json
# ingress:
#   - hostname: vpn-admin.yourdomain.com
#     service: http://localhost:8110
#   - service: http_status:404

cloudflared tunnel route dns vpn-builder vpn-admin.yourdomain.com
cloudflared service install
```

Now you can put Cloudflare Access (OAuth / Google / GitHub login) in front of `vpn-admin.yourdomain.com`.

### Tailscale overlay

```bash
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up
```

Then in `/etc/vpnbuilder/control-plane.yaml`, set:

```yaml
server:
  http_addr: "100.x.x.x:8110" # your Tailscale IP
```

The admin panel will only be reachable by devices on your private Tailscale network.

### Nginx IP whitelist

```nginx
location / {
    allow 203.0.113.0/24; # your home or office IP
    deny all;
    proxy_pass http://localhost:8110;
}
```
