# Contributing

## Prerequisites

Install these tools before starting:

```bash
go install github.com/air-verse/air@latest          # live reload
go install github.com/sqlc-dev/sqlc/cmd/sqlc@latest # db code generation
go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest
```

Also install: `protoc`, `golangci-lint`, `golang-migrate`.

## Local setup

```bash
git clone https://github.com/ivanchik-byte/Simple-VPN-Builder.git
cd Simple-VPN-Builder

cp .env.example .env
# Fill in DATABASE_URL, REDIS_URL, JWT_SECRET, ADMIN_PASSWORD

make deps
make migrate
make run
```

## Code standards

- Run `gofmt -w .` and `golangci-lint run` before committing
- No emojis in code, comments, templates, or commit messages
- Follow existing package structure; place new handlers in `internal/controlplane/api/handler/`
- Write tests for all new handlers and service functions

## Project layout

```
cmd/            # Binary entrypoints
internal/
  controlplane/ # REST API, Web UI, Bot, AI Copilot, DB layer
  agent/        # WireGuard, AmneziaWG, Xray node agent
proto/          # gRPC service definitions
migrations/     # Ordered SQL migration files
docs/           # Extended documentation
```

## Database changes

1. Add a new migration file to `migrations/` following the naming pattern `NNNN_description.up.sql` / `NNNN_description.down.sql`
2. Update the sqlc queries in `internal/controlplane/db/queries/`
3. Run `make sqlc` to regenerate the Go database layer
4. Run `make migrate` to apply

## gRPC changes

1. Edit `.proto` files in `proto/`
2. Run `make proto` to regenerate Go stubs
3. Implement new methods in the server and client

## Commit messages

Use [Conventional Commits](https://www.conventionalcommits.org/):

```
feat(bot): add Telegram Stars payment flow
fix(api): return 400 on missing peer public key
docs(deployment): update Docker Compose example
refactor(agent): extract wireguard key rotation to separate func
test(billing): add integration test for crypto payment webhook
```

Types: `feat`, `fix`, `docs`, `refactor`, `test`, `chore`, `ci`, `perf`

## Pull request process

1. Fork the repository and create a branch from `master`
2. Write tests for your changes
3. Run the full test suite: `make test && make lint`
4. Open a PR against `master` with a clear description of what changed and why
5. Reference any related issues

## Code of Conduct

By participating, you agree to follow the [Code of Conduct](CODE_OF_CONDUCT.md).
