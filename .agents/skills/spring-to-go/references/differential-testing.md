# Evidence-based Spring vs Go differential testing

## Minimum harness plan
- Build and launch Spring and Go **against independent PostgreSQL clones with the same schema and deterministic seed**; isolate caches, queues, files, emails, webhooks and other external side effects (test doubles or sandbox). Control clock, locale, timezone, generated IDs/sequence start, random values and application profiles where possible.
- Generate a case catalog from audit matrix for every observable operation: route/verb, request input, principal/roles/tenant, expected response headers/status/body, relevant database rows and external side effects, negative and concurrency cases.
- Capture sanitized JSON snapshots for both backends. Use `scripts/compare_snapshots.py source.json target.json` as a **strict structural differential**. Capture schema/rows separately with approved canonicalization only for genuine non-semantic variability.
- Investigate each mismatch: either fix Go or get explicit approval for a documented intentional contract change. No generic field ignores, no treating 500==500 as success when content/effects differ.

## Example capture schema
```
{
  "cases": [
    {
      "id": "create-product:valid",
      "request": {"method": "POST", "path": "/api/products", "body": {"name":"Monitor","price":"10.50"}},
      "response": {"status": 201, "headers": {"content-type": "application/json"}, "body": {"id": 7, "name":"Monitor","price":"10.50"}},
      "database": {"products": [{"id":7,"name":"Monitor","price":"10.50"}]},
      "side_effects": []
    }
  ]
}
```
- This schema is just a transport for **pre-existing recorded data**, not a runnable capture harness. A contract fixture can choose another canonical decimal representation if it matches the original API. Compare outputs, not untrusted shell commands.
- Preserve array order (unless source explicitly non-deterministic and an approved normalizer documents it), compare missing vs null, field types, error messages, headers that form part of the contract, relational changes and external effects. Stable test-generated ID is preferred to ignoring ID.
- Compare snapshots by case ID. An omitted/extra case is a failure. Do not compare secrets or include real customer records. The script does not connect to endpoints/databases or produce claims of total parity.

## Key edge cases
1. Unauthorized/forbidden/expired credentials, roles/ownership.
2. Missing/zero/null values, invalid payloads, malformed JSON, request size.
3. BigDecimal rounding/overflow, JSON number/string representation, BIGINT limits.
4. LocalDateTime/Instant with DST/zone and micro/nanosecond handling.
5. Duplicate IDs, conflicts, unique constraint violation, not-found.
6. Transaction rollbacks, retries, optimistic locks and multiple affected tables.
7. Pagination/sorting/filtering, response error and cookie/header formats.
8. Scheduled jobs, side effects, external retry semantics and idempotency.

## Gate
Go build/test/security checks and full catalog differential, plus bidirectional PostgreSQL tests and actual cutover rollback drill in staging. A snapshot equality test is only **one layer of evidence**, not end-to-end safety proof.
