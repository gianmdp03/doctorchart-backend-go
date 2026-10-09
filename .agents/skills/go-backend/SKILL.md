---
name: go-backend
description: Design, implement, review, test, optimize, or maintain production Go 1.27.1 HTTP APIs and backend services using idiomatic Go, minimal justified architecture, safe PostgreSQL, security, tests, and operational readiness. Use for new Go services and as the required engineering standard during Spring Boot to Go migrations.
---

# Go Backend Engineering — Go 1.27.1

## Mission and priority
Produce **correct, secure, idiomatic, comprehensible, production-ready Go backends** with the *least necessary complexity*. Optimize based on measurements, never by breaking business semantics. This skill is reusable for new Go projects and must be loaded by `spring-to-go` for migration implementation.

Priority: **preserve specified behavior and invariants > security/correctness > operational reliability > clarity/maintainability > measured performance > incidental style**. All mandatory requirements must hold; never trade away security to reduce lines of code.

Target Go toolchain: **go1.27.1** (verified release). Respect the project's `go.mod` minimum language level and dependency compatibility. Record the actual toolchain used. For a production security update to a newer supported patch, document/approve the change instead of silently overriding the target. Avoid assuming latest dependencies are compatible.

## Activation procedure
1. Inspect the repository, task, `go.mod`, API contracts, tests, data stores, constraints, and deployment shape. Prefer existing conventions unless demonstrably harmful; do not redesign outside requested scope.
2. Read **only needed references** below; always read `references/idioms.md` and `references/architecture.md` for architecture or substantial code changes. Read `references/postgres.md` for database work, `references/http-security.md` for HTTP/auth, `references/testing-quality.md` for tests/review, `references/operations.md` for production changes.
3. State a short implementation approach **only if necessary**, implement coherent vertical slices, and test incrementally. Do not demand approval for routine safe changes; get authorization for destructive data or production operations.
4. Explain relevant tradeoffs and test results with actual evidence. Distinguish tests executed, tests unavailable, and tests that failed. Never claim production readiness without verifying it.

## Non-negotiable engineering rules
- **Go, not translated Java.** No fake inheritance, Java-style service/repository hierarchies, omnipresent `GetX`/`SetX`, unnecessary interfaces, generic base CRUD classes, application-wide dependency container, or annotations/frameworks merely to recreate Spring.
- **Simplest useful design.** Organize by cohesive feature or package boundaries, not a file-per-type/forced multilayer template. Start with the standard library (`net/http`, `context`, `encoding/json`, `errors`, `slog`, `testing`); bring dependencies only for a demonstrated requirement. Do not chase minimum file counts at the expense of readability.
- **Explicit dependencies.** Prefer regular constructors and explicit wiring. Declare small interfaces at consumer boundaries only when needed for actual substitution or tests. A single concrete type is fine.
- **Clear Go idioms.** `gofmt`; short meaningful names; pointer vs value semantics intentionally; no leaking mutable slices/maps; handle typed nils, nil maps, and zero vs absent JSON values. Return and wrap errors with `%w`; use `errors.Is`/`errors.As`, not error-string comparisons. `panic` is not routine control flow; no silently ignored errors.
- **Context and lifecycle.** Pass request contexts to I/O, cancel work, bound concurrency and timeouts, avoid detached goroutines and fire-and-forget critical operations. Respect cancellation, synchronization, resource closure, and graceful shutdown.
- **Database safety.** Parameterize SQL; bound connection pools; use consistent transaction ownership and `tx` throughout a transaction; validate affected rows/invariants; preserve exact decimal/time/null semantics. No schema modifications without explicit authorization.
- **HTTP/security.** Respect specified routes, status codes, headers, JSON shape, validation, authN/authZ, idempotency, error mapping, payload bounds, CORS/CSRF requirements, TLS/proxy and cookie semantics. Authentication is not authorization. Do not log secrets or personal data.
- **Predictable production behavior.** Configuration validated at startup; structured and redacted logs; health/readiness separation; observability appropriate to system size; sane timeouts; safe shutdown, signals, TLS at an appropriate trusted boundary.
- **Performance discipline.** Benchmark *equivalent* workloads and inspect p95/p99, allocations, memory RSS, CPU, database work; avoid premature channels, caching, generics, unsafe, manual pooling, microservices or concurrency. Preserve semantics before optimizing.

## Default technology choices (preferences, not mandates)
- HTTP: standard `net/http` + `ServeMux` unless demonstrably insufficient.
- Persistence: `database/sql` or `pgx`/`pgxpool`; consider `sqlc` for stable typed SQL. Do not force an ORM or `sqlc` on every project.
- Logging: `log/slog`. Tests: standard `testing`, table-driven tests, integration tests with isolated dependencies. Profiling: `pprof` when indicated.
- Deployment: small Linux binary or container as infrastructure requires; no mandatory Docker or Clean Architecture.

## Review/gate protocol
Before reporting completion, use `scripts/check_go.py` at module root where possible (Python 3); run equivalent commands manually if Python is unavailable. It checks format, build, vet, tests, and race tests; optional `govulncheck` may be installed separately. Supplement with integration tests, auth/security tests, schema/database checks, and benchmarks appropriate to changed behavior.

**Do not claim a gate ran merely because it is documented.** `govulncheck` unavailable is **not a pass**. If the build requires missing services, mark checks **BLOCKED**, say what was missing, and give exact reproduction instructions. Avoid any command that writes to production data by default.

For migrations, **the `spring-to-go` compatibility rules take precedence on external contracts and data preservation**, while this skill governs the internal Go quality. Do not make an API "more idiomatic" if that changes the original behavior.

## Reference map (load on demand)
- [Idiomatic Go and correctness traps](references/idioms.md)
- [Architecture without overengineering](references/architecture.md)
- [HTTP and security](references/http-security.md)
- [PostgreSQL, transactions and types](references/postgres.md)
- [Testing and verification](references/testing-quality.md)
- [Operations and performance](references/operations.md)

## Definition of done
The implementation works for agreed use cases, has no known contract-breaking changes, has adequate tests for business-critical cases, has an explicit error/security/resource model, uses justified abstractions, and includes honest evidence for quality checks and unresolved risks. Otherwise report **PARTIAL/BLOCKED**, never an invented success.
