// Package admin assembles a secure, explicit, side-loadable admin subtree (http.Handler).
//
// # What is admin?
//
// admin is an *assembly layer* that wires together operational handlers provided by zkit
// (primarily package ops, plus a small set of httpx middlewares) into a single net/http
// subtree handler.
//
// You mount the returned handler anywhere in your existing HTTP stack, without affecting
// your business routing/middlewares:
//
//	mux := http.NewServeMux()
//	mux.Handle("/-/", admin.New(...))
//	mux.Handle("/", yourBusinessHandler)
//
// admin is designed for operators. It only outputs text/json (no UI / JS).
//
// # Design priorities
//
//  1. Security (default-safe)
//  2. Control (explicit enable/disable, explicit guards, fail-fast on assembly errors)
//  3. Stability (paths and outputs aim to be stable)
//  4. Ease-of-use (reasonable defaults, low mental model)
//
// # Quick start
//
// A minimal setup is: pick a Guard, enable a few capabilities, and mount the handler:
//
//	tokenGuard := admin.Tokens([]string{"s3cr3t"})
//	h := admin.New(
//		admin.EnableHealthz(admin.HealthzSpec{Guard: tokenGuard}),
//		admin.EnableRuntime(admin.RuntimeSpec{Guard: tokenGuard}),
//	)
//	mux := http.NewServeMux()
//	mux.Handle("/-/", h)
//
// # Core rules (important)
//
// ## Explicit enable + explicit guard
//
// Nothing is mounted unless explicitly enabled via EnableXxx options.
// Every enabled capability must have a non-nil Guard (nil is an assembly error and will panic).
//
// ## Capability = Path
//
// Each capability is identified by its path.
//
// Read endpoints support GET and HEAD on the same path.
//
// ## Assembly errors are fail-fast
//
// admin treats invalid configuration as a programming/assembly error and fails fast (panic),
// including (but not limited to):
//   - nil Guard
//   - invalid Path
//   - duplicated Path
//
// # Output format
//
// Most endpoints default to text output. Users can override per request with:
//   - ?format=json
//   - ?format=text
//
// This behavior is aligned with package ops.
//
// # Capability list (EnableXxx) and defaults
//
// Each EnableXxx enables exactly one capability (or one read capability for GET+HEAD).
// Every Spec supports:
//   - Guard (required, non-nil)
//   - Path (optional; empty => default path)
//
// Default paths (relative to the mounted admin subtree):
//
// Report (human-oriented, GET/HEAD, text-only):
//   - EnableReport:            "/report"
//
// Basic read endpoints (GET/HEAD):
//   - EnableHealthz:           "/healthz"
//   - EnableReadyz:            "/readyz"
//   - EnableBuildInfo:         "/buildinfo"
//   - EnableRuntime:           "/runtime"
//   - EnableLogLevelGet:       "/log/level"
//   - EnableTuningSnapshot:    "/tuning/snapshot"
//   - EnableTuningOverrides:   "/tuning/overrides"
//   - EnableTuningLookup:      "/tuning/lookup"   (?key=)
//   - EnableTasksSnapshot:     "/tasks/snapshot"
//   - EnableProvidedSnapshot:  "/provided"
//
// Write endpoints (POST):
//   - EnableLogLevelSet:         "/log/level/set"          (?level=)
//   - EnableTuningSet:           "/tuning/set"             (?key=&value=)
//   - EnableTuningResetDefault:  "/tuning/reset-default"   (?key=)
//   - EnableTuningResetLast:     "/tuning/reset-last"      (?key=)
//   - EnableTaskTrigger:         "/tasks/trigger"          (?name=)
//   - EnableTaskTriggerAndWait:  "/tasks/trigger-and-wait" (?name=&timeout=)
//
// Notes on task write endpoints:
//   - Task control is name-based: the admin/ops layer looks up tasks via task.Manager.Lookup.
//   - Unnamed tasks are not indexed by task.Manager and therefore cannot be triggered by name.
//
// Notes:
//   - If two enabled capabilities collide on the same path, admin panics (fail-fast).
//   - Multi tuning instances are supported by overriding Path per endpoint. If you want two tuning
//     trees, you must explicitly set distinct paths for each endpoint to avoid collisions.
//
// # Security model: Guard
//
// admin exposes its own Guard interface so users do not need to import or understand httpx.
//
//	type Guard interface {
//		// Middleware returns a net/http middleware that enforces this guard.
//		// It must be fast and must not block; it must not do I/O.
//		Middleware() func(http.Handler) http.Handler
//	}
//
// Denied requests always respond with HTTP 403 (Forbidden).
//
// # Guard helpers
//
// admin includes a small set of guard constructors:
//   - DenyAll(), AllowAll()
//   - Tokens / HotTokens (token from a header)
//   - IPAllowList (client IP allowlist; integrates with WithRealIP)
//   - TokensOrIPAllowList / TokensAndIPAllowList (token + IP composite guards)
//   - Check(fn) (custom fast predicate)
//
// Notes:
//   - Static token/IP lists are fail-closed: empty/invalid inputs deny all.
//   - Token header can be customized via WithTokenHeader (applies to all token-based guards).
//
// # Real IP (for IP-based guards)
//
// If you use IP guards, correct behavior behind proxies requires real IP extraction.
// Configure it with WithRealIP:
//
//	admin.WithRealIP(admin.RealIPSpec{
//		TrustedProxies: []string{"10.0.0.0/8"}, // your LB/proxy CIDRs
//	})
//
// Default-safe behavior:
//   - If TrustedProxies is empty, headers are not trusted, and IP checks fall back to RemoteAddr.
//
// # Report (/report)
//
// The report endpoint outputs a human-oriented overview of *already enabled* read capabilities
// (observation endpoints) in a single plain-text page.
//
// Design notes:
//   - /report is text-only (no ?format= negotiation).
//   - /report includes only what is enabled in the same admin instance.
//   - /report is guarded by its own Guard and does not attempt per-capability re-authorization.
//   - The "provided" section is truncated to a conservative max size (reportProvidedMaxBytes).
//
// # Example: minimal admin
//
// This example shows a typical setup: protect everything with a static token, but restrict
// write endpoints to a separate (stronger) guard if desired.
//
//	tokenGuard := admin.Tokens([]string{"s3cr3t"})
//
//	h := admin.New(
//		admin.EnableReport(admin.ReportSpec{Guard: tokenGuard}),
//		admin.EnableHealthz(admin.HealthzSpec{Guard: tokenGuard}),
//		admin.EnableRuntime(admin.RuntimeSpec{Guard: tokenGuard}),
//	)
//
// # Example: tuning + write allowlist
//
// Write endpoints must specify Access; empty Access denies all writes (fail-closed).
//
//	t := tuning.New()
//	// ... register tunings ...
//
//	read := admin.Tokens([]string{"read-token"})
//	write := admin.Tokens([]string{"write-token"})
//
//	h := admin.New(
//		admin.EnableTuningSnapshot(admin.TuningSnapshotSpec{
//			Guard:  read,
//			T:      t,
//			Access: admin.TuningAccessSpec{}, // empty => no filtering for reads
//		}),
//		admin.EnableTuningSet(admin.TuningSetSpec{
//			Guard: write,
//			T:     t,
//			Access: admin.TuningAccessSpec{
//				AllowPrefixes: []string{"feature.", "ops."},
//			},
//		}),
//	)
package admin
