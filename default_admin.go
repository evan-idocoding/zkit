package zkit

import (
	"log/slog"
	"net/http"

	"github.com/evan-idocoding/zkit/admin"
	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

// AdminSpec configures NewDefaultAdmin.
//
// Assembly errors are fail-fast and will panic.
type AdminSpec struct {
	// ReadGuard is required. It protects all read endpoints.
	ReadGuard admin.Guard

	// RealIP optionally configures client IP extraction for IP-based guards.
	// Default-safe: if TrustedProxies is empty, headers are not trusted and RemoteAddr is used.
	RealIP admin.RealIPSpec

	// ReadyChecks are optional. /readyz is enabled regardless of checks (empty => ok).
	ReadyChecks []admin.ReadyCheck

	// LogLevelVar enables /log/level (read) when non-nil.
	LogLevelVar *slog.LevelVar

	// Tuning enables tuning read endpoints when non-nil.
	Tuning           *tuning.Tuning
	TuningReadAccess admin.TuningAccessSpec // optional filter for reads (zero => no filter)

	// TaskManager enables /tasks/snapshot (read) when non-nil.
	TaskManager    *task.Manager
	TaskReadAccess admin.TaskAccessSpec // optional filter for reads (zero => no filter)

	// Provided controls /provided (high sensitivity). It is disabled by default.
	Provided struct {
		Enable   bool
		Items    map[string]any
		MaxBytes int // optional; <=0 uses ops default
	}

	// Writes controls write endpoints. nil => disable all writes (default).
	Writes *AdminWriteSpec
}

// AdminWriteSpec controls write endpoints (coarse-grained groups).
type AdminWriteSpec struct {
	// Guard is required when Writes != nil.
	Guard admin.Guard

	// EnableLogLevelSet enables /log/level/set (requires LogLevelVar != nil).
	EnableLogLevelSet bool

	// Tuning enables the tuning write group when non-nil:
	//   - /tuning/set
	//   - /tuning/reset-default
	//   - /tuning/reset-last
	Tuning *TuningWriteSpec

	// Tasks enables the task write group when non-nil:
	//   - /tasks/trigger
	//   - /tasks/trigger-and-wait
	Tasks *TaskWriteSpec
}

type TuningWriteSpec struct {
	// Access is a required allowlist for writes; empty => deny-all (fail-closed).
	Access admin.TuningAccessSpec
}

type TaskWriteSpec struct {
	// Access is a required allowlist for writes; empty => deny-all (fail-closed).
	Access admin.TaskAccessSpec
}

// NewDefaultAdmin assembles a default-safe admin subtree handler.
//
// Paths are fixed (v1 default kit). For custom paths / more composition, use:
//
//	admin.New(...) + admin.EnableXxx(...)
func NewDefaultAdmin(spec AdminSpec) http.Handler {
	if spec.ReadGuard == nil {
		panic("zkit: NewDefaultAdmin: nil ReadGuard")
	}

	opts := make([]admin.Option, 0, 16)

	// Global admin subtree settings.
	opts = append(opts, admin.WithRealIP(spec.RealIP))

	// Always-enabled reads.
	opts = append(opts,
		admin.EnableReport(admin.ReportSpec{Guard: spec.ReadGuard}),
		admin.EnableHealthz(admin.HealthzSpec{Guard: spec.ReadGuard}),
		admin.EnableReadyz(admin.ReadyzSpec{Guard: spec.ReadGuard, Checks: spec.ReadyChecks}),
		admin.EnableBuildInfo(admin.BuildInfoSpec{Guard: spec.ReadGuard}), // compact by default
		admin.EnableRuntime(admin.RuntimeSpec{Guard: spec.ReadGuard}),
	)

	// Optional reads.
	if spec.LogLevelVar != nil {
		opts = append(opts, admin.EnableLogLevelGet(admin.LogLevelGetSpec{
			Guard: spec.ReadGuard,
			Var:   spec.LogLevelVar,
		}))
	}
	if spec.Tuning != nil {
		opts = append(opts,
			admin.EnableTuningSnapshot(admin.TuningSnapshotSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: spec.TuningReadAccess,
			}),
			admin.EnableTuningOverrides(admin.TuningOverridesSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: spec.TuningReadAccess,
			}),
			admin.EnableTuningLookup(admin.TuningLookupSpec{
				Guard:  spec.ReadGuard,
				T:      spec.Tuning,
				Access: spec.TuningReadAccess,
			}),
		)
	}
	if spec.TaskManager != nil {
		opts = append(opts, admin.EnableTasksSnapshot(admin.TasksSnapshotSpec{
			Guard:  spec.ReadGuard,
			Mgr:    spec.TaskManager,
			Access: spec.TaskReadAccess,
		}))
	}
	if spec.Provided.Enable {
		opts = append(opts, admin.EnableProvidedSnapshot(admin.ProvidedSnapshotSpec{
			Guard:    spec.ReadGuard,
			Items:    spec.Provided.Items,
			MaxBytes: spec.Provided.MaxBytes,
		}))
	}

	// Writes: default off.
	if spec.Writes != nil {
		if spec.Writes.Guard == nil {
			panic("zkit: NewDefaultAdmin: Writes != nil but Writes.Guard is nil")
		}
		if spec.Writes.EnableLogLevelSet {
			if spec.LogLevelVar == nil {
				panic("zkit: NewDefaultAdmin: EnableLogLevelSet requires LogLevelVar")
			}
			opts = append(opts, admin.EnableLogLevelSet(admin.LogLevelSetSpec{
				Guard: spec.Writes.Guard,
				Var:   spec.LogLevelVar,
			}))
		}
		if spec.Writes.Tuning != nil {
			if spec.Tuning == nil {
				panic("zkit: NewDefaultAdmin: tuning writes enabled but Tuning is nil")
			}
			access := spec.Writes.Tuning.Access
			opts = append(opts,
				admin.EnableTuningSet(admin.TuningSetSpec{
					Guard:  spec.Writes.Guard,
					T:      spec.Tuning,
					Access: access,
				}),
				admin.EnableTuningResetDefault(admin.TuningResetDefaultSpec{
					Guard:  spec.Writes.Guard,
					T:      spec.Tuning,
					Access: access,
				}),
				admin.EnableTuningResetLast(admin.TuningResetLastSpec{
					Guard:  spec.Writes.Guard,
					T:      spec.Tuning,
					Access: access,
				}),
			)
		}
		if spec.Writes.Tasks != nil {
			if spec.TaskManager == nil {
				panic("zkit: NewDefaultAdmin: task writes enabled but TaskManager is nil")
			}
			access := spec.Writes.Tasks.Access
			opts = append(opts,
				admin.EnableTaskTrigger(admin.TaskTriggerSpec{
					Guard:  spec.Writes.Guard,
					Mgr:    spec.TaskManager,
					Access: access,
				}),
				admin.EnableTaskTriggerAndWait(admin.TaskTriggerAndWaitSpec{
					Guard:  spec.Writes.Guard,
					Mgr:    spec.TaskManager,
					Access: access,
				}),
			)
		}
	}

	return admin.New(opts...)
}
