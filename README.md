# Simple-VPN-Builder

> **Developer-First Multi-Node VPN Control Plane & Framework**

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?logo=go)](https://golang.org/)
[![Build Status](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions/workflows/ci.yml/badge.svg)](https://github.com/ivanchik-byte/Simple-VPN-Builder/actions)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

A clean Go control plane + lightweight node agents for building commercial VPN services or private mesh networks. Built with protocol adapters (WireGuard, Xray, sing-box), first-class API, and Infrastructure-as-Code support.

## Features

- **Multi-Node Control Plane** — Manage 10s of exit nodes from a single API
- **Protocol Adapters** — WireGuard (native), VLESS/VMess/Trojan/Shadowsocks (via Xray)
- **Developer-First API** — REST + gRPC, OpenAPI 3.1, generated SDKs
- **Infrastructure as Code** — Terraform provider (planned)
- **White-Label Ready** — Embed in your product, custom branding
- **Modern Stack** — Go 1.23, PostgreSQL, Redis, gRPC/mTLS, HTMX admin UI

## Architecture

```
┌─────────────┐     gRPC/mTLS      ┌─────────────┐
│  Control    │ ◄─────────────────► │   Node      │
│  Plane      │   Config sync      │   Agent     │
│  (API, DB)  │   Heartbeats       │  (WG, Xray) │
└─────────────┘   Metrics          └─────────────┘
       │
       ▼
┌─────────────┐
│  Admin UI   │  (HTMX + Go templates)
│  REST API   │
│  gRPC API   │
└─────────────┘
```

## Quick Start (Development)

### Prerequisites
- Go 1.23+
- Docker & Docker Compose
- `buf`, `sqlc`, `golangci-lint`, `air` (installed via `make install-tools`)

### Start Local Stack
```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder
cd Simple-VPN-Builder

# Install dev tools
make install-tools

# Generate code (protobuf, sqlc)
make generate

# Start Postgres + Redis + Control Plane + Agent
make dev-up

# Or run locally with hot reload
make run-cp      # Control Plane on :8110
make run-agent   # Agent (requires root/CAP_NET_ADMIN)
```

### API Endpoints
- **REST API**: `http://localhost:8110/api/v1`
- **gRPC**: `localhost:9090` (mTLS)
- **Admin UI**: `http://localhost:8110/admin`
- **OpenAPI Spec**: `http://localhost:8110/openapi.yaml`

## Project Structure

```
.
├── cmd/
│   ├── control-plane/    # CP entrypoint
│   └── agent/            # Node agent entrypoint
├── internal/
│   ├── controlplane/     # CP private code
│   │   ├── api/          # REST handlers
│   │   ├── grpc/         # gRPC server
│   │   ├── service/      # Business logic
│   │   ├── store/        # Database access (sqlc)
│   │   └── auth/         # JWT, API keys
│   ├── agent/            # Agent private code
│   │   ├── grpc/         # gRPC client
│   │   ├── manager/      # Process/interface manager
│   │   ├── syncer/       # Config sync
│   │   └── metrics/      # Collection
│   └── shared/           # Config, logger, middleware
├── pkg/
│   ├── adapter/          # Protocol adapters
│   │   ├── wireguard/
│   │   └── xray/
│   ├── models/           # Domain models
│   ├── proto/            # Generated protobuf
│   └── openapi/          # Generated OpenAPI types
├── proto/                # .proto definitions
├── migrations/           # SQL migrations
├── docker/               # Dockerfiles, docker-compose
├── docs/                 # Documentation
└── scripts/              # Dev scripts
```

## Development Phases

| Phase | Status | Focus |
|-------|--------|-------|
| 0 | [COMPLETED] | Foundation, tooling, dev stack |
| 1 | [COMPLETED] | Data layer, migrations, repos |
| 2 | [COMPLETED] | Auth, OpenAPI, middleware |
| 3 | [IN PROGRESS] | REST resources CRUD |
| 4 | [PENDING] | gRPC agent sync (mTLS) |
| 5 | [PENDING] | Agent core + WireGuard |
| 6 | [PENDING] | Xray adapter |
| 7 | [PENDING] | Admin Web UI (HTMX) |
| 8 | [PENDING] | Subscription generation |
| 9 | [PENDING] | Hardening, observability |
| 10 | [PENDING] | Release, docs |

See [PHASES.md](PHASES.md) for detailed breakdown.

## Commands

```bash
make help           # Show all commands
make build          # Build binaries
make test           # Run tests with race detector
make test-coverage  # Coverage report
make lint           # Run golangci-lint
make generate       # Generate all code (proto, sqlc, openapi)
make migrate-up     # Run DB migrations
make dev-up         # Start local stack
make docker-build   # Build Docker images
```

## Configuration

Control Plane (`config.yaml`):
```yaml
server:
  http_addr: ":8080"
  grpc_addr: ":9090"
database:
  dsn: "postgres://user:pass@localhost/vpnbuilder?sslmode=disable"
auth:
  jwt_secret: "your-32-char-secret-minimum"
```

Agent (environment variables):
```bash
VPNBUILDER_AGENT_NODE_NAME=agent-1
VPNBUILDER_AGENT_CONTROL_PLANE=cp.example.com:9090
VPNBUILDER_AGENT_CA_CERT=/etc/vpnbuilder/ca.pem
VPNBUILDER_AGENT_CERT_FILE=/etc/vpnbuilder/agent.pem
VPNBUILDER_AGENT_KEY_FILE=/etc/vpnbuilder/agent-key.pem
```

## License

MIT License — see [LICENSE](LICENSE) for details.

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for development guidelines.

---

**Status**: Early development (Phase 0). Not ready for production use.