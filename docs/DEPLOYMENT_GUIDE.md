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
# Outputs: ./bin/controlplane and ./bin/agent
```

**2. Create a system user and directories:**

```bash
useradd --system --shell /usr/sbin/nologin vpnbuilder
mkdir -p /opt/vpn-builder
cp bin/controlplane /opt/vpn-builder/
cp .env.example /opt/vpn-builder/.env
chown -R vpnbuilder:vpnbuilder /opt/vpn-builder
```

**3. Configure `/opt/vpn-builder/.env`:**

```env
DATABASE_URL=postgres://vpnbuilder:password@127.0.0.1:5432/vpnbuilder
REDIS_URL=redis://127.0.0.1:6379
JWT_SECRET=replace-with-64-random-chars
ADMIN_PASSWORD=replace-with-strong-password
TELEGRAM_BOT_TOKEN=         # optional
CRYPTOBOT_TOKEN=            # optional
AI_ENDPOINT=                # optional
AI_API_KEY=                 # optional
AI_MODEL=                   # optional
TOTP_ENABLED=false
```

**4. Apply migrations:**

```bash
/opt/vpn-builder/controlplane migrate
```

**5. Create the systemd unit at `/etc/systemd/system/vpn-builder.service`:**

```ini
[Unit]
Description=Simple VPN Builder Control Plane
After=network.target postgresql.service redis.service

[Service]
Type=simple
User=vpnbuilder
WorkingDirectory=/opt/vpn-builder
EnvironmentFile=/opt/vpn-builder/.env
ExecStart=/opt/vpn-builder/controlplane serve
Restart=on-failure
RestartSec=5s
AmbientCapabilities=CAP_NET_BIND_SERVICE
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
```

**6. Enable and start:**

```bash
systemctl daemon-reload
systemctl enable vpn-builder
systemctl start vpn-builder
systemctl status vpn-builder
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
  - curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/bootstrap-node.sh | bash
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
