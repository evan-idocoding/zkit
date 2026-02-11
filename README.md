# zkit

A standard-library-oriented runtime base layer for `net/http` services: default-safe building blocks and out-of-the-box ops surface assembly.

Documentation: [pkg.go.dev](https://pkg.go.dev/github.com/evan-idocoding/zkit)

## Why zkit

In production, teams often fall into one of two extremes:

- Adopt a heavy framework, and pay with glue-code noise, architectural lock-in, implicit “magic”, reduced control, and a larger supply-chain surface.
- Build everything by hand, and pay with repetitive scaffolding, inconsistent conventions, and reliability gaps that reappear across services.

zkit takes a third path. It embraces the Go standard library and tries hard not to interfere with your existing `net/http` habits, while smoothing out the boring scaffolding and providing a safe, controllable runtime base layer for your services.

## Principles

- **Standard-library-oriented with zero external dependencies**: a toolkit, not a framework; no router, no DSL, no new programming model. It also avoids baking in policy-heavy HTTP client features (retries/circuit breakers/rate-limits).
- **Default-safe ops surface**: reads must be explicitly guarded; writes are off by default.
- **All knobs are explicit and configurable**: `Default*` provides convenience defaults, but you can override parameters or assemble from building blocks.

## Status

zkit is currently in an **experimental** stage (targeting **v0.1.x**). APIs may change as the project evolves towards production-grade stability.

Code size snapshot (approx., as of v0.1.x):

- Core (non-test): ~10k LOC
- Tests: ~8k LOC
- Total: ~18k LOC

(LOC here is an approximate “code-ish” count: non-empty lines with comments removed; exact numbers vary by counting method.)

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

Note: admin endpoints are text/JSON (not HTML pages).

## Quick start

zkit’s primary entry point is `NewDefaultService`: it assembles a runnable, shutdownable service (servers + optional tasks/tuning/log level), with admin either mounted under a prefix or served on a dedicated port.

### Primary server + mounted admin (recommended for typical HTTP services)

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
	Admin: &zkit.ServiceAdminSpec{
		Spec: zkit.AdminSpec{
			// For demos only. In production, protect admin with token/IP-based guards.
			ReadGuard: admin.Tokens([]string{"read-token"}),
		},
		Mount: &zkit.AdminMountSpec{Prefix: "/-/"},
	},
})

_ = svc.Run(context.Background())
```

### Worker-only process + standalone admin server (no primary server)

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
	Tasks: &zkit.TasksSpec{
		Manager:       mgr,
		ExposeToAdmin: true,
	},
	Admin: &zkit.ServiceAdminSpec{
		Spec: zkit.AdminSpec{
			ReadGuard: admin.Tokens([]string{"read-token"}),
		},
		Server: &zkit.HTTPServerSpec{
			Addr: ":8081",
		},
	},
})

_ = svc.Run(context.Background())
```

### Example: enable log level + tuning + tasks + provided snapshot

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
read := admin.Tokens([]string{"read-token"})   // for GET/HEAD endpoints
writeTokens := httpx.NewAtomicTokenSet() // hot-update token set (rotate without redeploy)
writeTokens.Update([]string{"write-token"})
write := admin.HotTokens(writeTokens) // for POST endpoints (stronger token)

// ---- business handlers ----
mux := http.NewServeMux()
mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
	_, _ = w.Write([]byte("hello"))
})

// ---- admin assembly (reads + opt-in writes + provided snapshot) ----
adminSpec := zkit.AdminSpec{
	ReadGuard: read,
	Writes: &zkit.AdminWriteSpec{
		Guard:             write,
		EnableLogLevelSet: true,
		Tuning: &zkit.TuningWriteSpec{Access: admin.TuningAccessSpec{
			AllowPrefixes: []string{"feature."},
		}},
		Tasks: &zkit.TaskWriteSpec{Access: admin.TaskAccessSpec{
			AllowNames: []string{"rebuild-index"},
		}},
	},
}
adminSpec.Provided.Enable = true
adminSpec.Provided.Items = map[string]any{
	// Common use case: publish static configuration snapshots.
	"config": map[string]any{"env": "prod"},
}

// ---- service assembly ----
svc := zkit.NewDefaultService(zkit.ServiceSpec{
	Primary: &zkit.HTTPServerSpec{Addr: ":8080", Handler: mux},
	Log:     &zkit.LogSpec{LevelVar: &lv, ExposeToAdmin: true},
	Tuning:  &zkit.TuningSpec{Tuning: tu, ExposeToAdmin: true},
	Tasks:   &zkit.TasksSpec{Manager: mgr, ExposeToAdmin: true},
	Admin: &zkit.ServiceAdminSpec{
		Spec:  adminSpec,
		Mount: &zkit.AdminMountSpec{Prefix: "/-/"},
	},
})

_ = svc.Run(context.Background())
```

## Default admin endpoints

With the default admin kit, the always-on read endpoints include:

- `/report`
- `/healthz`
- `/readyz`
- `/buildinfo`
- `/runtime`

Optional endpoints become available only when their sources are wired and enabled (e.g. log level var, tuning registry, task manager, provided snapshot).

### Optional capabilities (opt-in)

zkit’s admin surface can also expose a small runtime control plane when you wire the sources in (still guarded; writes remain off by default):

- **Log level**: read (and optionally set) a `slog.LevelVar` via `/log/level` (and `/log/level/set` when enabled), useful for incident debugging without redeploying.
- **Tuning**: runtime-tunable parameters via `/tuning/*` (writes require an explicit allowlist), useful for feature flags and operational thresholds.
- **Tasks**: observe and trigger background tasks via `/tasks/*` (writes require an explicit allowlist), useful for on-demand jobs like reindex/rebuild/refresh.
- **Provided snapshot**: publish your own diagnostic snapshot via `/provided` (disabled by default; enable explicitly), useful for exposing app-specific state for debugging or serving static configuration snapshots.

### Output formats

Most endpoints default to text output. You can override per request with:

- `?format=text`
- `?format=json`

## Security model (read vs write)

zkit’s admin surface is designed to be default-safe:

- **Reads are explicit and guarded**: `AdminSpec.ReadGuard` is required and protects all read endpoints. A nil guard is an assembly error and will panic (fail-fast).
- **Writes are off by default**: `AdminSpec.Writes == nil` disables all write endpoints.
- **Write guard is required**: if `AdminSpec.Writes != nil`, then `Writes.Guard` is required (nil => panic).
- **Write allowlists are fail-closed**:
  - Tuning writes require an allowlist; empty access denies all writes.
  - Task writes require an allowlist; empty access denies all writes.
- **Real IP is default-safe**: if trusted proxies are not configured, proxy headers are ignored and IP checks fall back to `RemoteAddr`.

## Packages (building blocks)

When you need finer-grained control, use the subpackages directly:

- `admin`: admin subtree assembler (explicit EnableXxx + explicit Guard)
- `ops`: operational handlers (health/runtime/buildinfo/tasks/tuning/loglevel/provided snapshots)
- `httpx`: net/http middleware chain helpers (recover/request id/real ip/access guard/timeout/body limit/cors)
- `httpx/client`: HTTP client builder (independent transport + RoundTripper middlewares + I/O guard helpers)
- `rt/task`: background task primitives + manager + snapshot/trigger-and-wait
- `rt/tuning`: runtime-tunable parameters (typed vars, lock-free reads)
- `rt/safego`: panic/error observable goroutine runner

## Stability & compatibility (v0.1.x)

zkit is experimental in v0.1.x. In general, Go APIs may change.

That said, we try to keep a small set of **external contracts** stable to reduce operational churn:

- **Default admin paths (relative to the admin subtree)**:
  - Always-on reads: `/report`, `/healthz`, `/readyz`, `/buildinfo`, `/runtime`
  - Opt-in capabilities (when wired/enabled): `/log/*`, `/tuning/*`, `/tasks/*`, `/provided`
- **Format negotiation** via `?format=text|json` (where supported).
- **Security posture**: reads are guarded; writes are off by default; write endpoints require an explicit guard and explicit allowlists (fail-closed).

Everything else should be treated as best-effort until v1.

## Support

Please open a GitHub issue.

## Contributing

Issues and PRs are welcome.