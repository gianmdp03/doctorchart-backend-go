# Migration state (target project; update after every batch)

**Status:** IN PROGRESS / NOT READY / READY (READY only after Phase 4 mandatory gates)
**Phase:** 1 AUDIT | 2 DESIGN | 3 MIGRATE | 4 VERIFY
**Spring source revision:** [commit/hash]
**Go target revision:** [commit/hash]
**Original Spring Boot version:** 4.1.1 (verify actual)
**Go toolchain used:** go1.27.1 (verify actual)
**Loaded go-backend path/version:** [actual absolute/relative path + revision]
**DB fixture/clone identifier:** [non-secret ID, NOT production connection string]
**Contract matrix:** [path]
**Report:** [path]

## Completed facts (max ~10 bullets)
- [ ] Source feature inventory and schema ground truth inspected
- [ ] Target architecture and read/write equivalence approach agreed

## Phase-3 batches
| Batch | Scope | Status (NOT_STARTED/PARTIAL/COMPLETE/BLOCKED) | Test/evidence | Remaining gap |
| --- | --- | --- | --- | --- |
| B01 | [vertical feature] | NOT_STARTED | | |

## Mandatory Phase-4 gates
| Gate | PASS/FAIL/BLOCKED/NOT RUN | Evidence path / reason |
| --- | --- | --- |
| Go quality | NOT RUN | |
| HTTP/auth/side effects differential | NOT RUN | |
| PostgreSQL schema unchanged | NOT RUN | |
| Spring writes → Go reads | NOT RUN | |
| Go writes → Spring reads | NOT RUN | |
| Rollback rehearsal after Go writes | NOT RUN | |
| Operational/security readiness | NOT RUN | |

## Decisions and blockers
- [brief; reference detailed doc if needed]

**Next concrete action:** [one smallest verifiable step]
