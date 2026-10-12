# Mandatory security control matrix

For each control record: asset, applicability, owner, method, test case, evidence, status (PASS/FAIL/NOT TESTED/NOT APPLICABLE), and risk. A NOT APPLICABLE control requires rationale.

| ID | Area | Minimum required checks |
|---|---|---|
| A01 | Identity | JWT signature and allowed algorithm, issuer/audience/expiry, key rotation/JWKS behavior, revocation strategy, session/cookie protection, auth bypass |
| A02 | Authorization | Per-endpoint actor × role × tenant × resource × operation matrix; cross-user/tenant IDOR/BOLA, BFLA, ownership checks on reads/writes/downloads, deny-by-default |
| A03 | Gateway | Untrusted identity/entitlement headers stripped, direct backend bypass, trusted proxies/XFF, service-to-service auth, internal routes, rate-limit identity |
| A04 | Inputs | Parameterized SQL, SQL identifier allowlists, validation, overposting/mass assignment, path traversal, command injection, request limits, error sanitization |
| A05 | Network | SSRF allowlists and redirect/DNS behavior, outbound access, CORS origins/credentials, CSRF where cookies used, TLS and security headers |
| A06 | Business | Payment/webhook authenticity, replay/idempotency, atomic balances, expired entitlements, price/plan enforcement, concurrent state transitions |
| A07 | Files | MIME and magic-byte checks, file size/pixel/page limits, archive bombs, malware policy, storage isolation, signed/private download auth, no public PII URLs |
| A08 | PostgreSQL | Least-privilege role, nonpublic listener, TLS where required, safe SQL, tenant scoping, transaction boundaries, migrations, backup encryption and tested restore |
| A09 | Secrets/supply chain | History/worktree secret scans with safe redaction, dependency reachability, pinned CI actions/images, verified binaries, SBOM where useful |
| A10 | Runtime | Docker rootless/nonroot where feasible, caps/volumes, no exposed admin ports, SSH hardening, firewall, Nginx/Cloudflare origin restrictions, TLS, backups |
| A11 | Operations | PII/PHI-safe logs, access audit trail, alerts, incident response, retention, recovery objectives, credential rotation plan |
| A12 | Resilience | Timeouts, bounded queues, upload/resource limits, safe errors, race tests, health endpoints without sensitive data, safe shutdown |

## Coverage requirements

- Inventory all public and internal routes; explicitly flag any route whose policy could not be determined.
- For every sensitive resource, test at least owner, unrelated same-role user, other tenant, unauthenticated, and lower-privileged actor as applicable.
- Review read, create, update, delete, export, bulk and background pathways independently.
- Include synthetic multi-tenant fixture setup and teardown. Never reuse real patient/customer data.
- Critical/high confirmed issues block release; mandatory controls with insufficient evidence yield INCOMPLETE unless already FAIL.
