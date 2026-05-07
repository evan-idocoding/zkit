# AI Navigation for `zkit`

This document is an AI-oriented map of the repository. It is optimized for fast codebase entry, locating the right abstraction layer, and avoiding common misreads of the package boundaries.

## 1. Repository Identity

- Module: `github.com/evan-idocoding/zkit`
- Language: Go `1.21`
- Positioning: standard-library-first runtime base for HTTP services, with an explicit admin surface and opt-in operational building blocks
- Primary audience of the code:
  - callers that want one-shot assembly via the root `zkit` package
  - callers that want lower-level building blocks via subpackages

## 2. Fast Mental Model

Read the repository as a layered stack:

1. `zkit` root package
   - High-level assembly facade.
   - Main entry points: `NewDefaultService`, `NewDefaultAdmin`.
   - Re-exports admin guard helpers so callers can stay in the root package.
2. `admin`
   - Admin subtree assembler.
   - Turns explicit `EnableXxx(...)` options into mounted HTTP endpoints.
   - Owns assembly-time validation and path/guard rules.
3. `ops`
   - Raw operational HTTP handlers.
   - No routing decisions, no auth decisions, no process lifecycle.
4. `httpx`
   - HTTP middleware chain and admin-oriented middlewares.
5. `rt/*`
   - Runtime primitives:
     - `rt/task`: background task manager and trigger/scheduler model
     - `rt/tuning`: runtime-settable typed knobs
     - `rt/safego`: panic/error-observable goroutine runner
6. `httpx/client`
   - Separate HTTP client builder utilities, independent from server-side admin assembly

If you are unsure where behavior lives, first decide whether you are looking at:

- assembly policy: `zkit`, `admin`
- raw endpoint behavior: `ops`
- HTTP middleware behavior: `httpx`
- runtime state primitives: `rt/task`, `rt/tuning`, `rt/safego`

## 3. Recommended Reading Order

### If the goal is “understand the public product”

1. [readme.md](./readme.md)
2. [doc.go](./doc.go)
3. [default_service.go](./default_service.go)
4. [default_admin.go](./default_admin.go)
5. package `doc.go` files under `admin`, `ops`, `httpx`, `rt/*`

### If the goal is “understand service assembly and lifecycle”

1. [default_service.go](./default_service.go)
2. [default_admin.go](./default_admin.go)
3. [guard.go](./guard.go)
4. [default_service_test.go](./default_service_test.go)
5. [default_service_runtime_test.go](./default_service_runtime_test.go)
6. [default_admin_test.go](./default_admin_test.go)

### If the goal is “understand admin endpoint composition/security”

1. [default_admin.go](./default_admin.go)
2. [admin/doc.go](./admin/doc.go)
3. [admin/admin.go](./admin/admin.go)
4. [admin/enable.go](./admin/enable.go)
5. [admin/guard.go](./admin/guard.go)
6. [admin/mount.go](./admin/mount.go)
7. [admin/report.go](./admin/report.go)

### If the goal is “understand raw operational handlers”

1. [ops/doc.go](./ops/doc.go)
2. `ops/*.go` by topic:
   - [ops/health.go](./ops/health.go)
   - [ops/runtime.go](./ops/runtime.go)
   - [ops/buildinfo.go](./ops/buildinfo.go)
   - [ops/log_level.go](./ops/log_level.go)
   - [ops/tuning.go](./ops/tuning.go)
   - [ops/task.go](./ops/task.go)
   - [ops/provided_snapshot.go](./ops/provided_snapshot.go)

### If the goal is “understand middleware/guard behavior”

1. [httpx/doc.go](./httpx/doc.go)
2. [httpx/chain.go](./httpx/chain.go)
3. guard-related pieces:
   - [httpx/mw_access_guard.go](./httpx/mw_access_guard.go)
   - [httpx/access_guard_token_set.go](./httpx/access_guard_token_set.go)
   - [httpx/access_guard_ip_allow_list.go](./httpx/access_guard_ip_allow_list.go)
   - [httpx/access_guard_hook_stderr.go](./httpx/access_guard_hook_stderr.go)
4. identity/recovery/network middleware:
   - [httpx/mw_request_id.go](./httpx/mw_request_id.go)
   - [httpx/mw_real_ip.go](./httpx/mw_real_ip.go)
   - [httpx/mw_recover.go](./httpx/mw_recover.go)
   - [httpx/mw_timeout.go](./httpx/mw_timeout.go)
   - [httpx/mw_body_limit.go](./httpx/mw_body_limit.go)
   - [httpx/mw_cors.go](./httpx/mw_cors.go)

### If the goal is “understand runtime primitives”

1. [rt/task/doc.go](./rt/task/doc.go), then [rt/task/task.go](./rt/task/task.go), [rt/task/manager.go](./rt/task/manager.go), [rt/task/runtime.go](./rt/task/runtime.go)
2. [rt/tuning/doc.go](./rt/tuning/doc.go), then [rt/tuning/tuning.go](./rt/tuning/tuning.go), [rt/tuning/types.go](./rt/tuning/types.go), [rt/tuning/parse.go](./rt/tuning/parse.go)
3. [rt/safego/doc.go](./rt/safego/doc.go), then [rt/safego/safego.go](./rt/safego/safego.go), [rt/safego/options.go](./rt/safego/options.go)

## 4. Package Map

### Root package `zkit`

Purpose:

- provide the easiest public entry points
- assemble servers, admin, tasks, tuning, and log-level exposure into a runnable service

Start here when the question is about:

- mount vs standalone admin
- service lifecycle
- default server construction
- root-level public API shape

Key files:

- [doc.go](./doc.go): public package contract and concepts
- [default_service.go](./default_service.go): service assembly, lifecycle, signal/shutdown logic, server assembly
- [default_admin.go](./default_admin.go): flat `AdminSpec` to default admin subtree
- [guard.go](./guard.go): re-exports of admin guard helpers
- [default_service_signals_unix.go](./default_service_signals_unix.go), [default_service_signals_other.go](./default_service_signals_other.go): platform-specific signal defaults

### Package `admin`

Purpose:

- explicit assembly of an admin subtree
- one capability per path
- fail-fast on invalid configuration

Start here when the question is about:

- which admin endpoint exists
- how endpoints are enabled
- path collisions
- read/write guard enforcement
- how report is assembled

Key files:

- [admin/admin.go](./admin/admin.go): builder entry, subtree-wide chain
- [admin/enable.go](./admin/enable.go): `EnableXxx` capability surface and per-capability assembly
- [admin/guard.go](./admin/guard.go): guard constructors and semantics
- [admin/mount.go](./admin/mount.go): mount helpers
- [admin/report.go](./admin/report.go): `/report` assembly

### Package `ops`

Purpose:

- individual HTTP handlers for operational endpoints
- format negotiation and data rendering
- no auth/routing/lifecycle opinions

Start here when the question is about:

- exact response behavior of `/healthz`, `/runtime`, `/tuning/*`, `/tasks/*`, `/provided`
- text vs JSON output
- endpoint-specific validation

### Package `httpx`

Purpose:

- `net/http` middleware composition and small HTTP helpers

Start here when the question is about:

- middleware order
- request id and real IP extraction
- access control middleware internals
- timeout/body-limit/CORS mechanics

Important anchor:

- [httpx/chain.go](./httpx/chain.go): `Chain(a, b, c).Handler(h) == a(b(c(h)))`

### Package `httpx/client`

Purpose:

- small HTTP client builder, independent transport, RoundTripper middlewares, I/O guard helpers

Start here when the question is about:

- outbound HTTP client defaults
- transport ownership and reuse
- response-body drain/close helpers

Key files:

- [httpx/client/client.go](./httpx/client/client.go)
- [httpx/client/options.go](./httpx/client/options.go)
- [httpx/client/middleware.go](./httpx/client/middleware.go)
- [httpx/client/io.go](./httpx/client/io.go)

### Package `rt/task`

Purpose:

- runtime-managed background tasks
- trigger tasks and periodic tasks
- graceful shutdown and bounded overlap semantics

Start here when the question is about:

- scheduling policy
- trigger semantics
- task snapshot / observability
- overlap handling and shutdown

Likely anchors:

- [rt/task/manager.go](./rt/task/manager.go)
- [rt/task/task.go](./rt/task/task.go)
- [rt/task/runtime.go](./rt/task/runtime.go)
- [rt/task/types.go](./rt/task/types.go)

### Package `rt/tuning`

Purpose:

- typed runtime knobs with lock-free reads and serialized writes

Start here when the question is about:

- register/set/reset semantics
- parsing from strings
- redaction
- callback behavior and re-entrant write rules

Likely anchors:

- [rt/tuning/tuning.go](./rt/tuning/tuning.go)
- [rt/tuning/types.go](./rt/tuning/types.go)
- [rt/tuning/parse.go](./rt/tuning/parse.go)
- [rt/tuning/internal.go](./rt/tuning/internal.go)

### Package `rt/safego`

Purpose:

- run functions with panic/error reporting rather than returning failures to the caller

Start here when the question is about:

- background goroutine failure visibility
- recover vs repanic policy
- finalizer behavior

## 5. High-Value Entry Points by Question

| Question | Start here | Then drill into |
| --- | --- | --- |
| How does a caller boot a service? | [default_service.go](./default_service.go) | [doc.go](./doc.go), [readme.md](./readme.md) |
| How is admin mounted or served standalone? | [default_service.go](./default_service.go) | [default_admin.go](./default_admin.go), [admin/mount.go](./admin/mount.go) |
| Why does a bad config panic? | [default_service.go](./default_service.go), [default_admin.go](./default_admin.go) | [admin/enable.go](./admin/enable.go), [admin/admin.go](./admin/admin.go) |
| Where is read/write access controlled? | [guard.go](./guard.go), [default_admin.go](./default_admin.go) | [admin/guard.go](./admin/guard.go), [httpx/mw_access_guard.go](./httpx/mw_access_guard.go) |
| How does `/report` know what to show? | [admin/report.go](./admin/report.go) | [admin/admin.go](./admin/admin.go), [admin/enable.go](./admin/enable.go) |
| What is the exact behavior of `/tuning/*`? | [ops/tuning.go](./ops/tuning.go) | [rt/tuning/tuning.go](./rt/tuning/tuning.go), [admin/enable.go](./admin/enable.go) |
| What is the exact behavior of `/tasks/*`? | [ops/task.go](./ops/task.go) | [rt/task/manager.go](./rt/task/manager.go), [rt/task/task.go](./rt/task/task.go) |
| How are client IPs derived behind proxies? | [httpx/mw_real_ip.go](./httpx/mw_real_ip.go) | [admin/admin.go](./admin/admin.go), [admin/guard.go](./admin/guard.go) |
| How do middleware chains compose? | [httpx/chain.go](./httpx/chain.go) | [httpx/doc.go](./httpx/doc.go) |
| What are the runtime lifecycle guarantees for tasks? | [rt/task/doc.go](./rt/task/doc.go) | [rt/task/runtime.go](./rt/task/runtime.go), [rt/task/manager.go](./rt/task/manager.go) |
| What are tuning invariants and failure modes? | [rt/tuning/doc.go](./rt/tuning/doc.go) | [rt/tuning/tuning.go](./rt/tuning/tuning.go), [rt/tuning/errors.go](./rt/tuning/errors.go) |

## 6. Important Invariants and “Do Not Misread” Notes

These are the highest-value facts to keep in working memory when editing the repo:

- Invalid assembly is generally treated as a programming error and fails fast with `panic`.
- `ReadGuard` is mandatory for `NewDefaultAdmin` and for `ServiceSpec.Admin`.
- Write endpoints are off by default. `WriteGuard != nil` alone is not enough; the relevant write group must also be explicitly enabled.
- Tuning/task write allowlists are fail-closed. An enabled write group with an empty allowlist means “enabled endpoint, but deny all writes”.
- `AdminMountPrefix` and `AdminStandaloneServer` are mutually exclusive.
- Mounted admin requires `Primary`; standalone admin supports worker-only processes.
- `ops` handlers do not own routing, auth, or lifecycle. If behavior looks “missing”, check `admin` or root assembly instead of modifying `ops` first.
- `httpx` is not a router. Most package semantics assume standard `net/http` composition.
- `rt/tuning` reads are designed to be lock-free; writes are serialized and callbacks run synchronously on the write path.
- `rt/tuning` callbacks must not perform re-entrant writes into the same `Tuning` instance.
- `rt/task` `Start` is not idempotent; `Shutdown` is safe even if `Start` never happened.
- Triggering tasks before start does not queue work for later; it is effectively a no-op / non-running path.
- `rt/safego` reports failures instead of returning them to the caller; if you are looking for returned errors, you are probably in the wrong abstraction layer.

## 7. Best Files for Examples and Expected Behavior

Prefer examples and tests when you need exact intended semantics:

- root assembly:
  - [default_service_test.go](./default_service_test.go)
  - [default_admin_test.go](./default_admin_test.go)
  - [default_service_runtime_test.go](./default_service_runtime_test.go)
- admin:
  - [admin/example_test.go](./admin/example_test.go)
  - [admin/admin_test.go](./admin/admin_test.go)
- ops:
  - [ops/example_test.go](./ops/example_test.go)
  - `ops/*_test.go`
- http middleware:
  - [httpx/mw_access_guard_example_test.go](./httpx/mw_access_guard_example_test.go)
  - [httpx/mw_cors_example_test.go](./httpx/mw_cors_example_test.go)
  - `httpx/*_test.go`
- client:
  - [httpx/client/example_test.go](./httpx/client/example_test.go)
  - `httpx/client/*_test.go`
- runtime:
  - [rt/task/example_test.go](./rt/task/example_test.go)
  - [rt/tuning/example_test.go](./rt/tuning/example_test.go)
  - [rt/safego/example_test.go](./rt/safego/example_test.go)
  - `rt/*/*_test.go`

## 8. Practical Navigation Hints for Future AI Runs

- When changing public behavior, inspect both the package `doc.go` and tests before editing implementation.
- When changing root assembly, verify whether the same rule also exists one layer lower in `admin`.
- When debugging an endpoint, trace in this order:
  1. root `zkit` assembly
  2. `admin` capability enable/mount
  3. `ops` handler behavior
  4. `rt/*` primitive if the endpoint delegates into a runtime subsystem
- When debugging access decisions, inspect both:
  - guard construction in `guard.go` or `admin/guard.go`
  - middleware enforcement in [httpx/mw_access_guard.go](./httpx/mw_access_guard.go)
- When debugging proxy/IP behavior, inspect `admin.WithRealIP(...)` plus [httpx/mw_real_ip.go](./httpx/mw_real_ip.go); do not assume headers are trusted by default.

## 9. Suggested Search Patterns

Useful repository-local search prompts:

- public service assembly:
  - `rg "NewDefaultService|ServiceSpec|HTTPServerSpec"`
- default admin assembly:
  - `rg "NewDefaultAdmin|AdminSpec|EnableLogLevelSet|TuningWritesEnabled|TaskWritesEnabled"`
- admin capability mounting:
  - `rg "Enable[A-Z]" admin`
- raw ops handlers:
  - `rg "Handler\\(" ops`
- middleware composition:
  - `rg "type Middleware|type Middlewares|func Chain" httpx`
- tasks:
  - `rg "TriggerAndWait|Overlap|Start\\(|Shutdown\\(" rt/task`
- tuning:
  - `rg "SetFromString|ResetToDefault|ResetToLastValue|ErrReentrantWrite" rt/tuning`

## 10. If You Only Read Five Files

If time or context window is constrained, read these first:

1. [readme.md](./readme.md)
2. [doc.go](./doc.go)
3. [default_service.go](./default_service.go)
4. [default_admin.go](./default_admin.go)
5. [admin/enable.go](./admin/enable.go)

That set is enough to recover most of the repository’s architectural intent and to route yourself to the right lower-level package.
