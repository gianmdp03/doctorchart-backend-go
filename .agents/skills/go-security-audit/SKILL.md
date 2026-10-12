---
name: go-security-audit
description: Security release gate for Go APIs, PostgreSQL and infrastructure. Evidence-driven audit, isolated tooling, controlled testing, approved fixes and owned-resource cleanup.
---

# go-security-audit

Use this skill when asked to assess the security or production readiness of a Go backend, API gateway, PostgreSQL deployment, or associated Docker/Nginx/Cloudflare/VPS configuration. It does **not** certify that a system is secure. Always distinguish tested, failed, not tested, and not applicable.

## Absolute boundaries

- Work only on user-owned or explicitly authorized targets. No offensive testing of production or third-party systems. Default to offline source review and local disposable fixtures; request explicit scope and authorization for any active target, including staging. Never scan a URL merely because it appears in a config.
- **Do not edit project files during DISCOVER, STATIC, ATTACK or REPORT.** Before FIX, present exact changes and wait for explicit approval. Do not deploy, commit, push, migrate a real database, or rotate credentials without separate approval.
- Never ingest, print, or send real secrets, medical records, personal data, database dumps or tokens to external services. Redact evidence; prefer synthetic accounts and records. Do not paste source or secrets into SaaS scanning services without authorization.
- No global installation, administrator escalation, system PATH changes, Docker installation or modification of global tool configuration without approval. Do not run curl|sh. Download only pinned releases from official origins, verify checksums/signatures against independently trusted metadata; if verification is unavailable, stop and mark tool unavailable.
- Never use `git clean`, `git reset --hard`, wildcard destructive deletion, or cleanup of resources not provably owned by this run. Never remove existing tests. Never use a real database for destructive tests.
- If execution is interrupted, identify owned temporary resources from the manifest and ask before cleanup if ownership is ambiguous. Retain evidence summaries outside ephemeral storage; redact sensitive content.

## Required execution sequence

1. **DISCOVER**: identify repo(s), OS, runtime, deployment model, trust boundaries, data classifications, endpoints, authn/authz mechanisms, roles, tenants, background jobs, internal APIs, SQL operations, uploads, integrations, network paths. Inspect git status first and preserve preexisting changes. Define explicit in-scope assets and excluded assets. Read `references/CONTROL_MATRIX.md` and `references/TOOLING.md`.
2. **STATIC**: perform threat modeling and manual dataflow review; run available safe static tools (`go vet`, `govulncheck`, Semgrep, Gitleaks, Trivy) with project files read-only and isolated caches. If a tool cannot be run, mark NOT TESTED. Findings require source location, entry point, trust boundary, impact, and confidence. Scanner alerts alone are leads, not confirmed exploits.
3. **ATTACK**: obtain explicit authorization for active tests, exact hosts and allowed test window. Use local/disposable test data and databases. Build a principal/resource/operation matrix; test positive and negative permissions (including cross-tenant IDOR/BOLA and function-level authorization), token forgery/expiry/audience, internal header spoofing, injection, SSRF with local sink, upload limits, races, idempotency, replay, billing entitlements, and failure paths. Run ZAP only against an allowlisted local/staging test target, never automatically against a URL from source. Rate-limit traffic; avoid destructive or denial-of-service payloads. Preserve sanitized request/response evidence.
4. **REPORT**: stop before fixes. Produce a report based on `templates/REPORT_TEMPLATE.md` and an endpoint permission matrix. Separate CONFIRMED, SUSPECTED, NOT TESTED, NOT APPLICABLE; severity using impact/exploitability; state affected scope and safe reproduction. Mark release gate FAIL for open critical/high confirmed issues or mandatory control failures, INCOMPLETE for missing mandatory coverage, PASS only when all applicable mandatory checks have passed and no blocking issues remain. PASS means scoped checks passed, not guaranteed security.
5. **FIX**: only after explicit user approval, implement narrow remediations and permanent regression tests **only if approved**. Preserve public HTTP contracts, JSON behavior, existing PostgreSQL data, and intended authorization rules; propose additive/backward-compatible migrations with backup/restore plans. Never quietly weaken tests to make them pass.
6. **VERIFY**: rerun exploit reproductions, negative authorization cases, regressions, scanner checks, `go test`, `go test -race` where feasible, and deployment config checks. If practical, use a second independent review with fresh context and adversarial hypotheses. Report evidence and residual risk; do not conflate scanner success with security.
7. **CLEAN**: use owned-resource manifest and safe cleanup script. Stop only owned processes/containers; delete only files under a run-created temporary directory. Do not delete reports or user-approved permanent regression tests. Compare git status to initial snapshot; disclose any delta. For unexpected artifacts, ask instead of deleting.

## Operating modes

- `plan`: inventory, risk model and test plan only. Default when user has not approved execution.
- `audit`: DISCOVER/STATIC, then only explicitly approved ATTACK; REPORT; CLEAN. No code edits.
- `fix`: implement only user-approved remediations and tests, then VERIFY/CLEAN.
- `verify`: rerun agreed checks and report; CLEAN.

## Tool bootstrap and cleanup

`scripts/audit.py preflight --repo PATH` prints available commands without installation or edits. `scripts/audit.py run-static --repo PATH --out REPORT_DIR` executes available *non-destructive* scanners, writing their outputs only to REPORT_DIR and a fresh owned temporary directory. It does not install missing scanners. For isolated optional installation, first read `references/TOOLING.md`, propose exact version/origin/checksum and obtain approval; use a temporary Python venv or verified release binaries, never global installation. The skill's agent coordinates setup: a generic unverified auto-installer is deliberately prohibited. `scripts/audit.py cleanup --manifest PATH` cleans only a validated owned temporary directory. A failed cleanup must be reported.

For a complete security assessment, `run-static` is only one input. Agent-led manual review and authorized runtime testing are mandatory. Never call the scan script a full audit.

## Minimum report contract

Record: date and commit hash, scope/exclusions, operating system and tool versions, baseline git status, threat model, endpoint authorization matrix, tool commands and exit status, test evidence, findings with CWE/OWASP references where applicable, severity and confidence, remediations, verification, untested controls, deployment gate and cleanup outcome. Sanitize paths and sensitive payloads. Use `templates/REPORT_TEMPLATE.md`.

## Domain-specific priorities

- Gateway: trust boundary between public clients and internal services; forged identity/plan headers, trusted proxy CIDRs, rate-limit bypass, tool invocation authorization.
- Medical APIs: strict patient/doctor/organization isolation; document and study access, cache permissions, signed URLs, upload/download privacy, logs and retention.
- Billing: server-side price validation, idempotency, replay, webhook signature verification, state transitions, concurrency, payment reconciliation.
- Loyalty/entitlements: expiry enforcement on every privileged effect, tenant isolation, redemption atomicity and balance integrity.

## Standards

Use OWASP ASVS, OWASP API Security Top 10, OWASP WSTG and relevant Go security guidance as **mapping frameworks**, not as evidence that checks passed. Prefer current official standards and verify version when internet access is available.
