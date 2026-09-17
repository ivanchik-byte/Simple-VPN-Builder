# Automated Installation Guide for AI Agents and CI/CD

This document provides non-interactive, machine-executable installation instructions designed for autonomous AI agents (Cursor, Claude Code, Roo Code, Aider, Antigravity) and unattended automation scripts.

---

## 1. Target Environment Requirements

- **Supported Operating Systems**: Ubuntu 22.04 LTS, Ubuntu 24.04 LTS, Debian 12
- **Architecture**: `linux/amd64` or `linux/arm64`
- **Privileges**: Root access (`sudo`)
- **Required Ports**:
  - `8110/tcp` (Control Plane Web UI and REST API)
  - `9090/tcp` (mTLS gRPC Hub for Edge Nodes)
  - `51820/udp` (WireGuard and AmneziaWG VPN traffic)
  - `443/tcp` (Xray VLESS-Reality TLS mimicry proxy)

---

## 2. Fast Path: Unattended 1-Command Installer

For a fresh Linux host, execute the official installation script non-interactively:

```bash
export DEBIAN_FRONTEND=noninteractive
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

### Health Check Verification
```bash
curl -sf http://127.0.0.1:8110/healthz || echo "HEALTH_CHECK_FAILED"
```

---

## 3. Containerized Path: Docker Compose Setup

If Docker is available on the host:

```bash
# 1. Clone repository
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git /opt/vpn-builder
cd /opt/vpn-builder

# 2. Generate secure production .env
cat << 'EOF' > .env
VPNBUILDER_DATABASE_DSN=postgres://vpnbuilder:SecretPass123@postgres:5432/vpnbuilder?sslmode=disable
VPNBUILDER_REDIS_ADDR=redis:6379
VPNBUILDER_SERVER_HTTP_ADDR=:8110
VPNBUILDER_SERVER_GRPC_ADDR=:9090
VPNBUILDER_AUTH_JWT_SECRET=$(openssl rand -hex 32)
CONTROL_PLANE_API_KEY=$(openssl rand -hex 16)
VPNBUILDER_LOG_LEVEL=info
VPNBUILDER_LOG_FORMAT=json
# Initial owner credentials (auto-seeded): admin@vpnbuilder.local / Admin1234!
# Recommended: create personal Owner account via Web UI and delete this default account.
EOF

# 3. Start services in background (migrations apply automatically on startup)
docker compose -f docker/docker-compose.yml up -d

# 4. Wait for services to initialize
sleep 5

# 5. Verify service health
docker compose -f docker/docker-compose.yml ps
curl -sf http://127.0.0.1:8110/healthz
```

---

## 4. Source Compilation Path (Go 1.25+)

When building and testing locally from source code:

```bash
# 1. Install prerequisites (Debian/Ubuntu)
apt-get update && apt-get install -y git curl build-essential wireguard-tools nftables postgresql redis

# 2. Install Go toolchain dependencies
go install github.com/air-verse/air@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# 3. Clone and configure
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder
cp .env.example .env

# 4. Run database migrations
make migrate-up

# 5. Run full test suite with race detector
make test

# 6. Build production binaries
make build
# Artifacts: ./bin/vpnbuilder-cp, ./bin/vpnbuilder-agent, and ./bin/vpnbuilder-bot
```

---

## 5. Edge Node Registration Workflow for AI Agents

Token-based auto-enrollment (`POST /api/v1/nodes/tokens`, CSR/CA-sign) is NOT implemented yet. To enroll a remote edge node autonomously:

1. Pre-create the node in the control plane (unknown nodes are refused at gRPC Register):
   ```bash
   curl -s -X POST http://127.0.0.1:8110/api/v1/nodes      -H "Authorization: Bearer $ADMIN_JWT_TOKEN"      -H "Content-Type: application/json"      -d '{"name": "agent-node-01"}' | jq .
   ```
   The `name` must equal the edge host's agent `node_name` (default: hostname).

2. Execute the bootstrap command on the target edge host:
   ```bash
   curl -fsSL https://YOUR_PANEL_IP:8110/bootstrap/node.sh | bash -s --      --panel "https://YOUR_PANEL_IP:8110"      --grpc "YOUR_PANEL_IP:9090"      --node-name "agent-node-01"
   ```
   The script installs `vpnbuilder-agent` via `scripts/install.sh --agent`. The `--token` flag is accepted but reserved (stored as a comment in `agent.yaml`).

3. Confirm node connection via API:
   ```bash
   curl -s http://127.0.0.1:8110/api/v1/nodes -H "Authorization: Bearer $ADMIN_JWT_TOKEN" | jq .
   ```

---

## 6. Common Troubleshooting for Agents

- **Port 9090 blocked**: Run `ufw allow 9090/tcp` on the control plane.
- **Clock Skew Error (mTLS)**: Run `timedatectl set-ntp true` on both nodes.
- **Packet Forwarding Disabled**: Run `sysctl -w net.ipv4.ip_forward=1`.
- **SSE Stream Buffered**: Set `proxy_buffering off;` in reverse proxy configuration.
