# Configuration Reference

All settings in Simple VPN Builder are configured via environment variables (or `.env` file) and YAML configuration files. The control plane uses Viper with automatic environment variable binding using the `VPNBUILDER_` prefix.

---

## 1. Control Plane Configuration

### Server & Network

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_SERVER_HTTP_ADDR` | String | No | `:8110` | Listen address for Web Admin UI, REST API, and subscription endpoints |
| `VPNBUILDER_SERVER_GRPC_ADDR` | String | No | `:9090` | Listen address for mTLS gRPC hub (agent communication) |
| `VPNBUILDER_SERVER_TLS_CERT` | String | No | `""` | Path to TLS certificate for HTTPS (optional, if terminated by CP) |
| `VPNBUILDER_SERVER_TLS_KEY` | String | No | `""` | Path to TLS private key for HTTPS |
| `VPNBUILDER_SERVER_CORS_ALLOWED_ORIGINS` | String | No | `http://localhost:3000` | Comma-separated list of allowed CORS origins |

### Database (PostgreSQL 16+)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_DATABASE_DSN` | String | Yes | `postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable` | PostgreSQL connection DSN |
| `VPNBUILDER_DATABASE_MAX_OPEN_CONNS` | Integer | No | `25` | Maximum open connections in the pool |
| `VPNBUILDER_DATABASE_MAX_IDLE_CONNS` | Integer | No | `5` | Maximum idle connections in the pool |
| `VPNBUILDER_DATABASE_CONN_MAX_LIFETIME` | Duration | No | `5m` | Maximum connection reuse duration |
| `VPNBUILDER_DATABASE_CONN_MAX_IDLE_TIME` | Duration | No | `1m` | Maximum idle connection duration |

### In-Memory Cache (Redis 7+)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_REDIS_ADDR` | String | Yes | `localhost:6379` | Redis host:port address |
| `VPNBUILDER_REDIS_PASSWORD` | String | No | `""` | Redis authentication password |
| `VPNBUILDER_REDIS_DB` | Integer | No | `0` | Redis logical database index |

### Authentication & Security

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_AUTH_JWT_SECRET` | String | Yes | - | High-entropy random secret (64+ chars) for signing session JWT tokens |
| `VPNBUILDER_AUTH_JWT_ACCESS_TTL` | Duration | No | `15m` | Access token time-to-live |
| `VPNBUILDER_AUTH_JWT_REFRESH_TTL` | Duration | No | `168h` | Refresh token time-to-live (7 days) |
| `VPNBUILDER_AUTH_BCRYPT_COST` | Integer | No | `12` | Bcrypt hash computational cost factor |
| `CONTROL_PLANE_API_KEY` | String | No | `dev-api-key-change-in-production` | Machine API key for bot and automation integrations |

> **Administrator Security Recommendation**:  
> The initial administrator account is automatically seeded into PostgreSQL on first launch:
> - **Email**: `admin@vpnbuilder.local`
> - **Password**: `Admin1234!`
>
> **Recommended Production Procedure**: Log in to Web UI (`/admin/dashboard-v2`), go to **Settings → Administrators** (`/admin/settings-v2`), create your personal account with role **Owner**, log in with it, and **delete** the default `admin@vpnbuilder.local` account. The system prevents deleting the last remaining Owner, so the default account can only be removed once your replacement Owner account is active.  
> *`ADMIN_PASSWORD` is not read as an environment variable by the codebase.*

### Internal Certificate Authority (mTLS)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_CA_CERT_TTL` | Duration | No | `720h` | Lifetime of issued node client certificates (30 days) |
| `VPNBUILDER_CA_CERT_FILE` | String | No | `/etc/vpnbuilder/ca.pem` | Internal CA public certificate path |
| `VPNBUILDER_CA_KEY_FILE` | String | No | `/etc/vpnbuilder/ca-key.pem` | Internal CA private key path |

### WireGuard Adapter Settings

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_ADAPTER_WIREGUARD_INTERFACE_PREFIX` | String | No | `wg` | Prefix for WireGuard network interfaces |
| `VPNBUILDER_ADAPTER_WIREGUARD_SUBNET_V4` | String | No | `10.8.0.0/16` | IPv4 overlay subnet allocated for peers |
| `VPNBUILDER_ADAPTER_WIREGUARD_SUBNET_V6` | String | No | `fd00::/64` | IPv6 overlay subnet allocated for peers |
| `VPNBUILDER_ADAPTER_WIREGUARD_DNS` | String | No | `1.1.1.1, 1.0.0.1` | Default DNS resolvers pushed to clients |
| `VPNBUILDER_ADAPTER_WIREGUARD_MTU` | Integer | No | `1280` | MTU for WireGuard tunnel interfaces |
| `VPNBUILDER_ADAPTER_WIREGUARD_KEEPALIVE` | Integer | No | `25` | Persistent keepalive interval in seconds |

### Logging

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_LOG_LEVEL` | String | No | `info` | Logging verbosity: `debug`, `info`, `warn`, `error` |
| `VPNBUILDER_LOG_FORMAT` | String | No | `json` | Log format: `json` or `text` |

---

## 2. Telegram Sales Bot Configuration (`cmd/bot`)

The bot daemon communicates with the Control Plane over the REST API.

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `CONTROL_PLANE_URL` | String | No | `http://localhost:8110` | Base URL of the control plane instance |
| `CONTROL_PLANE_API_KEY` | String | No | `dev-key-change-in-production` | API key matching the control plane configuration |
| `TELEGRAM_BOT_TOKEN` | String | No | `""` | Telegram Bot API token from @BotFather (can also be managed via Web UI Settings) |
| `CRYPTOBOT_TOKEN` | String | No | `""` | CryptoPay API token from @CryptoBot for cryptocurrency payments |

---

## 3. Node Agent Configuration (`cmd/agent`)

The agent daemon runs on edge servers and connects back to the control plane.

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_AGENT_NODE_NAME` | String | No | Hostname | Identifier of the node in the control plane |
| `VPNBUILDER_AGENT_CONTROL_PLANE` | String | Yes | `""` | gRPC hub address (`panel-host:9090`) |
| `VPNBUILDER_AGENT_CA_CERT` | String | No | `""` | Path to control plane CA certificate for verification |
| `VPNBUILDER_AGENT_CERT_FILE` | String | No | `""` | Path to client mTLS certificate issued by CP |
| `VPNBUILDER_AGENT_KEY_FILE` | String | No | `""` | Path to client mTLS private key |
| `VPNBUILDER_AGENT_SYNC_INTERVAL` | Duration | No | `30s` | Configuration sync polling interval |
| `VPNBUILDER_AGENT_METRICS_INTERVAL`| Duration | No | `30s` | Traffic telemetry reporting interval |

---

## 4. Production Example (`.env`)

```env
# ==============================================================================
# Simple VPN Builder — Production Environment
# ==============================================================================

# Database & Cache
VPNBUILDER_DATABASE_DSN=postgres://vpnbuilder:VeryStrongDatabasePassword456@127.0.0.1:5432/vpnbuilder?sslmode=disable
VPNBUILDER_REDIS_ADDR=127.0.0.1:6379
VPNBUILDER_REDIS_PASSWORD=StrongRedisPasswordHere

# Network
VPNBUILDER_SERVER_HTTP_ADDR=:8110
VPNBUILDER_SERVER_GRPC_ADDR=:9090

# Authentication & Security
VPNBUILDER_AUTH_JWT_SECRET=4f9c8d2a6e1b7f0c5a3d8e2b6f1a9c4e7d0b3f5a8c1e4d7a9b2c5e8f1a3d6e0b
VPNBUILDER_AUTH_JWT_ACCESS_TTL=15m
VPNBUILDER_AUTH_JWT_REFRESH_TTL=168h
VPNBUILDER_AUTH_BCRYPT_COST=12
CONTROL_PLANE_API_KEY=prod-api-key-replace-with-secure-value

# Telegram Sales Bot
CONTROL_PLANE_URL=http://127.0.0.1:8110
TELEGRAM_BOT_TOKEN=123456789:ABCdefGHIjklMNOpqrSTUvwxYZ
CRYPTOBOT_TOKEN=12345:AAABBBCCCDDDEEEFFF

# Logging
VPNBUILDER_LOG_LEVEL=info
VPNBUILDER_LOG_FORMAT=json
```
