# zkit

Standard-library-first runtime base for http services, with default-safe building blocks and an explicit ops-admin endpoint.

Documentation: [pkg.go.dev](https://pkg.go.dev/github.com/evan-idocoding/zkit)

## Contents

- [Why zkit](#why-zkit)
  - [Batteries included](#batteries-included)
- [Requirements](#requirements)
- [Install](#install)
- [Quick start](#quick-start)
  - [Example 1: HTTP service + mounted admin (recommended)](#example-1-http-service--mounted-admin-recommended)
  - [Example 2: Worker-only + standalone admin server](#example-2-worker-only--standalone-admin-server)
  - [Example 3: Enable runtime controls (log level, tuning, tasks, provided snapshot)](#example-3-enable-runtime-controls-log-level-tuning-tasks-provided-snapshot)
- [Architecture (high level)](#architecture-high-level)
- [Principles](#principles)
- [What it is not](#what-it-is-not)
- [Default admin endpoints](#default-admin-endpoints)
- [Security model (read vs write)](#security-model-read-vs-write)
- [Stability & compatibility (v0.1.x)](#stability--compatibility-v01x)
- [License](#license)
- [Support](#support)

## Why zkit

In production, teams often fall into one of two extremes:

- Adopt a heavy framework, and pay with glue-code noise, architectural lock-in, implicit “magic”, reduced control, and a larger supply-chain surface.
- Build everything by hand, and pay with repetitive scaffolding, inconsistent conventions, and reliability gaps that reappear across services.

zkit takes a middle path: it embraces the Go standard library and tries hard not to interfere with your existing `net/http` habits, while smoothing out the boring runtime scaffolding and keeping operational knobs explicit and default-safe.

### Batteries included

zkit ships a small set of operational building blocks you typically end up needing around `net/http`:

- Service lifecycle (graceful start/shutdown)
- Safe goroutines (panic/error observable runners)
- Companion tasks that live alongside your primary HTTP service (periodic jobs and on-demand triggers)
- Runtime tuning: typed, runtime-settable parameters (“knobs”) for operational control (e.g. feature toggles, thresholds, timeouts, sampling rates)
- Publish process-owned diagnostic/config state for debugging and incident response as text/JSON (e.g. a static configuration view)

Instead of leaving you to wire these pieces together yourself, zkit provides a mountable (or standalone) admin surface that unifies them behind a small, operator-friendly control plane with text/JSON outputs—ready to use with minimal wiring.

## Requirements

- Go 1.21+

## Install

```bash
go get github.com/evan-idocoding/zkit@latest
```

Import:

```go
import "github.com/evan-idocoding/zkit"
```

## Quick start

zkit’s primary entry point is `NewDefaultService`: it assembles a runnable, shutdownable service (servers + optional tasks/tuning/log level), with admin either mounted under a prefix or served on a dedicated port.

### Example 1: HTTP service + mounted admin (recommended)

Mount admin under `/-/` on the same server:

```go
mux := http.NewServeMux()
mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("hello"))
})

svc := zkit.NewDefaultService(zkit.ServiceSpec{
	Primary: &zkit.HTTPServerSpec{
		Addr:    ":8080",
		Handler: mux,
	},
	Admin: &zkit.AdminSpec{
		// For demos only. In production, protect admin with token/IP-based guards.
		ReadGuard: zkit.AllowAll(),
	},
	AdminMountPrefix: "/-/",
})

_ = svc.Run(context.Background())
```

### Example 2: Worker-only + standalone admin server

If your process has no primary HTTP server (e.g. a worker), serve admin on its own port:

```go
// A minimal worker process: run background jobs, and expose admin on a dedicated port.
mgr := task.NewManager()

// Example background job (runs periodically).
_ = mgr.MustAdd(task.Every(10*time.Second, func(ctx context.Context) error {
	// ... do work ...
	return nil
}), task.WithName("heartbeat"))

svc := zkit.NewDefaultService(zkit.ServiceSpec{
	TasksManager:       mgr,
	TasksExposeToAdmin: true,
	Admin: &zkit.AdminSpec{
		ReadGuard: zkit.Tokens([]string{"read-token"}),
	},
	// Handler may be omitted; zkit injects the admin handler for AdminStandaloneServer.
	AdminStandaloneServer: &zkit.HTTPServerSpec{
		Addr: ":8081",
	},
})

_ = svc.Run(context.Background())
```

### Example 3: Enable runtime controls (log level, tuning, tasks, provided snapshot)

This example demonstrates the opt-in “runtime control plane” capabilities on the admin surface.

```go
// ---- sources (owned by your process) ----
var lv slog.LevelVar   // slog.LevelVar backing /log/level
tu := tuning.New()    // runtime knobs backing /tuning/*
mgr := task.NewManager() // background jobs backing /tasks/*

// Register a runtime-tunable knob.
_, _ = tu.Bool("feature.x", false)
// Example runtime change (in production you may do this via the admin endpoint /tuning/set).
_ = tu.SetFromString("feature.x", "on")

// Register an on-demand job (triggerable by name).
_ = mgr.MustAdd(
	task.Trigger(func(ctx context.Context) error {
		// ... do work ...
		return nil
	}),
	task.WithName("rebuild-index"),
)

// ---- access control ----
read := zkit.Tokens([]string{"read-token"})   // for GET/HEAD endpoints
writeTokens := httpx.NewAtomicTokenSet() // hot-update token set (rotate without redeploy)
writeTokens.Update([]string{"write-token"})
write := zkit.HotTokens(writeTokens) // for POST endpoints (stronger token)

// ---- business handlers ----
mux := http.NewServeMux()
mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("hello"))
})

// ---- admin assembly (reads + opt-in writes + provided snapshot) ----
adminSpec := zkit.AdminSpec{
	ReadGuard:          read,
	WriteGuard:         write,
	EnableLogLevelSet:  true,
	TuningWritesEnabled: true,
	TuningWriteAllowPrefixes: []string{"feature."},
	TaskWritesEnabled:  true,
	TaskWriteAllowNames:      []string{"rebuild-index"},
	ProvidedItems: map[string]any{
		// Common use case: publish static configuration snapshots.
		"config": map[string]any{"env": "prod"},
	},
}

// ---- service assembly ----
svc := zkit.NewDefaultService(zkit.ServiceSpec{
	Primary:            &zkit.HTTPServerSpec{Addr: ":8080", Handler: mux},
	LogLevelVar:        &lv,
	LogExposeToAdmin:   true,
	Tuning:             tu,
	TuningExposeToAdmin: true,
	TasksManager:       mgr,
	TasksExposeToAdmin: true,
	Admin:              &adminSpec,
	AdminMountPrefix:   "/-/",
})

_ = svc.Run(context.Background())
```

## Architecture (high level)

```text

                           +------------------------+
                           |      Your services     |
                           +-----------+------------+
                                       |
                  +--------------------+--------------------+
                  |                    |                    |
         +--------v---------+          |         +----------v---------+
         |  DefaultService  |------------------->|     DefaultAdmin   |
         +--------+---------+          |         +----------+---------+
                  |                    |                    |
                  |                    |                    |
                  v                    v                    |
  +-------------------------------------------------------------------------+
  |                        Base modules (opt-in)            |               |
  |                                                         |               |
  |                                                         |  +---------+  |
  |                                                         |->|  admin  |  |
  |                                                            +---------+  |
  |                                                                         |
  |  +--------+   +---------+    +----------+   +-----------+               |
  |  |   ops  |   |  httpx  |    | rt/task  |   | rt/tuning |               |
  |  +--------+   +---------+    +----------+   +-----------+               |
  |              +------------+  +-----------+                              |
  |              |  mw|client |  | rt/safego |                              |
  |              +------------+  +-----------+                              |
  +-------------------------------------------------------------------------+

```

Don't be intimidated by the diagram above: `DefaultService` and `DefaultAdmin` don't add extra capabilities—they simply assemble the base modules below with default parameters for an out-of-the-box experience.

You can skip them entirely and use the base modules directly. Everything is optional, and each piece can be used on its own.

When you need finer-grained control, use the subpackages directly:

- `admin`: admin subtree assembler (explicit EnableXxx + explicit Guard)
- `ops`: operational handlers (health/runtime/buildinfo/tasks/tuning/loglevel/provided snapshots)
- `httpx`: net/http middleware chain helpers (recover/request id/real ip/access guard/timeout/body limit/cors)
- `httpx/client`: HTTP client builder (independent transport + RoundTripper middlewares + I/O guard helpers)
- `rt/task`: background task primitives + manager + snapshot/trigger-and-wait
- `rt/tuning`: runtime-tunable parameters (typed vars, lock-free reads)
- `rt/safego`: panic/error observable goroutine runner

Note: admin endpoints are text/JSON (not HTML pages).

## Principles

- Standard library first, standard-library-oriented
- Default-safe, with sensible defaults
- Minimize implicit behavior and “magic”; keep knobs explicit and configurable
- Out-of-the-box as a whole, while still encouraging you to use modules independently as building blocks

## What it is not

- Not a framework: no router, no DSL, no new programming model, and no intrusion into your business code.
- Not a kitchen-sink: zero dependencies beyond the standard library; focused on ops, not business features.
- Not an observability or policy suite: no automatic oTel/Prom wiring; no built-in retries/circuit breakers/rate limits.
- zkit includes an HTTP middleware subsystem (primarily used by the admin surface), and you can also use it directly with the standard library to build your own handler stack. But it does not aim to replace whatever router/mux/middleware chain you already prefer.

## Default admin endpoints

zkit’s default admin surface exposes text/JSON endpoints (not HTML pages).

- **Always-on reads** (guarded by `AdminSpec.ReadGuard`): `/report`, `/healthz`, `/readyz`, `/buildinfo`, `/runtime`.
- **Optional reads** (available when the corresponding sources are wired): `/log/level`, `/tuning/snapshot`, `/tuning/overrides`, `/tuning/lookup`, `/tasks/snapshot`, `/provided`.
- **Writes**: off by default; when enabled, endpoints are: `/log/level/set`, `/tuning/set`, `/tuning/reset-default`, `/tuning/reset-last`, `/tasks/trigger`, `/tasks/trigger-and-wait`. They require `AdminSpec.WriteGuard`, explicit enable flags, and allowlists where applicable (see “Security model” below).
- **Output formats**: defaults to text; use `?format=text` or `?format=json` (where supported).

## Security model (read vs write)

zkit’s admin surface is designed to be default-safe:

- **Reads are explicit and guarded**: `AdminSpec.ReadGuard` is required and protects all read endpoints. A nil guard is an assembly error and will panic (fail-fast).
- **Writes are off by default**: `AdminSpec.WriteGuard == nil` disables all write endpoints.
- **Write guard and allowlists**: when `WriteGuard` is non-nil, enable write groups explicitly via `EnableLogLevelSet`, `TuningWritesEnabled`, `TaskWritesEnabled`; allowlist (empty = deny-all) applies for tuning and task writes.
- **Real IP is default-safe**: if trusted proxies are not configured, proxy headers are ignored and IP checks fall back to `RemoteAddr`.

## Stability & compatibility (v0.1.x)

zkit is experimental in v0.1.x. In general, Go APIs may change.

That said, we try to keep a small set of **external contracts** stable to reduce operational churn:

- **Default admin paths (relative to the admin subtree)**:
  - Always-on reads: `/report`, `/healthz`, `/readyz`, `/buildinfo`, `/runtime`
  - Opt-in capabilities (when wired/enabled): `/log/*`, `/tuning/*`, `/tasks/*`, `/provided`
- **Format negotiation** via `?format=text|json` (where supported).
- **Security posture**: reads are guarded; writes are off by default; write endpoints require an explicit guard, explicit enable flags (`EnableLogLevelSet`, `TuningWritesEnabled`, `TaskWritesEnabled`), and allowlists (fail-closed).

Everything else should be treated as best-effort until v1.

## License

MIT License. See [LICENSE](LICENSE).

## Support

Issues(pls open a GitHub issue) and PRs are welcome!




