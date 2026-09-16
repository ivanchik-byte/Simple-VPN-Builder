# Simple-VPN-Builder API Reference

Simple-VPN-Builder exposes two APIs:
1. **REST API v1** (`http://host:8110/api/v1`): resource management, authentication, billing, and AI endpoints.
2. **gRPC API** (`host:9090`): agent bidirectional stream synchronization over mutual TLS.

The OpenAPI 3.1 schema source lives in `api/openapi.yaml`.

---

## 1. REST API Authentication

Admin endpoints accept two schemes (resolved by the global `Authenticate` middleware):

### 1.1 Bearer Token (JWT)

```http
Authorization: Bearer <jwt-access-token>
```

The admin web console sends the same token via the `vpn_admin_token` HttpOnly cookie. Access tokens are short-lived; refresh them via `/auth/refresh`. Revoked tokens are tracked in Redis (`jwt:revoked:*`).

### 1.2 API Key

```http
X-API-Key: vpn_<48-hex-chars>
```

Keys are stored as SHA-256 hashes and carry scopes. Manage them under `/api/v1/api-keys` (owner and superadmin roles only).

### 1.3 Roles

`owner` > `superadmin` > `admin`. Owners bypass all role checks. Admin accounts additionally carry granular flags (`can_broadcast`, `can_manage_users`, `can_delete_users`, `can_manage_nodes`, `can_manage_plans`, `can_view_audit`, `can_access_ai_copilot`); the AI Copilot flag is owner-only by default and grantable per admin.

---

## 2. Endpoints

### 2.1 System and probes

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/healthz` | None | Liveness probe |
| `GET` | `/readyz` | None | Readiness probe (PostgreSQL and Redis status) |
| `GET` | `/metrics` | JWT / API key | Prometheus metrics |
| `GET` | `/api/v1/system/telemetry` | JWT / API key | Control-plane telemetry snapshot |

### 2.2 Authentication

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | None (5/min per IP) | Exchange credentials for access/refresh tokens |
| `POST` | `/api/v1/auth/refresh` | Refresh token | Rotate token pair |
| `POST` | `/api/v1/auth/logout` | JWT | Revoke refresh token and clear cookies |
| `POST` | `/api/v1/auth/totp/setup` | JWT | Start TOTP enrollment |
| `POST` | `/api/v1/auth/totp/verify` | JWT | Confirm TOTP enrollment |

### 2.3 Admins and API keys

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/admins` | superadmin | List administrator accounts |
| `POST` | `/api/v1/admins` | superadmin | Create administrator |
| `GET` | `/api/v1/api-keys` | superadmin, owner | List API keys |
| `POST` | `/api/v1/api-keys` | superadmin, owner | Issue scoped API key |
| `DELETE` | `/api/v1/api-keys/{id}` | superadmin, owner | Revoke API key |

### 2.4 Nodes

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/nodes` | JWT / API key | List exit nodes with status |
| `POST` | `/api/v1/nodes` | JWT / API key | Register exit node |
| `GET` | `/api/v1/nodes/{id}` | JWT / API key | Node details and interfaces |
| `PATCH` | `/api/v1/nodes/{id}` | JWT / API key | Update region, capacity, or status |
| `DELETE` | `/api/v1/nodes/{id}` | JWT / API key | Decommission node |
| `GET` | `/api/v1/nodes/{id}/stats` | JWT / API key | Traffic statistics |

### 2.5 Users

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/users` | JWT / API key | List users with traffic usage |
| `POST` | `/api/v1/users` | JWT / API key | Create user and assign plan |
| `POST` | `/api/v1/users/trial` | JWT / API key | Issue trial account |
| `GET` | `/api/v1/users/{id}` | JWT / API key | User details and credentials |
| `PATCH` | `/api/v1/users/{id}` | JWT / API key | Update status, email, or quotas |
| `DELETE` | `/api/v1/users/{id}` | JWT / API key | Delete user and revoke credentials |
| `POST` | `/api/v1/users/{id}/reset-traffic` | JWT / API key | Reset traffic counters |
| `GET` | `/api/v1/users/{id}/subscription` | JWT / API key | Subscription payload |
| `POST` | `/api/v1/users/{id}/subscription/rotate` | JWT / API key | Rotate subscription token |
| `POST` | `/api/v1/users/{id}/rotate-keys` | JWT / API key | Rotate protocol keys |
| `POST` | `/api/v1/users/upsert-lead` | JWT / API key | Create or update Telegram lead |
| `POST` | `/api/v1/users/link-email` | JWT / API key | Link email to Telegram account |
| `POST` | `/api/v1/users/restore-account` | JWT / API key | Restore Telegram account |
| `POST` | `/api/v1/users/request-email-otp` | JWT / API key | Send email OTP (10 min TTL, 3 attempts) |
| `POST` | `/api/v1/users/verify-email-otp` | JWT / API key | Verify email OTP |
| `GET` | `/api/v1/users/by-telegram/{tg_id}` | JWT / API key | Lookup by Telegram ID |
| `GET` | `/api/v1/users/by-telegram/{tg_id}/referrals` | JWT / API key | Referral list |

### 2.6 Plans

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/plans` | JWT / API key | List tariff plans |
| `POST` | `/api/v1/plans` | JWT / API key | Create plan |
| `GET` | `/api/v1/plans/trial` | JWT / API key | Trial plan template |
| `GET` | `/api/v1/plans/{id}` | JWT / API key | Plan details |
| `PATCH` | `/api/v1/plans/{id}` | JWT / API key | Update plan |
| `DELETE` | `/api/v1/plans/{id}` | JWT / API key | Delete plan |

### 2.7 Billing (CryptoBot, Telegram Stars, manual)

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/billing/webhooks/{gateway}` | HMAC signature | Payment webhook; rejected when the gateway has no secret configured |
| `POST` | `/api/v1/billing/invoices` | JWT / API key | Create invoice |
| `POST` | `/api/v1/billing/promos/validate` | JWT / API key | Validate promo code |
| `GET` | `/api/v1/billing/gateways` | JWT / API key | List payment gateways |
| `PUT` | `/api/v1/billing/gateways` | JWT / API key | Create or update gateway |
| `GET` | `/api/v1/billing/settings` | JWT / API key | Billing settings |
| `PUT` | `/api/v1/billing/settings` | JWT / API key | Update billing settings |
| `GET` | `/api/v1/billing/bot-replies` | JWT / API key | Bot reply templates |
| `GET` | `/api/v1/billing/broadcasts` | JWT / API key | List broadcasts |
| `POST` | `/api/v1/billing/broadcasts` | JWT / API key | Create broadcast |
| `GET` | `/api/v1/billing/broadcasts/{id}` | JWT / API key | Broadcast details |

### 2.8 Credentials

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/credentials` | JWT / API key | List protocol credentials |
| `POST` | `/api/v1/credentials` | JWT / API key | Provision WireGuard, AmneziaWG, or VLESS credential |
| `GET` | `/api/v1/credentials/{id}` | JWT / API key | Credential details |
| `DELETE` | `/api/v1/credentials/{id}` | JWT / API key | Revoke credential |
| `POST` | `/api/v1/credentials/{id}/rotate` | JWT / API key | Rotate keys |

### 2.9 Analytics

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/analytics/overview` | JWT / API key | Totals across nodes and users |
| `GET` | `/api/v1/analytics/nodes` | JWT / API key | Per-node breakdown |
| `GET` | `/api/v1/analytics/users/{id}` | JWT / API key | Per-user breakdown |

### 2.10 AI Infra Copilot

Restricted to owners by default; standard admins need the `can_access_ai_copilot` grant. The Copilot talks to an OpenAI-compatible endpoint (OpenAI API or self-hosted, e.g. Ollama or NVIDIA NIM) and proposes infrastructure actions that a human confirms before execution (two-phase safety).

| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/ai/chat` | JWT + Copilot grant | Streaming chat (SSE) over nodes, users, telemetry |
| `POST` | `/api/v1/ai/actions/{token}/execute` | JWT + Copilot grant | Execute a confirmed proposal |
| `GET` | `/api/v1/ai/settings` | JWT + Copilot grant | Current LLM endpoint config |
| `POST` | `/api/v1/ai/settings` | JWT + Copilot grant | Update LLM endpoint credentials |

### 2.11 Subscription delivery (public token URLs)

| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/sub/{token}` | Token | Raw protocol config or subscription feed |

User-Agent autodetection selects the format:

- Official WireGuard: standard `.conf`
- AmneziaVPN: AmneziaWG `.conf` with obfuscation parameters
- Sing-box: JSON configuration
- Clash Meta (Mihomo): YAML configuration
- Others: Base64 subscription bundle

Response headers:

```http
Subscription-Userinfo: upload=1073741824; download=5368709120; total=107374182400; expire=1788551400
Profile-Update-Interval: 24
```

---

## 3. gRPC Agent API

Defined in `proto/agent/v1/agent.proto`.

```protobuf
service AgentService {
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}
```

- `AgentMessage`: `Register` (identity, IP, region, capabilities), `Heartbeat` (status, tunnels, CPU/memory), `MetricsReport` (per-peer byte deltas), `ConfigAck` (applied version confirmation).
- `ServerMessage`: `ConfigUpdate` (desired WireGuard/AWG/Xray state), `Command` (reload, restart, clear rules).
