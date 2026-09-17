# Deployment Guide

## One-line installer

The fastest path for Debian or Ubuntu:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

The script installs the control plane binary, creates a `vpn-builder` system user, writes a systemd unit, applies database migrations, and starts the service.

---

## Bare metal with systemd

**Requirements:** Go 1.25, PostgreSQL 16, Redis 7, Linux kernel 5.6+

**1. Build the binaries:**

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder
make build
# Outputs: ./bin/vpnbuilder-cp, ./bin/vpnbuilder-agent and ./bin/vpnbuilder-bot
```

**2. Create a system user and directories:**

```bash
useradd --system --shell /usr/sbin/nologin vpnbuilder
mkdir -p /etc/vpnbuilder /var/lib/vpnbuilder
cp bin/vpnbuilder-cp /usr/local/bin/
chown root:vpnbuilder /etc/vpnbuilder && chmod 0750 /etc/vpnbuilder
chown vpnbuilder:vpnbuilder /var/lib/vpnbuilder && chmod 0700 /var/lib/vpnbuilder
```

**3. Configure `/etc/vpnbuilder/control-plane.yaml`:**

```yaml
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
```

```bash
chown root:vpnbuilder /etc/vpnbuilder/control-plane.yaml
chmod 0640 /etc/vpnbuilder/control-plane.yaml
```

Database migrations apply automatically on startup. Manual migration runs use the `migrate` tool:

```bash
DATABASE_URL="postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable" make migrate-up
```

**4. Create the systemd unit at `/etc/systemd/system/vpnbuilder-cp.service`**
(see `packaging/systemd/vpnbuilder-cp.service`):

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

**5. Enable and start:**

```bash
systemctl daemon-reload
systemctl enable vpnbuilder-cp
systemctl start vpnbuilder-cp
systemctl status vpnbuilder-cp
```

---

## Docker Compose

**1. Copy and configure:**

```bash
cp .env.example .env
# Edit .env with your secrets
```

**2. Start all services (migrations apply automatically on startup):**

```bash
make dev-up
# Or directly via docker compose:
docker compose -f docker/docker-compose.yml up -d
```

**3. Check logs:**

```bash
docker compose -f docker/docker-compose.yml logs -f control-plane
```

The compose file starts PostgreSQL 16, Redis 7, the control plane, and a default node agent on the same host.

---

## Node agent deployment

Deploy the node agent on each VPN server:

**1. Copy the binary:**

```bash
scp bin/agent root@vpn-server:/opt/vpn-builder/agent
```

**2. Create `/opt/vpn-builder/agent.env` on the VPN server:**

```env
CONTROL_PLANE_ADDR=your-control-plane:9090
AGENT_CERT_PATH=/opt/vpn-builder/certs/agent.crt
AGENT_KEY_PATH=/opt/vpn-builder/certs/agent.key
CA_CERT_PATH=/opt/vpn-builder/certs/ca.crt
```

Download the mTLS certificates from the admin panel under Nodes > Add Node > Download certificates.

**3. Create `/etc/systemd/system/vpn-agent.service`:**

```ini
[Unit]
Description=Simple VPN Builder Node Agent
After=network.target

[Service]
Type=simple
EnvironmentFile=/opt/vpn-builder/agent.env
ExecStart=/opt/vpn-builder/agent serve
Restart=on-failure
AmbientCapabilities=CAP_NET_ADMIN CAP_SYS_MODULE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

**4. Enable and start:**

```bash
systemctl daemon-reload
systemctl enable vpn-agent
systemctl start vpn-agent
```

---

## Cloud-init (automated VPS bootstrap)

Use this cloud-init script to provision a new VPN node automatically:

```yaml
#cloud-config
packages:
  - curl
  - wireguard

runcmd:
  - curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash -s -- --agent --cp-url cp.vpn.example.com:9090
```

---

## Reverse proxy

Run the admin panel and API behind a reverse proxy. Never expose port 8110 directly to the internet.

### Caddy

```caddyfile
vpn.yourdomain.com {
    reverse_proxy localhost:8110
}
```

Caddy handles HTTPS certificate provisioning automatically.

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
        # Required for SSE (AI Copilot streaming)
        proxy_buffering    off;
        proxy_cache        off;
    }
}
```

---

## Admin panel hardening

Pick one of these approaches to restrict access to the admin panel:

### Cloudflare Tunnel

```bash
# Install cloudflared and authenticate
cloudflared tunnel login
cloudflared tunnel create vpn-builder

# Create config at ~/.cloudflared/config.yml
cat > ~/.cloudflared/config.yml << EOF
tunnel: <your-tunnel-id>
credentials-file: /root/.cloudflared/<your-tunnel-id>.json

ingress:
  - hostname: vpn-admin.yourdomain.com
    service: http://localhost:8110
  - service: http_status:404
EOF

cloudflared tunnel route dns vpn-builder vpn-admin.yourdomain.com
cloudflared service install
```

The admin panel is then accessible only through Cloudflare Access with your identity provider.

### Tailscale overlay

```bash
# Install Tailscale on the server
curl -fsSL https://tailscale.com/install.sh | sh
tailscale up

# Bind the control plane to the Tailscale interface only
LISTEN_ADDR=100.x.x.x:8110  # set this env variable
```

Access `http://100.x.x.x:8110` from any device on your Tailscale network.

### Nginx IP whitelist

```nginx
location / {
    allow 203.0.113.0/24;   # your office or home IP range
    deny all;
    proxy_pass http://localhost:8110;
}
```
