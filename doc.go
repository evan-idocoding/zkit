// Package zkit provides default-safe assembly helpers for building net/http services with an
// operator-friendly admin surface.
//
// The main entry points are:
//   - NewDefaultService: assemble a runnable, shutdownable Service (servers + optional tasks/tuning/log level),
//     with admin either mounted under a prefix or served on a dedicated port.
//   - NewDefaultAdmin: assemble the admin subtree as an http.Handler (fixed v1 paths).
//
// You can start using zkit by only calling NewDefaultService and then writing your business handlers
// as usual. When you need more control, zkit exposes lower-level building blocks in subpackages
// (listed below).
//
// # Quick start: primary server + mounted admin
//
// This example mounts admin under "/-/" on the same server:
//
//	mux := http.NewServeMux()
//	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
//		_, _ = w.Write([]byte("hello"))
//	})
//
//	svc := zkit.NewDefaultService(zkit.ServiceSpec{
//		Primary: &zkit.HTTPServerSpec{
//			Addr:    ":8080",
//			Handler: mux,
//		},
//		Admin: &zkit.AdminSpec{
//			// For demos only. In production, protect admin with token/IP-based guards.
//			ReadGuard: zkit.AllowAll(),
//		},
//		AdminMountPrefix: "/-/",
//	})
//
//	_ = svc.Run(context.Background())
//
// With the default admin kit, the always-on read endpoints include:
//   - /report
//   - /healthz
//   - /readyz
//   - /buildinfo
//   - /runtime
//
// # Security model (read vs write)
//
// zkit's admin endpoints are designed to be default-safe:
//
//   - Reads are explicit and guarded:
//
//   - AdminSpec.ReadGuard is required and protects all read endpoints.
//
//   - A nil ReadGuard is treated as an assembly error and will panic (fail-fast).
//
//   - Writes are off by default:
//
//   - AdminSpec.WriteGuard == nil disables all write endpoints.
//
//   - If WriteGuard is non-nil, enable write groups explicitly: EnableLogLevelSet for /log/level/set;
//     TuningWritesEnabled / TaskWritesEnabled for tuning and task writes. Allowlist (empty = deny-all) applies for tuning/task writes.
//
//   - /provided (custom diagnostic snapshot) is disabled by default because it is typically more sensitive;
//     enable it explicitly by setting AdminSpec.ProvidedItems to a non-nil map.
//
// Tip: it is common to use separate guards for reads and writes (e.g. a read token and a stronger write token).
//
// # Admin serving mode: mount vs standalone (mutually exclusive)
//
// NewDefaultService supports two admin modes:
//
//   - Mount: route requests to admin first when URL path matches the reserved prefix.
//
//   - Prefix defaults to "/-/".
//
//   - If a request hits the base path without the trailing slash (e.g. "/-"), it is redirected to the
//     normalized prefix (e.g. "/-/") with HTTP 307.
//
//   - Mount requires ServiceSpec.Primary (otherwise it panics).
//
//   - Standalone: run admin on its own managed server (usually a dedicated port).
//
//   - This is required for "worker-only" processes with no primary server.
//
// # Service lifecycle and defaults
//
// Service provides Start/Wait/Shutdown/Run:
//   - Start: starts managed components (tasks if enabled) and starts serving on managed servers (not idempotent)
//   - Wait: waits until the service fully stops (idempotent)
//   - Shutdown: triggers graceful shutdown (idempotent), using ShutdownTimeout (default: 30s)
//   - Run: Start → wait for an exit condition (ctx.Done or OS signal) → Shutdown → Wait
//
// Signal handling in Run:
//   - Enabled by default (SignalsDisable=false).
//   - Default signals:
//   - Unix: SIGINT + SIGTERM
//   - Non-Unix: os.Interrupt
//
// HTTP server defaults (only when you use Addr+Handler and let zkit build *http.Server):
//   - ReadHeaderTimeout: 5s
//   - IdleTimeout: 60s
//
// If you provide HTTPServerSpec.Server, zkit does not override your timeouts/BaseContext/ErrorLog/etc.
//
// # Optional components: tasks / tuning / log level
//
// NewDefaultService can host these optional components and (optionally) expose them to admin:
//
//   - Tasks: TasksManager!=nil = enabled and started; TasksExposeToAdmin = wire into admin when admin enabled.
//
//   - Tuning: Tuning != nil = enabled; TuningExposeToAdmin = wire into admin when admin enabled.
//
//   - Log: LogLevelVar!=nil = enabled; LogExposeToAdmin = wire /log/level (read) and optionally /log/level/set (write).
//
// Note: if admin config (especially write groups) requires a component (e.g. task writes require a task manager),
// NewDefaultService may create the component to satisfy assembly, but write capabilities still require explicit
// enabling via AdminSpec.WriteGuard and the enable flags (EnableLogLevelSet, TuningWritesEnabled, TaskWritesEnabled) plus allowlists.
//
// # Spec reference (parameters at a glance)
//
// ServiceSpec (NewDefaultService): SignalsDisable, Signals, ShutdownTimeout, Primary, Extra, Admin (*AdminSpec), AdminMountPrefix, AdminStandaloneServer, TasksManager, TasksExposeToAdmin, Tuning, TuningExposeToAdmin, LogLevelVar, LogExposeToAdmin, OnStart, OnShutdown, OnServeError.
//
// AdminSpec (Admin field / NewDefaultAdmin): ReadGuard (required), TrustedProxies, TrustedHeaders, ReadyChecks, LogLevelVar, Tuning, TaskManager, TuningReadAllowPrefixes/Keys/Func, TaskReadAllowPrefixes/Names/Func, ProvidedItems, ProvidedMaxBytes, WriteGuard, EnableLogLevelSet, TuningWritesEnabled, TuningWriteAllowPrefixes/Keys/Func, TaskWritesEnabled, TaskWriteAllowPrefixes/Names/Func.
//
// HTTPServerSpec (Primary, Extra, AdminStandaloneServer): Name, Critical, Server (or Addr+Handler).
//
// # Building blocks (when you need finer-grained control)
//
//   - github.com/evan-idocoding/zkit/admin: admin subtree assembler (Option-based, explicit EnableXxx + explicit Guard)
//   - github.com/evan-idocoding/zkit/ops: operational handlers (health/runtime/buildinfo/tasks/tuning/loglevel/provided snapshots)
//   - github.com/evan-idocoding/zkit/httpx: net/http middleware chain helpers
//   - github.com/evan-idocoding/zkit/httpx/client: HTTP client builder (independent transport + RoundTripper middlewares)
//   - github.com/evan-idocoding/zkit/rt/task: background task primitives
//   - github.com/evan-idocoding/zkit/rt/tuning: runtime-tunable parameters
//   - github.com/evan-idocoding/zkit/rt/safego: panic/error observable goroutine runner
package zkit
