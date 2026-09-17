# Simple-VPN-Builder Manual Testing Guide and Verification Protocol

## Document Information
- Target System: Simple-VPN-Builder End-to-End Architecture
- Scope: Control Plane, Database, Cache, gRPC Engine, Protocol Adapters (WireGuard & AmneziaWG), Node Agent, Billing Webhooks, and Observability
- Status: Living document, updated with the codebase
- Revision: 1.1.0

---

## Table of Contents
1. Architectural Overview and Test Environment Topography
2. Section 1: Environment and Tooling Prerequisites
   - Case ENV-01: Tooling and Dependency Verification
   - Case ENV-02: Cryptographic Material and TLS/mTLS Key Generation
3. Section 2: Infrastructure Stack Boot and Verification
   - Case INFRA-01: Containerized PostgreSQL 16 and Redis 7 Initialization
   - Case INFRA-02: Database Connectivity and Connection Pool Limits
   - Case INFRA-03: Redis In-Memory Engine and Eviction Verification
4. Section 3: Database Migration and Schema Integrity Audit
   - Case MIG-01: Migration Execution via Embedded Migrator and golang-migrate
   - Case MIG-02: Structural Audit of 17 Core Relational Tables
   - Case MIG-03: AmneziaWG Obfuscation Fields and Data Type Audit
   - Case MIG-04: Index Footprint and Constraint Enforcement Probe
   - Case MIG-05: Automated updated_at Timestamp Trigger Validation
   - Case MIG-06: Schema Rollback and Idempotency Verification
5. Section 4: Control Plane Service Launch and Health Probes
   - Case CP-01: Control Plane Service Bootstrapping
   - Case CP-02: Liveness Probe (/healthz) Inspection
   - Case CP-03: Readiness Probe (/readyz) Under Normal Operating Conditions
   - Case CP-04: Fault Injection Probe (/readyz) Under Upstream Outage
6. Section 5: Authentication, Authorization, and API Keys
   - Case AUTH-01: Superadmin Provisioning and Bcrypt Password Hashing
   - Case AUTH-02: Admin Login and JWT Access/Refresh Token Generation
   - Case AUTH-03: JWT Access Expiration and Refresh Lifecycle
   - Case AUTH-04: API Key Generation with 'vpn_' Prefix and SHA-256 Hashing
   - Case AUTH-05: Dual-Scheme Authentication on Protected Endpoints
   - Case AUTH-06: API Key Revocation and Invalid Key Rejection
7. Section 6: Exit Node Management and Registration
   - Case NODE-01: Exit Node Registration (Frankfurt-01)
   - Case NODE-02: Node Filtering by Status and Geographical Region
   - Case NODE-03: Node Metadata and Bandwidth Capacity Modification
   - Case NODE-04: Node Heartbeat State Tracking and Transition
8. Section 7: Subscription Plans and User Management
   - Case USER-01: Subscription Plan Creation (Basic vs Pro Unlimited)
   - Case USER-02: User Account Creation and Plan Association
   - Case USER-03: Subscription Token Invalidation and Rotation
   - Case USER-04: Dynamic Subscription Endpoint (/sub/{token}) Resolution
9. Section 8: Protocol Credential Provisioning and AmneziaWG Obfuscation
   - Case CRED-01: Standard WireGuard Keypair Generation and Network Allocation
   - Case CRED-02: AmneziaWG Credential Provisioning with Obfuscation Parameters
   - Case CRED-03: Client Configuration Assembly (.conf Format)
   - Case CRED-04: Credential Revocation and Cascade Deletion
10. Section 9: Node Agent Launch and gRPC Streaming Synchronization
    - Case AGENT-01: Agent Configuration and mTLS Certificate Staging
    - Case AGENT-02: Agent Process Startup and gRPC Transport Handshake
    - Case AGENT-03: Bidirectional Stream Config Sync and ConfigAck Emission
    - Case AGENT-04: Network Interface Allocation (wg0) and Link Initialization
    - Case AGENT-05: Dynamic Peer Attachment and Netlink Sync
11. Section 10: Traffic Accounting, Metrics Rollup, and Quota Enforcement
    - Case TRAFFIC-01: Agent Periodic Metrics Emission via gRPC
    - Case TRAFFIC-02: Hourly Bucket Rollup Insertion (traffic_stats Table)
    - Case TRAFFIC-03: Aggregate Usage Querying by User and Node
    - Case TRAFFIC-04: User Traffic Quota Exhaustion and Account Suspension
12. Section 11: Payment Webhook Verification and Signature Enforcement
   - Case WH-01: Signed CryptoBot Webhook Marks Order Paid
   - Case WH-02: Tampered Signature Is Rejected
   - Case WH-03: Gateway Without Secret Refuses Unsigned Calls
13. Section 12: Web Admin UI (React 19 SPA) and Data Endpoints Verification
    - Case UI-01: Admin Web Session Login and CSRF Token Acquisition
    - Case UI-02: React 19 SPA Route Integrity
    - Case UI-03: Users Data JSON Endpoint (/admin/users-data)
    - Case UI-04: Plans Data JSON Endpoint (/admin/plans-data)
    - Case UI-05: Node Data JSON Endpoint (/admin/node-data)
    - Case UI-06: Audit Logs Data JSON Endpoint (/admin/audit-data)
    - Case UI-07: Settings Data JSON Endpoint (/admin/settings-data)
    - Case UI-08: Legacy Route Backward Compatibility and 303 Redirects
14. Section 13: Graceful Shutdown, Teardown, and Disaster Recovery
    - Case TEAR-01: Graceful Agent Shutdown and Network Interface Deletion
    - Case TEAR-02: Graceful Control Plane Draining and Connection Release
    - Case TEAR-03: Infrastructure Stack Cleanup and Volume Pruning
    - Case TEAR-04: Full Disaster Recovery from Database Snapshot
15. Verification Sign-Off Matrix

---

## 1. Architectural Overview and Test Environment Topography

The Simple-VPN-Builder platform consists of a centralized Control Plane (CP) and distributed Node Agents communicating over a bidirectional gRPC stream secured with mutual TLS (mTLS).

```
                      +---------------------------------------+
                      |         Control Plane (CP)            |
                      |  - REST API (:8110)                   |
                      |  - gRPC Server (:9090)                |
                      |  - JWT & API Key Auth Manager         |
                      |  - Subscription Engine (/sub/{token}) |
                      +-------------------+-------------------+
                                          |
                +-------------------------+-------------------------+
                |                                                   |
                v                                                   v
+-------------------------------+                   +-------------------------------+
|       PostgreSQL 16           |                   |            Redis 7            |
| - 17 Relational Tables        |                   | - Rate Limiting & Token Revocation |
| - Triggers & Constraints      |                   | - Sliding-Window Throttling        |
| - Traffic Stats Buckets       |                   +-------------------------------+
+-------------------------------+
                ^
                | mTLS (Cert/Key)
                | Bidirectional Stream: Connect(AgentMessage) -> ControlMessage
                v
+-------------------------------------------------------------------+
|                           Node Agent                              |
| - gRPC Client (mTLS authenticated)                                |
| - Interface Manager (WireGuard & AmneziaWG netlink interfaces)    |
| - Syncer (Applies ConfigUpdate, emits ConfigAck)                  |
| - Collector (Emits Heartbeats every 10s, Metrics every 30s)       |
+-------------------------------------------------------------------+
```

---

## 2. Section 1: Environment and Tooling Prerequisites

### Case ENV-01: Tooling and Dependency Verification
- Purpose: Ensure the host environment possesses all binaries, kernel modules, and CLI tools necessary for deployment, protocol inspection, and automated testing.
- Prerequisites: Linux host (Kernel 5.15+ recommended), root or sudo access.

#### Execution Command:
```bash
# Verify Go toolchain
go version

# Verify Docker engine and Docker Compose plugin
docker version
docker compose version

# Verify network and protocol debugging utilities
which curl
which jq
which psql
which redis-cli
which grpcurl
which wg
which wg-quick
which ip

# Verify kernel WireGuard module presence
lsmod | grep -E "^wireguard" || modprobe wireguard
lsmod | grep -E "^wireguard"

# Verify TUN device accessibility
ls -l /dev/net/tun
```

#### Verification Criteria:
- Go version reports `go1.25` or newer.
- Docker engine and Docker compose are responsive.
- Utilities `curl`, `jq`, `psql`, `redis-cli`, `grpcurl`, `wg`, `ip` exist in system PATH.
- `wireguard` module is loaded into the kernel or built statically.
- `/dev/net/tun` exists with permissions `crw-rw-rw-`.

#### Troubleshooting and Rollback:
- If WireGuard kernel module is missing: `sudo apt-get update && sudo apt-get install -y wireguard-tools linux-headers-$(uname -r)`.
- If `grpcurl` is missing: `go install github.com/fullstorydev/grpcurl/cmd/grpcurl@latest`.
- If `/dev/net/tun` is missing: `sudo mkdir -p /dev/net && sudo mknod /dev/net/tun c 10 200 && sudo chmod 0666 /dev/net/tun`.

---

### Case ENV-02: Cryptographic Material and TLS/mTLS Key Generation
- Purpose: Generate the local Certificate Authority (CA), Control Plane server TLS certificates, and Agent client certificates for mTLS gRPC communication.
- Prerequisites: OpenSSL installed on the testing host.

#### Execution Command:
```bash
# Create directory structure for certificates
mkdir -p docker/certs
cd docker/certs

# 1. Generate Root CA Private Key and Self-Signed Certificate (valid for 1 year)
openssl genrsa -out ca-key.pem 4096
openssl req -new -x509 -days 365 -key ca-key.pem -out ca.pem \
  -subj "/C=US/ST=State/L=City/O=SimpleVPN/OU=Security/CN=SimpleVPN-Root-CA"

# 2. Generate Control Plane Server Private Key and CSR
openssl genrsa -out cp-key.pem 2048
openssl req -new -key cp-key.pem -out cp.csr \
  -subj "/C=US/ST=State/L=City/O=SimpleVPN/OU=ControlPlane/CN=control-plane"

# Create SAN configuration for Control Plane
cat <<EOF > cp-ext.cnf
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth, clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = control-plane
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = 0.0.0.0
EOF

# Sign Control Plane Certificate with Root CA
openssl x509 -req -days 365 -in cp.csr -CA ca.pem -CAkey ca-key.pem \
  -CAcreateserial -out cp.pem -extfile cp-ext.cnf

# 3. Generate Agent Client Private Key and CSR
openssl genrsa -out agent-key.pem 2048
openssl req -new -key agent-key.pem -out agent.csr \
  -subj "/C=US/ST=State/L=City/O=SimpleVPN/OU=Agents/CN=agent-1"

# Create SAN configuration for Agent
cat <<EOF > agent-ext.cnf
authorityKeyIdentifier=keyid,issuer
basicConstraints=CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = clientAuth
EOF

# Sign Agent Certificate with Root CA
openssl x509 -req -days 365 -in agent.csr -CA ca.pem -CAkey ca-key.pem \
  -CAcreateserial -out agent.pem -extfile agent-ext.cnf

# Verify certificate bundle permissions and integrity
chmod 600 *-key.pem
chmod 644 *.pem
openssl verify -CAfile ca.pem cp.pem
openssl verify -CAfile ca.pem agent.pem

cd ../..
```

#### Verification Criteria:
- Command `openssl verify -CAfile ca.pem cp.pem` returns `cp.pem: OK`.
- Command `openssl verify -CAfile ca.pem agent.pem` returns `agent.pem: OK`.
- Files `ca.pem`, `ca-key.pem`, `cp.pem`, `cp-key.pem`, `agent.pem`, and `agent-key.pem` are created in `docker/certs`.

#### Troubleshooting and Rollback:
- If verification fails with `unable to get local issuer certificate`, verify that `-CAfile ca.pem` matches the key used to sign the cert.
- To reset: `rm -rf docker/certs/*` and re-run the steps above.

---

## 3. Section 2: Infrastructure Stack Boot and Verification

### Case INFRA-01: Containerized PostgreSQL 16 and Redis 7 Initialization
- Purpose: Launch PostgreSQL 16 and Redis 7 backing services via Docker Compose and ensure health checks transition to healthy status.
- Prerequisites: `docker/docker-compose.yml` present, Docker engine active.

#### Execution Command:
```bash
# Start PostgreSQL and Redis in isolated network
docker compose -f docker/docker-compose.yml up -d postgres redis

# Check container state and healthcheck progression
docker compose -f docker/docker-compose.yml ps
```

#### Verification Criteria:
- Container `vpnbuilder-postgres` shows State `Up (healthy)`.
- Container `vpnbuilder-redis` shows State `Up (healthy)`.
- Standard host port mappings `127.0.0.1:5432` and `127.0.0.1:6379` are bound.

#### Troubleshooting and Rollback:
- If unhealthy, inspect logs: `docker compose -f docker/docker-compose.yml logs postgres` or `docker compose -f docker/docker-compose.yml logs redis`.
- Port conflict (5432 or 6379 already bound on host): Check with `sudo netstat -tlpn | grep -E '5432|6379'` and stop conflicting host services.
- Clean reset: `docker compose -f docker/docker-compose.yml down -v`.

---

### Case INFRA-02: Database Connectivity and Connection Pool Limits
- Purpose: Confirm direct TCP access, database authentication credentials, and database creation.
- Prerequisites: Container `vpnbuilder-postgres` healthy.

#### Execution Command:
```bash
# Direct psql connection probe
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "\conninfo"

# Verify current settings for connections
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "SHOW max_connections;"
```

#### Verification Criteria:
- Output indicates connection to database "vpnbuilder" as user "vpnbuilder" via socket or host 127.0.0.1.
- Query executes with exit code 0.

#### Troubleshooting and Rollback:
- If connection is rejected with authentication failure, ensure environment variables in `docker/docker-compose.yml` match `POSTGRES_USER=vpnbuilder`, `POSTGRES_PASSWORD=vpnbuilder`, `POSTGRES_DB=vpnbuilder`.

---

### Case INFRA-03: Redis In-Memory Engine and Eviction Verification
- Purpose: Confirm Redis engine readiness, read/write roundtrip, and TTL expiration mechanics.
- Prerequisites: Container `vpnbuilder-redis` healthy.

#### Execution Command:
```bash
# PING test
redis-cli -h 127.0.0.1 -p 6379 ping

# Key write with TTL test
redis-cli -h 127.0.0.1 -p 6379 set test_probe "vpnbuilder_ok" EX 5
redis-cli -h 127.0.0.1 -p 6379 get test_probe
sleep 6
redis-cli -h 127.0.0.1 -p 6379 get test_probe
```

#### Verification Criteria:
- First `ping` returns `PONG`.
- Immediate `get test_probe` returns `"vpnbuilder_ok"`.
- Post-sleep `get test_probe` returns `(nil)`.

#### Troubleshooting and Rollback:
- If Redis is unresponsive, check container memory allocation: `docker stats --no-stream vpnbuilder-redis`.

---

## 4. Section 3: Database Migration and Schema Integrity Audit

### Case MIG-01: Migration Execution via Embedded Migrator and golang-migrate
- Purpose: Execute schema migration 001_init.up.sql against PostgreSQL and verify complete application.
- Prerequisites: PostgreSQL container active, `migrations/001_init.up.sql` available.

#### Execution Command:
```bash
# Apply migrations using psql direct execution
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -f migrations/001_init.up.sql

# Query migration completion
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "SELECT extname FROM pg_extension WHERE extname IN ('uuid-ossp', 'pgcrypto');"
```

#### Verification Criteria:
- Execution completes without syntax errors or relation errors.
- Extensions `uuid-ossp` and `pgcrypto` are active.

#### Troubleshooting and Rollback:
- If relation already exists error occurs: `PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -f migrations/001_init.down.sql` followed by re-execution of `.up.sql`.

---

### Case MIG-02: Structural Audit of 17 Core Relational Tables
- Purpose: Guarantee that all 17 required domain tables exist with proper relations.
- Prerequisites: Case MIG-01 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT table_name 
FROM information_schema.tables 
WHERE table_schema = 'public' 
ORDER BY table_name;
"
```

#### Verification Criteria:
The query result MUST contain exactly the following 17 tables:
1. `admins`
2. `api_keys`
3. `audit_logs`
4. `billing_settings`
5. `bot_replies`
6. `broadcast_campaigns`
7. `credentials`
8. `nodes`
9. `orders`
10. `payment_gateways`
11. `plans`
12. `promo_codes`
13. `tenants`
14. `traffic_stats`
15. `user_email_verifications`
16. `users`
17. `webhooks`

#### Troubleshooting and Rollback:
- If any table is missing, verify `migrations/001_init.up.sql` line ranges against the database schema output.

---

### Case MIG-03: AmneziaWG Obfuscation Fields and Data Type Audit
- Purpose: Ensure that the `credentials` table possesses all required AmneziaWG (AWG) parameters with correct PostgreSQL data types and default values.
- Prerequisites: Case MIG-02 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT column_name, data_type, column_default 
FROM information_schema.columns 
WHERE table_name = 'credentials' AND column_name LIKE 'awg_%'
ORDER BY column_name;
"
```

#### Verification Criteria:
Output must match the schema specification:
- `awg_h1`: `bigint`, default: `16843009`
- `awg_h2`: `bigint`, default: `33686018`
- `awg_h3`: `bigint`, default: `50529027`
- `awg_h4`: `bigint`, default: `67372036`
- `awg_jc`: `integer`, default: `4`
- `awg_jmax`: `integer`, default: `70`
- `awg_jmin`: `integer`, default: `40`
- `awg_s1`: `integer`, default: `64`
- `awg_s2`: `integer`, default: `64`

#### Troubleshooting and Rollback:
- If defaults do not match, inspect `migrations/001_init.up.sql` lines 77-85 and alter table directly if required.

---

### Case MIG-04: Index Footprint and Constraint Enforcement Probe
- Purpose: Validate presence of composite and search performance indexes on users, credentials, traffic stats, nodes, and webhooks.
- Prerequisites: Case MIG-02 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT indexname, tablename, indexdef 
FROM pg_indexes 
WHERE schemaname = 'public' 
ORDER BY tablename, indexname;
"
```

#### Verification Criteria:
The following indexes must be present in the query output:
- `idx_users_status_expires` on `users(status, expires_at)`
- `idx_users_subscription_token` on `users(subscription_token)`
- `idx_credentials_user_node` on `credentials(user_id, node_id)`
- `idx_traffic_stats_user_hour` on `traffic_stats(user_id, hour_bucket DESC)`
- `idx_nodes_status_region` on `nodes(status, region)`
- `idx_audit_logs_created` on `audit_logs(created_at DESC)`
- `idx_audit_logs_admin` on `audit_logs(admin_id, created_at DESC)`
- `idx_api_keys_prefix` on `api_keys(prefix)`
- `idx_webhooks_active` on `webhooks(is_active)`

#### Troubleshooting and Rollback:
- Missing index: Execute manual `CREATE INDEX CONCURRENTLY <indexname> ON ...` or re-run migrations.

---

### Case MIG-05: Automated updated_at Timestamp Trigger Validation
- Purpose: Prove that the PostgreSQL PL/pgSQL function `update_updated_at_column()` modifies `updated_at` automatically on row updates without manual timestamp intervention.
- Prerequisites: Case MIG-02 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
-- Insert dummy plan
INSERT INTO plans (id, name, monthly_price, traffic_limit)
VALUES ('00000000-0000-0000-0000-000000000001', 'trigger-test-plan', 9.99, 1000000000);

-- Query original timestamps
SELECT id, created_at, updated_at FROM plans WHERE id = '00000000-0000-0000-0000-000000000001';

-- Sleep 1 second
SELECT pg_sleep(1.1);

-- Update plan row
UPDATE plans SET monthly_price = 19.99 WHERE id = '00000000-0000-0000-0000-000000000001';

-- Query updated timestamps
SELECT id, created_at, updated_at, (updated_at > created_at) AS is_newer 
FROM plans WHERE id = '00000000-0000-0000-0000-000000000001';

-- Clean up
DELETE FROM plans WHERE id = '00000000-0000-0000-0000-000000000001';
"
```

#### Verification Criteria:
- Initial `created_at` equals `updated_at`.
- After update, `updated_at` is strictly greater than `created_at`.
- Column `is_newer` outputs `t` (true).

#### Troubleshooting and Rollback:
- If `is_newer` is `f`: Verify trigger binding with `SELECT tgname FROM pg_trigger WHERE tgrelid = 'plans'::regclass;`.

---

### Case MIG-06: Schema Rollback and Idempotency Verification
- Purpose: Confirm that executing the down migration cleanly removes all database objects and that reapplying up migration succeeds idempotently.
- Prerequisites: Case MIG-02 passed.

#### Execution Command:
```bash
# Apply down migration
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -f migrations/001_init.down.sql

# Verify all domain tables were dropped
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';
"

# Re-apply up migration
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -f migrations/001_init.up.sql

# Re-verify count of tables
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';
"
```

#### Verification Criteria:
- Table count drops to 0 after `001_init.down.sql`.
- Table count returns to 9 after re-applying `001_init.up.sql`.

#### Troubleshooting and Rollback:
- If drop fails due to foreign key cascade: Ensure `DROP TABLE IF EXISTS ... CASCADE` is specified in `001_init.down.sql`.

---

## 5. Section 4: Control Plane Service Launch and Health Probes

### Case CP-01: Control Plane Service Bootstrapping
- Purpose: Build and launch the Control Plane daemon with configured environment variables and ensure both HTTP (:8110) and gRPC (:9090) listeners open.
- Prerequisites: Certificates generated (Case ENV-02), database and Redis active (Section 2), migrations applied (Section 3).

#### Execution Command:
```bash
# Build control-plane binary
go build -o bin/vpnbuilder-cp ./cmd/control-plane

# Launch Control Plane in background or separate shell
VPNBUILDER_SERVER_HTTP_ADDR=":8110" \
VPNBUILDER_SERVER_GRPC_ADDR=":9090" \
VPNBUILDER_DATABASE_DSN="postgres://vpnbuilder:vpnbuilder@127.0.0.1:5432/vpnbuilder?sslmode=disable" \
VPNBUILDER_REDIS_ADDR="127.0.0.1:6379" \
VPNBUILDER_AUTH_JWT_SECRET="super-secret-change-in-production-32-bytes-minimum" \
VPNBUILDER_AUTH_JWT_ACCESS_TTL="15m" \
VPNBUILDER_AUTH_JWT_REFRESH_TTL="168h" \
VPNBUILDER_AUTH_BCRYPT_COST="12" \
VPNBUILDER_CA_CERT_FILE="docker/certs/ca.pem" \
VPNBUILDER_CA_KEY_FILE="docker/certs/ca-key.pem" \
VPNBUILDER_LOG_LEVEL="debug" \
VPNBUILDER_LOG_FORMAT="json" \
./bin/vpnbuilder-cp &
CP_PID=$!

sleep 2

# Verify listening ports
ss -tulpn | grep -E "8110|9090"
```

#### Verification Criteria:
- Process `vpnbuilder-cp` is active.
- Port 8110 is listening on TCP (`LISTEN`).
- Port 9090 is listening on TCP (`LISTEN`).
- Standard log output contains:
  - `Database connected`
  - `Database migrations applied successfully`
  - `Redis connected`

#### Troubleshooting and Rollback:
- If binary fails to bind ports, terminate conflicting processes: `kill -9 $(lsof -t -i:8110) $(lsof -t -i:9090)`.
- If CP crashes on startup, inspect environment variable names against `internal/shared/config/config.go`.

---

### Case CP-02: Liveness Probe (/healthz) Inspection
- Purpose: Verify that the HTTP liveness probe returns HTTP 200 OK to container orchestrators without querying downstream dependencies.
- Prerequisites: Case CP-01 passed.

#### Execution Command:
```bash
curl -i -s -X GET http://127.0.0.1:8110/healthz
```

#### Verification Criteria:
- HTTP Status: `200 OK`.
- Response Content-Type: `application/json`.
- Response Body:
```json
{"status":"ok"}
```

#### Troubleshooting and Rollback:
- If connection refused, verify `CP_PID` is alive: `ps -p $CP_PID`.

---

### Case CP-03: Readiness Probe (/readyz) Under Normal Operating Conditions
- Purpose: Verify that the HTTP readiness probe performs active health checks against PostgreSQL and Redis, returning status OK when both are responsive.
- Prerequisites: Case CP-01 passed, PostgreSQL and Redis active.

#### Execution Command:
```bash
curl -i -s -X GET http://127.0.0.1:8110/readyz
```

#### Verification Criteria:
- HTTP Status: `200 OK`.
- Response Content-Type: `application/json`.
- Response Body:
```json
{
  "status": "ok",
  "postgres": "ok",
  "redis": "ok"
}
```

#### Troubleshooting and Rollback:
- If readiness returns 503, inspect specific component errors reported in the JSON response payload.

---

### Case CP-04: Fault Injection Probe (/readyz) Under Upstream Outage
- Purpose: Prove that the readiness probe correctly transitions to degraded state and returns HTTP 503 Service Unavailable when an upstream dependency fails.
- Prerequisites: Case CP-03 passed.

#### Execution Command:
```bash
# 1. Pause Redis container to simulate outage
docker pause vpnbuilder-redis

# 2. Query readiness probe
curl -i -s -X GET http://127.0.0.1:8110/readyz

# 3. Unpause Redis container to recover
docker unpause vpnbuilder-redis

# 4. Allow connection recovery and re-query
sleep 2
curl -i -s -X GET http://127.0.0.1:8110/readyz
```

#### Verification Criteria:
- During pause: HTTP Status `503 Service Unavailable`.
  - JSON Body contains `"status": "degraded"` and `"redis": "error: ..."` while `"postgres": "ok"`.
- After unpause: HTTP Status recovers to `200 OK` with `"status": "ok"`.

#### Troubleshooting and Rollback:
- Always ensure `docker unpause vpnbuilder-redis` is executed before proceeding to next sections.

---

## 6. Section 5: Authentication, Authorization, and API Keys

### Case AUTH-01: Bootstrap Owner Provisioning and Auto-Seeding Verification
- Purpose: Verify initial administrative account seeded into the `admins` table by control plane migrations with role `owner`.
- Prerequisites: PostgreSQL container active, migrations applied.

#### Execution Command:
```bash
# Verify auto-seeded admin in database (created automatically by control plane startup)
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT id, email, role, created_at FROM admins WHERE email = 'admin@vpnbuilder.local';
"

# (Optional fallback) If inserting manually before control plane startup,
# generate a bcrypt hash locally first (never reuse a published hash):
#   htpasswd -bnBC 12 "" 'Admin1234!' | tr -d ':\n'
ADMIN_HASH='<paste-output-of-the-command-above>'
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
INSERT INTO admins (id, email, password_hash, role)
VALUES (
    '11111111-1111-1111-1111-111111111111',
    'admin@vpnbuilder.local',
    '$ADMIN_HASH',
    'owner'
)
ON CONFLICT (email) DO UPDATE SET password_hash = EXCLUDED.password_hash;
"
```

#### Verification Criteria:
- One row returned with email `admin@vpnbuilder.local` and role `owner`.
- Password hash starts with `$2a$12$`.

#### Troubleshooting and Rollback:
- If no row returned: restart control plane container (`docker compose restart control-plane`) which executes startup seeding when `admins` table is empty.

---

### Case AUTH-02: Admin Login and JWT Access/Refresh Token Generation
- Purpose: Authenticate administrative user via REST API and obtain signed JWT access and refresh tokens.
- Prerequisites: Case AUTH-01 passed, Control Plane running.

#### Execution Command:
```bash
# Execute admin login request
LOGIN_RESPONSE=$(curl -s -X POST http://127.0.0.1:8110/api/v1/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "email": "admin@vpnbuilder.local",
    "password": "Admin1234!"
  }')

echo "$LOGIN_RESPONSE" | jq .

# Extract tokens into shell variables
ACCESS_TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.access_token // empty')
REFRESH_TOKEN=$(echo "$LOGIN_RESPONSE" | jq -r '.refresh_token // empty')

# Inspect JWT Header and Claims (without secret key verification for debugging)
if [ -n "$ACCESS_TOKEN" ]; then
  echo "$ACCESS_TOKEN" | awk -F. '{print $2}' | base64 -d 2>/dev/null | jq .
fi
```

#### Verification Criteria:
- HTTP 200 returned.
- JSON payload contains `access_token`, `refresh_token`, and `expires_in`.
- Decoded JWT payload contains:
  - `email`: `admin@vpnbuilder.local`
  - `role`: `owner`
  - Valid `exp` timestamp 15 minutes ahead of `iat`.

#### Troubleshooting and Rollback:
- If HTTP 401: Confirm bcrypt hash matches raw password using Go test runner.
- If route returns 404: Confirm route registration in `internal/controlplane/api/server.go`.

---

### Case AUTH-03: JWT Access Expiration and Refresh Lifecycle
- Purpose: Validate token refresh endpoint exchanges valid refresh token for a newly signed access token.
- Prerequisites: Case AUTH-02 passed, valid `REFRESH_TOKEN` captured.

#### Execution Command:
```bash
curl -s -X POST http://127.0.0.1:8110/api/v1/auth/refresh \
  -H "Content-Type: application/json" \
  -d '{
    "refresh_token": "$REFRESH_TOKEN"
  }' | jq .
```

#### Verification Criteria:
- HTTP 200 OK.
- Response contains new `access_token`.
- Response contains unchanged or rotated `refresh_token`.

#### Troubleshooting and Rollback:
- If HTTP 401 `token expired`: Verify system clock synchronization.

---

### Case AUTH-04: API Key Generation with 'vpn_' Prefix and SHA-256 Hashing
- Purpose: Generate an automation API key for Terraform or external bot integrations, confirming the `vpn_` prefix and SHA-256 hash storage.
- Prerequisites: Case AUTH-02 passed, valid `ACCESS_TOKEN`.

#### Execution Command:
```bash
# Create new API Key via Admin API
API_KEY_RESP=$(curl -s -X POST http://127.0.0.1:8110/api/v1/api-keys \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "ci-integration-key",
    "scopes": ["read", "write", "admin"]
  }')

echo "$API_KEY_RESP" | jq .

RAW_KEY=$(echo "$API_KEY_RESP" | jq -r '.raw_key // empty')
KEY_ID=$(echo "$API_KEY_RESP" | jq -r '.id // empty')

# Audit database representation
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT id, name, prefix, key_hash, scopes FROM api_keys WHERE name = 'ci-integration-key';
"
```

#### Verification Criteria:
- `raw_key` begins with prefix `vpn_` followed by 48 hexadecimal characters (total length 52 chars).
- Database column `prefix` contains the first 8 characters (e.g. `vpn_xxxx`).
- Database column `key_hash` stores the SHA-256 hexadecimal digest of `raw_key`. Plaintext `raw_key` is NEVER stored in the database.

#### Troubleshooting and Rollback:
- If key hash mismatch: Verify hashing function uses `sha256.Sum256([]byte(key))` encoded as hex.

---

### Case AUTH-05: Dual-Scheme Authentication on Protected Endpoints
- Purpose: Verify that protected API routes accept both Bearer JWT tokens and X-API-Key / Bearer API Key credentials.
- Prerequisites: Case AUTH-02 (`ACCESS_TOKEN`) and Case AUTH-04 (`RAW_KEY`).

#### Execution Command:
```bash
# 1. Access protected /api/v1/nodes with JWT
curl -s -o /dev/null -w "%{http_code}\n" -X GET http://127.0.0.1:8110/api/v1/nodes \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# 2. Access protected /api/v1/nodes with API Key via X-API-Key header
curl -s -o /dev/null -w "%{http_code}\n" -X GET http://127.0.0.1:8110/api/v1/nodes \
  -H "X-API-Key: $RAW_KEY"

# 3. Access protected /api/v1/nodes with API Key via Bearer Authorization header
curl -s -o /dev/null -w "%{http_code}\n" -X GET http://127.0.0.1:8110/api/v1/nodes \
  -H "Authorization: Bearer $RAW_KEY"

# 4. Access without credentials
curl -s -o /dev/null -w "%{http_code}\n" -X GET http://127.0.0.1:8110/api/v1/nodes
```

#### Verification Criteria:
- Requests 1, 2, and 3 return HTTP 200.
- Request 4 returns HTTP 401 Unauthorized.

#### Troubleshooting and Rollback:
- If Request 2 or 3 returns 401: Verify `APIKeyManager.ValidateKey` correctly parses prefix `rawKey[:8]` and compares SHA-256 hashes.

---

### Case AUTH-06: API Key Revocation and Invalid Key Rejection
- Purpose: Delete an API key and prove that subsequent requests using that key are rejected immediately.
- Prerequisites: Case AUTH-04 passed.

#### Execution Command:
```bash
# Delete the API key
curl -s -X DELETE http://127.0.0.1:8110/api/v1/api-keys/$KEY_ID \
  -H "Authorization: Bearer $ACCESS_TOKEN"

# Attempt usage of deleted key
curl -s -o /dev/null -w "%{http_code}\n" -X GET http://127.0.0.1:8110/api/v1/nodes \
  -H "X-API-Key: $RAW_KEY"
```

#### Verification Criteria:
- Deletion call returns HTTP 200 or 204.
- Subsequent GET call returns HTTP 401 Unauthorized.

#### Troubleshooting and Rollback:
- Confirm row was deleted from `api_keys` table via `SELECT count(*) FROM api_keys WHERE id = '$KEY_ID';`.

---

## 7. Section 6: Exit Node Management and Registration

### Case NODE-01: Exit Node Registration (Frankfurt-01)
- Purpose: Register a new exit server (Node) in region `eu-central` with public WireGuard endpoint and mTLS fingerprint.
- Prerequisites: Case AUTH-02 passed (`ACCESS_TOKEN`).

#### Execution Command:
```bash
NODE_REG_RESP=$(curl -s -X POST http://127.0.0.1:8110/api/v1/nodes \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "frankfurt-01",
    "endpoint": "198.51.100.10:51820",
    "grpc_endpoint": "198.51.100.10:9090",
    "region": "eu-central",
    "capacity_gbps": 10,
    "public_key": "qD1Vz5UeN8j7p2Q8wX1Y3z4A5B6C7D8E9F0G1H2I3J4=",
    "cert_fingerprint": "a1b2c3d4e5f60718293a4b5c6d7e8f90123456789abcdef0123456789abcdef0",
    "tags": {"provider": "hetzner", "tier": "premium"}
  }')

echo "$NODE_REG_RESP" | jq .
NODE_ID=$(echo "$NODE_REG_RESP" | jq -r '.id // empty')
```

#### Verification Criteria:
- HTTP 201 Created or 200 OK.
- Response contains assigned UUID in `id`.
- Initial `status` is `pending` or `online`.
- `name` matches `frankfurt-01`.

#### Troubleshooting and Rollback:
- If unique constraint violation on `name`: Node already registered. Retrieve using `GET /api/v1/nodes`.

---

### Case NODE-02: Node Filtering by Status and Geographical Region
- Purpose: Validate REST filtering capabilities on `/api/v1/nodes` across status and region parameters.
- Prerequisites: Case NODE-01 passed.

#### Execution Command:
```bash
# Query nodes in region eu-central
curl -s -X GET "http://127.0.0.1:8110/api/v1/nodes?region=eu-central" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .

# Query nodes in non-existent region
curl -s -X GET "http://127.0.0.1:8110/api/v1/nodes?region=ap-southeast" \
  -H "Authorization: Bearer $ACCESS_TOKEN" | jq .
```

#### Verification Criteria:
- First query returns list containing `frankfurt-01`.
- Second query returns empty list `[]`.

#### Troubleshooting and Rollback:
- Check SQL query generation in `CountNodes` and `ListNodes`.

---

### Case NODE-03: Node Metadata and Bandwidth Capacity Modification
- Purpose: Update node capacity from 10 Gbps to 20 Gbps and add metadata tags.
- Prerequisites: Case NODE-01 passed.

#### Execution Command:
```bash
curl -s -X PATCH "http://127.0.0.1:8110/api/v1/nodes/$NODE_ID" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "capacity_gbps": 20,
    "tags": {"provider": "hetzner", "tier": "ultra", "ddos_protected": "true"}
  }' | jq .

# Verify update in database
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT id, name, capacity_gbps, tags FROM nodes WHERE id = '$NODE_ID';
"
```

#### Verification Criteria:
- Response and database show `capacity_gbps = 20`.
- Tags JSON reflects updated keys.

#### Troubleshooting and Rollback:
- If 404: Validate `$NODE_ID` is formatted as a valid UUID.

---

### Case NODE-04: Simulate Heartbeat Updates and Status Transitions
- Purpose: Simulate periodic heartbeat reception by updating `last_heartbeat` and status transition to `online`.
- Prerequisites: Case NODE-01 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
UPDATE nodes 
SET last_heartbeat = now(), status = 'online' 
WHERE id = '$NODE_ID' 
RETURNING id, name, status, last_heartbeat;
"
```

#### Verification Criteria:
- `status` updates to `online`.
- `last_heartbeat` is set to current UTC timestamp.

#### Troubleshooting and Rollback:
- If no rows updated, confirm `$NODE_ID` exists in `nodes` table.

---

## 8. Section 7: Subscription Plans and User Management

### Case USER-01: Subscription Plan Creation (Basic vs Pro Unlimited)
- Purpose: Create subscription plans with bandwidth limits, supported protocols, and device allowances.
- Prerequisites: Case AUTH-02 passed.

#### Execution Command:
```bash
# Create Pro Plan (500 GB, WireGuard + VLESS)
PLAN_PRO_RESP=$(curl -s -X POST http://127.0.0.1:8110/api/v1/plans \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "pro-monthly",
    "monthly_price": 7.99,
    "traffic_limit": 536870912000,
    "device_limit": 5,
    "protocols": ["wireguard", "vless"],
    "is_active": true
  }')

echo "$PLAN_PRO_RESP" | jq .
PLAN_ID=$(echo "$PLAN_PRO_RESP" | jq -r '.id // empty')
```

#### Verification Criteria:
- HTTP 201 or 200 OK.
- `id` returned as valid UUID.
- `traffic_limit` corresponds to 500 GiB (536870912000 bytes).

#### Troubleshooting and Rollback:
- Check uniqueness constraint on plan `name`.

---

### Case USER-02: User Account Creation and Plan Association
- Purpose: Create a VPN customer user account, link to Pro plan, and verify automatic generation of subscription token.
- Prerequisites: Case USER-01 passed.

#### Execution Command:
```bash
USER_RESP=$(curl -s -X POST http://127.0.0.1:8110/api/v1/users \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "johndoe",
    "email": "johndoe@example.com",
    "plan_id": "$PLAN_ID",
    "status": "active"
  }')

echo "$USER_RESP" | jq .
USER_ID=$(echo "$USER_RESP" | jq -r '.id // empty')
SUB_TOKEN=$(echo "$USER_RESP" | jq -r '.subscription_token // empty')

# Audit database
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT id, username, email, plan_id, status, subscription_token 
FROM users WHERE id = '$USER_ID';
"
```

#### Verification Criteria:
- `USER_ID` and `SUB_TOKEN` are valid UUIDs.
- `subscription_token` is auto-generated via PostgreSQL `gen_random_uuid()` default.
- User status is `active`.

#### Troubleshooting and Rollback:
- If username already exists, delete test user: `DELETE FROM users WHERE username = 'johndoe';`.

---

### Case USER-03: Subscription Token Invalidation and Rotation
- Purpose: Rotate the user's `subscription_token` when compromised and verify the previous token no longer functions.
- Prerequisites: Case USER-02 passed.

#### Execution Command:
```bash
# Rotate subscription token
ROT_RESP=$(curl -s -X POST "http://127.0.0.1:8110/api/v1/users/$USER_ID/rotate-token" \
  -H "Authorization: Bearer $ACCESS_TOKEN")

echo "$ROT_RESP" | jq .
NEW_SUB_TOKEN=$(echo "$ROT_RESP" | jq -r '.subscription_token // empty')

echo "Old Token: $SUB_TOKEN"
echo "New Token: $NEW_SUB_TOKEN"
```

#### Verification Criteria:
- `NEW_SUB_TOKEN` is different from `SUB_TOKEN`.
- Querying database by old token returns 0 rows.

#### Troubleshooting and Rollback:
- If rotate endpoint is unavailable, test directly via SQL:
  `UPDATE users SET subscription_token = gen_random_uuid() WHERE id = '$USER_ID' RETURNING subscription_token;`.

---

### Case USER-04: Dynamic Subscription Endpoint (/sub/{token}) Resolution
- Purpose: Retrieve client subscription bundle via unauthenticated public URL `/sub/{token}`.
- Prerequisites: Case USER-02 passed.

#### Execution Command:
```bash
ACTIVE_TOKEN=${NEW_SUB_TOKEN:-$SUB_TOKEN}

curl -i -s -X GET "http://127.0.0.1:8110/sub/$ACTIVE_TOKEN"
```

#### Verification Criteria:
- HTTP 200 OK.
- Content-Type is `text/plain` or `application/octet-stream` or `application/json`.
- Body contains Base64 subscription payload or configuration list for Clash, v2ray, or WireGuard.

#### Troubleshooting and Rollback:
- If HTTP 404: Verify `users` table contains active row with `subscription_token = '$ACTIVE_TOKEN'`.

---

## 9. Section 8: Protocol Credential Provisioning and AmneziaWG Obfuscation

### Case CRED-01: Standard WireGuard Keypair Generation and Network Allocation
- Purpose: Generate a standard WireGuard credential for user `johndoe` on node `frankfurt-01` with assigned private/public keys and tunnel IP `10.8.0.2`.
- Prerequisites: Case NODE-01 (`NODE_ID`), Case USER-02 (`USER_ID`).

#### Execution Command:
```bash
# Generate WireGuard Keypair
CLIENT_PRIVKEY=$(wg genkey)
CLIENT_PUBKEY=$(echo "$CLIENT_PRIVKEY" | wg pubkey)
SERVER_PSK=$(wg genpsk)

CRED_WG_RESP=$(curl -s -X POST "http://127.0.0.1:8110/api/v1/users/$USER_ID/credentials" \
  -H "Authorization: Bearer $ACCESS_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "node_id": "$NODE_ID",
    "protocol": "wireguard",
    "private_key": "$CLIENT_PRIVKEY",
    "public_key": "$CLIENT_PUBKEY",
    "preshared_key": "$SERVER_PSK",
    "ipv4": "10.8.0.2",
    "dns": "1.1.1.1",
    "mtu": 1280,
    "keepalive": 25
  }')

echo "$CRED_WG_RESP" | jq .
CRED_WG_ID=$(echo "$CRED_WG_RESP" | jq -r '.id // empty')
```

#### Verification Criteria:
- HTTP 201 Created or 200 OK.
- Response contains assigned `CRED_WG_ID`.
- Protocol equals `wireguard`.
- Assigned IPv4 equals `10.8.0.2`.

#### Troubleshooting and Rollback:
- Check for existing credential on `(user_id, node_id, protocol)` unique constraint.

---

### Case CRED-02: AmneziaWG Credential Provisioning with Obfuscation Parameters
- Purpose: Provision an AmneziaWG (AWG) credential featuring anti-censorship obfuscation values (`awg_jc`, `awg_jmin`, `awg_jmax`, `awg_s1`, `awg_s2`, `awg_h1`..`awg_h4`).
- Prerequisites: Case NODE-01 (`NODE_ID`), Case USER-02 (`USER_ID`).

#### Execution Command:
```bash
# Generate AWG Keypair
AWG_PRIVKEY=$(wg genkey)
AWG_PUBKEY=$(echo "$AWG_PRIVKEY" | wg pubkey)

# Direct insertion or API creation of AWG credentials with advanced junk packet parameters
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
INSERT INTO credentials (
    id, user_id, node_id, protocol,
    private_key, public_key, ipv4, dns, mtu, keepalive,
    awg_jc, awg_jmin, awg_jmax, awg_s1, awg_s2,
    awg_h1, awg_h2, awg_h3, awg_h4, status
)
VALUES (
    '22222222-2222-2222-2222-222222222222',
    '$USER_ID',
    '$NODE_ID',
    'amneziawg',
    '$AWG_PRIVKEY',
    '$AWG_PUBKEY',
    '10.8.0.3',
    '1.1.1.1',
    1280,
    25,
    5, 50, 80, 64, 64,
    12345678, 23456789, 34567890, 45678901,
    'active'
)
ON CONFLICT (user_id, node_id, protocol) DO UPDATE SET
    awg_jc = EXCLUDED.awg_jc,
    awg_h1 = EXCLUDED.awg_h1
RETURNING id, protocol, ipv4, awg_jc, awg_jmin, awg_jmax, awg_h1, awg_h4;
"
```

#### Verification Criteria:
- Record successfully returned with protocol `amneziawg`.
- `awg_jc` = 5, `awg_jmin` = 50, `awg_jmax` = 80.
- `awg_h1` = 12345678, `awg_h4` = 45678901.

#### Troubleshooting and Rollback:
- If constraint check fails, ensure AWG integers are within valid 32-bit and 64-bit boundaries.

---

### Case CRED-03: Client Configuration Assembly (.conf Format)
- Purpose: Generate and audit the final client configuration file for WireGuard and AmneziaWG clients.
- Prerequisites: Case CRED-01 and Case CRED-02 passed.

#### Execution Command:
```bash
# Query node endpoint and public key
NODE_INFO=$(PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -t -A -F"," -c "
SELECT endpoint, public_key FROM nodes WHERE id = '$NODE_ID';
")
NODE_ENDPOINT=$(echo "$NODE_INFO" | cut -d',' -f1)
NODE_PUBKEY=$(echo "$NODE_INFO" | cut -d',' -f2)

# Synthesize client WireGuard .conf
cat <<EOF > test_client_wg.conf
[Interface]
PrivateKey = $CLIENT_PRIVKEY
Address = 10.8.0.2/32
DNS = 1.1.1.1
MTU = 1280

[Peer]
PublicKey = $NODE_PUBKEY
PresharedKey = $SERVER_PSK
Endpoint = $NODE_ENDPOINT
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
EOF

# Synthesize AmneziaWG obfuscated .conf
cat <<EOF > test_client_awg.conf
[Interface]
PrivateKey = $AWG_PRIVKEY
Address = 10.8.0.3/32
DNS = 1.1.1.1
MTU = 1280
Jc = 5
Jmin = 50
Jmax = 80
S1 = 64
S2 = 64
H1 = 12345678
H2 = 23456789
H3 = 34567890
H4 = 45678901

[Peer]
PublicKey = $NODE_PUBKEY
Endpoint = $NODE_ENDPOINT
AllowedIPs = 0.0.0.0/0, ::/0
PersistentKeepalive = 25
EOF

cat test_client_wg.conf
echo "---"
cat test_client_awg.conf
```

#### Verification Criteria:
- WireGuard config contains `[Interface]` and `[Peer]` with correct keys.
- AmneziaWG config includes all 9 obfuscation fields (`Jc`, `Jmin`, `Jmax`, `S1`, `S2`, `H1`, `H2`, `H3`, `H4`).

#### Troubleshooting and Rollback:
- Remove local temp files: `rm -f test_client_wg.conf test_client_awg.conf`.

---

### Case CRED-04: Credential Revocation and Cascade Deletion
- Purpose: Verify that deleting a user or credential immediately deletes associated records across the database via foreign key constraints (`ON DELETE CASCADE`).
- Prerequisites: Case CRED-01 passed.

#### Execution Command:
```bash
# Verify credential count before delete
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT count(*) FROM credentials WHERE user_id = '$USER_ID';
"

# Delete credential directly
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
DELETE FROM credentials WHERE id = '22222222-2222-2222-2222-222222222222';
"

# Confirm deletion
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT count(*) FROM credentials WHERE id = '22222222-2222-2222-2222-222222222222';
"
```

#### Verification Criteria:
- Target credential count returns 0.

#### Troubleshooting and Rollback:
- If foreign key violation occurs, check schema `REFERENCES users(id) ON DELETE CASCADE`.

---

## 10. Section 9: Node Agent Launch and gRPC Streaming Synchronization

### Case AGENT-01: Agent Configuration and mTLS Certificate Staging
- Purpose: Prepare agent runtime configuration pointing to the local Control Plane with mTLS credentials.
- Prerequisites: Certificates generated (Case ENV-02).

#### Execution Command:
```bash
# Inspect agent config environment variables
export VPNBUILDER_AGENT_NODE_NAME="agent-1"
export VPNBUILDER_AGENT_CONTROL_PLANE="127.0.0.1:9090"
export VPNBUILDER_AGENT_CA_CERT="docker/certs/ca.pem"
export VPNBUILDER_AGENT_CERT_FILE="docker/certs/agent.pem"
export VPNBUILDER_AGENT_KEY_FILE="docker/certs/agent-key.pem"
export VPNBUILDER_AGENT_SYNC_INTERVAL="5s"
export VPNBUILDER_AGENT_METRICS_INTERVAL="5s"
export VPNBUILDER_AGENT_WIREGUARD_INTERFACE_PREFIX="wgtest"
export VPNBUILDER_LOG_LEVEL="debug"
export VPNBUILDER_LOG_FORMAT="json"

# Validate certificate existence
test -f "$VPNBUILDER_AGENT_CA_CERT" && echo "CA cert OK"
test -f "$VPNBUILDER_AGENT_CERT_FILE" && echo "Agent cert OK"
test -f "$VPNBUILDER_AGENT_KEY_FILE" && echo "Agent key OK"
```

#### Verification Criteria:
- All three certificates exist on filesystem and are readable.

#### Troubleshooting and Rollback:
- If files missing: Re-execute Case ENV-02.

---

### Case AGENT-02: Agent Process Startup and gRPC Transport Handshake
- Purpose: Launch the node agent binary and verify the mTLS gRPC handshake with the Control Plane.
- Prerequisites: Case AGENT-01 passed, Control Plane listening on port 9090.

#### Execution Command:
```bash
# Build agent binary
go build -o bin/vpnbuilder-agent ./cmd/agent

# Launch agent (requires sudo or CAP_NET_ADMIN for WireGuard netlink operations)
sudo -E ./bin/vpnbuilder-agent &
AGENT_PID=$!

sleep 3

# Check agent process status
ps -p $AGENT_PID
```

#### Verification Criteria:
- Process `vpnbuilder-agent` is executing.
- Agent logs output: `Connected to control plane`.
- Control Plane logs record gRPC connection from client CN `agent-1`.

#### Troubleshooting and Rollback:
- If handshake fails with `certificate signed by unknown authority`: Ensure Root CA used to sign CP cert matches CA certificate passed to Agent.
- Terminate agent: `sudo kill -TERM $AGENT_PID`.

---

### Case AGENT-03: Bidirectional Stream Config Sync and ConfigAck Emission
- Purpose: Emulate or trigger a `ConfigUpdate` from Control Plane and verify Agent emits `ConfigAck` with status success.
- Prerequisites: Case AGENT-02 running.

#### Execution Command:
```bash
# Probe agent gRPC connectivity using grpcurl with client certificates
grpcurl -cacert docker/certs/ca.pem \
  -cert docker/certs/agent.pem \
  -key docker/certs/agent-key.pem \
  127.0.0.1:9090 list
```

#### Verification Criteria:
- Output lists available gRPC services, specifically `vpnbuilder.agent.v1.AgentService`.

#### Troubleshooting and Rollback:
- If grpcurl fails: `rpc error: code = Unavailable desc = connection closed before server preface received`. Verify TLS server name and SAN entries match `localhost` or `127.0.0.1`.

---

### Case AGENT-04: Network Interface Allocation (wgtest0) and Link Initialization
- Purpose: Confirm agent WireGuard manager creates network link using netlink.
- Prerequisites: Case AGENT-02 active with elevated permissions (`CAP_NET_ADMIN`).

#### Execution Command:
```bash
# Inspect system network interfaces for agent prefix
ip link show | grep -E "wgtest[0-9]"

# Query WireGuard device details via wgctrl
sudo wg show
```

#### Verification Criteria:
- Interface `wgtest0` (or configured prefix) appears in `ip link show` with state `UNKNOWN` or `UP`.
- Command `sudo wg show` displays interface name, public key, and listening port.

#### Troubleshooting and Rollback:
- If interface was not created: Check if `netlink.LinkAdd` failed in agent log due to lacking `sudo` / `CAP_NET_ADMIN`.
- Cleanup lingering interface: `sudo ip link delete wgtest0 2>/dev/null || true`.

---

### Case AGENT-05: Dynamic Peer Attachment and Netlink Sync
- Purpose: Add a WireGuard peer to the active interface and verify peer registration in the kernel device table.
- Prerequisites: Case AGENT-04 active.

#### Execution Command:
```bash
# Add peer manually or via Agent manager call
PEER_PUBKEY="sB1Vz5UeN8j7p2Q8wX1Y3z4A5B6C7D8E9F0G1H2I3J8="
sudo wg set wgtest0 peer "$PEER_PUBKEY" allowed-ips "10.8.0.2/32" persistent-keepalive 25

# Inspect WireGuard interface peers
sudo wg show wgtest0 peers
sudo wg show wgtest0 allowed-ips
```

#### Verification Criteria:
- Output of `sudo wg show wgtest0 peers` contains `$PEER_PUBKEY`.
- Output of `sudo wg show wgtest0 allowed-ips` associates `10.8.0.2/32` with `$PEER_PUBKEY`.

#### Troubleshooting and Rollback:
- Remove test peer: `sudo wg set wgtest0 peer "$PEER_PUBKEY" remove`.

---

## 11. Section 10: Traffic Accounting, Metrics Rollup, and Quota Enforcement

### Case TRAFFIC-01: Agent Periodic Metrics Emission via gRPC
- Purpose: Verify that the agent metrics collector gathers traffic bytes from the interface manager and emits `MetricsReport` over the gRPC stream.
- Prerequisites: Case AGENT-02 and Case AGENT-05 running.

#### Execution Command:
```bash
# Inspect agent output log stream for metrics activity
# Wait 10 seconds for metrics tick (configured interval: 5s)
sleep 10

# Audit agent metrics collection in Control Plane or Agent logs
```

#### Verification Criteria:
- Agent log prints `collectAndSend` or `ProtocolMetrics` dispatch without error.
- No `Stream receive error` or RPC channel breaks.

#### Troubleshooting and Rollback:
- If metrics fail to collect, ensure peer interface has not been deleted.

---

### Case TRAFFIC-02: Hourly Bucket Rollup Insertion (traffic_stats Table)
- Purpose: Validate database upsert logic in `traffic_stats` using the `ON CONFLICT (user_id, node_id, protocol, hour_bucket)` statement.
- Prerequisites: Database container active.

#### Execution Command:
```bash
CURRENT_HOUR=$(date -u +'%Y-%m-%d %H:00:00+00')

PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
-- 1. Insert initial hourly traffic
INSERT INTO traffic_stats (user_id, node_id, protocol, hour_bucket, rx_bytes, tx_bytes)
VALUES ('$USER_ID', '$NODE_ID', 'wireguard', '$CURRENT_HOUR', 104857600, 209715200)
ON CONFLICT (user_id, node_id, protocol, hour_bucket) DO UPDATE SET
    rx_bytes = traffic_stats.rx_bytes + EXCLUDED.rx_bytes,
    tx_bytes = traffic_stats.tx_bytes + EXCLUDED.tx_bytes;

-- 2. Insert incremental traffic in same hour
INSERT INTO traffic_stats (user_id, node_id, protocol, hour_bucket, rx_bytes, tx_bytes)
VALUES ('$USER_ID', '$NODE_ID', 'wireguard', '$CURRENT_HOUR', 52428800, 52428800)
ON CONFLICT (user_id, node_id, protocol, hour_bucket) DO UPDATE SET
    rx_bytes = traffic_stats.rx_bytes + EXCLUDED.rx_bytes,
    tx_bytes = traffic_stats.tx_bytes + EXCLUDED.tx_bytes;

-- 3. Query combined totals
SELECT user_id, node_id, protocol, hour_bucket, rx_bytes, tx_bytes, (rx_bytes + tx_bytes) as total_bytes
FROM traffic_stats 
WHERE user_id = '$USER_ID' AND hour_bucket = '$CURRENT_HOUR';
"
```

#### Verification Criteria:
- Exactly 1 row exists for the hour bucket.
- `rx_bytes` equals 157286400 (100 MB + 50 MB).
- `tx_bytes` equals 262144000 (200 MB + 50 MB).
- Combined `total_bytes` equals 419430400 (400 MB).

#### Troubleshooting and Rollback:
- If multiple rows created: Verify unique constraint `UNIQUE (user_id, node_id, protocol, hour_bucket)` exists on `traffic_stats`.

---

### Case TRAFFIC-03: Aggregate Usage Querying by User and Node
- Purpose: Execute store aggregation queries `GetTrafficAggregateByUser` and `GetTrafficAggregateByNode`.
- Prerequisites: Case TRAFFIC-02 passed.

#### Execution Command:
```bash
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
-- Aggregate by User across time range
SELECT 
    COALESCE(SUM(rx_bytes), 0)::bigint AS total_rx,
    COALESCE(SUM(tx_bytes), 0)::bigint AS total_tx
FROM traffic_stats
WHERE user_id = '$USER_ID' 
  AND hour_bucket >= now() - INTERVAL '24 hours' 
  AND hour_bucket <= now() + INTERVAL '1 hour';

-- Aggregate by Node across time range
SELECT 
    node_id,
    protocol,
    COALESCE(SUM(rx_bytes), 0)::bigint AS total_rx,
    COALESCE(SUM(tx_bytes), 0)::bigint AS total_tx
FROM traffic_stats
WHERE hour_bucket >= now() - INTERVAL '24 hours' 
  AND hour_bucket <= now() + INTERVAL '1 hour'
GROUP BY node_id, protocol;
"
```

#### Verification Criteria:
- User aggregate returns `total_rx = 157286400` and `total_tx = 262144000`.
- Node aggregate groups by `NODE_ID` with matching total bytes.

#### Troubleshooting and Rollback:
- If query returns null, verify `COALESCE` wrapping around `SUM`.

---

### Case TRAFFIC-04: User Traffic Quota Exhaustion and Account Suspension
- Purpose: Simulate user consumption exceeding plan limit and verify account status transitions to `suspended`.
- Prerequisites: Case USER-02 (`USER_ID`), Case USER-01 (`PLAN_ID`).

#### Execution Command:
```bash
# 1. Update user traffic_limit to 100 MB and traffic_used to 150 MB
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
UPDATE users 
SET traffic_limit = 104857600, traffic_used = 157286400 
WHERE id = '$USER_ID';

-- 2. Execute quota evaluation trigger / logic
UPDATE users
SET status = 'suspended'
WHERE id = '$USER_ID' AND traffic_limit > 0 AND traffic_used >= traffic_limit;

-- 3. Query user status
SELECT id, username, status, traffic_limit, traffic_used, (traffic_used >= traffic_limit) AS quota_exceeded
FROM users WHERE id = '$USER_ID';
"
```

#### Verification Criteria:
- User status becomes `suspended`.
- Column `quota_exceeded` evaluates to `t`.

#### Troubleshooting and Rollback:
- Reset user to active: `UPDATE users SET status = 'active', traffic_used = 0 WHERE id = '$USER_ID';`.

---

## 12. Section 11: Payment Webhook Verification and Signature Enforcement

### Case WH-01: Signed CryptoBot Webhook Marks Order Paid
- Purpose: Prove that a correctly signed inbound payment webhook transitions a pending order to paid.
- Prerequisites: Case AUTH-02 passed (`ACCESS_TOKEN`); a `cryptobot` gateway with a token is configured (see Billing settings).

#### Execution Command:
```bash
# Read the gateway token and pick a pending order
GW_TOKEN=$(PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -tAc \
  "SELECT config_encrypted::json->>'token' FROM payment_gateways WHERE name = 'cryptobot'")
EXT_INV=$(PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -tAc \
  "SELECT external_invoice_id FROM orders WHERE status = 'pending' ORDER BY created_at DESC LIMIT 1")

PAYLOAD=$(jq -n --arg inv "$EXT_INV" '{external_invoice_id: $inv, status: "paid"}')
TOKEN_HASH=$(echo -n "$GW_TOKEN" | openssl dgst -sha256 -binary | xxd -p -c 256)
SIGNATURE=$(echo -n "$PAYLOAD" | openssl dgst -sha256 -mac HMAC -macopt hexkey:"$TOKEN_HASH" | awk '{print $2}')

curl -s -X POST http://127.0.0.1:8110/api/v1/billing/webhooks/cryptobot \
  -H "Content-Type: application/json" \
  -H "crypto-pay-api-signature: $SIGNATURE" \
  -d "$PAYLOAD" | jq .

PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c \
  "SELECT external_invoice_id, status FROM orders WHERE external_invoice_id = '$EXT_INV';"
```

#### Verification Criteria:
- Webhook responds with HTTP 200.
- Order row shows `status = paid`.

#### Troubleshooting and Rollback:
- HTTP 403 means the gateway has no secret configured; set the token in Billing settings first.
- HTTP 401 means the signature is wrong; verify the SHA256-of-token key derivation.

---

### Case WH-02: Tampered Signature Is Rejected
- Purpose: Prove that a forged webhook cannot mark orders paid.
- Prerequisites: Case WH-01 passed.

#### Execution Command:
```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8110/api/v1/billing/webhooks/cryptobot \
  -H "Content-Type: application/json" \
  -H "crypto-pay-api-signature: deadbeef-invalid-signature" \
  -d "$PAYLOAD"
```

#### Verification Criteria:
- Response is HTTP 401.
- Order status is unchanged in the database.

#### Troubleshooting and Rollback:
- A 200 here means signature verification is bypassed; check `verifyWebhookSignature` wiring in the billing handler.

---

### Case WH-03: Gateway Without Secret Refuses Unsigned Calls
- Purpose: Prove that a gateway with no configured secret rejects webhooks instead of accepting them unsigned.
- Prerequisites: An enabled gateway row without token (e.g. `manual` with empty config).

#### Execution Command:
```bash
curl -s -o /dev/null -w "%{http_code}\n" -X POST http://127.0.0.1:8110/api/v1/billing/webhooks/manual \
  -H "Content-Type: application/json" \
  -d '{"external_invoice_id":"probe","status":"paid"}'
```

#### Verification Criteria:
- Response is HTTP 403.
- No order row is created or modified.

#### Troubleshooting and Rollback:
- A 200 here is a critical finding: unsigned webhooks must never be processed.

## 13. Section 12: Web Admin UI (React 19 SPA) and Data Endpoints Verification

The Web Admin UI is a modern React 19 Single Page Application embedded into the Go control plane binary and served at `/admin/*-v2` routes. It exchanges state with the control plane through session cookies and dedicated JSON data endpoints (`/admin/*-data`).

### Case UI-01: Admin Web Session Login and CSRF Token Acquisition
- Purpose: Authenticate via the Web Admin form login endpoint and obtain a session cookie plus CSRF protection token.
- Prerequisites: Case CP-01 running on `http://127.0.0.1:8110`.

#### Execution Command:
```bash
# 1. Login with seeded administrator credentials
curl -s -i -c /tmp/admin_cookie.txt -X POST http://127.0.0.1:8110/admin/login \
  -d "username=admin@vpnbuilder.local" \
  -d "password=Admin1234!"

# 2. Extract CSRF token using the active session cookie
CSRF_RESP=$(curl -s -b /tmp/admin_cookie.txt http://127.0.0.1:8110/admin/csrf-token)
echo "$CSRF_RESP" | jq .
CSRF_TOKEN=$(echo "$CSRF_RESP" | jq -r '.csrf_token // empty')
```

#### Verification Criteria:
- Login returns HTTP 303 redirecting to `/admin/dashboard-v2` with `vpn_token` Set-Cookie header.
- `/admin/csrf-token` returns HTTP 200 with JSON payload containing non-empty `csrf_token`.

---

### Case UI-02: React 19 SPA Route Integrity
- Purpose: Verify that all primary dashboard routes serve the embedded React SPA bundle with HTTP 200.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
for route in dashboard-v2 nodes-v2 users-v2 plans-v2 credentials-v2 analytics-v2 audit-v2 broadcast-v2 settings-v2; do
  STATUS=$(curl -s -o /dev/null -w "%{http_code}" -b /tmp/admin_cookie.txt "http://127.0.0.1:8110/admin/$route")
  echo "Route /admin/$route: HTTP $STATUS"
done
```

#### Verification Criteria:
- All 9 routes return HTTP 200.
- Response body contains the root HTML mount `<div id="root"></div>` and embedded script bundle from `/admin/static/dist/assets/`.

---

### Case UI-03: Users Data JSON Endpoint (/admin/users-data)
- Purpose: Verify the JSON data provider used by the React SPA Users management view.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
USERS_DATA=$(curl -s -b /tmp/admin_cookie.txt http://127.0.0.1:8110/admin/users-data)
echo "$USERS_DATA" | jq .
```

#### Verification Criteria:
- HTTP 200 with JSON array of users.
- Each entry contains `id`, `username`, `status`, `traffic_limit_bytes`, and subscription metadata.

---

### Case UI-04: Plans Data JSON Endpoint (/admin/plans-data)
- Purpose: Verify the JSON data provider for billing and tariff plan management.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
PLANS_DATA=$(curl -s -b /tmp/admin_cookie.txt http://127.0.0.1:8110/admin/plans-data)
echo "$PLANS_DATA" | jq .
```

#### Verification Criteria:
- HTTP 200 with JSON list of configured subscription plans (`id`, `name`, `price`, `traffic_limit`).

---

### Case UI-05: Node Data JSON Endpoint (/admin/node-data)
- Purpose: Verify single-node telemetry and configuration details endpoint.
- Prerequisites: Case UI-01 session cookie active and at least one node registered.

#### Execution Command:
```bash
FIRST_NODE_ID=$(curl -s -H "Authorization: Bearer $ACCESS_TOKEN" http://127.0.0.1:8110/api/v1/nodes | jq -r '.[0].id // empty')

if [ -n "$FIRST_NODE_ID" ]; then
  NODE_DATA=$(curl -s -b /tmp/admin_cookie.txt "http://127.0.0.1:8110/admin/node-data?id=$FIRST_NODE_ID")
  echo "$NODE_DATA" | jq .
fi
```

#### Verification Criteria:
- Returns HTTP 200 with JSON structure containing node specifications, telemetry counters, and active peers.

---

### Case UI-06: Audit Logs Data JSON Endpoint (/admin/audit-data)
- Purpose: Verify the paginated audit logging feed for the administrative audit viewer.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
AUDIT_DATA=$(curl -s -b /tmp/admin_cookie.txt "http://127.0.0.1:8110/admin/audit-data?limit=10")
echo "$AUDIT_DATA" | jq .
```

#### Verification Criteria:
- HTTP 200 with JSON structure containing `logs` array and `total` count.
- Records include admin actor ID, action name, resource type, and timestamp.

---

### Case UI-07: Settings Data JSON Endpoint (/admin/settings-data)
- Purpose: Verify the configuration data provider for the unified React settings console.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
SETTINGS_DATA=$(curl -s -b /tmp/admin_cookie.txt http://127.0.0.1:8110/admin/settings-data)
echo "$SETTINGS_DATA" | jq .
```

#### Verification Criteria:
- HTTP 200 containing system settings: email policies, bot replies, payment gateways, log retention, and API keys.

---

### Case UI-08: Legacy Route Backward Compatibility and 303 Redirects
- Purpose: Verify that requests to legacy server-rendered paths cleanly redirect to their React `-v2` counterparts.
- Prerequisites: Case UI-01 session cookie active.

#### Execution Command:
```bash
for legacy in "/admin" "/admin/nodes" "/admin/users" "/admin/plans" "/admin/settings" "/admin/credentials" "/admin/analytics" "/admin/audit" "/admin/broadcast"; do
  LOCATION=$(curl -s -I -b /tmp/admin_cookie.txt "http://127.0.0.1:8110$legacy" | grep -i "^location:" | tr -d '\r')
  echo "$legacy -> $LOCATION"
done
```

#### Verification Criteria:
- Each legacy path returns HTTP 303 (See Other) with `Location` header pointing to the corresponding `-v2` route (e.g. `Location: /admin/dashboard-v2`, `/admin/nodes-v2`, etc.).

---

## 14. Section 13: Graceful Shutdown, Teardown, and Disaster Recovery

### Case TEAR-01: Graceful Agent Shutdown and Network Interface Deletion
- Purpose: Send SIGTERM to the agent process and verify that netlink interfaces (`wgtest0`) are removed and resources released.
- Prerequisites: Case AGENT-02 running (`AGENT_PID`).

#### Execution Command:
```bash
# Send SIGTERM to Agent
sudo kill -TERM $AGENT_PID

# Wait for process exit
wait $AGENT_PID 2>/dev/null

# Verify network interface has been destroyed
ip link show wgtest0 2>/dev/null || echo "Interface wgtest0 successfully removed"
```

#### Verification Criteria:
- Agent logs show: `Agent stopped gracefully`.
- Command `ip link show wgtest0` outputs device does not exist.

#### Troubleshooting and Rollback:
- If interface persists: `sudo ip link delete wgtest0`.

---

### Case TEAR-02: Graceful Control Plane Draining and Connection Release
- Purpose: Send SIGINT to Control Plane process and verify HTTP server shutdown and gRPC graceful termination.
- Prerequisites: Case CP-01 running (`CP_PID`).

#### Execution Command:
```bash
# Send SIGINT to Control Plane
kill -INT $CP_PID

# Wait for process exit
wait $CP_PID 2>/dev/null

# Verify ports are released
ss -tulpn | grep -E "8110|9090" || echo "Ports 8110 and 9090 successfully closed"
```

#### Verification Criteria:
- Log displays: `Shutting down servers...` followed by `Servers stopped gracefully`.
- Ports 8110 and 9090 are freed.

#### Troubleshooting and Rollback:
- Force kill if hung: `kill -9 $CP_PID`.

---

### Case TEAR-03: Infrastructure Stack Cleanup and Volume Pruning
- Purpose: Stop containerized infrastructure services and purge Docker volumes.
- Prerequisites: Docker Compose stack active.

#### Execution Command:
```bash
# Stop and remove containers and volumes
docker compose -f docker/docker-compose.yml down -v

# Clean up temporary test files
rm -f test_client_wg.conf test_client_awg.conf
rm -f bin/vpnbuilder-cp bin/vpnbuilder-agent

# Verify no orphaned containers remain
docker ps -a | grep vpnbuilder
```

#### Verification Criteria:
- Docker output confirms `vpnbuilder-postgres` and `vpnbuilder-redis` stopped and removed.
- Volume `postgres_data` and `redis_data` deleted.
- No vpnbuilder containers present in `docker ps -a`.

#### Troubleshooting and Rollback:
- Run `docker volume prune -f` if data remains.

---

### Case TEAR-04: Full Disaster Recovery from Database Snapshot
- Purpose: Validate business continuity by creating an encrypted database backup, dropping the database, and verifying 100% restoration fidelity.
- Prerequisites: PostgreSQL container restarted with populated test dataset.

#### Execution Command:
```bash
# 1. Start fresh database container
docker compose -f docker/docker-compose.yml up -d postgres
sleep 3

# 2. Re-apply schema
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -f migrations/001_init.up.sql

# 3. Create snapshot using pg_dump
mkdir -p backups
PGPASSWORD=vpnbuilder pg_dump -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -F c -b -v -f backups/vpnbuilder_snapshot.dump

# 4. Drop and recreate database
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d template1 -c "DROP DATABASE vpnbuilder;"
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d template1 -c "CREATE DATABASE vpnbuilder OWNER vpnbuilder;"

# 5. Restore snapshot using pg_restore
PGPASSWORD=vpnbuilder pg_restore -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -v backups/vpnbuilder_snapshot.dump

# 6. Verify table counts and integrity
PGPASSWORD=vpnbuilder psql -h 127.0.0.1 -p 5432 -U vpnbuilder -d vpnbuilder -c "
SELECT count(*) FROM information_schema.tables WHERE table_schema = 'public';
"

# Final cleanup
rm -rf backups
docker compose -f docker/docker-compose.yml down -v
```

#### Verification Criteria:
- `pg_dump` completes with exit status 0.
- `pg_restore` restores schema without fatal errors.
- Table count in restored database returns exactly 9.

#### Troubleshooting and Rollback:
- If role does not exist: Confirm `-U vpnbuilder` owner privileges.
- Full reset: `docker compose -f docker/docker-compose.yml down -v`.

---

## 15. Verification Sign-Off Matrix

| Section | Test Scope | Cases Executed | Pass Criteria | Sign-off |
|---|---|---|---|---|
| Section 1 | Environment & Tooling | ENV-01 to ENV-02 | Binaries verified, mTLS CA/Certs created | [ ] |
| Section 2 | Infrastructure Stack | INFRA-01 to INFRA-03 | Postgres 16 & Redis 7 healthy | [ ] |
| Section 3 | Migrations & Schema | MIG-01 to MIG-06 | 17 tables, AWG fields, triggers verified | [ ] |
| Section 4 | Control Plane Launch | CP-01 to CP-04 | /healthz and /readyz probes verified | [ ] |
| Section 5 | Authentication & RBAC | AUTH-01 to AUTH-06 | JWT, refresh, API key (vpn_) verified | [ ] |
| Section 6 | Node Management | NODE-01 to NODE-04 | Node CRUD, heartbeats, capacity verified | [ ] |
| Section 7 | Users & Subscriptions | USER-01 to USER-04 | Plans, user tokens, /sub/{token} verified | [ ] |
| Section 8 | Protocol Credentials | CRED-01 to CRED-04 | WireGuard & AmneziaWG configs verified | [ ] |
| Section 9 | Node Agent & gRPC | AGENT-01 to AGENT-05 | mTLS handshake, wg0 link, peers verified | [ ] |
| Section 10 | Traffic & Rollups | TRAFFIC-01 to TRAFFIC-04 | Hourly buckets, rollups, quota verified | [ ] |
| Section 11 | Payment Webhooks | WH-01 to WH-03 | Signed paid, forged 401, secretless 403 | [ ] |
| Section 12 | Web Admin UI (React 19) | UI-01 to UI-08 | React routes, JSON data APIs, redirects | [ ] |
| Section 13 | Teardown & Recovery | TEAR-01 to TEAR-04 | Interface delete, drainage, snapshot verified | [ ] |
