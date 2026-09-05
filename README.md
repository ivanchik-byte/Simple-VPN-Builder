# Simple-VPN-Builder

> **Developer-First Multi-Node Distributed VPN Control Plane & Framework**

[![Go Version](https://img.shields.io/badge/Go-1.25+-00ADD8?logo=go)](https://golang.org/)
[![Build Status](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Security Policy](https://img.shields.io/badge/Security-Policy-green.svg)](SECURITY.md)

Simple-VPN-Builder is a distributed, multi-tenant VPN infrastructure orchestration platform designed for commercial VPN providers, enterprise overlay networks, and censorship-circumvention deployments. It couples a central Go Control Plane with lightweight, autonomous Node Agents running on remote exit servers worldwide.

---

## Key Capabilities

- **Distributed Control Plane**: Centralized orchestration for unlimited exit nodes over bidirectional gRPC streams secured with mutual TLS (mTLS).
- **Multi-Protocol Engine**: Native Linux kernel WireGuard (netlink atomic diffs), AmneziaWG (custom junk packets & obfuscated headers), and Xray-core (VLESS-Reality with Vision flow).
- **Universal Subscription Delivery**: Dynamic content negotiation and User-Agent autodetection delivering configurations for Sing-box (v1.10+), Clash Meta (Mihomo), official WireGuard, AmneziaVPN, and base64 bundles.
- **Client Web Portal**: Responsive Obsidian/Zinc dark subscription portal (`/client/{token}`) with 1-click client scheme imports, QR codes, and real-time quota telemetry.
- **Built-in Node Diagnostics**: Standalone `doctor` command inspecting Linux kernel modules, sysctl packet forwarding, BBR congestion control, nftables firewall capabilities, and mTLS reachability.
- **Production Hardening & Reliability**: Prometheus RED and USE metrics pipeline, OpenTelemetry distributed tracing with W3C TraceContext, Token Bucket rate limiting, and automated database disaster recovery.
- **Turnkey Packaging & Release**: GoReleaser v2 multi-architecture binaries (`amd64`, `arm64`), native `.deb` and `.rpm` packages, Syft SBOM generation, and Sigstore Cosign keyless signatures.

---

## Architecture

```
+---------------------------------------------------------------------------------+
|                                 CONTROL PLANE                                   |
|                                                                                 |
|   +-------------------+   +--------------------+   +------------------------+   |
|   |   REST API v1     |   |   Admin Web UI     |   |  Subscription Portal   |   |
|   |  (:8110 /api/v1)  |   |   (HTMX + Alpine)  |   |     (/client/{token})  |   |
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
|               |  (Persistent Store)|  |   (Tokens/Limiter) |                    |
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

---

## Quick Start

### 1. Universal One-Line Installer
Deploy both Control Plane and Node Agent on any modern Linux server (Ubuntu, Debian, CentOS, AlmaLinux, Rocky, Alpine, Arch):

```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- --all
```

For remote exit nodes only:
```bash
curl -fsSL https://raw.githubusercontent.com/ivanchik-byte/Simple-VPN-Builder/master/scripts/install.sh | sudo bash -s -- \
  --agent \
  --cp-url cp.vpn.example.com:9090
```

### 2. Operational Diagnostics
Verify that kernel modules, packet forwarding, and firewall capabilities are operational:

```bash
vpnbuilder-agent doctor
vpnbuilder-agent doctor --json
```

### 3. Local Development Stack
```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder

# Install developer tools (buf, sqlc, oapi-codegen, golangci-lint)
make install-tools

# Start local PostgreSQL 16 + Redis 7
make dev-up

# Compile binaries
make build

# Run tests with race detector
make test

# Start Control Plane with live-reload
make run-cp
```

---

## Service Endpoints

- **REST API**: `http://localhost:8110/api/v1`
- **gRPC Stream Server**: `localhost:9090` (mTLS)
- **Admin Panel**: `http://localhost:8110/admin`
- **Client Subscription Portal**: `http://localhost:8110/client/{token}`
- **OpenAPI 3.1 Spec**: `http://localhost:8110/openapi.yaml`
- **Prometheus Metrics**: `http://localhost:8110/metrics`
- **Health & Readiness Probes**: `http://localhost:8110/healthz`, `http://localhost:8110/readyz`

---

## Project Structure

```
.
├── .github/              # CI/CD workflows, release pipelines, issue templates
├── api/                  # OpenAPI 3.1 specifications (openapi.yaml)
├── cmd/
│   ├── control-plane/    # Control plane entrypoint
│   └── agent/            # Node agent entrypoint (daemon & doctor)
├── deploy/
│   ├── cloud-init/       # 1-click cloud exit node templates (Hetzner, DO, AWS)
│   └── helm/             # Kubernetes Helm chart stub
├── docker/               # Multi-arch Dockerfiles and docker-compose.yml
├── docs/                 # Production guides, architecture, migration runbooks
├── internal/
│   ├── controlplane/     # REST API, auth, business services, sqlc store, web UI
│   ├── agent/            # WireGuard/AWG netlink, Xray manager, syncer, doctor
│   └── shared/           # Configuration, structured logger, metrics, tracing
├── migrations/           # PostgreSQL migration files (golang-migrate)
├── packaging/
│   └── systemd/          # Production systemd service units with sandboxing
├── pkg/
│   ├── openapi/          # Generated REST API server and models
│   └── proto/            # Generated gRPC Protobuf stubs
├── proto/                # Protocol Buffers schema (agent.proto)
├── scripts/              # Universal installer, DB backup, DB restore
└── tests/                # E2E integration and packaging test suites
```

---

## Development Roadmap Status

| Phase | Description | Status | Deliverables |
|---|---|---|---|
| 0 | Foundation & Tooling | COMPLETED | Go module, Makefile, Protobuf, sqlc, Docker Compose, CI |
| 1 | Control Plane Data Layer | COMPLETED | PostgreSQL pgxpool, migrations, 9 core repositories |
| 2 | Auth & API Framework | COMPLETED | JWT, API keys, TOTP 2FA, RFC 7807 problem details, Chi router |
| 3 | REST API Resources CRUD | COMPLETED | Nodes, Users, Plans, Credentials, Analytics, Audit logs |
| 4 | gRPC Agent Synchronization | COMPLETED | Bidirectional streaming, mTLS, delta config updates, watchdog |
| 5 | Node Agent & WireGuard Core | COMPLETED | wgctrl netlink diffing, AmneziaWG obfuscation, nftables NAT, BBR |
| 6 | Xray VLESS-Reality Core | COMPLETED | Reality camouflage, gRPC AlterInbound user management, stats |
| 7 | Admin Web UI | COMPLETED | Obsidian/zinc dark dashboard, HTMX, Alpine.js, Chart.js |
| 8 | Subscription Delivery Engine | COMPLETED | Sing-box, Clash Meta, WireGuard .conf, /client/{token} portal |
| 9 | Hardening & Reliability | COMPLETED | Prometheus RED/USE metrics, OTel tracing, token bucket, DR scripts |
| 10 | Release Automation & Packaging | COMPLETED | GoReleaser v2, .deb/.rpm, install.sh, doctor, cloud-init, docs |

---

## Documentation

- [System Architecture](docs/ARCHITECTURE.md)
- [Production Deployment Guide](docs/DEPLOYMENT_GUIDE.md)
- [Migration Guide (3X-UI, Marzban, Standalone WG)](docs/MIGRATION_GUIDE.md)
- [Developer Guide](docs/DEVELOPMENT.md)
- [REST & gRPC API Reference](docs/API_REFERENCE.md)
- [Manual Testing Guide](docs/MANUAL_TESTING_GUIDE.md)

---

## Community & Contributing

- [Contributing Guidelines](CONTRIBUTING.md)
- [Security Policy](SECURITY.md)
- [Code of Conduct](CODE_OF_CONDUCT.md)

---

## License

Simple-VPN-Builder is open-source software licensed under the [MIT License](LICENSE).