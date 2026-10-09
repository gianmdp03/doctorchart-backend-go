# Observable behavior and drop-in compatibility contract

## Compatibility is wider than route parity
- URL: route path (including trailing slash/context path), HTTP verb, query names/defaults/duplicates, required headers, MIME types, path variable decoding, search/filter/order/page semantics.
- Request and response: JSON exact field names/types/null-vs-absent/zero/empty arrays, enum strings, numeric precision/rounding, time strings/zones, validation errors/field order where clients rely on it, content type, status code, location/etag/cookies/CORS/cache headers, streaming/multipart formats, content encoding when contractual.
- Error: map source behavior across 400/401/403/404/409/422/429/500 etc; preserve domain error codes, response schema and localization when visible. Do not expose internal errors.
- Authentication: preserve password hashes/authentication method, bearer issuer/audience/signing strategy, token acceptance/claims, refresh/expiry behavior, session cookie/registry rules, logout/revocation, anonymous endpoints; do not assume existing session infrastructure can be shared without proof.
- Authorization: preserve endpoint AND object/tenant/role permissions; explicit denial cases required. Avoid accidental permissive fallback.
- Other: observable order, generated identifiers, optimistic conflicts, email/webhook/event payload, scheduled jobs, idempotency, retry behavior, cache invalidation and job exactly-once/at-least-once semantics as applicable.

## Equivalent does not mean same Go implementation
- Header ordering and JSON object key ordering generally should not be used as equality criteria unless consumers truly depend on raw bytes/signatures. Arrays and documented ordering remain contractual. Build normalizers ONLY for empirically harmless, explicitly approved volatile data.
- Framework differences (`http.Error` vs Spring ProblemDetail, different `null` serialization, different validation messages) are *not* automatically acceptable.
- Fixing a source bug may be a worthy later change but requires separate acceptance. Compatibility-first by default.

## Status tracking
Use CONTRACT_MATRIX and statuses: `DISCOVERED`, `IMPLEMENTED`, `VERIFIED`, `PARTIAL`, `BLOCKED`, `UNVERIFIED`. A pass requires positive evidence for its specific case, not mere presence of a handler.

## Release gate
No frontend configuration/code change; identical public API routes/behavior on tested use cases, auth continuity, equivalent side effects, same PostgreSQL schema/data, bidirectional persistence compatibility and a rehearsed rollback. Claims of complete identity must acknowledge untested states; never claim mathematical proof from finite tests.
