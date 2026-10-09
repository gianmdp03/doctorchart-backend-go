# Spring Boot 4.1.1 → Go 1.27.1 migration verification

**Overall decision:** NOT READY (until all mandatory gates PASS)
**Source Spring commit:** [hash]
**Go commit:** [hash]
**Toolchain:** [actual version]
**Baseline database clone/schema snapshot:** [ID]

## Inventory and behavioral parity
Discovered HTTP routes: [count] | Verified routes: [count] | Missing/blocked: [count]
Discovered jobs/integrations: [count] | Verified: [count] | Missing/blocked: [count]
Contract matrix: [path]

## Gate evidence
| Required gate | PASS/FAIL/BLOCKED/NOT RUN | Executed command / trace / fixture / finding |
| --- | --- | --- |
| Build, format, vet, unit/integration, race | NOT RUN | |
| Security/auth/authorization cases | NOT RUN | |
| HTTP contract differential and side effects | NOT RUN | |
| PostgreSQL schema unchanged | NOT RUN | |
| Spring→Go read/write behavior | NOT RUN | |
| Go→Spring read/write behavior | NOT RUN | |
| Source restart/rollback after Go writes | NOT RUN | |
| Background jobs/integrations | NOT RUN | |
| Performance baseline: RSS/CPU/p95/p99 | NOT RUN | |
| Staging deployment readiness | NOT RUN | |

## Differences, exceptions and risks
- [Each explicit difference, severity, evidence, accepted-by if applicable]

## Production cutover
**Not authorized by this report.** Release requires explicit human approval, tested backup/restore, coordinated jobs/traffic management and a rollback owner.
