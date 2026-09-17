# Configuration Reference

Here is the complete configuration reference for Simple VPN Builder.

I use Viper to manage configuration across both the control plane and node agents. Viper binds environment variables automatically with the `VPNBUILDER_` prefix. You can set them in a local `.env` file, pass them as system environment variables, or load them from a YAML file.

---

## 1. Control Plane Configuration

### Server and network

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_SERVER_HTTP_ADDR` | String | No | `:8110` | Listen address for the Web UI, REST API, and subscription links |
| `VPNBUILDER_SERVER_GRPC_ADDR` | String | No | `:9090` | Listen address for the mTLS gRPC hub (agent connections) |
| `VPNBUILDER_SERVER_TLS_CERT` | String | No | `""` | Path to TLS certificate for HTTPS (optional if using Caddy or Nginx) |
| `VPNBUILDER_SERVER_TLS_KEY` | String | No | `""` | Path to TLS private key for HTTPS |
| `VPNBUILDER_SERVER_CORS_ALLOWED_ORIGINS` | String | No | `http://localhost:3000` | Comma-separated list of allowed CORS origins for development |

### Database (PostgreSQL 16+)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_DATABASE_DSN` | String | Yes | `postgres://vpnbuilder:vpnbuilder@localhost:5432/vpnbuilder?sslmode=disable` | PostgreSQL connection DSN |
| `VPNBUILDER_DATABASE_MAX_OPEN_CONNS` | Integer | No | `25` | Maximum open connections in the pgx pool |
| `VPNBUILDER_DATABASE_MAX_IDLE_CONNS` | Integer | No | `5` | Minimum idle connections kept warm in the pool |
| `VPNBUILDER_DATABASE_CONN_MAX_LIFETIME` | Duration | No | `5m` | Maximum connection reuse duration |
| `VPNBUILDER_DATABASE_CONN_MAX_IDLE_TIME` | Duration | No | `1m` | Maximum idle connection duration |

### Cache (Redis 7+)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_REDIS_ADDR` | String | Yes | `localhost:6379` | Redis host and port |
| `VPNBUILDER_REDIS_PASSWORD` | String | No | `""` | Redis auth password |
| `VPNBUILDER_REDIS_DB` | Integer | No | `0` | Redis logical database index |

### Authentication and security

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_AUTH_JWT_SECRET` | String | Yes | - | Random secret (at least 32 characters, 64 recommended) for signing session tokens |
| `VPNBUILDER_AUTH_JWT_ACCESS_TTL` | Duration | No | `15m` | Lifetime of an access token |
| `VPNBUILDER_AUTH_JWT_REFRESH_TTL` | Duration | No | `168h` | Lifetime of a refresh token (7 days) |
| `VPNBUILDER_AUTH_BCRYPT_COST` | Integer | No | `12` | Bcrypt cost factor for hashing passwords |
| `CONTROL_PLANE_API_KEY` | String | No | `dev-api-key-change-in-production` | Machine API key for bot and automation integrations |

> **Important note on initial administrator account**:  
> On first start, the database seeds a default owner account:
> - **Email**: `admin@vpnbuilder.local`
> - **Password**: `Admin1234!`
>
> In production, log in to the web panel (`/admin/dashboard-v2`), go to Settings -> Administrators (`/admin/settings-v2`), create your own personal account with the **Owner** role, log in with it, and delete `admin@vpnbuilder.local`.
> I added code that prevents deleting the last remaining Owner, so you can safely create your account first and then delete the default.
> *Note: `ADMIN_PASSWORD` is not read as an environment variable by the codebase.*

### Internal Certificate Authority (mTLS)

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_CA_CERT_TTL` | Duration | No | `720h` | Lifetime of issued node client certificates (30 days) |
| `VPNBUILDER_CA_CERT_FILE` | String | No | `/etc/vpnbuilder/ca.pem` | Internal CA public certificate path |
| `VPNBUILDER_CA_KEY_FILE` | String | No | `/etc/vpnbuilder/ca-key.pem` | Internal CA private key path |

### WireGuard defaults

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

The bot daemon communicates with the control plane over the REST API using an API key.

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `CONTROL_PLANE_URL` | String | No | `http://localhost:8110` | Base URL of the control plane instance |
| `CONTROL_PLANE_API_KEY` | String | No | `dev-key-change-in-production` | API key matching the control plane configuration |
| `TELEGRAM_BOT_TOKEN` | String | No | `""` | Telegram Bot API token from @BotFather |
| `CRYPTOBOT_TOKEN` | String | No | `""` | CryptoPay API token from @CryptoBot for cryptocurrency payments |

---

## 3. Node Agent Configuration (`cmd/agent`)

The agent daemon runs on remote VPN servers and connects back to the control plane gRPC hub.

| Variable | Type | Required | Default | Description |
|---|---|---|---|---|
| `VPNBUILDER_AGENT_NODE_NAME` | String | No | Hostname | Identifier of the node in the dashboard |
| `VPNBUILDER_AGENT_CONTROL_PLANE` | String | Yes | `""` | gRPC hub address (`panel-host:9090`) |
| `VPNBUILDER_AGENT_CA_CERT` | String | No | `""` | Path to control plane CA certificate for verification |
| `VPNBUILDER_AGENT_CERT_FILE` | String | No | `""` | Path to client mTLS certificate issued by the control plane |
| `VPNBUILDER_AGENT_KEY_FILE` | String | No | `""` | Path to client mTLS private key |
| `VPNBUILDER_AGENT_SYNC_INTERVAL` | Duration | No | `30s` | Configuration sync polling interval |
| `VPNBUILDER_AGENT_METRICS_INTERVAL`| Duration | No | `30s` | Traffic telemetry reporting interval |

---

## 4. Production Example (`.env`)

```env
# Simple VPN Builder: Production Environment Sample

# Database & Cache
VPNBUILDER_DATABASE_DSN=postgres://vpnbuilder:YourStrongDatabasePassword456@127.0.0.1:5432/vpnbuilder?sslmode=disable
VPNBUILDER_REDIS_ADDR=127.0.0.1:6379
VPNBUILDER_REDIS_PASSWORD=YourStrongRedisPasswordHere

# Network
VPNBUILDER_SERVER_HTTP_ADDR=:8110
VPNBUILDER_SERVER_GRPC_ADDR=:9090

# Authentication & Security
# Generate: openssl rand -hex 64
VPNBUILDER_AUTH_JWT_SECRET=replace-with-openssl-rand-hex-64-output
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
