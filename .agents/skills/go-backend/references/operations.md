# Production operations, performance and deployment

- Validate env/config at startup: ports, DSNs, required secrets, environment mode, allowed origins, timeouts, pool size and service URLs. Never embed secrets in binaries, logs, images or examples.
- Use structured `slog` fields with correlation IDs and intentional redaction. Health/liveness != readiness (DB connectivity if needed, dependencies as appropriate). Expose metrics/traces/OpenTelemetry proportional to system complexity.
- Graceful shutdown: stop accepting new requests on SIGINT/SIGTERM, allow in-flight tasks bounded time, stop workers/schedulers, flush logs/traces when supported, close pools. Avoid losing durable work; use queues/outbox/recovery if needed.
- Build for intended architecture, libc/CGO requirements and TLS certificates. Test binary startup with real production-like configuration in staging. Small binaries do not eliminate runtime, GC or the need for resource budgets.
- Define separate startup/readiness/shutdown targets and deploy rollback plan. Beware proxy headers/remote IP, HTTP/2, forwarded proto, CORS, TLS termination and connection limits.
- Optimize measured bottlenecks only: baseline CPU, RSS, heap, GC, p50/p95/p99, DB calls/query plans, error rate, concurrent connections, actual production-like workload. Compare same hardware/data/load/config before claiming advantage over Spring Boot.
- Cache only when coherent invalidation, consistency, observability and failure behavior are defined; otherwise it may create new bugs.
