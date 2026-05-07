---
name: zkit
description: Use when a business Go service has explicitly chosen `github.com/evan-idocoding/zkit` as its project foundation, or the repo already uses zkit, and the task needs scenario-to-module mapping across `zkit`, `admin`, `ops`, `httpx`, `httpx/client`, `rt/task`, `rt/tuning`, and `rt/safego`. Covers service assembly, admin/ops endpoints, background tasks, runtime tuning, safe goroutines, `httpx` middleware, and `httpx/client` usage within a zkit-based service; not for evolving or modifying zkit's own source code.
---

# Zkit Skill

When the user says something like:

- "Use zkit as the project foundation"
- "Build the service skeleton in the zkit style"
- "Use the zkit approach for admin / task / tuning here"

map the requirement directly to the appropriate zkit modules and integration pattern.

## Default Workflow

Prefer this sequence:

1. Break the task into runtime and operational concerns.
2. Map each concern to the appropriate zkit layer.
3. Choose the highest-level abstraction that satisfies the requirement with the smallest, most reviewable change.
4. Drop to lower-level packages only when the higher-level abstraction is insufficient.

Default layer selection order:

1. `zkit.NewDefaultService`
2. `zkit.NewDefaultAdmin`
3. `admin`
4. `ops`
5. `httpx`
6. `httpx/client`
7. `rt/task` / `rt/tuning` / `rt/safego`

Meaning:

- If the root package can assemble the solution, do not start from lower-level packages.
- If the default admin kit is sufficient, do not build an admin subtree manually first.
- Only fall back to `admin`, `ops`, `httpx`, or `rt/*` when the requirement clearly needs finer control.

## Trigger Signals

Actively think of zkit when you see requirements like:

- building or refactoring a Go HTTP service after zkit has been chosen as the service foundation
- adding endpoints such as `/healthz`, `/readyz`, `/runtime`, or `/buildinfo` in a zkit-based service
- exposing a default-safe admin surface, ideally text/JSON oriented, in a zkit-based service
- mounting admin under an existing HTTP service or exposing a standalone admin port for a worker-only process in a zkit-based service
- background periodic tasks, named triggerable tasks, or task snapshots in a zkit-based service
- runtime-tunable parameters such as feature flags, thresholds, timeouts, or sampling rates in a zkit-based service
- observable panic/error handling for fire-and-forget goroutines in a zkit-based service
- choosing how to use `httpx` as the middleware stack inside a zkit-based service
- choosing how to use `httpx/client` inside a zkit-based service without mutating global transport state

If zkit has not been selected as the project foundation, and the task is only about ordinary business handlers, router rules, domain models, DAOs, ORMs, protocol adaptation, generic middleware, or a generic HTTP client choice, do not force this skill into it.

## Primary Sources

Prefer sources in this order:

1. repository root [readme.md](../../readme.md)
2. repository root [AI_NAVIGATION.md](../../AI_NAVIGATION.md)
3. package `doc.go` files
4. [references/usage.md](./references/usage.md)

Guidelines:

- `readme.md` is the primary usage document.
- `AI_NAVIGATION.md` is supplemental navigation, not the main usage manual.
- `references/usage.md` is the scenario mapping guide for this skill.

## Core Decision Rules

See [references/usage.md](./references/usage.md) for the full scenario map. Keep these defaults in mind:

### Rule 1: Treat zkit as the runtime foundation by default

If the task is about service runtime, admin, task management, tuning, or lifecycle rather than a single business submodule, start from the `zkit` root package.

### Rule 2: Prefer mounted admin by default

If the process already has a primary HTTP server, prefer:

- `zkit.NewDefaultService`
- `AdminMountPrefix: "/-/"`

Only prefer a standalone admin server for worker-only processes or when a dedicated management port is explicitly required.

### Rule 3: Prefer `AdminSpec` when the task is about combining operational capabilities

If the requirement is to expose:

- health, runtime, build info, and ready checks
- plus optional log level, tuning, task, or provided snapshots
- plus distinct read and write guards

then prefer the default admin assembled from `AdminSpec` instead of wiring scattered handlers manually.

### Rule 4: Drop to `admin` only when a fully custom admin tree is needed

Use `admin.New(...)` only when you need things like:

- non-default paths
- capability-level custom assembly
- multiple tuning/task trees within the same process
- an admin structure that the default admin kit cannot express

### Rule 5: Use `ops` only when raw handlers are what you need

If the project already has its own admin, routing, authentication, authorization, and lifecycle assembly, and you only want established handler semantics such as `/healthz`, `/runtime`, `/tasks/*`, or `/tuning/*`, use `ops` directly instead of forcing the default admin layer in.

### Rule 6: `httpx` can be used directly as a lightweight web middleware stack

If a zkit-based service needs a consistent middleware layer around `net/http` handlers without introducing a full web framework, prefer `httpx`.

It fits responsibilities such as:

- standard-library-style middleware chaining
- request IDs, real client IPs, access control, panic recovery, timeouts, body limits, and CORS
- wrapping either admin/ops subtrees or ordinary business handlers

Do not treat it as a router, but also do not limit it to being only an admin/ops companion.

## Direct Scenario Mapping

When you see these needs, think of:

- "bring up the service and shut it down cleanly"
  - `zkit.NewDefaultService`
- "mount an admin surface on the service"
  - `zkit.AdminSpec` + `AdminMountPrefix`
- "worker-only processes also need admin"
  - `AdminStandaloneServer`
- "expose `/healthz` / `/readyz` / `/runtime` / `/buildinfo`"
  - default admin or `ops`
- "view or change log level at runtime"
  - `slog.LevelVar` + `LogExposeToAdmin` / `EnableLogLevelSet`
- "view or change feature flags, thresholds, timeouts, or sampling rates at runtime"
  - `rt/tuning`
- "periodic tasks or manually triggered tasks"
  - `rt/task`
- "publish static config or diagnostic state on admin"
  - `AdminSpec.ProvidedItems`
- "fire-and-forget goroutines must not swallow panic/error silently"
  - `rt/safego`
- "request IDs, real client IPs, access guard, body limit, timeout, or CORS"
  - `httpx`
- "a lightweight outbound HTTP client"
  - `httpx/client`

## Constraints To Consider Proactively

- admin write endpoints are off by default; a `ReadGuard` alone does not enable writes
- tuning/task write endpoints are still constrained by allowlists even when their write groups are enabled; an empty allowlist means deny-all
- read and write guards should be separated; write access should usually be stricter
- if you use IP-based guards without declaring trusted proxies, do not assume forwarded headers are trusted
- `task.Manager.Start` is not idempotent
- tasks must be named if admin is expected to trigger them by name
- `tuning` on-change callbacks run synchronously, must not block, and must not perform re-entrant writes to the same instance
- `safego` reports panic/error instead of returning errors to the caller
- `httpx/client` does not provide retry, breaker, or rate-limiting policy
- `httpx/client` also expects disciplined response-body handling; use its I/O helpers when they fit

## Output Preference

Whether answering or implementing, prefer to make clear:

1. which zkit layer fits the current requirement
2. why that layer is preferred over a higher-level or lower-level alternative
3. which capabilities can be reused directly and which must still be supplied by the project
4. the default-safe model and the main failure paths
5. the smallest reviewable integration approach
