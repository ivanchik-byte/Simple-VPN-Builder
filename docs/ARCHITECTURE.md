# Simple-VPN-Builder System Architecture

## 1. High-Level Overview

Simple-VPN-Builder is a distributed, multi-tenant VPN infrastructure orchestration platform designed for commercial VPN providers, enterprise overlay networks, and censorship-circumvention providers.

Instead of managing monolithic servers with embedded panels, Simple-VPN-Builder cleanly splits responsibility between a central **Control Plane** and distributed **Node Agents**:

```
+---------------------------------------------------------------------------------+
|                                 CONTROL PLANE                                   |
|                                                                                 |
|   +-------------------+   +--------------------+   +------------------------+   |
|   |   REST API v1     |   |   Admin Web UI     |   |  Subscription Portal   |   |
|   |  (:8110 /api/v1)  |   |   (HTMX + Alpine)  |   |     (/client/{token})  |   |
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
  2. OpenTelemetry W3C TraceContext propagation.
  3. Structured request logging via `log/slog`.
  4. Global and IP-based Token Bucket Rate Limiting.
  5. Authentication: Dual JWT (Bearer token / HttpOnly Cookie) and API Key (`X-API-Key`).
  6. Panic Recovery with RFC 7807 problem details output.
  7. Prometheus RED metrics instrumentation (Rate, Errors, Duration).

### 2.2 Database & Data Access Layer
- PostgreSQL 16 connection pooling via `github.com/jackc/pgx/v5/pgxpool`.
- Query layer generated with `sqlc` for compile-time type safety.
- Transaction management using atomic `WithTx` helpers.
- Nine core database models: `nodes`, `users`, `plans`, `credentials`, `traffic_stats`, `admins`, `api_keys`, `audit_logs`, `webhooks`.

### 2.3 gRPC Agent Management Service
- Runs on port `:9090` enforcing strict mutual TLS (mTLS).
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

## 4. Universal Client Subscription Delivery

The Control Plane provides a dedicated endpoint `GET /client/{token}` and `GET /sub/{token}`:
- Auto-detects client capabilities via User-Agent negotiation:
  - Official WireGuard -> Standard `.conf`
  - AmneziaVPN -> AmneziaWG `.conf` with obfuscation parameters
  - Sing-box -> Experimental JSON configuration (v1.10+)
  - Clash / Clash Meta (Mihomo) -> Formatted YAML configuration
  - V2Ray / Shadowsocks -> Standard Base64 subscription bundle
- Returns standard HTTP subscription headers:
  `Subscription-Userinfo: upload=...; download=...; total=...; expire=...`
- Web Portal UI: Responsive Obsidian dark dashboard with 1-click import schemes (`sing-box://`, `clash://`, `wireguard://`), QR code generation, and live bandwidth quotas.

---

## 5. Security & Observability Architecture

- **Mutual TLS**: Control Plane acts as Internal CA or uses external CA certificates to issue 30-day agent certificates.
- **Prometheus Metrics**:
  - Control Plane: HTTP request durations, active gRPC streams, database pool statistics.
  - Node Agent: Per-peer bytes received/transmitted, active handshake timestamps, firewall drops.
- **OpenTelemetry Tracing**: Distributed tracing using W3C TraceContext headers across HTTP and gRPC boundaries.
- **Disaster Recovery**: Automated database backup scripts with SHA256 verification and 7-day retention (`scripts/backup_db.sh`).
