# Four phases, short vertical batches, reliable resumption

Only PHASE 1 AUDIT, PHASE 2 DESIGN, PHASE 3 MIGRATE, PHASE 4 VERIFY. No additional phase names, nested phase machinery or per-file approval loops. Routine implementation belongs in phase 3 batches.

## Batch sizing
- Each batch should be a coherent **vertical slice** (HTTP entrypoint + validation + authorization + business logic + persistence + error mapping + tests) or a cohesive background integration. Prefer a batch small enough to compile, test and inspect before moving to next. Batch count depends on project; phases remain exactly four.
- Prioritize shared contracts/config/security foundations then business modules in dependency order. Group tightly coupled operations when splitting would make transaction semantics wrong.
- No repeated full project audit after every batch. Consult current inventory/contract matrix and targeted source only.

## Status and gate
- `COMPLETE`: implementation + targeted tests + differential evidence with concrete artifact/command.
- `PARTIAL`: meaningful work exists but unverified/missing cases, explicitly listed.
- `BLOCKED`: cannot proceed safely; identify smallest needed information/permission/fixture.
- `NOT_STARTED`: untouched.
- At phase 4, only `VERIFIED` contract items count toward parity, not merely `COMPLETE` Go code.

## State updates
- Use target-project `MIGRATION_STATE.md` from template. Update **after each batch** and when exiting a phase. Keep under roughly 200 lines; use CSV matrix for per-endpoint details; do not flood context.
- Record source revision hash, target revision, toolchain, selected DB clone fixture, companion go-backend path, completed/partial/blocked batches, evidence paths, decisions, next single concrete step. No secrets or PII.
- On resume: read state and matrix; verify environment/revisions match; continue last incomplete batch. Re-audit only changes since pinned revision; do not restart from scratch. If prior records don't exist, perform phase 1.

## Agent autonomy
- Avoid asking for approval after each safe, local change. Require confirmation for destructive DB operations, real external side effects, changing auth protocols, altering API, production deploy/cutover, and unresolved safety-critical ambiguity.
- If time/context is insufficient, record honest state and smallest next step. Never claim future background continuation, fabricated test results or silent completion.
