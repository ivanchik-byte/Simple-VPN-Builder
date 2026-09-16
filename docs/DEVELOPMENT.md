# Development

## Tool requirements

Install all tools before running local services:

```bash
# Go tools
go install github.com/air-verse/air@latest
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# System tools (Debian/Ubuntu)
apt-get install -y protobuf-compiler golangci-lint

# golang-migrate CLI
curl -L https://github.com/golang-migrate/migrate/releases/download/v4.18.1/migrate.linux-amd64.tar.gz | tar xz
mv migrate /usr/local/bin/
```

---

## Local services

Start PostgreSQL and Redis with Docker (no full compose needed):

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

Set your `.env`:

```env
DATABASE_URL=postgres://vpnbuilder:devpassword@127.0.0.1:5432/vpnbuilder
REDIS_URL=redis://127.0.0.1:6379
JWT_SECRET=dev-secret-at-least-32-characters-long
ADMIN_PASSWORD=devpassword
```

---

## Apply migrations

```bash
make migrate
```

To roll back the last migration:

```bash
migrate -database "$DATABASE_URL" -path migrations down 1
```

---

## Start with live reload

```bash
make run
```

Air watches for `.go` file changes and rebuilds automatically. The admin panel is at `http://localhost:8110`.

---

## Code generation

### Regenerate the database layer

After adding or editing SQL queries in `internal/controlplane/db/queries/`:

```bash
make sqlc
```

This runs `sqlc generate` and outputs type-safe Go code to `internal/controlplane/db/`.

### Regenerate gRPC stubs

After editing `.proto` files in `proto/`:

```bash
make proto
```

This outputs Go stubs to `internal/grpc/`.

---

## Testing

Run all tests:

```bash
make test
```

Run a specific package:

```bash
go test ./internal/controlplane/api/handler/... -v
```

Run with race detection:

```bash
go test -race ./...
```

Integration tests require a live database. Set `DATABASE_URL` before running:

```bash
DATABASE_URL=postgres://vpnbuilder:devpassword@127.0.0.1:5432/vpnbuilder_test go test ./tests/...
```

---

## Linting

```bash
make lint
```

The project uses `golangci-lint` with the config in `.golangci.yml`. Fix auto-fixable issues:

```bash
golangci-lint run --fix
```

---

## Pre-commit checks

Before opening a PR, run the full suite:

```bash
gofmt -w .
make lint
make test
make build
```

All four must pass without errors.

---

## Diagnostics

**Check the control plane logs:**

```bash
# If running via make run (air)
# Logs appear in the terminal

# If running via systemd
journalctl -u vpn-builder -f
```

**Check database connectivity:**

```bash
go run ./cmd/controlplane migrate --dry-run
```

**Check gRPC connection to a node:**

```bash
grpcurl -cacert certs/ca.crt \
  -cert certs/client.crt \
  -key certs/client.key \
  your-node:9090 list
```

**Check WireGuard peers on a node (run on the node server):**

```bash
wg show
```
