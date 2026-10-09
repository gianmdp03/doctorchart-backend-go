# Phase 4: release/cutover and rollback (manual authorization required)

## Before even proposing cutover
- Confirm all contract matrix items VERIFIED or explicitly approved exclusions; separate `NOT RUN` / `BLOCKED` from PASS.
- Go binary/image built with recorded toolchain/dependencies, pinned configuration, proxy settings, JWT/session compatibility, feature flags, health/readiness and logs.
- PostgreSQL physical schema unchanged and **bidirectional** read/write test passed on isolated copy; rollback to Spring after Go writes rehearsed. Backup and **restore tested**, not merely backup created.
- Plan singleton job ownership, queue consumers, outbox, webhooks, caches, cron, file writes and transaction continuity. Use readiness/traffic draining strategy. Define rollback triggers and observer/owner.
- Load test representative workloads on comparable CPU/RAM/db data; measure p50/p95/p99, error rates, RSS, CPU, DB load. No guaranteed Go superiority.

## High-level authorized cutover sequence
1. Announce/change window; verify backup/restore, credentials/secrets and rollback owner. Record source target builds and schema fingerprint.
2. Handle inflight writes and background jobs safely via planned switch/freeze/drain; avoid simultaneously running singleton tasks. If auth sessions cannot be shared, **block** seamless switch until approved strategy is proven.
3. Start Go in staging/prod with **DDL/migration runners disabled**, verify readiness/config and smoke tests, then change traffic only with explicit approval. Observe business and DB correctness, auth failures, p95, errors, queue depth and resource budget.
4. If rollback threshold occurs, safely drain Go, stop Go-only jobs/consumers, start Spring on **the same unmodified database** after verifying it can read/process Go-created data. Control side effects and pending messages; restore traffic only when safe.
5. Write final handoff with verification matrix, incident/rollback actions, baseline/performance and known residual risks.

## Never silently do
Automatic `flyway migrate`, `hibernate.hbm2ddl.auto=update`, production DDL, destructive `pg_dump`/restore commands against live DB, dual-write without coordination, raw production dumps in reports, forced logout, quiet contract change, production traffic switch.

## Required final report labels
`READY` only if all mandatory compatibility/DB/security gates pass and release authorization is separate; otherwise `NOT READY`, with PASS/FAIL/BLOCKED/NOT RUN for each check and next actions. A completed code translation != safe production replacement.
