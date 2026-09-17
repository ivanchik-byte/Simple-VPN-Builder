# Simple-VPN-Builder: Technical Specification

> **Developer-First Multi-Node VPN Control Plane & Framework**  
> Version: 0.1.0-draft | Status: Active Development

---

## 1. Vision & Scope

### 1.1 Mission Statement
Build a **clean Go control plane + lightweight node agents** that enables developers to launch a **commercial VPN service** or **private mesh network** in hours, not weeks, with first-class API, Terraform provider, and protocol adapters (WireGuard, Xray, sing-box).

### 1.2 Core Scope
| In Scope | Out of Scope / Planned |
|---|---|
| Control Plane (REST/gRPC API, PostgreSQL, Redis) | Client apps (iOS/Android/macOS/Win): uses standard clients |
| Node Agent (registration, config sync, metrics) | Mesh ACL / device-to-device routing |
| WireGuard & AmneziaWG protocol adapter (native) | Terraform provider (Planned) |
| Xray protocol adapter (VMess/VLESS/Trojan) | Prometheus/Grafana external dashboards (Planned) |
| User management, traffic limits, expiry | High-availability CP clustering (Planned) |
| Subscription link generation (clash/v2ray/sing-box) | |
| Commercial billing (Telegram Stars, CryptoBot, orders, promo codes) | |
| Admin Web UI (React 19 SPA + classic HTMX fallback) | |

### 1.3 Target Audience (MVP)
- **VPN service operators** running 3 to 50+ exit nodes
- **DevOps/Platform engineers** building internal zero-trust networks
- **SaaS builders** embedding VPN into their product (white-label support)

---

## 2. High-Level Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                        CONTROL PLANE (CP)                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐              │
│  │  REST API    │  │  gRPC API    │  │  Admin UI    │              │
│  │  (OpenAPI)   │  │  (Agent sync)│  │(React/HTMX)  │              │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘              │
│         │                 │                 │                       │
│         └─────────────────┼─────────────────┘                       │
│                           ▼                                         │
│  ┌──────────────────────────────────────────────┐                 │
│  │              CORE SERVICES                    │                 │
│  │  • Auth (JWT + API Keys)                     │                 │
│  │  • Node Registry & Health                    │                 │
│  │  • User / Subscription Manager               │                 │
│  │  • Config Generator (Protocol Adapters)      │                 │
│  │  • Traffic Accounting & Quotas               │                 │
│  └──────────────────────────┬───────────────────┘                 │
│                             │                                     │
│                    ┌────────┴────────┐                            │
│                    ▼                 ▼                            │
│           ┌───────────────┐  ┌───────────────┐                    │
│           │  PostgreSQL   │  │   Redis       │                    │
│           │  (Primary)    │  │   (Cache/     │                    │
│           │               │  │    PubSub)    │                    │
│           └───────────────┘  └───────────────┘                    │
└─────────────────────────────────────────────────────────────────────┘
                              │ gRPC (mTLS)
                              ▼
┌─────────────────────────────────────────────────────────────────────┐
│                      NODE AGENT (per server)                        │
│  ┌─────────────────┐  ┌─────────────────┐  ┌─────────────────┐    │
│  │  gRPC Client    │  │  Protocol       │  │  System         │    │
│  │  (CP sync)      │  │  Adapters       │  │  Manager        │    │
│  └────────┬────────┘  └────────┬────────┘  └────────┬────────┘    │
│           │                    │                    │              │
│           ▼                    ▼                    ▼              │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  WireGuard      │  Xray Core      │  sing-box (future)       │  │
│  │  (kernel/go)    │  (embedded)     │  (embedded)              │  │
│  └─────────────────────────────────────────────────────────────┘  │
│                              │                                     │
│                              ▼                                     │
│  ┌─────────────────────────────────────────────────────────────┐  │
│  │  nftables/iptables  │  Traffic Accounting (per user/peer)   │  │
│  └─────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────┘
```

### 2.1 Communication Patterns
| Path | Protocol | Auth | Purpose |
|---|---|---|---|
| Admin → CP | HTTPS/REST | JWT (short-lived) + CSRF | Management |
| Integration → CP | HTTPS/REST | API Key (long-lived) | Automation |
| Agent → CP | gRPC (mTLS) | Client cert (per node) | Config sync, heartbeats, metrics |
| CP → Agent | gRPC (mTLS) | Server cert | Push updates (optional) |

---

## 3. Database Schema (PostgreSQL)

The database schema consists of 17 core tables managed through sequential migrations (`migrations/001_init.up.sql` through `migrations/008_whitelabel_tenants.up.sql`).

### 3.1 Core Tables (17 Tables)

```sql
-- 1. Nodes (VPN exit servers)
CREATE TABLE nodes (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                VARCHAR(128) NOT NULL UNIQUE,
    endpoint            VARCHAR(255) NOT NULL,           -- public IP:port
    grpc_endpoint       VARCHAR(255) NOT NULL,           -- agent gRPC address
    region              VARCHAR(64),
    capacity_gbps       INT DEFAULT 1,
    status              VARCHAR(32) DEFAULT 'pending',   -- pending, online, offline, draining
    tags                JSONB DEFAULT '{}',
    public_key          VARCHAR(88) NOT NULL,            -- WireGuard public key
    cert_fingerprint    VARCHAR(64) NOT NULL,           -- mTLS cert SHA256
    last_heartbeat      TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

-- 2. Users (VPN customers & Telegram subscribers)
CREATE TABLE users (
    id                     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email                  VARCHAR(255) UNIQUE,
    username               VARCHAR(128) UNIQUE NOT NULL,
    password_hash          VARCHAR(255),                    -- bcrypt, null for API-only
    status                 VARCHAR(32) DEFAULT 'active',    -- active, suspended, expired
    plan_id                UUID REFERENCES plans(id),
    traffic_limit          BIGINT DEFAULT 0,                -- bytes, 0 = unlimited
    traffic_used           BIGINT DEFAULT 0,
    expires_at             TIMESTAMPTZ,
    subscription_token     UUID NOT NULL DEFAULT gen_random_uuid() UNIQUE,
    telegram_id            BIGINT UNIQUE,
    telegram_username      VARCHAR(64),
    telegram_first_name    VARCHAR(128),
    telegram_last_name     VARCHAR(128),
    telegram_language_code VARCHAR(16) DEFAULT 'en',
    trial_used             BOOLEAN DEFAULT false,
    referrer_id            UUID REFERENCES users(id) ON DELETE SET NULL,
    referral_code          VARCHAR(32) UNIQUE,
    is_banned              BOOLEAN DEFAULT false,
    ban_reason             TEXT,
    last_seen_at           TIMESTAMPTZ DEFAULT now(),
    is_bot_blocked         BOOLEAN DEFAULT false,
    tenant_id              UUID REFERENCES tenants(id) ON DELETE SET NULL,
    note                   TEXT,
    created_at             TIMESTAMPTZ DEFAULT now(),
    updated_at             TIMESTAMPTZ DEFAULT now()
);

-- 3. Subscription Plans
CREATE TABLE plans (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name                 VARCHAR(64) NOT NULL UNIQUE,
    monthly_price        DECIMAL(10,2) DEFAULT 0,
    traffic_limit        BIGINT DEFAULT 0,                -- bytes/month
    device_limit         INT DEFAULT 3,
    protocols            TEXT[] DEFAULT ARRAY['wireguard','vless'],
    features             JSONB DEFAULT '{}',
    is_active            BOOLEAN DEFAULT true,
    is_trial             BOOLEAN DEFAULT false,
    trial_duration_hours INT DEFAULT 24,
    price_stars          INT DEFAULT 0,
    max_devices          INT DEFAULT 3,
    traffic_limit_gb     INT DEFAULT 0,
    price_1m             DECIMAL(10,2) DEFAULT 0,
    price_3m             DECIMAL(10,2) DEFAULT 0,
    price_6m             DECIMAL(10,2) DEFAULT 0,
    price_12m            DECIMAL(10,2) DEFAULT 0,
    created_at           TIMESTAMPTZ DEFAULT now(),
    updated_at           TIMESTAMPTZ DEFAULT now()
);

-- 4. Protocol Credentials & AmneziaWG Obfuscation (per user per node)
CREATE TABLE credentials (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id         UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    protocol        VARCHAR(32) NOT NULL,            -- wireguard, vless, vmess, trojan, shadowsocks
    private_key     VARCHAR(88),                     -- WireGuard private key
    public_key      VARCHAR(88),                     -- WireGuard public key
    preshared_key   VARCHAR(88),                     -- optional
    uuid            UUID,                            -- VLESS/VMess UUID
    password        VARCHAR(128),                    -- Shadowsocks/Trojan password
    email           VARCHAR(255),                    -- for VLESS flow control
    flow            VARCHAR(64),                     -- xtls-rprx-vision, etc.
    ipv4            INET,                            -- assigned tunnel IP
    ipv6            INET,
    dns             VARCHAR(255) DEFAULT '1.1.1.1',
    mtu             INT DEFAULT 1280,
    keepalive       INT DEFAULT 25,
    allowed_ips     CIDR[] DEFAULT ARRAY['0.0.0.0/0'::cidr,'::/0'::cidr],
    status          VARCHAR(32) DEFAULT 'active',
    expires_at      TIMESTAMPTZ,
    -- AmneziaWG Obfuscation Parameters
    awg_jc          INT DEFAULT 4,
    awg_jmin        INT DEFAULT 40,
    awg_jmax        INT DEFAULT 70,
    awg_s1          INT DEFAULT 64,
    awg_s2          INT DEFAULT 64,
    awg_h1          BIGINT DEFAULT 16843009,
    awg_h2          BIGINT DEFAULT 33686018,
    awg_h3          BIGINT DEFAULT 50529027,
    awg_h4          BIGINT DEFAULT 67372036,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE (user_id, node_id, protocol)
);

-- 5. Traffic Accounting (hourly rollups)
CREATE TABLE traffic_stats (
    id              BIGSERIAL PRIMARY KEY,
    user_id         UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    node_id         UUID NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    protocol        VARCHAR(32) NOT NULL,
    hour_bucket     TIMESTAMPTZ NOT NULL,            -- truncated to hour
    rx_bytes        BIGINT DEFAULT 0,
    tx_bytes        BIGINT DEFAULT 0,
    created_at      TIMESTAMPTZ DEFAULT now(),
    UNIQUE (user_id, node_id, protocol, hour_bucket)
);

-- 6. Administrators (RBAC: owner, superadmin, admin)
CREATE TABLE admins (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    email           VARCHAR(255) NOT NULL UNIQUE,
    password_hash   VARCHAR(255) NOT NULL,
    role            VARCHAR(32) DEFAULT 'admin',     -- owner, superadmin, admin
    totp_secret     VARCHAR(32),                     -- for 2FA
    permissions     JSONB NOT NULL DEFAULT '{
        "can_broadcast": false,
        "can_manage_users": true,
        "can_delete_users": false,
        "can_reset_traffic": true,
        "can_manage_nodes": false,
        "can_manage_plans": false,
        "can_view_audit": false
    }'::jsonb,
    last_login      TIMESTAMPTZ,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- 7. API Keys (for integrations/Terraform)
CREATE TABLE api_keys (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name            VARCHAR(128) NOT NULL,
    key_hash        VARCHAR(64) NOT NULL UNIQUE,     -- SHA256 of raw key
    prefix          VARCHAR(16) NOT NULL,            -- vpn_abc123...
    scopes          TEXT[] DEFAULT ARRAY['read'],    -- read, write, admin
    expires_at      TIMESTAMPTZ,
    last_used_at    TIMESTAMPTZ,
    created_by      UUID REFERENCES admins(id),
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- 8. Audit Logs (WORM: tamper-proof write-once log)
CREATE TABLE audit_logs (
    id              BIGSERIAL PRIMARY KEY,
    admin_id        UUID REFERENCES admins(id),
    api_key_id      UUID REFERENCES api_keys(id),
    action          VARCHAR(64) NOT NULL,
    resource_type   VARCHAR(64),
    resource_id     UUID,
    diff            JSONB,
    ip_address      INET,
    user_agent      TEXT,
    created_at      TIMESTAMPTZ DEFAULT now()
);

-- 9. Webhooks (event notifications)
CREATE TABLE webhooks (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    url             TEXT NOT NULL,
    secret          VARCHAR(64) NOT NULL,
    events          TEXT[] NOT NULL,
    is_active       BOOLEAN DEFAULT true,
    created_at      TIMESTAMPTZ DEFAULT now(),
    updated_at      TIMESTAMPTZ DEFAULT now()
);

-- 10. Payment Gateways
CREATE TABLE payment_gateways (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name             VARCHAR(32) NOT NULL UNIQUE,
    is_enabled       BOOLEAN DEFAULT false,
    config_encrypted TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ DEFAULT now(),
    updated_at       TIMESTAMPTZ DEFAULT now()
);

-- 11. Commercial Orders & Invoices
CREATE TABLE orders (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id             UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    plan_id             UUID NOT NULL REFERENCES plans(id),
    gateway             VARCHAR(32) NOT NULL,
    external_invoice_id VARCHAR(128),
    amount              DECIMAL(10,2) NOT NULL,
    currency            VARCHAR(8) NOT NULL,
    status              VARCHAR(32) DEFAULT 'pending',
    duration_months     INT DEFAULT 1,
    metadata            JSONB DEFAULT '{}',
    paid_at             TIMESTAMPTZ,
    created_at          TIMESTAMPTZ DEFAULT now(),
    updated_at          TIMESTAMPTZ DEFAULT now()
);

-- 12. Promotional Discount Codes
CREATE TABLE promo_codes (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code             VARCHAR(32) NOT NULL UNIQUE,
    discount_percent INT DEFAULT 0,
    discount_amount  DECIMAL(10,2) DEFAULT 0,
    bonus_days       INT DEFAULT 0,
    bonus_bytes      BIGINT DEFAULT 0,
    max_uses         INT DEFAULT 0,
    used_count       INT DEFAULT 0,
    is_active        BOOLEAN DEFAULT true,
    expires_at       TIMESTAMPTZ,
    created_at       TIMESTAMPTZ DEFAULT now()
);

-- 13. Broadcast Campaigns
CREATE TABLE broadcast_campaigns (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title            VARCHAR(128) NOT NULL,
    target_segment   VARCHAR(32) NOT NULL,
    message_text     TEXT NOT NULL,
    inline_buttons   JSONB DEFAULT '[]',
    total_recipients INT DEFAULT 0,
    sent_count       INT DEFAULT 0,
    failed_count     INT DEFAULT 0,
    status           VARCHAR(32) DEFAULT 'draft',
    created_at       TIMESTAMPTZ DEFAULT now(),
    completed_at     TIMESTAMPTZ
);

-- 14. Billing Settings (singleton)
CREATE TABLE billing_settings (
    id                     INT PRIMARY KEY DEFAULT 1,
    cryptobot_api_token    TEXT NOT NULL DEFAULT '',
    cryptobot_enabled      BOOLEAN NOT NULL DEFAULT false,
    telegram_stars_enabled BOOLEAN NOT NULL DEFAULT true,
    stars_price_per_month  INT NOT NULL DEFAULT 250,
    webhook_secret         TEXT NOT NULL DEFAULT '',
    updated_at             TIMESTAMPTZ DEFAULT now(),
    CONSTRAINT single_billing_settings CHECK (id = 1)
);

-- 15. Customizable Bot Replies
CREATE TABLE bot_replies (
    key_name    VARCHAR(64) PRIMARY KEY,
    reply_text  TEXT NOT NULL,
    updated_at  TIMESTAMPTZ DEFAULT now()
);

-- 16. User Email Verification & OTP Recovery
CREATE TABLE user_email_verifications (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    telegram_id        BIGINT NOT NULL,
    email              VARCHAR(255) NOT NULL,
    otp_hash           VARCHAR(64) NOT NULL,
    purpose            VARCHAR(32) NOT NULL DEFAULT 'link_email',
    attempts_remaining INT NOT NULL DEFAULT 3,
    expires_at         TIMESTAMPTZ NOT NULL,
    verified_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 17. Multi-Brand / White-Label Partner Tenants
CREATE TABLE tenants (
    id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    admin_id            UUID REFERENCES admins(id) ON DELETE SET NULL,
    name                VARCHAR(128) NOT NULL,
    slug                VARCHAR(64) NOT NULL UNIQUE,
    bot_token           VARCHAR(255) NOT NULL,
    bot_username        VARCHAR(128),
    bot_webhook_secret  VARCHAR(64) NOT NULL DEFAULT encode(gen_random_bytes(24), 'hex'),
    channel_link        VARCHAR(255),
    channel_id          BIGINT,
    require_channel_sub BOOLEAN NOT NULL DEFAULT false,
    support_link        VARCHAR(255),
    custom_domain       VARCHAR(255),
    miniapp_url         VARCHAR(255),
    banner_url          TEXT,
    welcome_text        TEXT,
    is_active           BOOLEAN NOT NULL DEFAULT true,
    created_at          TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

### 3.2 Indexes & Constraints
```sql
CREATE INDEX idx_users_status_expires ON users(status, expires_at);
CREATE INDEX idx_users_subscription_token ON users(subscription_token);
CREATE INDEX idx_users_telegram_id ON users(telegram_id);
CREATE INDEX idx_users_referral_code ON users(referral_code);
CREATE INDEX idx_users_last_seen_at ON users(last_seen_at);
CREATE INDEX idx_users_status_tg ON users(status, telegram_id) WHERE telegram_id IS NOT NULL;
CREATE INDEX idx_users_tenant_id ON users(tenant_id);

CREATE INDEX idx_credentials_user_node ON credentials(user_id, node_id);
CREATE INDEX idx_traffic_stats_user_hour ON traffic_stats(user_id, hour_bucket DESC);
CREATE INDEX idx_nodes_status_region ON nodes(status, region);

CREATE INDEX idx_audit_logs_created ON audit_logs(created_at DESC);
CREATE INDEX idx_audit_logs_admin ON audit_logs(admin_id, created_at DESC);

CREATE INDEX idx_orders_user_id ON orders(user_id);
CREATE INDEX idx_orders_status ON orders(status);
CREATE INDEX idx_orders_external_invoice_id ON orders(external_invoice_id);

CREATE INDEX idx_promo_codes_code ON promo_codes(code);
CREATE INDEX idx_broadcast_campaigns_status ON broadcast_campaigns(status);
CREATE INDEX idx_email_verifications_active ON user_email_verifications(email, telegram_id, expires_at) WHERE verified_at IS NULL;

CREATE INDEX idx_tenants_admin_id ON tenants(admin_id);
CREATE INDEX idx_tenants_slug ON tenants(slug);
CREATE INDEX idx_tenants_is_active ON tenants(is_active);
```

---

## 4. API Specification

### 4.1 REST API (OpenAPI 3.1): Core Endpoints

#### Authentication
```
POST   /api/v1/auth/login           # Admin login → JWT
POST   /api/v1/auth/refresh         # Refresh access token
POST   /api/v1/auth/logout          # Revoke refresh token
```

#### Nodes
```
GET    /api/v1/nodes                # List nodes (filter: status, region)
POST   /api/v1/nodes                # Register node (admin)
GET    /api/v1/nodes/{id}           # Node details + health
PATCH  /api/v1/nodes/{id}           # Update node (tags, capacity)
DELETE /api/v1/nodes/{id}           # Decommission node
GET    /api/v1/nodes/{id}/stats     # Real-time metrics
```

#### Users
```
GET    /api/v1/users                # List users (pagination, filters)
POST   /api/v1/users                # Create user
GET    /api/v1/users/{id}           # User details + credentials
PATCH  /api/v1/users/{id}           # Update user (plan, limits, status)
DELETE /api/v1/users/{id}           # Delete user
POST   /api/v1/users/{id}/reset-traffic  # Admin reset
GET    /api/v1/users/{id}/subscription   # Generate subscription link
```

#### Plans
```
GET    /api/v1/plans
POST   /api/v1/plans
PATCH  /api/v1/plans/{id}
DELETE /api/v1/plans/{id}
```

#### Credentials (Protocol configs)
```
GET    /api/v1/users/{id}/credentials
POST   /api/v1/users/{id}/credentials       # Generate new credential
PATCH  /api/v1/credentials/{id}
DELETE /api/v1/credentials/{id}
POST   /api/v1/credentials/{id}/rotate      # Rotate keys
```

#### Traffic & Analytics
```
GET    /api/v1/analytics/traffic            # Aggregate stats
GET    /api/v1/analytics/traffic/{user_id}  # Per-user breakdown
GET    /api/v1/analytics/nodes              # Node utilization
```

#### Admin & API Keys
```
GET    /api/v1/admins
POST   /api/v1/admins
GET    /api/v1/api-keys
POST   /api/v1/api-keys
DELETE /api/v1/api-keys/{id}
```

### 4.2 gRPC API (Control Plane ↔ Agent)

```protobuf
// proto/agent/v1/agent.proto
syntax = "proto3";
package vpnbuilder.agent.v1;

service AgentService {
  // Bidirectional streaming for config sync + heartbeats
  rpc Connect(stream AgentMessage) returns (stream ControlMessage);
}

message AgentMessage {
  oneof payload {
    RegisterRequest     register = 1;
    Heartbeat           heartbeat = 2;
    MetricsReport       metrics = 3;
    LogEntry            log = 4;
  }
}

message ControlMessage {
  oneof payload {
    ConfigUpdate        config = 1;
    Command             command = 2;
  }
}

message RegisterRequest {
  string node_name = 1;
  string wireguard_public_key = 2;
  string version = 3;
  map<string, string> labels = 4;
}

message Heartbeat {
  int64 timestamp = 1;
  NodeStatus status = 2;
  SystemInfo system = 3;
}

message MetricsReport {
  int64 timestamp = 1;
  repeated ProtocolMetrics protocols = 2;
}

message ProtocolMetrics {
  string protocol = 1;
  repeated PeerMetric peers = 2;
}

message PeerMetric {
  string peer_id = 1;           // user_id or credential_id
  int64 rx_bytes = 2;
  int64 tx_bytes = 3;
  int64 last_handshake = 4;
}

message ConfigUpdate {
  string config_version = 1;
  repeated NodeUserConfig users = 2;
}

message NodeUserConfig {
  string user_id = 1;
  repeated CredentialConfig credentials = 2;
}

message CredentialConfig {
  string credential_id = 1;
  string protocol = 2;
  bytes  config_bytes = 3;      // Serialized protocol-specific config
}

message Command {
  string command_id = 1;
  string type = 2;              // restart_protocol, reload_config, reboot
  map<string, string> params = 3;
}
```

---

## 5. Protocol Adapters Architecture

### 5.1 Adapter Interface (Go)

```go
// pkg/adapter/adapter.go
package adapter

import (
    "context"
    "vpnbuilder/pkg/config"
)

type Adapter interface {
    // Protocol identifier: "wireguard", "vless", "vmess", "trojan", "shadowsocks"
    Protocol() string
    
    // Generate server-side config for a node
    GenerateServerConfig(ctx context.Context, node *config.Node, creds []*config.Credential) ([]byte, error)
    
    // Generate client config for a user (subscription link / config file)
    GenerateClientConfig(ctx context.Context, cred *config.Credential, node *config.Node) (ClientConfig, error)
    
    // Validate credential before persisting
    ValidateCredential(cred *config.Credential) error
    
    // Parse traffic stats from protocol-specific output
    ParseMetrics(raw []byte) ([]PeerMetric, error)
    
    // Lifecycle hooks
    Start(ctx context.Context, node *config.Node) error
    Stop(ctx context.Context) error
    Reload(ctx context.Context, node *config.Node) error
}

type ClientConfig struct {
    Format     string            // "clash", "v2ray", "sing-box", "wireguard", "qr"
    Content    []byte
    Filename   string
    MIMEType   string
}

type PeerMetric struct {
    PeerID    string
    RXBytes   int64
    TXBytes   int64
    LastSeen  time.Time
}
```

### 5.2 WireGuard Adapter (Native Go)
- **Library**: `golang.zx2c4.com/wireguard` (userspace) + `wgctrl` for kernel
- **Config**: wg-quick compatible `.conf` + nftables rules for isolation
- **Metrics**: Parse `wg show <iface> transfer` + kernel counters

### 5.3 Xray Adapter (Embedded)
- **Library**: `github.com/xtls/xray-core` (embedded as library)
- **Protocols**: VLESS (XTLS-Reality), VMess, Trojan, Shadowsocks
- **Config**: JSON → Xray config format
- **Metrics**: Xray Stats API (gRPC) or log parsing

### 5.4 Registration Pattern
```go
// internal/adapter/registry.go
var adapters = map[string]func() adapter.Adapter{}

func Register(name string, factory func() adapter.Adapter) {
    adapters[name] = factory
}

func Get(name string) (adapter.Adapter, bool) {
    f, ok := adapters[name]
    if !ok { return nil, false }
    return f(), true
}
```

---

## 6. Node Agent Design

### 6.1 Responsibilities
1. **Register** with CP on startup (mTLS)
2. **Pull** full config on connect, then **incremental updates**
3. **Manage** WireGuard interfaces + Xray processes
4. **Collect** per-peer traffic stats every 30s
5. **Push** heartbeats + metrics to CP (streaming gRPC)
6. **Execute** commands (restart, reload, reboot)

### 6.2 Config Sync Strategy
```
CP                              Agent
  │                               │
  ├── ConfigUpdate(v=1, full) ───►│  Apply all, ack
  │                               │
  ├── Heartbeat req ─────────────►│  Respond with status
  │◄─── Heartbeat resp ───────────│
  │                               │
  ├── ConfigUpdate(v=2, delta) ──►│  Apply delta, ack
  │                               │
  │◄─── MetricsReport ────────────│  Every 30s
  │                               │
```

### 6.3 Process Management
| Protocol | Process Model |
|---|---|
| WireGuard | Kernel interface (`wg` + `ip link`), managed via netlink |
| Xray | Single embedded process per node, multiple inbounds |
| sing-box | Single embedded process (future) |

---

## 7. Security Model

### 7.1 Threat Model
| Asset | Threat | Mitigation |
|---|---|---|
| CP Database | SQLi, leakage | Parameterized queries, encryption at rest, read-replica for analytics |
| CP ↔ Agent | MITM, replay | mTLS with cert rotation (30d), certificate pinning |
| User Credentials | Theft | Encrypt at rest (age/NaCl), never log secrets |
| Admin Access | Credential stuffing | Argon2id, TOTP 2FA, rate limiting, audit log |
| API Keys | Leakage | Prefix + hash storage, scopes, expiry, rotation |

### 7.2 Secrets Handling
- **WireGuard private keys**: Encrypted in DB (age), decrypted only in agent memory
- **Xray UUIDs/passwords**: Same
- **mTLS certs**: Generated by CP CA, distributed via secure channel
- **JWT signing key**: Rotated weekly, stored in HSM/KMS in prod

---

## 8. Repository Structure

```
Simple-VPN-Builder/
├── .github/
│   └── workflows/           # CI/CD
├── cmd/
│   ├── control-plane/       # CP entrypoint
│   │   ├── main.go
│   │   └── wire.go          # Dependency injection
│   └── agent/               # Node agent entrypoint
│       ├── main.go
│       └── wire.go
├── internal/
│   ├── controlplane/        # CP private code
│   │   ├── api/             # REST handlers (OpenAPI generated)
│   │   ├── grpc/            # gRPC server (agent sync)
│   │   ├── service/         # Business logic
│   │   ├── store/           # Database access (sqlc)
│   │   ├── auth/            # JWT, API keys, sessions
│   │   ├── config/          # CP config
│   │   └── web/             # Embedded Admin UI (dist/ & templates/)
│   ├── agent/               # Agent private code
│   │   ├── grpc/            # gRPC client (CP connection)
│   │   ├── manager/         # Process/interface manager
│   │   ├── syncer/          # Config sync logic
│   │   └── metrics/         # Collection & reporting
│   └── shared/              # Shared internal packages
│       ├── config/          # Config structs
│       └── middleware/      # HTTP/gRPC middleware
├── web-ui/                  # Admin Dashboard v2 (React 19, Vite, Tailwind v4)
├── pkg/
│   ├── adapter/             # Protocol adapter interfaces + registry
│   │   ├── wireguard/
│   │   ├── xray/
│   │   └── registry.go
│   ├── models/              # Domain models (shared)
│   ├── proto/               # Generated protobuf (go)
│   └── openapi/             # Generated OpenAPI types
├── proto/
│   └── agent/v1/            # .proto definitions
├── migrations/              # SQL migrations (001_init to 008_whitelabel_tenants)
├── docker/
│   ├── control-plane.Dockerfile
│   ├── agent.Dockerfile
│   └── docker-compose.yml   # Local dev stack
├── docs/
│   ├── ARCHITECTURE.md
│   ├── API_REFERENCE.md
│   ├── CONFIGURATION.md
│   ├── DEPLOYMENT_GUIDE.md
│   ├── DEVELOPMENT.md
│   └── MANUAL_TESTING_GUIDE.md
├── examples/
│   └── terraform/           # Planned
├── scripts/
│   ├── dev.sh
│   ├── migrate.sh
│   └── generate.sh          # Code generation (sqlc, protobuf, openapi)
├── go.mod
├── go.sum
├── Makefile
├── README.md
├── READMEru.md
└── SPEC.md                  # Technical specification
```

---

## 9. Task Breakdown

### Milestone 1: Foundation
- [x] **T001** Initialize Go module, Makefile, golangci-lint config
- [x] **T002** Set up CI: build, test, lint, docker build
- [x] **T003** Define Protobuf schemas (`proto/agent/v1/agent.proto`)
- [x] **T004** Generate Go code from protobuf (`buf generate`)
- [x] **T005** Set up sqlc for type-safe SQL → generate models
- [x] **T006** Write database migrations (`001_init.sql` through `008_whitelabel_tenants.sql`)
- [x] **T007** Docker Compose for local dev (Postgres, Redis, CP, Agent)
- [x] **T008** Basic config management (Viper + env)
- [x] **T009** Structured logging (slog + OTLP)

### Milestone 2: Control Plane Core
- [x] **T010** PostgreSQL connection pool + health checks
- [x] **T011** Admin authentication: JWT + refresh tokens + bcrypt + RBAC (owner, superadmin, admin)
- [x] **T012** API Key authentication (scopes, prefix+hash)
- [x] **T013** REST API: Nodes CRUD (OpenAPI + chi)
- [x] **T014** REST API: Users CRUD + Plans CRUD
- [x] **T015** REST API: Credentials CRUD per protocol
- [x] **T016** gRPC server: AgentService.Connect (bidirectional streaming)
- [x] **T016a** Agent registration + certificate validation
- [x] **T016b** Config push (full + delta) with versioning
- [x] **T016c** Heartbeat handling + node health tracking
- [x] **T016d** Metrics ingestion → traffic_stats table
- [x] **T017** Subscription link generation (clash/v2ray/sing-box formats)
- [x] **T018** Admin Web UI: Dashboard, Nodes, Users, Credentials (React 19 SPA + classic HTMX fallback)

### Milestone 3: Node Agent & WireGuard
- [x] **T019** Agent gRPC client: mTLS dialer with cert rotation
- [x] **T020** Agent registration flow + persistent identity
- [x] **T021** Config syncer: apply full config, handle delta updates
- [x] **T022** WireGuard & AmneziaWG adapter: generate server config (wg-quick + nftables + junk/obfuscation)
- [x] **T023** WireGuard adapter: generate client config (.conf + QR)
- [x] **T024** WireGuard interface manager: create/up/down, peer add/remove
- [x] **T025** Traffic accounting: per-peer counters via netlink/wg
- [x] **T026** Metrics collector: 30s interval → gRPC stream
- [x] **T027** Command executor: restart, reload, reboot
- [x] **T028** Agent health endpoint + graceful shutdown

### Milestone 4: Xray Adapter
- [x] **T029** Embed Xray core as library
- [x] **T030** Xray adapter: server config generation (VLESS-Reality, VMess, Trojan, SS)
- [x] **T031** Xray adapter: client config generation (subscription formats)
- [x] **T032** Xray process manager: single process, multiple inbounds
- [x] **T033** Xray metrics: Stats API or log parsing → PeerMetric
- [x] **T034** Reality: automated shortId/keys generation + cert management

### Milestone 5: Polish & Hardening
- [x] **T035** Integration tests: CP + 2 Agents (Docker Compose)
- [x] **T036** Chaos testing: network partition, agent restart, CP restart
- [x] **T037** Rate limiting + DDoS protection on REST API
- [x] **T038** Audit logging for all mutating operations (tamper-proof WORM triggers)
- [x] **T039** Database backup/restore scripts
- [x] **T040** Documentation: API, deployment, architecture, testing guides
- [x] **T041** Release automation: goreleaser, Docker Hub, checksums

---

## 10. Development Workflow

### 10.1 Local Development
```bash
# Start full stack
make dev-up          # docker-compose up -d

# Run CP locally (hot reload)
make run-cp          # air -c .air.toml

# Run Agent locally
make run-agent       # air -c .air.agent.toml

# Run tests
make test            # go test ./... -race -count=1

# Generate code
make generate        # sqlc, protobuf, openapi
```

### 10.2 Git Strategy
- `main`: protected, only via PR
- `feature/*`: short-lived branches
- Conventional commits: `feat:`, `fix:`, `refactor:`, `docs:`, `test:`
- Semantic versioning after v1.0.0

### 10.3 Code Generation Commands
```bash
# sqlc
sqlc generate

# Protobuf (buf)
buf generate

# OpenAPI (oapi-codegen)
oapi-codegen -generate types,chi-server -package openapi api/openapi.yaml
```

---

## 11. Configuration

### 11.1 Control Plane (env)
```yaml
# config.yaml (loaded via Viper)
server:
  http_addr: ":8080"
  grpc_addr: ":9090"
  tls_cert: "/etc/vpnbuilder/cert.pem"
  tls_key: "/etc/vpnbuilder/key.pem"

database:
  dsn: "postgres://user:pass@localhost/vpnbuilder?sslmode=disable"
  max_open_conns: 25
  max_idle_conns: 5

redis:
  addr: "localhost:6379"
  password: ""

auth:
  jwt_secret: "CHANGE_ME_32_CHARS_MINIMUM"
  jwt_access_ttl: "15m"
  jwt_refresh_ttl: "168h"      # 7 days
  bcrypt_cost: 12

ca:
  cert_file: "/etc/vpnbuilder/ca.pem"
  key_file: "/etc/vpnbuilder/ca-key.pem"
  cert_ttl: "720h"             # 30 days

adapter:
  wireguard:
    interface_prefix: "wg"
    subnet_v4: "10.8.0.0/16"
    subnet_v6: "fd00::/64"
    dns: "1.1.1.1,1.0.0.1"
    mtu: 1280
  xray:
    log_level: "warning"
```

### 11.2 Agent (env)
```yaml
agent:
  node_name: ""                # Auto: hostname
  control_plane: "cp.example.com:9090"
  ca_cert: "/etc/vpnbuilder/ca.pem"
  cert_file: "/etc/vpnbuilder/agent.pem"
  key_file: "/etc/vpnbuilder/agent-key.pem"
  sync_interval: "30s"
  metrics_interval: "30s"
  wireguard:
    interface_prefix: "wg"
  xray:
    config_dir: "/etc/vpnbuilder/xray"
    assets_dir: "/usr/share/vpnbuilder/xray"
```

---

## 12. Testing Strategy

| Layer | Tool | Coverage Target |
|---|---|---|
| Unit | `testing` + `testify` + `gomock` | 80%+ |
| Integration | `testcontainers-go` (Postgres, Redis) | Critical paths |
| Contract | `protobuf` + `oapi-codegen` validation | 100% |
| E2E | Docker Compose + custom test binary | Happy paths |
| Chaos | `tc`, `iptables`, kill -9 | Manual CI job |

---

## 13. Observability

### 13.1 Metrics (Prometheus)
- `vpnbuilder_cp_requests_total`: by endpoint, status
- `vpnbuilder_cp_db_duration_seconds`: SQL latency
- `vpnbuilder_agent_nodes_connected`: gauge
- `vpnbuilder_agent_peers_total`: by protocol, node
- `vpnbuilder_traffic_bytes_total`: by user, node, protocol, direction

### 13.2 Logging
- Structured JSON (slog)
- Levels: DEBUG, INFO, WARN, ERROR
- Correlation IDs across CP ↔ Agent

### 13.3 Tracing
- OpenTelemetry (W3C trace-context)
- Spans: HTTP handlers, gRPC calls, DB queries, adapter operations

---

## 14. Deployment

### 14.1 Control Plane
- Single binary (`vpnbuilder-cp`)
- Systemd service or Kubernetes Deployment
- Postgres: managed (RDS/CloudSQL) or Patroni cluster
- Redis: managed or single instance (cache only)
- TLS: Let's Encrypt (public) or self-signed CA (private)

### 14.2 Node Agent
- Single binary (`vpnbuilder-agent`)
- Runs as root (netlink, wireguard, nftables)
- Systemd service with `Restart=always`
- Auto-registers on first boot
- mTLS certs rotated via CP (30d TTL)

---

## 15. Future Roadmap (Post-MVP)

| Milestone | Focus | Key Items |
|---|---|---|
| **Developer Experience** | Tooling & Integration | Terraform provider, Go SDK, OpenAPI client gen, Webhooks |
| **Mesh & White-label** | Networking & Branding | Device-to-device, ACL engine, Custom branding, OIDC/SAML |
| **Enterprise** | Scale & Governance | HA CP (Raft), Multi-region, Audit streaming, RBAC |
| **Ecosystem** | Clients & Monetization | Client apps, Marketplace, Billing integration |

---

## 16. Decision Log (ADR)

| ADR | Title | Status |
|---|---|---|
| 001 | Go as primary language | Accepted |
| 002 | PostgreSQL for primary storage | Accepted |
| 003 | gRPC + mTLS for CP↔Agent | Accepted |
| 004 | Protocol Adapter pattern | Accepted |
| 005 | WireGuard native (not userspace) | Accepted |
| 006 | Xray embedded (not subprocess) | Accepted |
| 007 | Admin UI: React 19 SPA with HTMX fallback | Accepted |
| 008 | sqlc for database access | Accepted |
| 009 | buf for protobuf management | Accepted |

---

## 17. Quick Reference

### Commands
```bash
# Generate all code
make generate

# Run tests with coverage
make test-coverage

# Build binaries
make build

# Build Docker images
make docker-build

# Run migrations
make migrate-up

# Lint
make lint
```

### Key Files to Create First
1. `go.mod`: module `github.com/ivanchik-byte/Simple-VPN-Builder`
2. `Makefile`: all targets above
3. `.golangci.yml`: lint config
4. `proto/agent/v1/agent.proto`: gRPC contract
5. `migrations/001_init.sql`: schema
6. `docker/docker-compose.yml`: local stack
7. `cmd/control-plane/main.go`: CP entrypoint
8. `cmd/agent/main.go`: Agent entrypoint

---

*End of SPEC.md: Single source of truth for architecture. Update it as decisions evolve.*