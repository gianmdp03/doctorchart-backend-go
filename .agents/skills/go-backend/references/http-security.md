# HTTP, JSON, authentication and authorization

## HTTP implementation
- `net/http` and `ServeMux` default. Keep handlers thin: parse and validate -> call domain operation -> deliberate status/header/body/error response.
- Limit request bodies before JSON decoding, close them, apply explicit parsing policies (unknown fields, multiple JSON values, content-type, charset), test 400/413/415 and field absence; do not assume default JSON library matches source API.
- Content negotiation, optional fields, null vs missing vs empty, whitespace, decimals, time formatting, arrays, enum spelling and JSON error shape are contracts. Stable order of array results when source guarantees order.
- Timeouts: server ReadHeaderTimeout, reasonable read/write/idle settings accounting for streaming and proxy deployment; I/O downstream bound by context/deadlines. Don't use a global timeout that kills legitimate streamed responses.
- Middleware for request IDs, recovery where appropriate, structured logging, auth, request size controls and observability. Recovery is not a substitute for correct error handling.
- Return context-appropriate HTTP statuses; avoid response double-writes and writing headers after body. Errors must not leak credentials, tokens, SQL details or stack traces.

## Security requirements
- Model **authentication** (identity validity) separately from **authorization** (scope/role/ownership per endpoint and object). Deny by default. Preserve source rules for anonymous, cross-tenant, role-based and ownership scenarios.
- If bearer JWT: verify signature and permitted algorithm, expiry/not-before, issuer/audience, key rotation and clock-skew rules with vetted libraries. Do not invent crypto. If Spring uses server-side sessions, preserve cookie/session validity or plan coordinated cutover; simple JWT parsing does not replicate Spring Security.
- Cookies: preserve name/domain/path/SameSite/Secure/HttpOnly, expiry semantics; CSRF when cookie authentication and browser behavior make it necessary. Respect CORS origin policy with credentials (no wildcard credentials).
- Parameterize SQL, validate pagination bounds, prevent injection in dynamic identifier/ORDER BY choices using allowlists. Validate remote URLs and redirects to avoid SSRF/open redirects; prevent path traversal and unsafe upload handling.
- Protect secrets in env/secret manager, logs, traces and test artifacts; use constant-time comparisons where applicable. Never disable certificate verification.
- Rate limiting, resource limits and idempotency depend on threat model, workload and original contract. Do not add silently if they change observable API behavior during migration.

## Security test minimum
Authenticated/anonymous, forbidden cross-role and cross-tenant access, expired/bad token, malformed JSON, missing/null fields, oversized inputs, conflicting requests, SQL injection attempts (queries parameterized), sensitive output/log leakage. Add abuse cases relevant to exposed functionality.
