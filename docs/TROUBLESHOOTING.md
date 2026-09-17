# Troubleshooting Guide

Common issues, diagnostic procedures, and recovery solutions for Simple VPN Builder.

## 1. Node Agent Fails to Connect to gRPC Hub

### Symptoms
- In the Admin Panel, the node status displays as `Offline`.
- The node agent logs contain `connection refused` or `x509: certificate signed by unknown authority`.

### Diagnostic Steps
1. Verify port 9090 is accessible from the edge node:
   ```bash
   nc -zv YOUR_CONTROL_PLANE_IP 9090
   ```
2. Verify firewall rules on the control plane host:
   ```bash
   ufw status
   # Allow gRPC port if needed:
   ufw allow 9090/tcp
   ```
3. Check system clocks. Clock skew between the control plane and edge nodes will cause mutual TLS (mTLS) certificate validation to fail:
   ```bash
   timedatectl status
   ```
4. Inspect live agent logs on the node:
   ```bash
   journalctl -u vpnbuilder-agent -n 50 --no-pager
   ```

---

## 2. WireGuard Clients Connect but Cannot Access Internet

### Symptoms
- The VPN client shows active handshake and bytes sent, but zero or minimal bytes received.
- DNS queries or web requests time out.

### Diagnostic Steps
1. Check if IPv4 packet forwarding is enabled in the Linux kernel on the edge node:
   ```bash
   sysctl net.ipv4.ip_forward
   ```
   If it returns `0`, enable it immediately and persist it:
   ```bash
   echo "net.ipv4.ip_forward = 1" >> /etc/sysctl.conf
   sysctl -p
   ```
2. Check `nftables` NAT rules on the edge node:
   ```bash
   nft list ruleset
   ```
   Ensure a masquerade rule exists for the egress interface (e.g. `eth0` or `ens3`).
3. Check WireGuard peer status directly:
   ```bash
   wg show
   ```

---

## 3. AI Copilot Shows "Streaming not supported"

### Symptoms
- When sending a message to the AI Copilot in the Admin Web UI, the drawer prints `Streaming not supported` instead of the response.

### Cause
The upstream reverse proxy (Nginx or Caddy) is buffering Server-Sent Events (SSE).

### Solution
Disable proxy buffering for the AI endpoint in your Nginx site configuration:
```nginx
location /api/v1/ai {
    proxy_pass http://localhost:8110;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 600s;
}
```

For Caddy, SSE streaming works automatically without buffering.

---

## 4. Subscription URL Returns 404 or Invalid Format

### Symptoms
- Client receives `404 Not Found` when opening `/sub/{token}`.
- Sing-box or Clash fails to import configuration.

### Diagnostic Steps
1. Verify the subscription token exists in the database:
   ```bash
   curl -i "http://localhost:8110/sub/YOUR_TOKEN_HERE"
   ```
2. Test User-Agent negotiation:
   ```bash
   # Test Sing-box JSON format:
   curl -H "User-Agent: sing-box/1.9.0" "http://localhost:8110/sub/YOUR_TOKEN_HERE"

   # Test Clash Meta YAML format:
   curl -H "User-Agent: ClashMeta/1.18.0" "http://localhost:8110/sub/YOUR_TOKEN_HERE"
   ```
