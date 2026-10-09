# Spring Boot 4.1.1 → idiomatic Go semantic mapping

This is a checklist of concerns, not a one-to-one class conversion guide. Consult `go-backend` for all implementation patterns.

| Spring mechanism | Preserve this semantics | Go implementation options |
| --- | --- | --- |
| `@RestController`, mappings | Routing, status, serialization, headers | `net/http` handlers/ServeMux |
| `@ControllerAdvice`, `ProblemDetail` | Error shape/status/messages | Explicit error-to-HTTP mapping |
| Bean validation | Required/conditional validation and error schema | Explicit/domain validation, optional package |
| Jackson config | Null omission, date/number format, enum, polymorphism | Carefully defined Go wire DTOs/marshalers |
| `@Service`, DI | Scope, dependency lifecycle, business rules | Concrete types + constructor wiring |
| Spring Security | Auth filter/order, session, token, roles + per-record access | Explicit middleware + domain authorization; proven token/session compatibility |
| `JpaRepository`/Hibernate | Queries, locking, dirty checks, generated IDs, cascades | `pgx`/`database/sql`/`sqlc`; explicit SQL/invariants |
| `@Transactional` | Rollback, propagation, isolation, transaction boundary | Explicit `tx`, transaction coordinator as needed |
| `@Version` | Optimistic concurrency | Version conditional update + affected-row check |
| Schedulers/`@Async` | Timers, job duplication, durability, retries | Worker/scheduler with lifecycle and persistence as required |
| Spring events/queues | Event ordering/payload/delivery | Equivalent publisher/outbox/consumer integration |
| Actuator / Micrometer | Readiness/health/metrics interfaces | Go health handlers / OpenTelemetry equivalents |
| `application.yml` | Environment-specific settings/secrets/defaults | Validated startup configuration |

## Specific traps
- Java `long` → Go `int64`, not architecture-dependent `int`; `BigDecimal` → exact decimal, never unchecked `float64`; JDBC/PG null mappings require nullable representations.
- Java enums stored as ordinal/string must preserve **actual** DB representation; JSON enum encoding may differ from DB representation.
- `LocalDateTime` vs `OffsetDateTime` vs `Instant` requires tests of clock/timezone/precision, DST boundaries and JSON formatting.
- Spring proxy-based behavior: self-invocation, propagation and exception/rollback subtleties; don't infer transaction behavior from `@Transactional` annotation alone.
- Defaults, validation messages and security filters may come from framework auto-configuration; inspect effective runtime properties/tests.
- Cookie-backed sessions often depend on JVM-side session store/serialization; sharing old cookie is not automatically possible. Either prove compatibility or present a controlled explicit user-approved strategy (not silent forced logout).
