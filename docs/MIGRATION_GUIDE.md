# Migration Guide: Migrating to Simple-VPN-Builder

This document provides step-by-step instructions to migrate from legacy or monolithic VPN management panels (3X-UI, Marzban) and standalone WireGuard servers to Simple-VPN-Builder.

---

## 1. Migrating from 3X-UI

### 1.1 Architectural Differences
- **3X-UI**: Single-server monolith. UI, database (SQLite), and Xray-core run in one container or machine. Multi-node orchestration requires multiple standalone panels.
- **Simple-VPN-Builder**: Central Control Plane orchestrates unlimited remote Node Agents via mTLS gRPC streaming.

### 1.2 Database Extraction
3X-UI stores inbound settings and client credentials in SQLite (`/etc/x-ui/x-ui.db`).

Extract client UUIDs and emails:
```bash
sqlite3 /etc/x-ui/x-ui.db "SELECT id, remark, port, settings FROM inbounds;" > inbounds.txt
```

Extract VLESS clients JSON array:
```bash
sqlite3 /etc/x-ui/x-ui.db "SELECT json_extract(settings, '$.clients') FROM inbounds WHERE protocol='vless';" > clients.json
```

### 1.3 Mapping to Simple-VPN-Builder REST API
Use the Simple-VPN-Builder REST API to recreate users and VLESS Reality credentials:

```bash
# 1. Create User in Simple-VPN-Builder
USER_RESP=$(curl -s -X POST http://localhost:8110/api/v1/users \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "username": "migrated_client_01",
    "email": "client01@example.com",
    "plan_id": "'"$PLAN_ID"'"
  }')
USER_ID=$(echo "$USER_RESP" | jq -r '.id')

# 2. Provision VLESS Credential with preserved UUID
curl -s -X POST http://localhost:8110/api/v1/credentials \
  -H "Authorization: Bearer $ADMIN_TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "user_id": "'"$USER_ID"'",
    "node_id": "'"$NODE_ID"'",
    "protocol": "vless",
    "xray_uuid": "'"$CLIENT_UUID"'"
  }'
```

### 1.4 Client Subscription Cutover
Issue the user their new universal subscription URL (`https://vpn.example.com/client/{token}`). The client can import configurations directly into Sing-box, Clash Meta, or V2Ray without manual reconfiguration.

---

## 2. Migrating from Marzban

### 2.1 Architectural Differences
- **Marzban**: Python/FastAPI based with local or multi-node Xray nodes, using MySQL or SQLite.
- **Simple-VPN-Builder**: Compiled Go platform with high concurrency, native WireGuard/AmneziaWG netlink kernel management, and sub-millisecond gRPC state synchronization.

### 2.2 Exporting Users from Marzban
Using the Marzban CLI or API:
```bash
marzban cli user list --json > marzban_users.json
```

Each user record contains:
- `username`
- `status` (`active`, `expired`, `limited`)
- `used_traffic`
- `data_limit`
- `expire`

### 2.3 Bulk Import Script
Create a simple migration script using `curl` and `jq`:
```bash
#!/bin/bash
ADMIN_TOKEN="YOUR_ADMIN_JWT_TOKEN"
CP_API="http://localhost:8110/api/v1"

jq -c '.[]' marzban_users.json | while read -r user; do
    USERNAME=$(echo "$user" | jq -r '.username')
    DATA_LIMIT=$(echo "$user" | jq -r '.data_limit // 0')

    # Create user in Simple-VPN-Builder
    USER_ID=$(curl -s -X POST "${CP_API}/users" \
      -H "Authorization: Bearer ${ADMIN_TOKEN}" \
      -H "Content-Type: application/json" \
      -d "{
        \"username\": \"${USERNAME}\",
        \"email\": \"${USERNAME}@imported.vpn\",
        \"data_limit_bytes\": ${DATA_LIMIT}
      }" | jq -r '.id')

    echo "Migrated user ${USERNAME} -> ID: ${USER_ID}"
done
```

---

## 3. Migrating from Standalone WireGuard (`wg0.conf`)

### 3.1 Parsing Existing `wg0.conf`
A standard `/etc/wireguard/wg0.conf` contains peer sections:
```ini
[Peer]
PublicKey = aaaaaabbbbbbccccccddddddeeeeeeffffff=
AllowedIPs = 10.8.0.2/32
```

### 3.2 Extracting Peers and Registering
Extract public keys and allocated IPs:
```bash
awk '/\[Peer\]/{flag=1; next} flag && /PublicKey/{pub=$3} flag && /AllowedIPs/{ip=$3; print pub, ip; flag=0}' /etc/wireguard/wg0.conf > peers.txt
```

Register peers as credentials in Simple-VPN-Builder:
```bash
while read -r PUBKEY IP; do
    # 1. Create a user corresponding to the IP
    CLEAN_IP=$(echo "$IP" | tr -d '/')
    USER_ID=$(curl -s -X POST http://localhost:8110/api/v1/users \
      -H "Authorization: Bearer $ADMIN_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{\"username\": \"wg_peer_${CLEAN_IP}\"}" | jq -r '.id')

    # 2. Re-register existing public key
    curl -s -X POST http://localhost:8110/api/v1/credentials \
      -H "Authorization: Bearer $ADMIN_TOKEN" \
      -H "Content-Type: application/json" \
      -d "{
        \"user_id\": \"${USER_ID}\",
        \"node_id\": \"${NODE_ID}\",
        \"protocol\": \"wireguard\",
        \"public_key\": \"${PUBKEY}\",
        \"assigned_ip\": \"${IP}\"
      }"
done < peers.txt
```

### 3.3 Seamless Cutover
1. Stop the legacy `wg-quick@wg0` service:
   ```bash
   sudo systemctl stop wg-quick@wg0
   sudo systemctl disable wg-quick@wg0
   ```
2. Launch `vpnbuilder-agent`:
   ```bash
   sudo systemctl start vpnbuilder-agent
   ```
3. The agent synchronizes state from the Control Plane, restores all WireGuard peers on the kernel interface, and resumes routing immediately with zero peer downtime.
