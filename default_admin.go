package zkit

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/evan-idocoding/zkit/admin"
	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

// ReadyCheck is a single readiness check for /readyz.
// Name is required (used in the report); Timeout is optional (zero = no extra timeout).
type ReadyCheck struct {
	Name    string
	Func    func(context.Context) error
	Timeout time.Duration
}

// AdminSpec configures NewDefaultAdmin. All fields are optional except ReadGuard.
//
// Assembly errors are fail-fast and will panic.
//
// # Required
//   - ReadGuard: must be non-nil; protects all read endpoints.
//
// # Optional reads (zero = disabled or no filter)
//   - TrustedProxies: CIDRs or IPs of trusted proxies; empty = do not trust proxy headers, use RemoteAddr.
//   - TrustedHeaders: header names used to extract client IP (e.g. X-Forwarded-For, X-Real-IP). Empty = default order.
//   - ReadyChecks: /readyz checks; empty slice = endpoint still enabled, no checks.
//   - LogLevelVar: enables /log/level (read) when non-nil.
//   - Tuning + TuningReadAllow*: tuning read endpoints; Tuning must be non-nil. Read allowlist: zero = no filter.
//   - TaskManager + TaskReadAllow*: /tasks/snapshot; TaskManager must be non-nil. Read allowlist: zero = no filter.
//   - ProvidedItems: when non-nil, enables /provided with this map; nil = disabled. ProvidedMaxBytes optional (<=0 = default).
//
// Note on overlap with ServiceSpec:
//   - AdminSpec.{LogLevelVar,Tuning,TaskManager} are admin handler data sources. When NewDefaultService is used and
//     both ServiceSpec.<X> and ServiceSpec.Admin.<X> are provided, Admin.<X> takes precedence for the admin subtree.
//   - Service lifecycle ownership is controlled by ServiceSpec (e.g. Service starts/shuts down TasksManager only when
//     ServiceSpec.TasksManager is non-nil or when Service creates/manages one for admin write needs).
//
// # Writes (WriteGuard nil = all write endpoints disabled)
//   - WriteGuard: when non-nil, write endpoints may be enabled; this guard protects them. Required for any write.
//   - EnableLogLevelSet: requires WriteGuard != nil and LogLevelVar != nil (coexistence).
//   - Tuning write group (/tuning/set, reset-default, reset-last): set TuningWritesEnabled true to enable; requires Tuning != nil. Allowlist (empty = deny-all) applies.
//   - Task write group (/tasks/trigger, trigger-and-wait): set TaskWritesEnabled true to enable; requires TaskManager != nil. Allowlist (empty = deny-all) applies.
//
// # Access allowlist rules (Tuning* and Task* Allow* fields)
//   - For reads: zero (nil slices + nil func) = no filtering. Non-zero = allowlist applied.
//   - For writes: set TuningWritesEnabled/TaskWritesEnabled to enable the group; allowlist then applies (empty = deny-all). AllowFunc is mutually exclusive with the slice fields (admin will panic if both set).
type AdminSpec struct {
	// Required. Protects all read endpoints.
	ReadGuard Guard

	// TrustedProxies: CIDRs or single IPs of trusted proxies. Empty = do not trust proxy headers, use RemoteAddr.
	TrustedProxies []string
	// TrustedHeaders: header names used to extract client IP (e.g. "X-Forwarded-For", "X-Real-IP"). Empty = default order.
	TrustedHeaders []string

	// Ready checks for /readyz. Empty = no checks (endpoint still responds OK).
	ReadyChecks []ReadyCheck

	// Optional read sources.
	LogLevelVar *slog.LevelVar
	Tuning      *tuning.Tuning
	TaskManager *task.Manager

	// Tuning read access: zero = no filter. AllowFunc mutually exclusive with AllowPrefixes/AllowKeys.
	TuningReadAllowPrefixes []string
	TuningReadAllowKeys     []string
	TuningReadAllowFunc     func(key string) bool

	// Task read access: zero = no filter. AllowFunc mutually exclusive with AllowPrefixes/AllowNames.
	TaskReadAllowPrefixes []string
	TaskReadAllowNames    []string
	TaskReadAllowFunc     func(name string) bool

	// Provided (sensitive). Non-nil = enable /provided with this map; nil = disabled.
	ProvidedItems   map[string]any
	ProvidedMaxBytes int // <= 0 uses ops default

	// Writes: nil = no write endpoints. Non-nil = guard for all write endpoints; individual groups gated by their Enable flag and allowlists.
	WriteGuard Guard

	// Enable /log/level/set. Requires WriteGuard != nil and LogLevelVar != nil.
	EnableLogLevelSet bool

	// Tuning writes: TuningWritesEnabled true = enable group (requires Tuning != nil). Allowlist applies; empty = deny-all. AllowFunc mutually exclusive with slices.
	TuningWritesEnabled    bool
	TuningWriteAllowPrefixes []string
	TuningWriteAllowKeys     []string
	TuningWriteAllowFunc     func(key string) bool

	// Task writes: TaskWritesEnabled true = enable group (requires TaskManager != nil). Allowlist applies; empty = deny-all. AllowFunc mutually exclusive with slices.
	TaskWritesEnabled    bool
	TaskWriteAllowPrefixes []string
	TaskWriteAllowNames    []string
	TaskWriteAllowFunc     func(name string) bool
}

// NewDefaultAdmin assembles a default-safe admin subtree handler from a flat spec.
//
// Paths are fixed (v1 default kit). For custom paths or composition, use admin.New(...) + admin.EnableXxx(...).
func NewDefaultAdmin(spec AdminSpec) http.Handler {
	if spec.ReadGuard == nil {
		panic("zkit: NewDefaultAdmin: nil ReadGuard")
	}

	opts := make([]admin.Option, 0, 16)

	opts = append(opts, admin.WithRealIP(admin.RealIPSpec{
		TrustedProxies: spec.TrustedProxies,
		TrustedHeaders: spec.TrustedHeaders,
	}))

	readyChecks := readyChecksToAdmin(spec.ReadyChecks)
	opts = append(opts,
		admin.EnableReport(admin.ReportSpec{Guard: spec.ReadGuard}),
		admin.EnableHealthz(admin.HealthzSpec{Guard: spec.ReadGuard}),
		admin.EnableReadyz(admin.ReadyzSpec{Guard: spec.ReadGuard, Checks: readyChecks}),
		admin.EnableBuildInfo(admin.BuildInfoSpec{Guard: spec.ReadGuard}),
		admin.EnableRuntime(admin.RuntimeSpec{Guard: spec.ReadGuard}),
	)

	if spec.LogLevelVar != nil {
		opts = append(opts, admin.EnableLogLevelGet(admin.LogLevelGetSpec{
			Guard: spec.ReadGuard,
			Var:   spec.LogLevelVar,
		}))
	}

	tuningReadAccess := tuningAccessSpec(spec.TuningReadAllowPrefixes, spec.TuningReadAllowKeys, spec.TuningReadAllowFunc)
	if spec.Tuning != nil {
		opts = append(opts,
			admin.EnableTuningSnapshot(admin.TuningSnapshotSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: tuningReadAccess,
			}),
			admin.EnableTuningOverrides(admin.TuningOverridesSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: tuningReadAccess,
			}),
			admin.EnableTuningLookup(admin.TuningLookupSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: tuningReadAccess,
			}),
		)
	}

	taskReadAccess := taskAccessSpec(spec.TaskReadAllowPrefixes, spec.TaskReadAllowNames, spec.TaskReadAllowFunc)
	if spec.TaskManager != nil {
		opts = append(opts, admin.EnableTasksSnapshot(admin.TasksSnapshotSpec{
			Guard:  spec.ReadGuard,
			Mgr:    spec.TaskManager,
			Access: taskReadAccess,
		}))
	}

	if spec.ProvidedItems != nil {
		opts = append(opts, admin.EnableProvidedSnapshot(admin.ProvidedSnapshotSpec{
			Guard:    spec.ReadGuard,
			Items:    spec.ProvidedItems,
			MaxBytes: spec.ProvidedMaxBytes,
		}))
	}

	if spec.WriteGuard != nil {
		if spec.EnableLogLevelSet {
			if spec.LogLevelVar == nil {
				panic("zkit: NewDefaultAdmin: EnableLogLevelSet requires LogLevelVar")
			}
			opts = append(opts, admin.EnableLogLevelSet(admin.LogLevelSetSpec{
				Guard: spec.WriteGuard,
				Var:   spec.LogLevelVar,
			}))
		}

		if tuningWritesEnabled(spec) {
			if spec.Tuning == nil {
				panic("zkit: NewDefaultAdmin: tuning writes enabled but Tuning is nil")
			}
			access := tuningAccessSpec(spec.TuningWriteAllowPrefixes, spec.TuningWriteAllowKeys, spec.TuningWriteAllowFunc)
			opts = append(opts,
				admin.EnableTuningSet(admin.TuningSetSpec{
					Guard:  spec.WriteGuard,
					T:      spec.Tuning,
					Access: access,
				}),
				admin.EnableTuningResetDefault(admin.TuningResetDefaultSpec{
					Guard:  spec.WriteGuard,
					T:      spec.Tuning,
					Access: access,
				}),
				admin.EnableTuningResetLast(admin.TuningResetLastSpec{
					Guard:  spec.WriteGuard,
					T:      spec.Tuning,
					Access: access,
				}),
			)
		}

		if taskWritesEnabled(spec) {
			if spec.TaskManager == nil {
				panic("zkit: NewDefaultAdmin: task writes enabled but TaskManager is nil")
			}
			access := taskAccessSpec(spec.TaskWriteAllowPrefixes, spec.TaskWriteAllowNames, spec.TaskWriteAllowFunc)
			opts = append(opts,
				admin.EnableTaskTrigger(admin.TaskTriggerSpec{
					Guard:  spec.WriteGuard,
					Mgr:    spec.TaskManager,
					Access: access,
				}),
				admin.EnableTaskTriggerAndWait(admin.TaskTriggerAndWaitSpec{
					Guard:  spec.WriteGuard,
					Mgr:    spec.TaskManager,
					Access: access,
				}),
			)
		}
	}

	return admin.New(opts...)
}

func readyChecksToAdmin(checks []ReadyCheck) []admin.ReadyCheck {
	if len(checks) == 0 {
		return nil
	}
	out := make([]admin.ReadyCheck, len(checks))
	for i, c := range checks {
		if c.Name == "" {
			panic("zkit: ReadyChecks[" + strconv.Itoa(i) + "] has empty Name")
		}
		if c.Func == nil {
			panic("zkit: ReadyChecks[" + strconv.Itoa(i) + "] " + c.Name + " has nil Func")
		}
		out[i] = admin.ReadyCheck{Name: c.Name, Func: c.Func, Timeout: c.Timeout}
	}
	return out
}

func tuningAccessSpec(prefixes, keys []string, fn func(key string) bool) admin.TuningAccessSpec {
	return admin.TuningAccessSpec{
		AllowPrefixes: prefixes,
		AllowKeys:     keys,
		AllowFunc:     fn,
	}
}

func taskAccessSpec(prefixes, names []string, fn func(name string) bool) admin.TaskAccessSpec {
	return admin.TaskAccessSpec{
		AllowPrefixes: prefixes,
		AllowNames:    names,
		AllowFunc:     fn,
	}
}

func tuningWritesEnabled(spec AdminSpec) bool {
	return spec.WriteGuard != nil && spec.TuningWritesEnabled
}

func taskWritesEnabled(spec AdminSpec) bool {
	return spec.WriteGuard != nil && spec.TaskWritesEnabled
}

