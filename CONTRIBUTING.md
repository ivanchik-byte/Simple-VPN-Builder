# Contributing

Thanks for considering contributing to Simple VPN Builder! I maintain this project solo, so bug reports, fixes, and improvements are always appreciated.

---

## Tools you will need

Before contributing code, install these tools:

```bash
go install github.com/air-verse/air@latest          # live reload for testing
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest # database queries
go install github.com/bufbuild/buf/cmd/buf@latest   # protobuf generation
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

You also need `protobuf-compiler` and `golangci-lint` installed on your system.

---

## Local setup

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder

cp .env.example .env
# Set your test VPNBUILDER_DATABASE_DSN and VPNBUILDER_REDIS_ADDR

make migrate-up
make test
```

---

## Code standards

- Format your code with `gofmt -w .` before committing.
- Keep commits clean: use conventional prefixes like `fix:`, `feat:`, `refactor:`, `docs:`, `test:`.
- No emojis in code, comments, commit messages, or templates.
- If you write a new handler or service method, please include unit tests.

---

## Project structure

```
cmd/            # Main entrypoints: control-plane, agent, bot
internal/
  controlplane/ # REST API, React SPA assets, Bot, AI Copilot, sqlc store
  agent/        # WireGuard, AmneziaWG, Xray process manager
  shared/       # Config structs, logger, metrics
proto/          # gRPC contract (.proto)
migrations/     # Ordered SQL migration files (golang-migrate)
docs/           # In-depth technical guides
```

---

## Modifying the database

1. Add your migration files to `migrations/` following the pattern `00X_name.up.sql` and `00X_name.down.sql`.
2. Add or update queries in `internal/controlplane/store/queries/`.
3. Run `make generate-sqlc` to regenerate Go models.
4. Run `make migrate-up` to test applying them.

---

## Modifying gRPC schemas

1. Edit the relevant `.proto` files in `proto/agent/v1/`.
2. Run `make generate-proto` to update the generated code in `pkg/proto/agent/v1/`.
3. Update the server implementation in `internal/controlplane/grpc/` and the agent client in `internal/agent/grpc/`.
