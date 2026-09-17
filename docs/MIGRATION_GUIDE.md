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

I do not bundle a standalone import script yet: import users through the REST API directly.
For each client email, create a user and assign a plan:

```bash
API=http://localhost:8110/api/v1
KEY=your-api-key

# 1. Create the user (returns id and subscription_token)
curl -s -X POST "$API/users" \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"username":"client@example.com","email":"client@example.com"}'

# 2. Assign a plan for 12 months with Telegram notice
curl -s -X POST "$API/users/{id}/assign-plan" \
  -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
  -d '{"plan_id":"{plan_id}","duration_days":365,"notify_user":true}'
```

Each user gets credentials provisioned on all active nodes and a
subscription URL. The exact field names match `api/openapi.yaml`.

### Step 3: Notify users

After import, each user gets a new subscription URL. The old 3X-UI links stop working when you decommission the old server.

Send the new subscription URLs from the admin panel: open each user and share
`/sub/{subscription_token}` (visible in Users > subscription modal with QR code).

### REST API endpoint mapping

If you built automation against 3X-UI's API, update your calls:

| 3X-UI endpoint | Simple VPN Builder equivalent |
|---|---|
| `POST /xui/inbound/add` | `POST /api/v1/users` + `POST /api/v1/users/{id}/assign-plan` |
| `POST /xui/inbound/del/{id}` | `DELETE /api/v1/users/{id}` |
| `GET /xui/inbound/list` | `GET /api/v1/users` |
| `POST /xui/inbound/clientIps/{email}` | `GET /api/v1/users/{id}/subscription` |

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

Import through the REST API directly (same calls as for 3X-UI above):

```bash
API=http://localhost:8110/api/v1
KEY=your-api-key

while read -r username _ _ _; do
  [ -z "$username" ] && continue
  curl -s -X POST "$API/users" \
    -H "X-API-Key: $KEY" -H "Content-Type: application/json" \
    -d "{\"username\":\"$username\"}" > /dev/null
done < /tmp/marzban_export.txt
```

The script maps Marzban data limits and expiry dates to Simple VPN Builder subscription plans
via `POST /api/v1/users/{id}/assign-plan` (see the 3X-UI section).

### Step 3: Client cutover

Marzban subscription links (`/sub/{token}`) use a different URL format. After import:

1. Generate new subscription URLs: `GET /api/v1/users/{id}/subscription` returns `subscription_token` and `subscription_url`
2. Distribute the new links to users, e.g. with a Broadcast campaign to the relevant segment

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
