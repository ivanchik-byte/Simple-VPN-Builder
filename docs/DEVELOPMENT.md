# Development Guide

This guide walks through how I work on Simple VPN Builder locally, from setting up databases to regenerating code and running test suites.

---

## Tools I use

Before running services locally, install these tools:

```bash
# Go tools
go install github.com/air-verse/air@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install github.com/bufbuild/buf/cmd/buf@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
go install github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@latest

# System tools (Debian / Ubuntu)
apt-get install -y protobuf-compiler golangci-lint
```

---

## Starting local databases

You do not need the full Docker Compose stack for daily coding. I usually just spin up lightweight containers for PostgreSQL and Redis:

```bash
docker run -d --name pg \
  -e POSTGRES_USER=vpnbuilder \
  -e POSTGRES_PASSWORD=devpassword \
  -e POSTGRES_DB=vpnbuilder \
  -p 5432:5432 \
  postgres:16-alpine

docker run -d --name redis \
  -p 6379:6379 \
  redis:7-alpine
```

Set up your local `.env`:

```env
VPNBUILDER_DATABASE_DSN=postgres://vpnbuilder:devpassword@127.0.0.1:5432/vpnbuilder?sslmode=disable
VPNBUILDER_REDIS_ADDR=127.0.0.1:6379
VPNBUILDER_AUTH_JWT_SECRET=dev-secret-at-least-32-characters-long
CONTROL_PLANE_API_KEY=dev-api-key-change-in-production
```

> **Note**: when the database is first initialized, it seeds a default owner account: `admin@vpnbuilder.local` with password `Admin1234!`.

---

## Running database migrations

To apply all schema migrations up to the latest version:

```bash
make migrate-up
```

To roll back the single most recent migration:

```bash
make migrate-down
```

---

## Running with live reload

I use Air for hot reloading during development:

```bash
make run
```

Air watches `.go` files in the repository and rebuilds the control plane binary on save. The web dashboard will be available at `http://localhost:8110`.

---

## Code generation

### Database layer with sqlc

Whenever you modify or add queries in `internal/controlplane/store/queries/`:

```bash
make generate-sqlc
```

This runs `sqlc generate` and writes type-safe query methods directly to `internal/controlplane/store/`.

### Protobuf stubs with buf

When you make changes to the gRPC contract in `proto/`:

```bash
make generate-proto
```

This invokes `buf generate` and updates the Go stubs in `pkg/proto/agent/v1/`.

### REST OpenAPI stubs

If you update `api/openapi.yaml`:

```bash
make generate-openapi
```

To regenerate everything at once:

```bash
make generate
```

---

## Testing

Run unit tests across all packages:

```bash
make test
```

Run tests for a single package:

```bash
go test ./internal/controlplane/api/handler/... -v
```

Run with the race detector enabled:

```bash
go test -race ./...
```

Integration tests need a live PostgreSQL database:

```bash
VPNBUILDER_DATABASE_DSN=postgres://vpnbuilder:devpassword@127.0.0.1:5432/vpnbuilder_test go test ./tests/...
```

---

## Code linting

I use `golangci-lint` with the project configuration in `.golangci.yml`:

```bash
make lint
```

To automatically fix formatting or trivial lint warnings:

```bash
golangci-lint run --fix
```

---

## Pre-PR checks

Before opening a pull request, run these four commands to make sure CI will pass:

```bash
gofmt -w .
make lint
make test
make build
```

---

## Useful debugging commands

**Check control plane logs:**
```bash
# If running with air, logs stream in your active terminal
# If running under systemd on a server:
journalctl -u vpnbuilder-cp -f
```

**Inspect WireGuard peers on a node (run on the node host):**
```bash
wg show
```
