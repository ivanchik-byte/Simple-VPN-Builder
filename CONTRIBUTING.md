# Contributing to Simple-VPN-Builder

Thank you for your interest in contributing to Simple-VPN-Builder. This document establishes guidelines for development, code standards, testing, and pull requests.

---

## 1. Code of Conduct

All contributors are expected to adhere to our [Code of Conduct](CODE_OF_CONDUCT.md). Please treat all participants with respect and professionalism.

---

## 2. Development Prerequisites

To build and run tests locally, you need:
- Go 1.25 or higher
- Docker & Docker Compose
- Development tools installed via `make install-tools`:
  - `golangci-lint` (v1.60+)
  - `buf`
  - `sqlc`
  - `oapi-codegen`
  - `migrate`

---

## 3. Getting Started

1. Fork the repository on GitHub.
2. Clone your fork locally:
   ```bash
   git clone https://github.com/your-username/Simple-VPN-Builder.git
   cd Simple-VPN-Builder
   ```
3. Install required developer tools:
   ```bash
   make install-tools
   ```
4. Start local development dependencies (PostgreSQL 16 and Redis 7):
   ```bash
   make dev-up
   ```
5. Verify your setup:
   ```bash
   make build
   make test
   make lint
   ```

---

## 4. Coding Standards & Conventions

### 4.1 Strict Zero-Emoji Policy
To maintain high-agency, professional engineering standards, emojis are strictly forbidden anywhere in the codebase. This includes:
- Go source code, identifiers, and comments
- HTML, CSS, JavaScript, and UI templates
- Structured log messages and log keys
- Markdown documentation and issue discussions
- Commit messages and pull request descriptions

### 4.2 Formatting and Linting
- All Go source code must be formatted using standard `gofmt -s -w`.
- Code must pass `golangci-lint run ./...` with zero errors or warnings.
- Avoid unchecked errors, empty `catch`/`recover` blocks, or dead code.

### 4.3 Architecture & Modularity
- Follow clean separation of concerns:
  - `internal/controlplane/store/`: Database access (sqlc-generated).
  - `internal/controlplane/service/`: Pure business logic.
  - `internal/controlplane/api/`: REST handlers and HTTP routing.
  - `internal/controlplane/grpc/`: gRPC server and stream management.
  - `internal/agent/manager/`: Network interface and kernel management.
  - `internal/agent/syncer/`: Configuration synchronization engine.
  - `internal/agent/doctor/`: Operational self-checks.

---

## 5. Testing Requirements

- All new features and bug fixes must include automated tests.
- Run tests with the race detector enabled:
  ```bash
  go test -v -race -count=1 ./...
  ```
- Coverage reports can be generated with:
  ```bash
  make test-coverage
  ```
- Integration tests must be clean and tear down mock resources or temporary files gracefully.

---

## 6. Commit Message Guidelines

We follow Conventional Commits format:
```
<type>(<scope>): <subject>
```

Types:
- `feat`: A new feature
- `fix`: A bug fix
- `docs`: Documentation updates
- `refactor`: Code restructuring without behavioral changes
- `perf`: Performance improvements
- `test`: Test suite additions or fixes
- `chore`: Dependency updates, tooling, or build configuration

Example:
```
feat(agent): add doctor diagnostic self-check engine
```

---

## 7. Submitting Pull Requests

1. Create a descriptive branch from `master`:
   ```bash
   git checkout -b feat/my-improvement
   ```
2. Commit your changes following commit guidelines.
3. Push to your fork:
   ```bash
   git push origin feat/my-improvement
   ```
4. Open a Pull Request against `master`.
5. Fill out the Pull Request template completely. Ensure CI checks pass.
