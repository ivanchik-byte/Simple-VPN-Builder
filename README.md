# Simple-VPN-Builder

> **Distributed multi-node VPN control plane**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org/)
[![Build Status](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/Security-Policy-green.svg)](SECURITY.md)
[![Telegram Chat](https://img.shields.io/badge/Telegram-Chat-26A5E4?logo=telegram)](https://t.me/ivanchikbyte)
[![Telegram Channel](https://img.shields.io/badge/Telegram-Channel_RU-26A5E4?logo=telegram)](https://t.me/ivanchik_byte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?logo=gmail)](mailto:ivanchikbyte@gmail.com)

| [Русская версия](READMEru.md) | [Report an issue](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues) |

Simple-VPN-Builder is a distributed, multi-tenant VPN orchestration platform for commercial VPN providers, enterprise overlay networks, and censorship-circumvention deployments. A central Go control plane manages lightweight autonomous node agents running on remote exit servers.

## Why this project

Single-server panels (3X-UI, Marzban, standalone WireGuard) hit a ceiling: one machine, one failure domain, manual per-node work. Simple-VPN-Builder starts from the opposite end — the control plane is separated from exit nodes from day one, so capacity grows by adding cheap servers in new regions instead of resizing one box. Nodes stay autonomous: if the control plane link drops, established tunnels keep passing traffic and the agent re-syncs itself when the stream is back.

## Contents

- [What it does](#what-it-does)
- [Architecture](#architecture)
- [Tech stack](#tech-stack)
- [Prerequisites](#prerequisites)
- [Quick start](#quick-start)
- [Usage](#usage)
- [Configuration](#configuration)
- [Service endpoints and ports](#service-endpoints-and-ports)
- [Project structure](#project-structure)
- [Available commands](#available-commands)
- [Testing](#testing)
- [Deployment](#deployment)
- [Documentation](#documentation)
- [Author and contacts](#author-and-contacts)
- [Contributing](#contributing)
- [License](#license)

## What it does

- **Control plane**: REST API, admin web console, subscription delivery, billing, Telegram CRM bot, and a gRPC hub that pushes config deltas to nodes over bidirectional mTLS streams.
- **Node agent**: applies WireGuard / AmneziaWG state via netlink, manages Xray VLESS-Reality inbounds, enforces nftables NAT rules, and reports health and telemetry.
- **Subscription delivery**: per-user token URLs (`/sub/{token}`) with content negotiation for Sing-box, Clash Meta (Mihomo), official WireGuard, AmneziaVPN, and base64 bundles.
- **Admin console**: server-rendered dashboard (HTMX + Alpine.js) with nodes, users, plans, credentials, billing, audit log, and broadcast management.
- **Telegram bot**: lead capture, trial issuance, payments, and account management backed by the same API.
- **Reliability**: Prometheus metrics, OpenTelemetry tracing, Redis sliding-window rate limiting, and database backup/restore scripts.
- **Packaging**: multi-arch binaries, `.deb`/`.rpm` packages, SBOM, Cosign signatures, Docker images, systemd units, and a one-line installer.

## Architecture

```
+---------------------------------------------------------------------------------+
|                                 CONTROL PLANE                                   |
|                                                                                 |
|   +-------------------+   +--------------------+   +------------------------+   |
|   |   REST API v1     |   |   Admin Web UI     |   |  Subscription Delivery |   |
|   |  (:8110 /api/v1)  |   |  (:8110 /admin)   |   |   (:8110 /sub/{token}) |   |
|   +---------+---------+   +---------+----------+   +-----------+------------+   |
|             |                       |                          |                |
|             +-----------------------+--------------------------+                |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |  Service Domain    |                                |
|                           |  (Business Logic)  |                                |
|                           +----+----------+----+                                |
|                                |          |                                     |
|               +----------------v---+  +---v----------------+                    |
|               |  PostgreSQL 16     |  |   Redis 7 Cache    |                    |
|               |  (Persistent Store)|  | (Limiter / Tokens) |                    |
|               +--------------------+  +--------------------+                    |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |   gRPC Agent Hub   |                                |
|                           |   (:9090 with mTLS)|                                |
|                           +---------+----------+                                |
+-------------------------------------|-------------------------------------------+
                                       |
                        Bidirectional gRPC Streaming
                        (mTLS + Token Bucket Limit)
                                       |
          +----------------------------+----------------------------+
          |                                                         |
+--------v---------------------------------+     +-----------------v-----------------------+
|          NODE AGENT (Region 1)           |     |         NODE AGENT (Region 2)           |
|                                          |     |                                         |
| +--------------------------------------+ |     | +-------------------------------------+ |
| |        gRPC Sync & Heartbeat         | |     | |        gRPC Sync & Heartbeat        | |
| +-------------------+------------------+ |     | +-------------------+-----------------+ |
|                     |                    |     |                     |                 |
|      +--------------+-------------+      |     |      +--------------+-------------+   |
|      |                            |      |     |      |                            |   |
| +----v-------------+     +--------v----+ |     | +----v-------------+     +--------v-+ |
| | WireGuard/AWG    |     | Xray VLESS  | |     | | WireGuard/AWG    |     | Xray     | |
| | Netlink Engine   |     | Reality Core| |     | | Netlink Engine   |     | Reality  | |
| +----+-------------+     +--------+----+ |     | +----+-------------+     +--------+-+ |
|      |                            |      |     |      |                            |   |
| +----v----------------------------v----+ |     | +----v----------------------------v-+ |
| |        nftables Firewall & NAT       | |     | |        nftables Firewall & NAT      | |
| +--------------------------------------+ |     | +-------------------------------------+ |
+------------------------------------------+     +-----------------------------------------+
```

## Tech stack

- **Language**: Go 1.25
- **HTTP**: Chi router, `html/template` admin console, HTMX + Alpine.js
- **Data**: PostgreSQL 16 (pgx, golang-migrate, sqlc), Redis 7 (rate limiting, revocation lists)
- **Node link**: gRPC bidirectional streaming with mTLS, Protobuf (`proto/agent/v1`)
- **VPN core**: WireGuard / AmneziaWG via netlink, Xray-core VLESS-Reality, nftables
- **Auth**: JWT access/refresh, API keys, TOTP 2FA, HMAC CSRF for web forms
- **Observability**: Prometheus metrics, OpenTelemetry tracing
- **Release**: GoReleaser, Docker, systemd, Helm chart stub, cloud-init templates

## Prerequisites

- Go 1.25+
- Docker and Docker Compose v2 (for the local stack)
- `buf`, `sqlc`, `oapi-codegen`, `golangci-lint`, `migrate` (install via `make install-tools`)
- Linux with WireGuard kernel module for running an exit node

## Quick start

### One-line install (server)

Control plane plus agent on any modern Linux server:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- --all
```

Exit node only:

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- \
  --agent \
  --cp-url cp.vpn.example.com:9090
```

### Node diagnostics

```bash
vpnbuilder-agent doctor
vpnbuilder-agent doctor --json
```

### Local development

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder

# Developer tools (buf, sqlc, oapi-codegen, golangci-lint, migrate, air)
make install-tools

# PostgreSQL 16 + Redis 7
make dev-up

# Build all binaries
make build

# Tests with race detector
make test

# Control plane with live reload
make run-cp
```

On first start with an empty database the control plane creates an `owner` account (`admin@vpnbuilder.local`). Change its password immediately after the first login. The seed account is created only on fresh installs and is never reset on restart.

## Usage

Get a subscription config (works in a browser, Sing-box, Clash Meta, AmneziaVPN):

```bash
curl -H "User-Agent: sing-box" http://localhost:8110/sub/<user-token>
```

Log in to the API and list nodes:

```bash
TOKEN=$(curl -s -X POST http://localhost:8110/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{"email":"admin@vpnbuilder.local","password":"changeme"}' | grep -o '"access_token":"[^"]*"' | cut -d'"' -f4)

curl -H "Authorization: Bearer $TOKEN" http://localhost:8110/api/v1/nodes/
```

Check node health and stream metrics:

```bash
vpnbuilder-agent doctor --json
curl -H "Authorization: Bearer $VPNBUILDER_METRICS_TOKEN" http://node:8081/metrics
```

## Configuration

Configuration is YAML plus environment variables prefixed with `VPNBUILDER_` (dots become underscores, e.g. `VPNBUILDER_AUTH_JWT_SECRET`).

| Variable | Description | Example |
| :--- | :--- | :--- |
| `VPNBUILDER_SERVER_HTTP_ADDR` | REST/admin listen address | `:8110` |
| `VPNBUILDER_SERVER_GRPC_ADDR` | gRPC hub listen address | `:9090` |
| `VPNBUILDER_DATABASE_DSN` | PostgreSQL connection string | `postgres://user:pass@localhost:5432/vpnbuilder` |
| `VPNBUILDER_REDIS_ADDR` | Redis address | `localhost:6379` |
| `VPNBUILDER_AUTH_JWT_SECRET` | JWT signing secret, min 32 bytes (required) | output of `openssl rand -hex 32` |
| `VPNBUILDER_SERVER_CORS_ALLOWED_ORIGINS` | Allowed CORS origins | `https://admin.example.com` |
| `VPNBUILDER_METRICS_TOKEN` | Bearer token for agent `:8081/metrics` | output of `openssl rand -hex 32` |
| `CONTROL_PLANE_API_KEY` | Internal API key for bot-to-CP calls | output of `openssl rand -hex 24` |
| `TELEGRAM_BOT_TOKEN` | Telegram bot token from @BotFather | `123456:ABC...` |

Never copy the dev defaults from `docker/docker-compose.yml` into production. Generate fresh secrets per environment.

## Service endpoints and ports

Control plane (`:8110` / `:9090`):

| Endpoint | Description |
| :--- | :--- |
| `/api/v1` | REST API (JWT or API key) |
| `/admin` | Admin web console (cookie session + CSRF) |
| `/sub/{token}` | Per-user subscription configs (public token URL) |
| `/metrics` | Prometheus metrics (authentication required) |
| `/healthz`, `/readyz` | Liveness and readiness probes |

Node agent:

| Port | Description |
| :--- | :--- |
| `:8081/healthz`, `:8081/readyz` | Agent probes |
| `:8081/metrics` | Agent metrics (`Bearer $VPNBUILDER_METRICS_TOKEN`) |
| `51820/udp` | WireGuard |
| `443/tcp` | VLESS-Reality |

Local development extras: PostgreSQL `5432`, Redis `6379`, Vite dev server `5173`.

## Project structure

```
.
├── .github/              # CI, release, CodeQL, secret scanning, issue templates
├── api/                  # OpenAPI 3.1 specification
├── cmd/
│   ├── control-plane/    # Control plane entrypoint
│   ├── agent/            # Node agent entrypoint (daemon and doctor)
│   └── bot/              # Telegram bot entrypoint
├── deploy/
│   ├── cloud-init/       # One-click cloud exit-node templates
│   └── helm/             # Kubernetes Helm chart
├── docker/               # Dockerfiles and Compose stack
├── docs/                 # Guides and references
├── internal/
│   ├── controlplane/     # REST API, auth, services, sqlc store, web console, AI tools
│   ├── agent/            # WireGuard/AWG netlink, Xray manager, syncer, doctor
│   ├── bot/              # Telegram bot engine, payments, i18n
│   └── shared/           # Config, logger, metrics, tracing
├── migrations/           # PostgreSQL migrations (golang-migrate, up and down)
├── packaging/
│   └── systemd/          # systemd units with sandboxing
├── pkg/
│   ├── openapi/          # Generated REST API server and models
│   └── proto/            # Generated gRPC stubs
├── proto/                # Protobuf schema
├── scripts/              # Installer, DB backup/restore, server hardening
└── tests/                # Integration and packaging suites
```

## Available commands

| Command | Description |
| :--- | :--- |
| `make build` | Build control-plane, agent, and bot binaries into `bin/` |
| `make test` | Run all tests with the race detector |
| `make test-coverage` | Tests with HTML coverage report |
| `make test-integration` | Integration tests (build tag `integration`) |
| `make lint` | Run golangci-lint |
| `make verify` | Tidy, lint, and test |
| `make generate` | Regenerate Protobuf, sqlc, and OpenAPI code |
| `make migrate-up` / `make migrate-down` | Apply / roll back one migration (`DATABASE_URL` required) |
| `make dev-up` / `make dev-down` / `make dev-logs` | Local PostgreSQL + Redis stack |
| `make run-cp` / `make run-agent` | Live reload via air |
| `make docker-build` | Build control-plane and agent images |
| `make release-check` / `make release-snapshot` | Validate / dry-run a GoReleaser release |
| `make doctor` | Run node diagnostics |
| `make install-tools` | Install buf, sqlc, oapi-codegen, migrate, golangci-lint, air |
| `make clean` | Remove build artifacts |

## Testing

```bash
make test             # unit tests, race detector
make test-coverage    # HTML coverage report
make test-integration # needs Docker (testcontainers) and external daemons
```

Conventions: table-driven tests next to the code (`*_test.go`), integration suites in `tests/e2e`, packaging checks in `tests/packaging`. Every security fix ships with a regression test.

## Deployment

- **Single server / exit node**: `scripts/install.sh` (`--all`, `--cp`, `--agent`), systemd units from `packaging/systemd`.
- **Docker**: `docker/control-plane.Dockerfile`, `docker/agent.Dockerfile`, `docker/bot.Dockerfile`, Compose stack in `docker/`.
- **Kubernetes**: Helm chart in `deploy/helm` (provide JWT secret, DB DSN, and Redis address via Secrets).
- **Cloud nodes**: templates in `deploy/cloud-init`.
- **Hardening**: `scripts/harden-server.sh`; production guide in `docs/DEPLOYMENT_GUIDE.md`.

Releases are cut by pushing a `v*.*.*` tag: GoReleaser builds multi-arch binaries, `.deb`/`.rpm` packages, SBOMs, and Cosign signatures; Docker images are published to GHCR.

## Documentation

- [System Architecture](docs/ARCHITECTURE.md)
- [Production Deployment Guide](docs/DEPLOYMENT_GUIDE.md)
- [Migration Guide](docs/MIGRATION_GUIDE.md)
- [Developer Guide](docs/DEVELOPMENT.md)
- [REST and gRPC API Reference](docs/API_REFERENCE.md)
- [Manual Testing Guide](docs/MANUAL_TESTING_GUIDE.md)

## Author and contacts

Built and maintained by [ivanchik-byte](https://github.com/ivanchik-byte).

[![Telegram Chat](https://img.shields.io/badge/Telegram-Direct-26A5E4?logo=telegram)](https://t.me/ivanchikbyte)
[![Telegram Channel RU](https://img.shields.io/badge/Telegram-Channel_RU-26A5E4?logo=telegram)](https://t.me/ivanchik_byte)
[![Email](https://img.shields.io/badge/Email-ivanchikbyte@gmail.com-EA4335?logo=gmail)](mailto:ivanchikbyte@gmail.com)
[![Issues](https://img.shields.io/badge/GitHub-Issues-181717?logo=github)](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues)

Bug reports and feature requests go to [GitHub Issues](https://github.com/ivanchik-byte/Simple-VPN-Builder/issues). Security vulnerabilities are handled privately per the [Security Policy](SECURITY.md).

## Contributing

- [Contributing Guidelines](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)

## License

Simple-VPN-Builder is open-source software licensed under the [MIT License](LICENSE).
