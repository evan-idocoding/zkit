# zkit Usage Reference

## 1. What zkit is

- module path: `github.com/evan-idocoding/zkit`
- repository: `https://github.com/evan-idocoding/zkit`
- Go package docs: `https://pkg.go.dev/github.com/evan-idocoding/zkit`
- repository overview document: `readme.md`
- repository AI navigation document: `AI_NAVIGATION.md`

zkit should be understood as a runtime and operations foundation for Go services, not as a business framework.

It primarily addresses these recurring concerns:

- service lifecycle
- a default-safe admin/ops surface
- background tasks
- runtime tuning
- observable goroutine panic/error handling
- standard-library-style middleware
- lightweight outbound HTTP client helpers

Its core stance is:

- standard-library-first
- explicit assembly
- default-safe
- does not try to take over the project's router or framework choices

## 2. How to interpret "use zkit as the project foundation"

Interpret that instruction as:

1. start with `zkit.NewDefaultService` for service assembly
2. look at `zkit.AdminSpec` first for the admin surface
3. drop to `admin` only when the default admin layer is not enough
4. use `ops` only when raw handlers are what the project needs
5. map task, tuning, and safe goroutine requirements to `rt/task`, `rt/tuning`, and `rt/safego`
6. map middleware and HTTP client concerns to `httpx` and `httpx/client`

In practice this usually means:

- do not re-implement runtime capabilities that zkit already provides
- prefer zkit abstractions when they fit cleanly
- add custom implementation only when the requirement exceeds zkit's abstraction boundary

## 3. Default scenario-to-module mapping

### Scenario: a new ordinary HTTP service

Prefer:

- `zkit.NewDefaultService`
- `AdminMountPrefix: "/-/"`
- `AdminSpec.ReadGuard`

Why:

- this is the highest-level default usage path
- it gives a consistent service lifecycle and admin surface quickly
- it is the best default for a project-wide service foundation

### Scenario: a new worker-only process

Prefer:

- `zkit.NewDefaultService`
- `AdminStandaloneServer`
- `rt/task`

Why:

- there is no primary HTTP server, but the process still often needs admin and background task support

### Scenario: add health/runtime/buildinfo/admin capability to an existing service

Prefer:

- `zkit.NewDefaultAdmin` or `AdminSpec` if the default admin structure is acceptable
- `ops` if the project already has a mature assembly layer

Decision rule:

- if you need a complete admin subtree, use `zkit` or `admin`
- if you only need individual handlers, use `ops`

### Scenario: background periodic tasks or manually triggered tasks

Prefer:

- `rt/task`

Common use cases:

- cache refresh
- index rebuild
- periodic synchronization
- manually triggered compensation work

If admin observability or triggering is also needed:

- `TasksManager`
- `TasksExposeToAdmin`
- `TaskWritesEnabled`
- `TaskWriteAllowNames` / `TaskWriteAllowPrefixes`

### Scenario: runtime-tunable parameters

Prefer:

- `rt/tuning`

Common use cases:

- feature flags
- thresholds
- sampling rates
- rate-limit thresholds
- timeout values

If admin visibility or mutation is also needed:

- `TuningExposeToAdmin`
- `TuningWritesEnabled`
- `TuningWriteAllowKeys` / `TuningWriteAllowPrefixes`

### Scenario: observable panic/error handling in background goroutines

Prefer:

- `rt/safego`

Common use cases:

- fire-and-forget asynchronous work
- background workflows coordinated with `sync.WaitGroup`
- companion goroutines where panic must not disappear silently

### Scenario: a standard-library-style middleware stack

Prefer:

- `httpx` within a zkit-based service

Common mapping:

- request IDs: `RequestID`
- real client IP extraction: `RealIP`
- access control: `AccessGuard`
- panic recovery: `Recover`
- request timeouts: `Timeout`
- body size limits: `BodyLimit`
- CORS: `CORS`

Built-in middleware summary:

- `Recover`
  - recovers panics and reports panic information
- `RequestID`
  - propagates or generates a request ID, writes it back to the response header, and stores it in context
- `RealIP`
  - extracts the client IP from trusted proxy headers for logging, auth, or IP allowlists
- `AccessGuard`
  - performs request admission control using token checks, IP checks, or a custom predicate
- `Timeout`
  - derives a request context deadline; it does not automatically generate a timeout response body for you
- `BodyLimit`
  - enforces request body size limits to guard against oversized or abusive requests
- `CORS`
  - writes CORS response headers and handles preflight requests

### Scenario: a lightweight HTTP client

Prefer:

- `httpx/client` within a zkit-based service

Common use cases:

- an HTTP client with its own transport clone
- setting common headers for all requests
- building the smallest possible outbound client wrapper while staying close to standard library usage
- reading bounded response bodies with `ReadAllAndCloseLimit`
- draining and closing response bodies with `DrainAndClose` when connection reuse matters

Do not interpret it as:

- a retry framework
- a circuit breaker framework
- a rate limiting framework
- a REST DSL

## 4. What each module is for

### `zkit` root package

Responsibility:

- assembles servers, admin, tasks, tuning, and log level exposure into a runnable service

Think of it first when:

- the project needs a consistent service foundation
- the service needs a unified lifecycle
- the admin surface should be part of the default runtime assembly instead of being wired from scratch

Primary entry points:

- `NewDefaultService`
- `NewDefaultAdmin`

### `admin`

Responsibility:

- assembles an admin subtree from explicit capabilities

Think of it when:

- the default admin kit is not detailed enough
- paths must be customized
- capability-level assembly is needed
- read/write guard policies must be defined explicitly

Characteristics:

- explicit `EnableXxx`
- explicit `Guard`
- fail-fast on assembly errors

### `ops`

Responsibility:

- provides raw operational handlers without routing, auth, or lifecycle ownership

Think of it when:

- the project already has its own higher-level assembly
- only the handler semantics for health, runtime, tasks, tuning, log level, or provided snapshots are needed

### `httpx`

Responsibility:

- provides a middleware chain and a small set of standard-library-style middlewares

Think of it when:

- request-level infrastructure is needed without introducing framework-heavy abstractions
- a zkit-based service needs a lightweight web middleware stack while staying in `net/http`
- admin or sensitive endpoints need protection
- a group of handlers needs consistent timeout, recover, request ID, body limit, or CORS behavior

Middleware summary:

- `Recover`
  - recovers and reports panics
- `RequestID`
  - propagates or generates request IDs
- `RealIP`
  - extracts the real client IP from trusted proxy headers
- `AccessGuard`
  - enforces request admission using tokens, IPs, or custom checks
- `Timeout`
  - derives a deadline on the request context
- `BodyLimit`
  - limits request body size
- `CORS`
  - writes CORS headers and handles preflight

### `httpx/client`

Responsibility:

- builds an `*http.Client` without mutating global transport state
- provides `RoundTripper` middleware and I/O helpers

Think of it when:

- a zkit-based service needs a lightweight outbound HTTP client abstraction
- the code should stay close to standard library habits while adding minimal structure

Typical helpers to remember:

- `SetHeader` for request-wide header injection
- `ReadAllAndCloseLimit` for bounded body reads
- `DrainAndClose` when the body is not otherwise consumed and connection reuse still matters

### `rt/task`

Responsibility:

- manages background tasks

Think of it when:

- periodic tasks are needed
- named on-demand triggers are needed
- task snapshots are needed
- operators should be able to trigger tasks via admin

### `rt/tuning`

Responsibility:

- provides runtime-tunable parameters

Think of it when:

- feature flags or thresholds should be hot-adjustable online
- the project benefits from explicit semantics with lock-free reads and serialized writes

### `rt/safego`

Responsibility:

- makes panic/error from background goroutines observable

Think of it when:

- the service has fire-and-forget work
- using bare `go func(){...}()` would hide failures too easily

## 5. Key constraints and failure paths

Consider these points proactively:

- `ReadGuard` is mandatory protection for admin read capabilities
- `WriteGuard == nil` means all admin write endpoints are disabled
- tuning/task write groups still deny all writes if the allowlist is empty
- `/provided` is sensitive by default and should be exposed only intentionally
- IP-based guards fall back to `RemoteAddr` if trusted proxies are not configured
- task triggers before start do not queue for later execution
- `task.Start` is not idempotent
- slow tuning callbacks block the write path
- tuning callbacks must not perform re-entrant writes to the same instance
- `safego` is a reporting abstraction, not a return-error abstraction
- `httpx/client` is not a high-level SDK with built-in timeout/retry policy sets
- `httpx/client` safety also depends on proper response-body handling, not just client construction

## 6. Non-goals

Do not reach for zkit first for:

- complex business routing design
- ORM / repository / DAO abstractions
- config-center protocol integration
- distributed transactions
- full service-governance platforms
- complex front-end admin UIs

## 7. Recommended implementation direction

If the task is fundamentally about adding a runtime foundation to a business service, prefer:

1. start with `NewDefaultService`
2. use mounted admin as the default shape
3. add `task`, `tuning`, and `slog.LevelVar` only as needed
4. separate read and write permissions
5. drop to `admin`, `ops`, `httpx`, or `rt/*` only when the default shape is insufficient
