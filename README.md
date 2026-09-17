<div align="center">

**English** | [Русский](READMEru.md)

# Simple VPN Builder

> **Status: active beta.** Pre-release development toward `0.1.0v`. The codebase already runs on my own live servers, though APIs and config schemas might still receive minor polish.
> If something breaks or you have ideas: open an [issue](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues) or message me directly on Telegram at [@ivanchikbyte](https://t.me/ivanchikbyte).

Self-hosted VPN control plane and subscription sales engine written in Go.  
I built it to manage WireGuard, AmneziaWG (with junk headers to bypass DPI), and Xray VLESS+Reality across distributed Linux edge servers from a single dashboard, without stitching together dozens of separate scripts.

[![CI](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-0.1.0v-blue?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/dl/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-@ivanchikbyte-2CA5E0?style=flat-square&logo=telegram&logoColor=white)](https://t.me/ivanchikbyte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?style=flat-square&logo=gmail&logoColor=white)](mailto:ivanchikbyte@gmail.com)

</div>

---

## Quick install

**One-line automated installer (Debian / Ubuntu):**

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

**Docker Compose (quickest for local testing):**

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git && cd Simple-VPN-Builder
cp .env.example .env
make dev-up
```

Once started, open `http://YOUR_SERVER_IP:8110` (or jump straight to the panel: `http://YOUR_SERVER_IP:8110/admin/dashboard-v2`).

> For automated CI pipelines and unattended AI coding agents, check [installAI.md](installAI.md).

---

## Why I built this

I built Simple VPN Builder for myself. I got tired of juggling bash scripts for WireGuard keys, running 3X-UI or Marzban for proxies, and writing separate bots to handle payments. I wanted one solid Go project that takes care of the whole workflow:

- **Control plane (`vpnbuilder-cp`)**: REST API, React 19 web dashboard (embedded directly inside the Go binary with `embed.FS`, zero external node runtime on your host), intelligent subscription routing (`/sub/{token}`) for multiple client apps, and a Telegram sales bot taking payments via Telegram Stars and CryptoBot.
- **Node agent (`vpnbuilder-agent`)**: a tiny binary for each VPN node that listens to control plane commands over gRPC (mTLS), drives kernel WireGuard via netlink, configures AmneziaWG obfuscation, and supervises Xray without brittle shell scripts.

The control plane and agents stay connected through persistent gRPC streams with mutual TLS. When you add a client or change a plan limit, the node applies the update within seconds on the fly, with zero connection drops.

---

## Architecture

```mermaid
sequenceDiagram
    autonumber
    actor User as Subscriber
    actor Admin as Administrator
    participant Bot as Telegram Sales Bot
    participant CP as Control Plane :8110
    participant Hub as gRPC Hub :9090
    participant Node as Edge Node Agent

    Admin->>CP: Onboard new edge node
    CP->>Hub: Register node and issue mTLS cert
    Hub->>Node: Establish bidirectional mTLS stream

    User->>Bot: /start, choose plan, pay via CryptoBot or Stars
    Bot->>CP: Create user and peer configuration
    CP->>Hub: Push peer public key to edge node
    Hub->>Node: Configure WireGuard / Xray via netlink
    Node-->>Hub: Peer active
    CP-->>Bot: Return subscription URL /sub/token
    Bot-->>User: Deliver 1-click config and QR code

    Node->>Hub: Stream bandwidth counters
    Hub->>CP: Ingest traffic delta and enforce limits
```

---

## Protocols supported

| Protocol | Transport | How it runs on Linux | DPI resistance | When to pick it |
|---|---|---|---|---|
| WireGuard | UDP 51820 | Kernel module via netlink | None, standard WG signatures | Clean networks, trusted links, high throughput |
| AmneziaWG | UDP 51820 (custom headers) | Kernel module or userspace shim | High (junk headers, randomized packet sizes) | When your ISP throttles or drops plain WireGuard |
| VLESS + Reality | TCP 443 (TLS mimicry) | Userspace Xray-core daemon | Maximum (mimics real TLS 1.3 handshakes) | Harsh firewalls, deep packet inspection, domain whitelists |

---

## Tested client apps

| App | Platforms | WireGuard | AmneziaWG | VLESS + Reality | Import format |
|---|---|---|---|---|---|
| Sing-box | iOS, Android, macOS, Windows, Linux | Yes | Yes (v1.9+) | Yes | One-click URL or JSON |
| Streisand | iOS | Yes | No | Yes | One-click URL or Base64 |
| Shadowrocket | iOS | Yes | No | Yes | One-click URL or Base64 |
| Happ | iOS, Android | Yes | No | Yes | One-click URL or VLESS |
| v2rayNG | Android | No | No | Yes | One-click URL or Base64 |
| AmneziaVPN | iOS, Android, macOS, Windows, Linux | Yes | Yes | No | Amnezia JSON config |
| WireGuard Official | All platforms | Yes | No | No | `.conf` file or QR code |
| Clash Verge / Mihomo | macOS, Windows, Linux | Yes | No | Yes | Clash YAML |

---

## Adding a new node

To attach a new Linux server, first pre-create the node in the panel (dashboard Nodes -> Add Node, or `POST /api/v1/nodes`) using the server hostname as the node name (unknown nodes are refused). Then run the bootstrap command on your remote VPS:

```bash
curl -fsSL https://YOUR_PANEL_IP:8110/bootstrap/node.sh | bash -s --   --panel "https://YOUR_PANEL_IP:8110"   --grpc "YOUR_PANEL_IP:9090"
```

The script installs `vpnbuilder-agent`, writes `/etc/vpnbuilder/agent.yaml`, and establishes a persistent stream to the control plane. If your panel requires mTLS, place the certificates referenced by `VPNBUILDER_AGENT_CA_CERT` / `CERT_FILE` / `KEY_FILE` before starting the service. Token-based auto-enrollment (`--token`) is reserved for a future release.

---

## Configuration

You can configure everything via `.env` or system environment variables:

| Variable | Required | What it does |
|---|---|---|
| `VPNBUILDER_DATABASE_DSN` | Yes | PostgreSQL connection string (`postgres://user:pass@host:5432/db?sslmode=disable`) |
| `VPNBUILDER_REDIS_ADDR` | Yes | Redis host and port (`localhost:6379`) |
| `VPNBUILDER_AUTH_JWT_SECRET` | Yes | Random string (at least 32 characters) for signing session tokens |
| `CONTROL_PLANE_API_KEY` | No | Shared API key for internal services like the bot (`dev-key-change-in-production`) |
| `TELEGRAM_BOT_TOKEN` | No | Bot token from @BotFather if you want the sales bot running |
| `CRYPTOBOT_TOKEN` | No | API token from @CryptoBot for cryptocurrency payments |
| `CONTROL_PLANE_URL` | No | Address of the control plane from the bot perspective (`http://localhost:8110`) |

> **Security notice (must read):**  
> On first start, the database seeds an initial owner account: `admin@vpnbuilder.local` with password `Admin1234!`.  
> Before exposing the dashboard to the public:
> 1. Log in to the web panel (`/admin/dashboard-v2`).
> 2. Go to Settings -> Administrators (`/admin/settings-v2`).
> 3. Create your own personal account with the **Owner** role and a strong password.
> 4. Log in with your new account, then **delete** `admin@vpnbuilder.local`.
> I built a check into the backend that forbids deleting the last remaining Owner, so you cannot accidentally lock yourself out.

For the full list of options, see [docs/CONFIGURATION.md](docs/CONFIGURATION.md).

---

## Default ports

| Service | Port | Protocol | Purpose |
|---|---|---|---|
| Control Plane Web & REST | `8110` | TCP (HTTP/HTTPS) | Admin dashboard, subscription link `/sub/{token}`, REST API |
| gRPC Hub | `9090` | TCP (mTLS) | Control plane to node agent streaming |
| Node Agent Health | `8081` | TCP (HTTP) | Health check `/healthz` and Prometheus telemetry |
| WireGuard & AmneziaWG | `51820` | UDP | VPN client traffic |
| VLESS Reality Proxy | `443` | TCP | Xray TLS mimicry proxy traffic |

---

## Author & support

I build this project solo. If you run into issues, find bugs, or want to discuss features:

- **Author**: Ivan Chik
- **Telegram**: [@ivanchikbyte](https://t.me/ivanchikbyte) (quickest response)
- **Email**: [ivanchikbyte@gmail.com](mailto:ivanchikbyte@gmail.com)
- **Issues & feedback**: [GitHub issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

---

## Documentation

| Guide | What it covers |
|---|---|
| [AI Agent Install Guide](installAI.md) | Unattended step by step instructions for AI coding agents and automated deployment scripts |
| [Architecture](docs/ARCHITECTURE.md) | How the subsystems fit together, database layout, and sequence flows |
| [Configuration Reference](docs/CONFIGURATION.md) | Full list of config options, defaults, and production sample `.env` |
| [Troubleshooting Guide](docs/TROUBLESHOOTING.md) | Common errors, sysctl settings, nftables issues, and SSE connection fixes |
| [API Reference](docs/API_REFERENCE.md) | REST API endpoints, auth flow, and request/response payloads |
| [Deployment Guide](docs/DEPLOYMENT_GUIDE.md) | Step by step setup with systemd, Docker, and reverse proxies |
| [Development Guide](docs/DEVELOPMENT.md) | Local dev environment, migrations, and code generation |
| [Migration Guide](docs/MIGRATION_GUIDE.md) | Moving users and credentials from 3X-UI or Marzban |
| [Manual Testing Guide](docs/MANUAL_TESTING_GUIDE.md) | Step by step manual test checklist to verify a deployment |

---

## License

[MIT](LICENSE) (c) Ivan Chik
