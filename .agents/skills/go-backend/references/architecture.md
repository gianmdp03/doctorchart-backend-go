# Minimal, purposeful backend architecture

## Default approach
- First map domain operations, data flows and external boundaries; structure follows these constraints. A small service may need only `main.go`, `handlers.go`, `store.go`, and tests. Grow packages when responsibilities warrant.
- `cmd/api/main.go` for composition/bootstrap when helpful; `internal/` for implementation inaccessible to consumers. Split features (`product`, `orders`) where it makes navigation easier. No mandatory `pkg/`, `domain/`, `application/`, `infrastructure/`, `adapters/` hexagon for every CRUD.
- For realistic complexity, handler (decode/auth/encode), service (business invariant), and store (persistence) are useful concepts, but combining related code is fine if tiny. Do not create an interface until a **consumer boundary** benefits from it (alternative implementation, test substitution, decoupling). Constructor injection by values/pointers.
- Distinguish entities/domain values from wire DTOs only when API contracts differ, security needs require it, or serialization changes are likely. Do not add mapping code for identical trivial shapes without a reason.
- Dependencies should flow toward stable business rules. No business code depending directly on `http.ResponseWriter` or process-global DB state if that makes invariants hard to test.
- Configuration: one validated config model, loaded once at startup, dependencies passed explicitly; no ambient global mutable registry.

## Avoid habitual Spring-style design
- Do not create: abstract generic service base, one interface for every concrete type, repository manager factory, global service locator, reflective DI, controllers that wrap pointless services, one file per trivial type.
- Avoid introducing CQRS/event buses/distributed transactions/caching layers unless actual business or scale demands them.
- No architecture migration without task need, especially during drop-in conversion. Prefer feature-scope changes and justified edits.

## Decision test (answer before adding abstraction)
1. What invariant or dependency boundary does this abstraction protect?
2. Could a simple function, method or small concrete struct do it?
3. Does it reduce net code and maintenance complexity?
4. Is failure handling, state and testability clearer?
5. Can another developer find where a request is handled in under a minute?

An abstraction with no strong answers is probably unnecessary. Minimal code is not minimum line count; favor explicit and legible over clever.
