# Simple-VPN-Builder API Reference

Simple-VPN-Builder provides two primary APIs:
1. **REST API v1** (`http://host:8110/api/v1`): Resource management, authentication, analytics, and subscription endpoints.
2. **gRPC API** (`host:9090`): Agent bidirectional stream synchronization over mutual TLS.

---

## 1. REST API Authentication

All administrative endpoints require authentication using one of the following methods:

### 1.1 Bearer Token (JWT)
Send access token via `Authorization` header:
```http
Authorization: Bearer <jwt-token>
```
Or via HTTP-only cookie named `vpnbuilder_token` (used automatically by the Admin Web UI).

### 1.2 API Key
Send scoped API key via `X-API-Key` header:
```http
X-API-Key: vpn_live_1234567890abcdef...
```

---

## 2. Core Endpoints Summary

Full OpenAPI 3.1 schema is available at `GET /openapi.yaml` or `api/openapi.yaml`.

### 2.1 System & Probes
| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/healthz` | None | Liveness probe (HTTP 200 OK) |
| `GET` | `/readyz` | None | Readiness probe checking PostgreSQL and Redis pools |
| `GET` | `/metrics` | None | Prometheus metrics scraper endpoint |

### 2.2 Authentication & Admin
| Method | Path | Auth | Description |
|---|---|---|---|
| `POST` | `/api/v1/auth/login` | None | Exchange credentials for JWT access/refresh tokens |
| `POST` | `/api/v1/auth/refresh` | None | Refresh expired access token |
| `POST` | `/api/v1/auth/logout` | JWT | Invalidate refresh token and session cookies |
| `GET` | `/api/v1/admins` | Admin | List administrative accounts |
| `POST` | `/api/v1/admins` | Admin | Create administrator |
| `GET` | `/api/v1/api-keys` | Admin | List issued API keys |
| `POST` | `/api/v1/api-keys` | Admin | Issue new API key with specific scopes |
| `DELETE` | `/api/v1/api-keys/{id}` | Admin | Revoke API key |

### 2.3 Nodes
| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/nodes` | Read | List registered exit nodes with status and latency |
| `POST` | `/api/v1/nodes` | Write | Register a new exit node |
| `GET` | `/api/v1/nodes/{id}` | Read | Get detailed node record and interfaces |
| `PATCH` | `/api/v1/nodes/{id}` | Write | Update node capacity, region, or status |
| `DELETE` | `/api/v1/nodes/{id}` | Write | Decommission and delete node |

### 2.4 Users & Plans
| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/plans` | Read | List subscription bandwidth/quota plans |
| `POST` | `/api/v1/plans` | Write | Create subscription plan |
| `GET` | `/api/v1/users` | Read | List users with active traffic usage |
| `POST` | `/api/v1/users` | Write | Create user and assign subscription plan |
| `GET` | `/api/v1/users/{id}` | Read | Get user details and credentials |
| `PATCH` | `/api/v1/users/{id}` | Write | Update status, email, or bandwidth quotas |
| `DELETE` | `/api/v1/users/{id}` | Write | Delete user and revoke all protocol credentials |
| `POST` | `/api/v1/users/{id}/reset-traffic` | Write | Reset accumulated traffic counters |

### 2.5 Credentials
| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/api/v1/credentials` | Read | List provisioned credentials |
| `POST` | `/api/v1/credentials` | Write | Provision WireGuard, AmneziaWG, or VLESS credential |
| `GET` | `/api/v1/credentials/{id}` | Read | Get credential details |
| `DELETE` | `/api/v1/credentials/{id}` | Write | Revoke credential |
| `POST` | `/api/v1/credentials/{id}/rotate` | Write | Rotate cryptographic keys for credential |

### 2.6 Universal Client Subscriptions (Public)
| Method | Path | Auth | Description |
|---|---|---|---|
| `GET` | `/client/{token}` | Token | User-facing Obsidian dark subscription web portal |
| `GET` | `/sub/{token}` | Token | Raw protocol configuration or subscription feed |

User-Agent autodetection routes to:
- `application/x-wireguard`: WireGuard `.conf`
- `application/json`: Sing-box configuration
- `application/x-yaml`: Clash Meta configuration
- `text/plain`: Base64 subscription bundle

Headers returned:
```http
Subscription-Userinfo: upload=1073741824; download=5368709120; total=107374182400; expire=1788551400
Profile-Update-Interval: 24
```

---

## 3. gRPC Agent API

The gRPC Agent Service is defined in `proto/agent/v1/agent.proto`.

### Service: `AgentService`
```protobuf
service AgentService {
  rpc Connect(stream AgentMessage) returns (stream ServerMessage);
}
```

### Stream Message Types
- `AgentMessage`:
  - `Register`: Node identity, public IP, region, hardware capabilities.
  - `Heartbeat`: Current status, active tunnel counts, CPU/memory telemetry.
  - `MetricsReport`: Uplink/downlink byte deltas per peer.
  - `ConfigAck`: Confirmation of applied configuration version.
- `ServerMessage`:
  - `ConfigUpdate`: Desired state of WireGuard interfaces, Amnezia parameters, and Xray inbounds.
  - `Command`: Administrative action triggers (reload, restart, clear rules).
