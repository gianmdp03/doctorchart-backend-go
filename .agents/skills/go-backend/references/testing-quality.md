# Tests and gates for Go

## Coverage strategy
- Table-driven unit tests of domain invariants; one or more integration tests for live PostgreSQL, HTTP handlers, auth rules and outbound integrations (with isolated fakes/sandbox). Test failure modes as seriously as happy paths.
- Use `httptest` for API response tests, `testing` for units, Go `-race` during CI/test. For DB tests, ephemeral PostgreSQL/Testcontainers/test database with explicit isolation; never point test data writers at production.
- Assert persistence effects and transaction rollback, not only response codes. Verify not-found vs null, precision, timezone, validation, duplicate IDs, permissions, concurrency conflicts and recovery.
- Use fuzz tests/property-based tests where parsers and input boundaries warrant; benchmarks for performance claims and representative request mixes. Profile CPU/heap where meaningful.

## Baseline checks
From **Go module root**:
```
python3 /path/to/go-backend/scripts/check_go.py .
```
This performs read-only gofmt check, `go build ./...`, `go vet ./...`, `go test ./...`, `go test -race ./...`; `govulncheck` when installed or `--require-vulncheck`. Some environments cannot support race tests (CGO/platform); report BLOCKED, not PASS.

Optional manual inspections: `go test -cover ./...`, `go test -bench=. ./...`, `go tool pprof`, targeted linters pinned to compatible versions, `go list -m all`, dependency/license review. Fix failures rather than papering them over with ignore directives.

## Evidence requirements
- Report real executed commands/exit codes and notable outputs, toolchain version, environment, and which DB fixture/service was used. Passing mock-only tests is not equivalent to integration tests.
- Treat skipped unavailable test runners, absent `govulncheck`, missing auth infrastructure, or untested production paths as unverified. For migration use the separate spring-to-go compatibility gates; Go quality gates alone do not prove functional parity.
