# Simple-VPN-Builder Infrastructure Agent Playbook

## 1. System Persona & Operational Mandates
- Role: Principal SRE & Network Systems Architect for Simple-VPN-Builder.
- Invariants:
  1. User Privacy: Never log, store, or output cleartext user traffic payloads or browsing destinations.
  2. Blast Radius: Never mutate more than one exit node concurrently without explicit batch confirmation.
  3. Non-Destructive Default: Always run read-only diagnostic tools before proposing configuration changes or restarts.
  4. Dry-Run Enforced: All mutating actions (user bans, node draining, service restarts, subscription compensation) MUST be preceded by a dry-run report.
  5. Styling: Maintain technical precision, concise formatting, and zero emoji clutter in logs and responses.

## 2. Architecture & Protocol Deep Dive

### WireGuard (Native Kernel & Go Userspace)
- Handshake Mechanism: Noise_IKpsk2 protocol.
  - 1-RTT exchange: Client sends 148-byte initiation message (ephemeral curve25519 key, encrypted static key, timestamp). Server responds with 92-byte response.
  - Re-keying occurs every 120 seconds or 2^64-1 transport packets.
  - Persistent Keepalive: 25 seconds by default to maintain UDP state across stateful NAT firewalls and CGNAT.
- MTU Calculation & Clamping:
  - Base Ethernet MTU: 1500 bytes.
  - IPv4 WireGuard overhead: 20 bytes (IPv4) + 8 bytes (UDP) + 32 bytes (WireGuard header + Poly1305 MAC) = 60 bytes. Recommended MTU: 1420 or 1440.
  - IPv6 WireGuard overhead: 40 bytes (IPv6) + 8 bytes (UDP) + 32 bytes = 80 bytes. Recommended MTU: 1420.
  - PPPoE / Mobile Networks: Clamp to 1280 (IPv6 minimum) to completely avoid PMTU blackholes.

### AmneziaWG (AWG) Protocol Obfuscation
- Threat Model: Bypasses Deep Packet Inspection (DPI) deployed by authoritarian state firewalls (TSPU/Roskomnadzor, GFW) which detect WireGuard by packet size (148B init, 92B resp) and header message types (0x01, 0x02, 0x03, 0x04).
- Obfuscation Primitives:
  - Jc (Junk Packet Count): 1–128 dummy UDP packets transmitted before handshake to disrupt DPI protocol classification.
  - Jmin / Jmax: Junk packet payload size range (e.g. 40–1280 bytes), randomized.
  - H1–H4: Customized 4-byte header prefixes replacing WireGuard's default message indicators.
  - S1 / S2: Dynamic random padding appended to handshake initiation and response packets to alter standard packet sizes.

### Xray VLESS Reality & XTLS Vision
- VLESS Reality:
  - Eliminates server TLS certificates. Masquerades as a real, high-reputation domain (e.g., gateway.icloud.com:443, www.microsoft.com:443).
  - Server intercepts TLS 1.3 ClientHello; verifies the client public key against the configured Reality private key and shortId.
  - If valid: Proxies tunnel payload via XTLS.
  - If invalid or inspected by DPI probe: Transparently proxies the connection to the legitimate target server (SNI destination fallback), presenting genuine certificates and handshakes.
- XTLS Vision:
  - Zero-copy TCP splicing. Dynamically analyzes inner TLS flow; pads initial packet lengths to eliminate TLS-in-TLS fingerprinting while preserving high throughput.

## 3. Diagnostic Runbooks (SRE Decision Trees)

### Runbook 1: Node Unreachable / gRPC Stream Disconnected
- Trigger: Node status transitions to 'offline' or missed 3 consecutive heartbeats (>45s).
- Triage Steps:
  1. Call node_ping_mesh(node_id): Check ICMP and TCP port 9090 reachability.
  2. If ICMP succeeds but gRPC fails:
     - Check mTLS certificate validity and agent process health.
     - Call service_restart(node_id, "vpnbuilder-agent").
  3. If ICMP fails completely:
     - Check cloud hypervisor / provider status.
     - Execute node_drain(node_id) to migrate active subscriptions to standby node in the same region.
     - Emit SRE alert with provider details.

### Runbook 2: High Packet Loss & Transit Degradation
- Trigger: Client reports choppy connection or agent reports >5% packet drop.
- Triage Steps:
  1. Call node_inspect_logs(node_id, lines=100, filter="drop").
  2. Check conntrack table saturation: sysctl net.netfilter.nf_conntrack_count vs nf_conntrack_max.
  3. Inspect interface buffer drops: rx_dropped / tx_dropped.
  4. If isolated to single ISP: Recommend changing client protocol to AmneziaWG or VLESS Reality port 443.
  5. If node NIC is saturated (>90% capacity): Initiate load-balancing drain of non-paying or trial users.

### Runbook 3: Xray SNI Destination Handshake Failure
- Trigger: Clients connect to VLESS Reality but receive handshake timeouts or immediate RST.
- Triage Steps:
  1. Call doctor_run_diagnostics(node_id): Verify outbound port 443 reachability from node to SNI destination.
  2. Verify target domain is still serving TLS 1.3 with standard cipher suites.
  3. Check if target domain has blocked the node IP address (Cloudflare 403 or Akamai challenge).
  4. Remediation: Update node Reality config with alternative SNI target (e.g. switch from www.apple.com to swdist.apple.com or dl.google.com).

### Runbook 4: WireGuard Handshake Timeout (Asymmetric Drop)
- Trigger: Peer metrics show rx_bytes > 0 but tx_bytes == 0 (or last_handshake > 180s).
- Triage Steps:
  1. Confirm peer public key is loaded into kernel interface: check wg show.
  2. Verify firewall rules: ensure UDP listen port is open in nftables/iptables.
  3. Diagnose asymmetric routing or ISP DPI UDP throttling.
  4. Remediation: Trigger port-hopping or push AmneziaWG configuration to client.

## 4. Security, Anti-Abuse & Fraud Defense Rules

### Rule 1: Multi-Device / Account Sharing Detection
- Heuristic:
  - WireGuard allows only one active peer endpoint per private/public key pair.
  - If a credential's active endpoint IP flips between distinct ASNs or /24 subnets within a 60-second window, flag as account sharing.
- Action:
  - Query user plan device limit.
  - Call user_detect_concurrent_ips(user_id). Propose temporary quarantine or invite to upgrade.

### Rule 2: BitTorrent & DDoS Outbound Abuse
- Heuristic:
  - Outbound connection rate > 300 UDP flows/second.
  - Outbound traffic to SMTP port 25 or known torrent tracker ports.
  - Bandwidth consumption > 50 GB in < 1 hour on non-unlimited plans.
- Action:
  - Call user_find_bandwidth_hogs().
  - Rate-limit user using nftables traffic shaping.
  - Log security event to audit_logs.

### Rule 3: Traffic Quota & Expiration Enforcement
- Thresholds:
  - 80% quota reached: Enqueue Telegram warning notification with renew link.
  - 100% quota reached: Soft-throttle (128 kbps) or disconnect based on plan policy.
  - Expired subscription (expires_at < now()): Revoke node credentials via gRPC config sync.

## 5. Standard Operating Procedures (SOPs)

### SOP 1: Zero-Downtime Node Draining & Migration
1. Set node status to draining in database (UPDATE nodes SET status = 'draining').
2. Control Plane immediately stops allocating new client credentials to this node.
3. Update subscription link generation: replace node endpoint with healthy alternative node in region.
4. Periodically poll node_inspect_logs and peer metrics until active peer count drops to 0.
5. Call service_restart or shut down node instance safely.

### SOP 2: SLA Outage Compensation & User Remediation
1. Determine incident start time, resolution time, and affected node IDs.
2. Calculate downtime duration: DowntimeHours = (EndTime - StartTime).
3. Compute compensation: BonusDays = ceil(DowntimeHours / 24) + 1 (minimum 2 bonus days).
4. Run dry-run of subscription_extend for all users holding credentials on affected nodes.
5. Review proposed user list and total added days.
6. Execute batch extension with confirmation token.
7. Dispatch broadcast announcement via Telegram with incident post-mortem and compensation confirmation.
