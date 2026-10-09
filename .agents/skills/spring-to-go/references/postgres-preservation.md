# Database-first preservation / bidirectional PostgreSQL compatibility

## Immutable production shape during drop-in migration
- Existing PostgreSQL schema is a **contract**, not a design suggestion. NO automatic migration, `CREATE/ALTER/DROP`, schema recreation, Flyway/Liquibase repair/baseline, data backfill, bulk update, sequence reset or destructive test against production without separate explicit authorization.
- Do not suggest Go-owned schema names/types merely for convenience. SQL and Go types must adapt to the current tables/views/functions and write/read rules.
- Existing Spring reads/writes and Go reads/writes should target identically structured **separate clones** during verification, not two services racing on real production data.

## Capture and compare real schema
- Collect through read-only PostgreSQL catalog queries or `pg_dump --schema-only` on the **isolated clone**; store sanitized snapshots and compare critical objects. Text dumps may contain noise; evaluate semantic differences in: schemas, tables/columns/types/nullability, constraints/FKs, indices, triggers/functions, sequences/identity/defaults, extensions, collations, enum labels, RLS and grants relevant to app role.
- Inspect effective Hibernate naming strategy and migration history. JPA source annotations are not sufficient to define production physical names.
- Do not write password-bearing connection strings, production records, secrets or personally identifying data into reports.

## Round-trip matrix (must test both directions)
1. Seed cloned DB with representative legacy rows, including null, empty, Decimal scale, BIGINT, enum, UUID, array/JSONB, timezone and boundary dates.
2. Spring writes -> Go reads and performs subsequent allowed business operations.
3. Go writes -> Spring reads and performs subsequent allowed business operations.
4. Repeat modify/delete, conflicts and rollback test cases. Spring can be restarted on a copy after Go writes with **no migration or data transformation**.
5. Run simultaneous-write/concurrency tests only on dedicated disposable DB under a carefully controlled scenario; check lost updates, unique conflicts and locking.
6. Verify SQL state after failed transactions, generated IDs/sequence behavior, optimistic versions, triggers/audit fields, cascading/delete behavior and expected effects.

## Cutover-specific cautions
- Preserve application role privileges/search_path, connection/session timezone, isolation/lock timeout and statement timeout; defaults can change query behavior.
- Don't start both apps' schedulers/consumers simultaneously when they must be singleton; external message delivery might duplicate or reorder.
- A naive rollback of executable without validating Go-created data is **not** a rollback plan.

## Evidence requirements
Report catalog snapshot identifiers/diff, fixture ID, precise tests for Java→Go and Go→Java, sample/edge coverage and failures. If actual production schema not accessible, mark schema compatibility **UNVERIFIED**; never infer safety from generated model alone.
