# Migration Guide

## Migrate from 3X-UI

3X-UI stores its data in a SQLite database at `/etc/x-ui/x-ui.db`. This guide covers extracting users and inbounds from that database and importing them into Simple VPN Builder.

### Step 1: Export from 3X-UI

On the 3X-UI server, export clients:

```bash
sqlite3 /etc/x-ui/x-ui.db \
  "SELECT email, id, settings FROM inbounds WHERE protocol='vless'" \
  > /tmp/3xui_export.txt
```

For WireGuard clients:

```bash
sqlite3 /etc/x-ui/x-ui.db \
  "SELECT email, id, settings FROM inbounds WHERE protocol='wireguard'" \
  >> /tmp/3xui_export.txt
```

Copy the file off the server:

```bash
scp root@your-3xui-server:/tmp/3xui_export.txt .
```

### Step 2: Import users

Use the import script (included in the repository):

```bash
./scripts/import-from-3xui.sh --input 3xui_export.txt --api-url http://localhost:8110 --api-key your-api-key
```

The script:

1. Creates a user record for each client email
2. Creates a VLESS+Reality or WireGuard peer on the target node
3. Generates a subscription URL for each user

### Step 3: Notify users

After import, each user gets a new subscription URL. The old 3X-UI links stop working when you decommission the old server.

Send the new subscription URLs from the admin panel: Users > select all > Send subscription link via Telegram.

### REST API endpoint mapping

If you built automation against 3X-UI's API, update your calls:

| 3X-UI endpoint | Simple VPN Builder equivalent |
|---|---|
| `POST /xui/inbound/add` | `POST /api/v1/peers` |
| `POST /xui/inbound/del/{id}` | `DELETE /api/v1/peers/{id}` |
| `GET /xui/inbound/list` | `GET /api/v1/peers` |
| `POST /xui/inbound/update/{id}` | `PATCH /api/v1/peers/{id}` |
| `POST /xui/inbound/clientIps/{email}` | `GET /api/v1/users/{id}` |

---

## Migrate from Marzban

Marzban stores users in SQLite or MySQL. The migration path is similar.

### Step 1: Export from Marzban

```bash
# SQLite backend
sqlite3 /var/lib/marzban/db.sqlite \
  "SELECT username, key, data_limit, expire FROM users" \
  > /tmp/marzban_export.txt
```

For MySQL backend:

```bash
mysqldump --no-create-info marzban users > /tmp/marzban_export.sql
```

### Step 2: Import users

```bash
./scripts/import-from-marzban.sh \
  --input marzban_export.txt \
  --api-url http://localhost:8110 \
  --api-key your-api-key \
  --node-id your-target-node-id
```

The script maps Marzban data limits and expiry dates to Simple VPN Builder subscription plans.

### Step 3: Client cutover

Marzban subscription links (`/sub/{token}`) use a different URL format. After import:

1. Generate new subscription URLs: `GET /api/v1/users/{id}` returns `subscription_url`
2. Distribute the new links to users. The easiest path is via the Telegram bot: Users > Broadcast > Send subscription link

### Protocol notes

Marzban supports VLESS and VMess. Simple VPN Builder supports VLESS+Reality and WireGuard/AmneziaWG. VMess peers have no direct equivalent; migrate them to VLESS+Reality.

---

## Post-migration checklist

After importing from any platform:

- [ ] Verify user count matches: `GET /api/v1/users?limit=1` returns correct `total`
- [ ] Test one subscription URL: `curl -A sing-box https://your-domain/sub/{token}`
- [ ] Confirm peers appear on the node: SSH to the node server, run `wg show`
- [ ] Decommission the old panel only after all users have confirmed their new configs work
- [ ] Update your DNS records if your VPN domain is changing
