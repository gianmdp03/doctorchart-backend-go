# Security release gate — <project>

- Date / commit / branch:
- In-scope assets / excluded assets / authorized targets:
- OS / runtime / tool versions:
- Initial and final git status:
- Sensitive data handling and synthetic fixtures:

## Executive decision

**Gate: PASS | FAIL | INCOMPLETE**
Rationale, scope limitations and residual risk. PASS is not a security guarantee.

## Threat model and attack surface

Trust boundaries, data classifications, principal types, internet exposure, internal services, databases, uploads, external integrations.

## Endpoint authorization matrix

| Method/path | Authn | Role | Tenant/resource check | Negative test | Result/evidence |
|---|---|---|---|---|---|

## Control coverage

| Control ID | Applicable? | Test / evidence | Status | Reason if untested |
|---|---|---|---|---|

## Findings

| ID | Severity | Confidence | Status | Component | Evidence reference | Remediation | Verification |
|---|---|---|---|---|---|---|---|

For each confirmed finding: entry point, trust boundary, impact, safe sanitized reproduction, root cause, CWE/OWASP mapping, recommended fix, regression case. Never include live secrets or patient data.

## Tools and reproducibility

Commands, pinned versions, exit codes, skipped tools and reasons. Note scan coverage and false-positive triage.

## Fix approvals and regressions

Explicit approved changes, permanent tests retained with approval, backward compatibility, data migration precautions.

## Infrastructure, backups and response

Nginx/Cloudflare/VPS/Docker/PostgreSQL coverage, restore evidence, logs/alerts/incident procedures; distinguish configuration reviewed from deployment verified.

## Cleanup and final integrity

Temporary run directory, manifest, deleted owned resources, failures/leftovers, final git diff/status, report output location.
