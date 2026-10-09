# PostgreSQL for Go backends

## Defaults
- `database/sql` is portable standard abstraction; `pgx`/`pgxpool` are suitable for PostgreSQL-specific capability. `sqlc` is useful for generated typed access from reviewed SQL. Pick based on requirements, not fashion.
- Use a process-owned pool with startup connectivity/readiness validation, bounded max open/idle connections, max lifetimes/idle times where helpful, and shutdown closure. Plan connection budgets across replicas.
- SQL must use positional parameters (`$1`, ...); identifiers/order clauses require explicit allowlists, not placeholders or untrusted concatenation. Inspect query plans before tuning indexes in a migration.

## Transactions and concurrency
- Start transaction with request context and suitable isolation; all statements participating in it must use the **transaction handle**, not global pool. Roll back on failure; check commit errors. Consider retries only for recognized transient failures and only for idempotent/transaction-safe operations.
- Preserve write ordering, locking strategy, optimistic versioning, isolation, conflict semantics and affected-row checks. Business invariants like stock >= 0 must be enforced atomically, not check-then-update across independent statements.
- Handle unique/foreign-key/check constraint violation by SQLSTATE or typed driver error where possible, and map deliberately to domain/API errors.
- Avoid transaction wrapping around slow external network calls; when existing semantics depend on it, document and design a compatible solution, potentially with explicit user decision.

## Type mapping hazards
- `NUMERIC/DECIMAL` to `float64` silently loses precision; use exact decimal/integer scaled representation for money and persisted exact values, preserving scale/rounding rules at boundaries.
- `TIMESTAMP WITHOUT TIME ZONE` has no timezone; `TIMESTAMPTZ` denotes an instant. Preserve original zone assumptions, UTC normalization, precision and JSON representation.
- Distinguish SQL NULL, missing JSON, zero number, false, and empty string. Choose pointer, `sql.Null*`, pgx nullables or custom option type matching contracts.
- UUID, bytea, arrays, JSONB, enums, BIGINT/INT, bool and generated columns require explicit round-trip tests.
- Preserve database-generated IDs/default values, sequence state, timezone/session settings, `search_path`, row-level security, triggers and indexes. ORM `@Version`, JPA cascades and flush timing can be material to business behavior.

## Existing database migrations
When part of `spring-to-go`, **no automatic DDL / schema alteration, data mass update or generated migration**. Compare catalog/schema snapshots of isolated copies; run cross-runtime bidirectional read/write tests against isolated clones. Do not rely solely on successful queries or `pg_dump` textual sameness to assert full compatibility. Rollback must be exercised after Go writes.
