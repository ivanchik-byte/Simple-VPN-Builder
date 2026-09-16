# Simple-VPN-Builder System Architecture

## 1. High-Level Overview

Simple-VPN-Builder is a distributed, multi-tenant VPN infrastructure orchestration platform designed for commercial VPN providers, enterprise overlay networks, and censorship-circumvention providers.

Instead of managing monolithic servers with embedded panels, Simple-VPN-Builder cleanly splits responsibility between a central **Control Plane** and distributed **Node Agents**:

```
+---------------------------------------------------------------------------------+
|                                 CONTROL PLANE                                   |
|                                                                                 |
|   +-------------------+   +--------------------+   +------------------------+   |
|   |   REST API v1     |   |   Admin Web UI     |   |  Subscription Delivery |   |
|   |  (:8110 /api/v1)  |   |  (:8110 /admin)   |   |   (:8110 /sub/{token}) |   |
|   +---------+---------+   +---------+----------+   +-----------+------------+   |
|             |                       |                          |                |
|             +-----------------------+--------------------------+                |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |  Service Domain    |                                |
|                           |  (Business Logic)  |                                |
|                           +----+----------+----+                                |
|                                |          |                                     |
|               +----------------v---+  +---v----------------+                    |
|               |  PostgreSQL 16     |  |   Redis 7 Cache    |                    |
|               |  (Persistent Store)|  |   (Tokens/Limiter) |                    |
|               +--------------------+  +--------------------+                    |
|                                     |                                           |
|                           +---------v----------+                                |
|                           |   gRPC Agent Hub   |                                |
|                           |   (:9090 with mTLS)|                                |
|                           +---------+----------+                                |
+-------------------------------------|-------------------------------------------+
                                      |
                       Bidirectional gRPC Streaming
                       (mTLS + Token Bucket Limit)
                                      |
         +----------------------------+----------------------------+
         |                                                         |
+--------v---------------------------------+     +-----------------v-----------------------+
|          NODE AGENT (Region 1)           |     |         NODE AGENT (Region 2)           |
|                                          |     |                                         |
| +--------------------------------------+ |     | +-------------------------------------+ |
| |        gRPC Sync & Heartbeat         | |     | |        gRPC Sync & Heartbeat        | |
| +-------------------+------------------+ |     | +-------------------+-----------------+ |
|                     |                    |     |                     |                 |
|      +--------------+-------------+      |     |      +--------------+-------------+   |
|      |                            |      |     |      |                            |   |
| +----v-------------+     +--------v----+ |     | +----v-------------+     +--------v-+ |
| | WireGuard/AWG    |     | Xray VLESS  | |     | | WireGuard/AWG    |     | Xray     | |
| | Netlink Engine   |     | Reality Core| |     | | Netlink Engine   |     | Reality  | |
| +----+-------------+     +--------+----+ |     | +----+-------------+     +--------+-+ |
|      |                            |      |     |      |                            |   |
| +----v----------------------------v----+ |     | +----v----------------------------v-+ |
| |        nftables Firewall & NAT       | |     | |        nftables Firewall & NAT      | |
| +--------------------------------------+ |     | +-------------------------------------+ |
+------------------------------------------+     +-----------------------------------------+
```

---

## 2. Control Plane Subsystems

### 2.1 REST API & Middleware Pipeline
- Implemented with Chi router (`github.com/go-chi/chi/v5`).
- Request Lifecycle:
  1. Request ID injection (`X-Request-ID`).
  2. Structured request logging via `log/slog`.
  3. Prometheus RED metrics instrumentation (Rate, Errors, Duration).
  4. Panic Recovery with RFC 7807 problem details output.
  5. Security headers and 1 MB body limit.
  6. CORS with explicit allowlist (no wildcard with credentials).
  7. Redis sliding-window rate limiting per client IP (health probes, metrics scrapes excluded from limits but still authenticated).
  8. Dual-scheme authentication: Bearer JWT or `vpn_admin_token` cookie, plus scoped `X-API-Key`.
- Admin console: server-rendered `html/template` pages with HTMX partials and Alpine.js; cookie session plus HMAC CSRF tokens on all mutations.

### 2.2 Database & Data Access Layer
- PostgreSQL 16 connection pooling via `github.com/jackc/pgx/v5/pgxpool`.
- Query layer generated with `sqlc` for compile-time type safety.
- All queries parameterized; no string-interpolated SQL.
- Eight migrations (`001`–`008`): core schema, commercial billing, plan builder, admin RBAC permissions, audit-log hardening (immutable trigger), Telegram CRM, email OTP policy, white-label tenants. Every migration ships a down file.
- Domain models: `nodes`, `users`, `plans`, `credentials`, `traffic_stats`, `admins`, `api_keys`, `audit_logs`, plus billing (`orders`, `payment_gateways`, `promo_codes`, `broadcasts`, `billing_settings`, `bot_replies`), CRM (`telegram_leads`, referrals), and white-label tenants.

### 2.3 Access Control
- Roles: `owner` > `superadmin` > `admin`; owners bypass role checks.
- Admins carry granular flags (`can_broadcast`, `can_manage_users`, `can_delete_users`, `can_manage_nodes`, `can_manage_plans`, `can_view_audit`, `can_access_ai_copilot`).
- The AI Copilot flag is denied by default and granted per admin by an owner.
- Webhook endpoints verify HMAC signatures and refuse unsigned calls when no secret is configured.

### 2.3 gRPC Agent Management Service- Runs on port `:9090` enforcing strict mutual TLS (mTLS).
- Interceptor chain:
  - Stream rate limiting: Token bucket per agent connection to prevent reconnect thundering herd.
  - Logging interceptor with contextual agent identity.
- Inactivity Watchdog: Detects dead streams if no heartbeat or ping is received for 25 seconds.
- Atomic Config Versioning: Monotonically increasing configuration version numbers ensure delta updates are applied sequentially without race conditions.

---

## 3. Node Agent Subsystems

### 3.1 Network Interface & WireGuard Engine
- Manages Linux kernel WireGuard interfaces (`wg0`, `wg1`...) using netlink via `golang.zx2c4.com/wireguard/wgctrl`.
- Peer diffing algorithm: Computes additions, deletions, and modifications in memory, pushing atomic batches to the kernel without dropping active connections.
- Endpoint roaming: Automatically preserves dynamic client IP/port changes while maintaining traffic accounting.

### 3.2 AmneziaWG Obfuscation Engine
- Supports obfuscated packet formats with custom header junk parameters:
  - Junk packet counts (`Jc`), packet length range (`Jmin`, `Jmax`).
  - Magic header identifiers (`H1`, `H2`, `H3`, `H4`) and sync offsets (`S1`, `S2`).
- Provides stealth bypassing of Deep Packet Inspection (DPI) censorship firewalls.

### 3.3 Xray-Core VLESS Reality Engine
- Embedded multi-inbound manager running on port 443 with TLS 1.3 Reality camouflage.
- Dynamic user provisioning: Uses Xray gRPC `HandlerService` (`AlterInbound`) to add/remove users on the fly without restarting the Xray process.
- Real-time accounting: Queries Xray gRPC `StatsService` for per-client uplink/downlink traffic deltas.
- Anti-abuse routing: Prevents server loopback, blocks LAN/private IP access (`geoip:private`), and blocks cloud instance metadata endpoints (`169.254.169.254/32`).

### 3.4 Firewall & Kernel Tuning
- Atomic `nftables` table management (`inet vpnbuilder`).
- Dynamic TCP MSS clamping to prevent fragmentation issues across WAN routes.
- Kernel sysctl tuning: Activates BBR congestion control (`net.ipv4.tcp_congestion_control = bbr`) and enables IPv4/IPv6 packet forwarding.

---

## 4. Subscription Delivery

The Control Plane serves token URLs at `GET /sub/{token}`:
- Auto-detects client capabilities via User-Agent negotiation:
  - Official WireGuard -> Standard `.conf`
  - AmneziaVPN -> AmneziaWG `.conf` with obfuscation parameters
  - Sing-box -> JSON configuration (v1.10+)
  - Clash / Clash Meta (Mihomo) -> YAML configuration
  - Others -> Base64 subscription bundle
- Returns standard subscription headers:
  `Subscription-Userinfo: upload=...; download=...; total=...; expire=...`
- Admin console pages embed 1-click import schemes (`sing-box://`, `clash://`, `wireguard://`), QR codes, and live bandwidth quotas.

---

## 5. Security & Observability Architecture

- **Mutual TLS**: Control Plane acts as Internal CA or uses external CA certificates to issue 30-day agent certificates.
- **Prometheus Metrics**:
  - Control Plane: HTTP request durations, active gRPC streams, database pool statistics. The `/metrics` endpoint requires JWT or API-key authentication.
  - Node Agent: Per-peer bytes received/transmitted, active handshake timestamps, firewall drops. The `:8081/metrics` endpoint requires a bearer token (`VPNBUILDER_METRICS_TOKEN`) and stays closed when unset.
- **OpenTelemetry Tracing**: Distributed tracing using W3C TraceContext headers across HTTP and gRPC boundaries.
- **Disaster Recovery**: Automated database backup scripts with SHA256 verification and 7-day retention (`scripts/backup_db.sh`).

---

## 6. Telegram Bot & AI Infra Copilot

### 6.1 Telegram Bot (`cmd/bot`, `internal/bot`)
- Lead capture and trial issuance, email linking with OTP verification, referral tracking, payments via CryptoBot and Telegram Stars, and account restore flows.
- Talks to the control plane over REST with the internal API key; reply texts are editable from billing settings.

### 6.2 AI Infra Copilot (`internal/controlplane/ai`)
- Chat interface over nodes, users, and telemetry through an OpenAI-compatible endpoint (public API or self-hosted, e.g. Ollama or NVIDIA NIM).
- Tool calls are scoped to read operations plus explicitly confirmed proposals: the model drafts an infrastructure action, a human approves it, and only then it executes (two-phase safety with single-use tokens).
- Access is owner-only by default and grantable per admin (`can_access_ai_copilot`). Endpoint credentials live in AI settings, never in chat history.
