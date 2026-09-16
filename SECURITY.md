# Security

## Supported versions

| Version | Supported |
|---|---|
| latest (master) | Yes |
| older releases | No |

Only the latest release receives security patches. Update to the latest version before reporting a vulnerability.

## Reporting a vulnerability

**Do not open a public GitHub issue for security vulnerabilities.**

Report privately through one of these channels:

- **GitHub private disclosure** — [Security tab](https://github.com/ivanchik-byte/Simple-VPN-Builder/security/advisories/new) (preferred)
- **Telegram** — contact the maintainer directly via the profile in the repository

Include:

- A description of the vulnerability
- Steps to reproduce
- Your assessment of the impact
- Any suggested fix, if you have one

**Response timeline:**

| Stage | Target |
|---|---|
| Acknowledgement | 48 hours |
| Initial triage | 5 business days |
| Patch release | 30 days for critical, 90 days for others |

## Security architecture

**Authentication layers:**

- JWT tokens for session-based admin access (short expiry, refresh rotation)
- API keys for machine-to-machine integrations (hashed with bcrypt in the database)
- TOTP (RFC 6238) as a second factor for admin login (optional, enable via `TOTP_ENABLED=true`)

**Transport security:**

- mTLS on all gRPC connections between the control plane and node agents; certificates rotate automatically
- HTTPS termination handled by the reverse proxy (Caddy or Nginx recommended)

**Isolation:**

- Node agents run with minimal Linux capabilities (only what WireGuard and nftables require)
- The AI Copilot requires explicit human approval before any infrastructure mutation executes

**Credentials:**

- All secrets load from environment variables; no secrets in source code or configuration files
- Database passwords and JWT secrets are never logged
