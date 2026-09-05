# Simple-VPN-Builder Developer Guide

This document covers workflows, code generation, mock generation, and debugging practices for developers contributing to Simple-VPN-Builder.

---

## 1. Local Environment Setup

### 1.1 Tool Installation
Install all required code generators and linters with:
```bash
make install-tools
```
This installs:
- `buf`: Protobuf compilation and linting
- `sqlc`: Type-safe SQL query generation
- `oapi-codegen`: OpenAPI 3.1 server and types generator
- `golangci-lint`: Static analysis and linting
- `migrate`: Database migration utility
- `air`: Live-reload file watcher

### 1.2 Local Services Boot
Start Postgres and Redis:
```bash
make dev-up
```

Stop local services and clear volumes:
```bash
make dev-down
```

---

## 2. Code Generation Workflows

Simple-VPN-Builder relies on code generators to prevent drift between specifications and implementations.

### 2.1 Complete Code Generation
```bash
make generate
```

### 2.2 Protobuf (gRPC) Generation
Protocol definitions live in `proto/agent/v1/agent.proto`.
To re-generate Go structs and gRPC interfaces:
```bash
make generate-proto
```
Output files are written to `pkg/proto/agent/v1/`.

### 2.3 SQL Query Generation (sqlc)
Queries and migrations live in:
- `migrations/*.sql`
- `internal/controlplane/store/queries/*.sql`

To regenerate type-safe Go repositories:
```bash
make generate-sqlc
```
Output files are written to `internal/controlplane/store/`.

### 2.4 OpenAPI Spec Generation
The HTTP REST specification lives in `api/openapi.yaml`.
To regenerate Chi server boilerplate and types:
```bash
make generate-openapi
```
Output files are written to `pkg/openapi/api.gen.go`.

---

## 3. Database Migrations

Migrations are stored in `migrations/` as numbered pairs (`XXX_name.up.sql` and `XXX_name.down.sql`).

### Create a New Migration
```bash
make migrate-create
# Enter migration name, e.g. add_feature_table
```

### Apply Migrations
```bash
DATABASE_URL="postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable" make migrate-up
```

### Rollback Migration
```bash
DATABASE_URL="postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable" make migrate-down
```

---

## 4. Running and Debugging

### 4.1 Running with Air (Hot-Reload)
Run the Control Plane with automatic recompilation on file change:
```bash
make run-cp
```

Run the Node Agent (requires root or `CAP_NET_ADMIN`):
```bash
sudo $(which air) -c .air.agent.toml
```

### 4.2 Diagnostics Check
Verify host networking and kernel capabilities:
```bash
go run ./cmd/agent doctor
go run ./cmd/agent doctor --json
```

---

## 5. Automated Verification

Before creating a commit or pull request, run the verification suite:
```bash
make verify
```
This executes:
1. `make tidy` (module verification)
2. `make lint` (golangci-lint with zero warnings)
3. `make test` (unit and integration tests with race detector enabled)
