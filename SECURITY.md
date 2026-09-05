# Security Policy

Simple-VPN-Builder handles sensitive network routing, cryptographic key exchanges, authentication tokens, and multi-tenant traffic isolation. We take security seriously and appreciate responsible disclosure.

---

## 1. Supported Versions

Security updates and critical bug fixes are provided for the following versions:

| Version | Supported          |
| ------- | ------------------ |
| 0.1.x   | Yes                |
| < 0.1.0 | No                 |

---

## 2. Reporting a Vulnerability

Please do NOT file public GitHub issues for security vulnerabilities or suspected cryptographic weaknesses.

### 2.1 Private Reporting Channels
- **GitHub Security Advisories (Recommended)**: Submit a private advisory via [https://github.com/ivanchik-byte/Simple-VPN-Builder/security/advisories/new](https://github.com/ivanchik-byte/Simple-VPN-Builder/security/advisories/new).
- **Email**: Send encrypted details to `security@vpnbuilder.dev`.

### 2.2 Information to Include
To help us triage and reproduce the issue rapidly, please provide:
1. Type of vulnerability (e.g. Authentication bypass, SSRF, memory safety, cryptographic flaw, privilege escalation).
2. Affected component (Control Plane, Node Agent, REST API, gRPC mTLS, Subscription Portal).
3. Step-by-step reproduction instructions or a minimal proof of concept (PoC).
4. System environment (OS, kernel version, Go runtime version).
5. Potential impact on operators or connected VPN clients.

---

## 3. Response Process and SLA

1. **Acknowledgment**: Within 48 hours of receipt.
2. **Triage & Validation**: Within 5 business days.
3. **Remediation & Patch**: A security patch will be developed, reviewed, and tested in a private repository branch.
4. **Public Disclosure**: A coordinated disclosure date and Common Vulnerabilities and Exposures (CVE) identifier will be arranged with the reporter.

---

## 4. Security Architecture Highlights

- **Control Plane to Node Agent Communication**: Secured via mutual TLS (mTLS) with internal CA verification, short-lived certificates, and token-bucket stream rate limiters.
- **Client Protocol Credentials**: WireGuard and AmneziaWG private keys are generated securely using crypto/rand curve25519 primitives and are never transmitted in unencrypted form.
- **Linux Sandboxing**: Node Agent runs with scoped Linux capabilities (`CAP_NET_ADMIN`, `CAP_NET_RAW`, `CAP_NET_BIND_SERVICE`) and `ProtectSystem=full` systemd isolation. Control Plane runs entirely unprivileged.
- **Supply Chain Security**: All release artifacts are signed keylessly with Sigstore Cosign via GitHub Actions OIDC and include automated Syft SBOMs.
