# Idiomatic Go and correctness — code review standard

Read when producing or reviewing core Go code. Go 1.27.1 toolchain target. Canonical sources: https://go.dev/wiki/CodeReviewComments ; https://google.github.io/styleguide/go/ ; https://go.dev/doc/effective_go ; https://go.dev/doc/go1.27

## Types, mutability and ownership
- Decide pointer vs value receiver deliberately: avoid copying types containing mutexes; preserve method sets; pointer receiver when mutating or expensive copies. Keep receiver choice consistent for a type.
- A slice copies its descriptor and commonly shares underlying storage; map is reference-like. Beware aliasing, concurrent access, appending and mutable return values. `[]byte`/`string` conversion and encoding must be intentional.
- `nil` slice encodes as JSON `null` while non-nil empty slice encodes `[]`; zero value vs absent input is not equivalent. `omitempty` and pointers can alter legacy API contracts. In migrations, retain source behavior.
- Maps are not safe for concurrent read/write. Use mutexes, ownership or designed channels. `sync.Map` only for workloads fitting its use cases.
- Interface with typed nil pointer may itself be non-nil. Avoid returning typed nil as `error`.
- `any` and reflection are not shortcuts around design. Prefer generics where they genuinely reduce duplication without hiding behavior. Do not invent generic repositories mechanically.
- `int` width varies by target; use `int64`, `uint64`, etc. for precise schema/protocol boundaries. Avoid numeric overflow in input conversion.

## Errors and resources
- `if err != nil` is normal. Wrap with `fmt.Errorf("operation: %w", err)` at context boundaries; preserve error identity through `errors.Is/As`. Make domain errors recognizable and HTTP mapping deliberate.
- `defer` cleanup after checking initialization errors; check `Close`, `Commit`, `Flush`, and response/write failures when actionable. Transactions: use `defer tx.Rollback()` and return `tx.Commit()` result; handle rollback on failed commit appropriately for observability.
- Do not use `panic` for expected invalid input, not-found, conflicts or network errors. Do not turn recover into blanket suppression of failures. Never discard `err` unless justified and documented.
- Treat `io.Reader` and streaming carefully: use size limits, return/close underlying handles, avoid unbounded `io.ReadAll`.

## Context and concurrency
- Accept `context.Context` as first argument for request-scoped I/O, pass it along, honor deadlines and cancellation. Avoid storing in structs or substituting `context.Background()` in request flow.
- Create goroutines only with a lifecycle, backpressure/limit, ownership, error channel or task group strategy; use `WaitGroup`, cancellation and select appropriately. Unhandled goroutine panic can crash process. Protect shared state.
- Avoid channel/mutex choreography when sequential code suffices. Use synchronization primitives intentionally and test with the race detector. Close a channel from its sending owner only as justified.

## Naming and modules
- Short lower-case package names, avoid `util`, `common`, `base`, `helpers`, `models` dumping grounds. Go exported names use upper case, unexported lower case.
- Small cohesive packages with clear imports; prevent circular dependencies without adding artificial interfaces everywhere. Keep package-level globals immutable or tightly controlled.
- Don't use redundant `ProductServiceImpl`, getters and setters, or Java hierarchy names. Prefer concise meaningful API methods.
- Use `gofmt` and standard idioms. For test-only utilities isolate in `_test.go`.

## Review checklist
Check: error path, nil/zero/missing, data race, resource closure, context propagation, mutable aliasing, external effects, existing API semantics, need for interface and package, test for edge conditions. Prefer a deletion of unnecessary abstraction over adding another layer.
