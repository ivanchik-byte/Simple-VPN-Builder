<div align="center">

**English** | [Русский](READMEru.md)

# Simple VPN Builder

> **Early beta.** The project is under active development toward `v0.1.0` — expect rough edges.
> Found a bug or have a fix idea? Open an [issue](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues),
> write to [ivanchikbyte@gmail.com](mailto:ivanchikbyte@gmail.com) or ping [@ivanchikbyte](https://t.me/ivanchikbyte) on Telegram.

Production self-hosted VPN management platform and automated subscription commerce engine.  
Controls WireGuard, AmneziaWG (censorship-resistant), and VLESS+Reality across distributed Linux edge servers from a single control plane.

[![CI](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml)
[![Version](https://img.shields.io/badge/version-1.0.0--dev-blue?style=flat-square)](https://github.com/ivanchik-byte/Simple-VPN-Builder)
[![Go Version](https://img.shields.io/badge/Go-1.25-00ADD8?style=flat-square&logo=go&logoColor=white)](https://go.dev/dl/)
[![PostgreSQL](https://img.shields.io/badge/PostgreSQL-16-4169E1?style=flat-square&logo=postgresql&logoColor=white)](https://www.postgresql.org/)
[![Redis](https://img.shields.io/badge/Redis-7-DC382D?style=flat-square&logo=redis&logoColor=white)](https://redis.io/)
[![License](https://img.shields.io/badge/License-MIT-green?style=flat-square)](LICENSE)
[![Telegram](https://img.shields.io/badge/Telegram-@ivanchikbyte-2CA5E0?style=flat-square&logo=telegram&logoColor=white)](https://t.me/ivanchikbyte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?style=flat-square&logo=gmail&logoColor=white)](mailto:ivanchikbyte@gmail.com)

</div>

---

## Quick Install

**One-line automated installer (Debian / Ubuntu):**

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | bash
```

**Docker Compose:**

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git && cd Simple-VPN-Builder
cp .env.example .env
make dev-up
```

After startup, open `http://YOUR_SERVER_IP:8110` in your browser.

> For autonomous AI agents and automated scripts, see [installAI.md](installAI.md).

---

## Table of Contents

- [Quick Install](#quick-install)
- [Overview](#overview)
- [Architecture](#architecture)
- [Protocol Comparison](#protocol-comparison)
- [Client Compatibility](#client-compatibility)
- [Edge Node Onboarding](#edge-node-onboarding)
- [Configuration](#configuration)
- [Ports Reference](#ports-reference)
- [Contact and Support](#contact-and-support)
- [Documentation](#documentation)
- [License](#license)

---

## Overview

Simple VPN Builder is an all-in-one VPN infrastructure platform for operators managing multiple edge servers and subscription sales:

- **Control Plane (`controlplane`)**: Central REST API, server-rendered Web UI (HTMX + Alpine.js), subscription router, Telegram commerce bot, and AI Infrastructure Copilot.
- **Node Agent (`agent`)**: Minimal daemon on each VPN host managing kernel WireGuard, AmneziaWG obfuscation, and Xray VLESS-Reality via netlink and process control.

All communication between the control plane and edge nodes runs over persistent gRPC streams secured with mutual TLS (mTLS).

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

## Protocol Comparison

| Dimension | WireGuard | AmneziaWG | VLESS + Reality |
|---|---|---|---|
| Transport | UDP 51820 | UDP 51820 (custom headers) | TCP 443 (TLS mimicry) |
| Linux Integration | Kernel module via netlink | Patched kernel module / userspace | Userspace Xray-core daemon |
| DPI Resistance | None (plain WireGuard signature) | High (junk headers, random sizes) | Maximum (TLS 1.3 fingerprint mimicry) |
| Throughput | Maximum line rate | Near-line rate | High |
| Target Use Case | Trusted networks, clean links | Regional ISP DPI blocks | Strict firewalls, GFW, egress filters |

---

## Client Compatibility

| Client App | Platforms | WireGuard | AmneziaWG | VLESS + Reality | Import Format |
|---|---|---|---|---|---|
| Sing-box | iOS, Android, macOS, Windows, Linux | Yes | Yes (v1.9+) | Yes | One-click URL / JSON |
| Streisand | iOS | Yes | No | Yes | One-click URL / Base64 |
| Shadowrocket | iOS | Yes | No | Yes | One-click URL / Base64 |
| Happ | iOS, Android | Yes | No | Yes | One-click URL / VLESS |
| v2rayNG | Android | No | No | Yes | One-click URL / Base64 |
| AmneziaVPN | iOS, Android, macOS, Windows, Linux | Yes | Yes | No | Amnezia Config JSON |
| WireGuard Official | All platforms | Yes | No | No | `.conf` file / QR Code |
| Clash Verge / Mihomo | macOS, Windows, Linux | Yes | No | Yes | Clash YAML |

---

## Edge Node Onboarding

Connecting a remote Linux node takes one command from the Admin Panel:

```bash
curl -fsSL https://YOUR_PANEL_IP:8110/bootstrap/node.sh | bash -s --   --token "ephemeral-registration-token"   --panel "https://YOUR_PANEL_IP:8110"   --grpc "YOUR_PANEL_IP:9090"
```

The script installs `vpn-agent`, generates local keys, receives signed mTLS certificates from the internal CA, configures `nftables`, and connects to the gRPC hub within seconds.

---

## Configuration

Configure via `.env` or system environment variables:

| Variable | Required | Description |
|---|---|---|
| `DATABASE_URL` | Yes | PostgreSQL connection string (`postgres://user:pass@host:5432/db`) |
| `REDIS_URL` | Yes | Redis connection string (`redis://localhost:6379`) |
| `JWT_SECRET` | Yes | Random 64-character secret for admin session tokens |
| `ADMIN_PASSWORD` | Yes | Initial master password for the `admin` account |
| `TELEGRAM_BOT_TOKEN` | No | API token from @BotFather for the sales bot |
| `CRYPTOBOT_TOKEN` | No | API token for CryptoBot payment gateway |
| `AI_ENDPOINT` | No | OpenAI-compatible base URL (e.g. `https://api.openai.com/v1`) |
| `AI_API_KEY` | No | API key for the AI Infrastructure Copilot |

See [docs/CONFIGURATION.md](docs/CONFIGURATION.md) for the complete reference and production examples.

---

## Ports Reference

| Service | Port | Protocol | Purpose |
|---|---|---|---|
| Control Plane Web & REST | `8110` | TCP (HTTP/HTTPS) | Admin UI, public subscription endpoint `/sub/{token}`, REST API |
| gRPC Hub | `9090` | TCP (mTLS) | Control plane to edge node agent streaming |
| Node Agent Health | `8081` | TCP (HTTP) | Health check and Prometheus telemetry scrape |
| WireGuard Interface | `51820` | UDP | Standard and AmneziaWG VPN client traffic |
| VLESS Reality Proxy | `443` | TCP | Xray TLS mimicry proxy traffic |

---

## Contact and Support

For questions, commercial deployments, or enterprise support:

- **Maintainer**: Ivan Chik
- **Telegram**: [https://t.me/ivanchikbyte](https://t.me/ivanchikbyte) (`@ivanchikbyte`)
- **Email**: [ivanchikbyte@gmail.com](mailto:ivanchikbyte@gmail.com)
- **Issues**: [https://github.com/ivanchik-byte/Simple-VPN-Builder/issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

---

## Documentation

| Guide | Description |
|---|---|
| [AI Agent Install Guide](installAI.md) | Non-interactive unattended instructions for AI agents |
| [Architecture](docs/ARCHITECTURE.md) | System design, zero-knowledge isolation, sequence diagrams |
| [Configuration Reference](docs/CONFIGURATION.md) | All environment variables, defaults, production `.env` |
| [Troubleshooting Guide](docs/TROUBLESHOOTING.md) | Common connection errors, sysctl, nftables, SSE fix |
| [API Reference](docs/API_REFERENCE.md) | REST API endpoints, auth, request and response schemas |
| [Deployment Guide](docs/DEPLOYMENT_GUIDE.md) | Bare metal systemd, Docker, reverse proxy, hardening |
| [Development Guide](docs/DEVELOPMENT.md) | Local development setup, migrations, code generation |
| [Migration Guide](docs/MIGRATION_GUIDE.md) | Import users and configs from 3X-UI or Marzban |
| [Manual Testing Guide](docs/MANUAL_TESTING_GUIDE.md) | Complete end-to-end verification test suite |

---

## License

[MIT](LICENSE) - Ivan Chik
