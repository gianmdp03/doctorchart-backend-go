# Phase 1: Evidence-based audit checklist — Spring Boot 4.1.1

## Source inventory (inspect actual code)
- Version: Java, Spring Boot exact patch, Maven/Gradle, extensions, profiles, application YAML/properties, externalized config, environment variables, feature toggles and deployment manifests.
- HTTP and serialization: every mapping (controller + interface/inheritance/meta-annotations), verb/path/context path, params, bodies, validation groups, exception advice, Jackson settings/modules/custom serializers, null omission, naming, enums, UUID/BigDecimal, date/time locales/zone, file uploads, multipart, streaming, pagination and sort, compression, response headers, redirects and cache headers.
- Security: filter chain order, custom filters, Spring Security matchers, method-level security, ownership/tenant restrictions, sessions/JWT/OAuth2, CSRF/CORS, cookie settings, password encoders, permitAll exceptions, token rotation/signing, websocket auth, actuator access. Find runtime configuration/profile overrides.
- Domain/services: validations, domain events, exceptions and business messages, idempotency, transactions (propagation/isolation/readOnly/rollback rules), async/scheduled jobs, retries/circuit breakers, caches and locks, messaging, mail, object storage, third-party APIs, webhooks, file operations.
- Persistence: entities, inherited mappings, converters, embedded IDs, column naming, enum storage, optimistic lock versions, relationships and cascades, fetch plans, Hibernate filters, listeners, JPA Criteria/projections/native queries, batching, flush timing, transaction boundaries, generated identifiers, Flyway/Liquibase history.
- PostgreSQL: **ground truth is actual database catalog/schema on safely isolated clone**, not guessed JPA annotations; collect schema, constraints, RLS policies, triggers, sequences, indexes, defaults, functions, collations, time zones, roles, privileges, extensions and data samples (scrubbed), not production secrets.
- Operational: health, readiness, metrics, logs, endpoints, connection pool, thread pools, shutdown, external callbacks, scheduled jobs, deployment and proxy/TLS behavior.

## Traceability output
Record **one row per observable operation**, not just a Java class: source ref + route/worker/job/integration + request/trigger + response/effects + authorization + transaction + state change + verification case + migration status. For complex behavior add nested details in prose; don't make a giant essay.

## Reference behavior capture
- Prefer existing OpenAPI + tests, but verify reality. Implement safe black-box tests against Spring running on **isolated clone** with known seed and fake external dependencies.
- Record statuses, content-type, relevant headers, body, SQL effects, side effects/events, ordering, date/decimal/null edge cases and error paths.
- Do not dump JWTs, personal data, secret configuration, full live data or credentials into artifacts.

## Audit gate
All discovered relevant source files covered, every endpoint/job/integration accounted for, unsupported components flagged UNVERIFIED with exact reason. If repo incomplete or tests unavailable, do not claim comprehensive inventory; clearly state missing sources and needed evidence.
