# API Reference

## Authentication

Every API request must include one of these credentials:

| Method | Header | Notes |
|---|---|---|
| JWT token | `Authorization: Bearer <token>` | Obtain via `POST /api/v1/auth/login` |
| API key | `X-API-Key: <key>` | Create in admin panel under Settings > API Keys |

### Roles

| Role | Access |
|---|---|
| `owner` | Full access including AI Copilot and destructive operations |
| `admin` | Full access except owner-only endpoints |
| `support` | Read-only access to users and peers |

---

## Auth endpoints

### POST /api/v1/auth/login

Authenticate with username and password. Returns a JWT token.

**Request:**

```json
{
  "username": "admin",
  "password": "your-password"
}
```

**Response:**

```json
{
  "token": "eyJ...",
  "expires_at": "2024-01-01T12:00:00Z"
}
```

If TOTP is enabled, also include `"totp_code": "123456"`.

### POST /api/v1/auth/refresh

Refresh a JWT token before it expires.

**Headers:** `Authorization: Bearer <token>`

**Response:** same as login response.

### POST /api/v1/auth/logout

Invalidate the current session.

---

## Nodes

### GET /api/v1/nodes

List all registered node servers.

**Response:**

```json
[
  {
    "id": "node-uuid",
    "name": "Frankfurt-01",
    "address": "1.2.3.4",
    "status": "online",
    "protocols": ["wireguard", "amneziawg", "vless"],
    "peer_count": 42,
    "created_at": "2024-01-01T00:00:00Z"
  }
]
```

### POST /api/v1/nodes

Register a new node server.

**Request:**

```json
{
  "name": "Frankfurt-01",
  "address": "1.2.3.4",
  "grpc_port": 9090
}
```

### GET /api/v1/nodes/{id}

Get details for a specific node.

### DELETE /api/v1/nodes/{id}

Remove a node. All associated peers are deleted.

---

## Peers

### GET /api/v1/peers

List WireGuard peers. Query params: `node_id`, `user_id`, `status`, `limit`, `offset`.

**Response:**

```json
[
  {
    "id": "peer-uuid",
    "node_id": "node-uuid",
    "user_id": "user-uuid",
    "public_key": "base64...",
    "allowed_ips": "10.0.0.2/32",
    "protocol": "wireguard",
    "expires_at": "2024-12-31T00:00:00Z",
    "status": "active"
  }
]
```

### POST /api/v1/peers

Create a new peer. The control plane generates the key pair and pushes the peer config to the target node agent.

**Request:**

```json
{
  "node_id": "node-uuid",
  "user_id": "user-uuid",
  "protocol": "wireguard",
  "expires_at": "2024-12-31T00:00:00Z"
}
```

**Response:** created peer object with `subscription_url`.

### GET /api/v1/peers/{id}

Get peer details including the subscription URL.

### PATCH /api/v1/peers/{id}

Update peer status or expiry.

```json
{
  "status": "suspended",
  "expires_at": "2025-01-01T00:00:00Z"
}
```

### DELETE /api/v1/peers/{id}

Remove peer from the node and database.

---

## Users

### GET /api/v1/users

List all users. Query params: `status`, `limit`, `offset`.

### POST /api/v1/users

Create a user.

```json
{
  "telegram_id": 123456789,
  "email": "user@example.com",
  "plan": "monthly"
}
```

### GET /api/v1/users/{id}

Get user details including active peers and billing history.

### PATCH /api/v1/users/{id}

Update user details or plan.

### DELETE /api/v1/users/{id}

Delete user and all associated peers.

---

## Billing

### GET /api/v1/billing/transactions

List payment transactions. Query params: `user_id`, `status`, `limit`, `offset`.

### POST /api/v1/billing/invoices

Create a payment invoice. Returns a CryptoBot or Telegram Stars payment link.

```json
{
  "user_id": "user-uuid",
  "amount": 5.00,
  "currency": "USDT",
  "plan": "monthly"
}
```

### POST /api/v1/billing/webhook

Webhook receiver for CryptoBot payment confirmations. Configure the webhook URL in your CryptoBot settings.

---

## AI Copilot

> Owner role required for all AI endpoints.

### POST /api/v1/ai/query

Send a natural language query to the AI Copilot.

**Request:**

```json
{
  "message": "How many active peers does Frankfurt-01 have and when do most expire?"
}
```

**Response:**

```json
{
  "response": "Frankfurt-01 currently has 42 active peers. 31 of them expire in January 2025...",
  "requires_approval": false
}
```

For mutation requests:

```json
{
  "response": "I will suspend 12 expired peers on Frankfurt-01 and remove their nftables rules.",
  "requires_approval": true,
  "proposal_id": "proposal-uuid",
  "actions": [...]
}
```

### POST /api/v1/ai/approve/{proposal_id}

Approve a pending AI mutation proposal. The control plane executes the actions immediately.

### DELETE /api/v1/ai/approve/{proposal_id}

Reject and discard a proposal.

---

## Subscriptions

### GET /sub/{token}

Public endpoint. Returns the VPN config for the given subscription token.

The response format is determined by the `User-Agent` header:

| User-Agent | Response format | Content-Type |
|---|---|---|
| Contains `sing-box` | Sing-box JSON | `application/json` |
| Contains `clash` | Clash YAML | `text/yaml` |
| Contains `amneziavpn` | AmneziaVPN JSON | `application/json` |
| Contains `wireguard` or absent | WireGuard .conf | `text/plain` |
| Anything else | base64 encoded | `text/plain` |

---

## Error responses

All errors follow this format:

```json
{
  "error": "peer not found",
  "code": "NOT_FOUND",
  "request_id": "req-uuid"
}
```

| HTTP status | Meaning |
|---|---|
| 400 | Invalid request body or parameters |
| 401 | Missing or invalid credentials |
| 403 | Authenticated but insufficient role |
| 404 | Resource not found |
| 409 | Conflict (duplicate key, etc.) |
| 422 | Validation failed |
| 429 | Rate limit exceeded |
| 500 | Internal server error |
