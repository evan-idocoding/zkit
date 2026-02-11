package admin

import (
	"context"
	"log/slog"
	"strings"
	"time"

	"github.com/evan-idocoding/zkit/ops"
	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

// --- report ---

type ReportSpec struct {
	Guard Guard
	Path  string // default "/report"
}

func EnableReport(spec ReportSpec) Option {
	return func(b *Builder) {
		requireBuilder(b)
		requireGuard(spec.Guard, "report")
		if b.report != nil {
			panic("admin: EnableReport called more than once")
		}
		if spec.Path == "" {
			spec.Path = "/report"
		}
		spec.Path = normalizePathOrPanic(spec.Path)
		b.report = &spec
	}
}

// --- health / ready ---

type HealthzSpec struct {
	Guard Guard
	Path  string // default "/healthz"
}

func EnableHealthz(spec HealthzSpec) Option {
	return func(b *Builder) {
		path := resolvePath(spec.Path, "/healthz")
		mountRead(b, "healthz", path, spec.Guard, ops.HealthzHandler())
	}
}

// ReadyCheck is a named readiness check.
type ReadyCheck struct {
	Name    string
	Func    func(context.Context) error
	Timeout time.Duration // <=0 means no extra timeout
}

type ReadyzSpec struct {
	Guard  Guard
	Path   string // default "/readyz"
	Checks []ReadyCheck
}

func EnableReadyz(spec ReadyzSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "readyz")
		path := resolvePath(spec.Path, "/readyz")
		checks := make([]ops.ReadyCheck, 0, len(spec.Checks))
		for i, c := range spec.Checks {
			if c.Name == "" {
				panic("admin: ready check[" + itoa(i) + "] has empty Name")
			}
			if c.Func == nil {
				panic("admin: ready check[" + itoa(i) + "] " + c.Name + " has nil Func")
			}
			checks = append(checks, ops.ReadyCheck{
				Name:    c.Name,
				Func:    c.Func,
				Timeout: c.Timeout,
			})
		}
		mountRead(b, "readyz", path, spec.Guard, ops.ReadyzHandler(checks))
	}
}

// --- buildinfo / runtime ---

type BuildInfoSpec struct {
	Guard Guard
	Path  string // default "/buildinfo"

	IncludeDeps     bool // default false
	IncludeSettings bool // default false
}

func EnableBuildInfo(spec BuildInfoSpec) Option {
	return func(b *Builder) {
		path := resolvePath(spec.Path, "/buildinfo")
		requireGuard(spec.Guard, "buildinfo")
		h := ops.BuildInfoHandler(
			ops.WithBuildInfoIncludeDeps(spec.IncludeDeps),
			ops.WithBuildInfoIncludeSettings(spec.IncludeSettings),
		)
		// /report always uses compact buildinfo (deps/settings omitted).
		reportH := ops.BuildInfoHandler(
			ops.WithBuildInfoIncludeDeps(false),
			ops.WithBuildInfoIncludeSettings(false),
		)
		mountRead(b, "buildinfo", path, spec.Guard, h)
		b.reportState.buildInfo = reportSource{path: path, h: reportH}
	}
}

type RuntimeSpec struct {
	Guard Guard
	Path  string // default "/runtime"
}

func EnableRuntime(spec RuntimeSpec) Option {
	return func(b *Builder) {
		path := resolvePath(spec.Path, "/runtime")
		raw := ops.RuntimeHandler()
		mountRead(b, "runtime", path, spec.Guard, raw)
		b.reportState.runtime = reportSource{path: path, h: raw}
	}
}

// --- log level ---

type LogLevelGetSpec struct {
	Guard Guard
	Path  string // default "/log/level"
	Var   *slog.LevelVar
}

func EnableLogLevelGet(spec LogLevelGetSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "log.level.get")
		if spec.Var == nil {
			panic("admin: log.level.get: nil slog.LevelVar")
		}
		path := resolvePath(spec.Path, "/log/level")
		raw := ops.LogLevelGetHandler(spec.Var)
		mountRead(b, "log.level.get", path, spec.Guard, raw)
		b.reportState.logLevelGet = reportSource{path: path, h: raw}
	}
}

type LogLevelSetSpec struct {
	Guard Guard
	Path  string // default "/log/level/set"
	Var   *slog.LevelVar
}

func EnableLogLevelSet(spec LogLevelSetSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "log.level.set")
		if spec.Var == nil {
			panic("admin: log.level.set: nil slog.LevelVar")
		}
		path := resolvePath(spec.Path, "/log/level/set")
		mountWrite(b, "log.level.set", path, spec.Guard, ops.LogLevelSetHandler(spec.Var))
	}
}

// --- tuning ---

type TuningAccessSpec struct {
	AllowPrefixes []string
	AllowKeys     []string
	AllowFunc     func(key string) bool
}

type TuningSnapshotSpec struct {
	Guard  Guard
	Path   string // default "/tuning/snapshot"
	T      *tuning.Tuning
	Access TuningAccessSpec // optional filter for reads
}

func EnableTuningSnapshot(spec TuningSnapshotSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.snapshot")
		requireTuning(spec.T, "tuning.snapshot")
		path := resolvePath(spec.Path, "/tuning/snapshot")
		opts := tuningReadOptionsOrPanic(spec.Access)
		raw := ops.TuningSnapshotHandler(spec.T, opts...)
		mountRead(b, "tuning.snapshot", path, spec.Guard, raw)
		b.reportState.tuningSnapshot = reportSource{path: path, h: raw}
	}
}

type TuningOverridesSpec struct {
	Guard  Guard
	Path   string // default "/tuning/overrides"
	T      *tuning.Tuning
	Access TuningAccessSpec // optional filter for reads
}

func EnableTuningOverrides(spec TuningOverridesSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.overrides")
		requireTuning(spec.T, "tuning.overrides")
		path := resolvePath(spec.Path, "/tuning/overrides")
		opts := tuningReadOptionsOrPanic(spec.Access)
		raw := ops.TuningOverridesHandler(spec.T, opts...)
		mountRead(b, "tuning.overrides", path, spec.Guard, raw)
		b.reportState.tuningOverrides = reportSource{path: path, h: raw}
	}
}

type TuningLookupSpec struct {
	Guard  Guard
	Path   string // default "/tuning/lookup"
	T      *tuning.Tuning
	Access TuningAccessSpec // optional filter for reads
}

func EnableTuningLookup(spec TuningLookupSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.lookup")
		requireTuning(spec.T, "tuning.lookup")
		path := resolvePath(spec.Path, "/tuning/lookup")
		opts := tuningReadOptionsOrPanic(spec.Access)
		mountRead(b, "tuning.lookup", path, spec.Guard, ops.TuningLookupHandler(spec.T, opts...))
	}
}

type TuningSetSpec struct {
	Guard  Guard
	Path   string // default "/tuning/set"
	T      *tuning.Tuning
	Access TuningAccessSpec // required allowlist for writes; empty => deny-all
}

func EnableTuningSet(spec TuningSetSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.set")
		requireTuning(spec.T, "tuning.set")
		path := resolvePath(spec.Path, "/tuning/set")
		opts := tuningWriteOptionsOrPanic(spec.Access)
		mountWrite(b, "tuning.set", path, spec.Guard, ops.TuningSetHandler(spec.T, opts...))
	}
}

type TuningResetDefaultSpec struct {
	Guard  Guard
	Path   string // default "/tuning/reset-default"
	T      *tuning.Tuning
	Access TuningAccessSpec // required allowlist for writes; empty => deny-all
}

func EnableTuningResetDefault(spec TuningResetDefaultSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.reset_default")
		requireTuning(spec.T, "tuning.reset_default")
		path := resolvePath(spec.Path, "/tuning/reset-default")
		opts := tuningWriteOptionsOrPanic(spec.Access)
		mountWrite(b, "tuning.reset_default", path, spec.Guard, ops.TuningResetToDefaultHandler(spec.T, opts...))
	}
}

type TuningResetLastSpec struct {
	Guard  Guard
	Path   string // default "/tuning/reset-last"
	T      *tuning.Tuning
	Access TuningAccessSpec // required allowlist for writes; empty => deny-all
}

func EnableTuningResetLast(spec TuningResetLastSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tuning.reset_last")
		requireTuning(spec.T, "tuning.reset_last")
		path := resolvePath(spec.Path, "/tuning/reset-last")
		opts := tuningWriteOptionsOrPanic(spec.Access)
		mountWrite(b, "tuning.reset_last", path, spec.Guard, ops.TuningResetToLastValueHandler(spec.T, opts...))
	}
}

// --- tasks ---

type TaskAccessSpec struct {
	AllowPrefixes []string
	AllowNames    []string
	AllowFunc     func(name string) bool
}

type TasksSnapshotSpec struct {
	Guard  Guard
	Path   string // default "/tasks/snapshot"
	Mgr    *task.Manager
	Access TaskAccessSpec // optional filter for reads
}

func EnableTasksSnapshot(spec TasksSnapshotSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tasks.snapshot")
		if spec.Mgr == nil {
			panic("admin: tasks.snapshot: nil task.Manager")
		}
		path := resolvePath(spec.Path, "/tasks/snapshot")
		opts := taskReadOptionsOrPanic(spec.Access)
		raw := ops.TasksSnapshotHandler(spec.Mgr, opts...)
		mountRead(b, "tasks.snapshot", path, spec.Guard, raw)
		b.reportState.tasksSnapshot = reportSource{path: path, h: raw}
	}
}

type TaskTriggerSpec struct {
	Guard Guard
	Path  string // default "/tasks/trigger"
	// Mgr is the task manager whose named tasks can be triggered by this endpoint.
	//
	// Notes:
	//   - Task triggering is name-based and uses task.Manager.Lookup.
	//   - Unnamed tasks are not indexed by task.Manager and therefore cannot be triggered by name.
	Mgr    *task.Manager
	Access TaskAccessSpec // required allowlist for writes; empty => deny-all
}

func EnableTaskTrigger(spec TaskTriggerSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tasks.trigger")
		if spec.Mgr == nil {
			panic("admin: tasks.trigger: nil task.Manager")
		}
		path := resolvePath(spec.Path, "/tasks/trigger")
		opts := taskWriteOptionsOrPanic(spec.Access)
		mountWrite(b, "tasks.trigger", path, spec.Guard, ops.TaskTriggerHandler(spec.Mgr, opts...))
	}
}

type TaskTriggerAndWaitSpec struct {
	Guard Guard
	Path  string // default "/tasks/trigger-and-wait"
	// Mgr is the task manager whose named tasks can be triggered by this endpoint.
	//
	// Notes:
	//   - Task triggering is name-based and uses task.Manager.Lookup.
	//   - Unnamed tasks are not indexed by task.Manager and therefore cannot be triggered by name.
	Mgr    *task.Manager
	Access TaskAccessSpec // required allowlist for writes; empty => deny-all
}

func EnableTaskTriggerAndWait(spec TaskTriggerAndWaitSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "tasks.trigger_and_wait")
		if spec.Mgr == nil {
			panic("admin: tasks.trigger_and_wait: nil task.Manager")
		}
		path := resolvePath(spec.Path, "/tasks/trigger-and-wait")
		opts := taskWriteOptionsOrPanic(spec.Access)
		mountWrite(b, "tasks.trigger_and_wait", path, spec.Guard, ops.TaskTriggerAndWaitHandler(spec.Mgr, opts...))
	}
}

// --- provided snapshot ---

type ProvidedSnapshotSpec struct {
	Guard Guard
	Path  string // default "/provided"

	Items    map[string]any
	MaxBytes int // optional; default conservative (aligned with ops)
}

func EnableProvidedSnapshot(spec ProvidedSnapshotSpec) Option {
	return func(b *Builder) {
		requireGuard(spec.Guard, "provided.snapshot")
		if spec.Items == nil {
			spec.Items = map[string]any{}
		}
		path := resolvePath(spec.Path, "/provided")
		var opts []ops.ProvidedSnapshotOption
		if spec.MaxBytes > 0 {
			opts = append(opts, ops.WithProvidedSnapshotMaxBytes(spec.MaxBytes))
		}
		raw := ops.ProvidedSnapshotHandler(spec.Items, opts...)
		mountRead(b, "provided.snapshot", path, spec.Guard, raw)
		// /report truncates provided output on its own; disable max-bytes in the report view
		// so a user-provided MaxBytes doesn't turn /report into "response too large".
		reportH := ops.ProvidedSnapshotHandler(spec.Items, ops.WithProvidedSnapshotMaxBytes(0))
		b.reportState.providedSnapshot = reportSource{path: path, h: reportH}
	}
}

// --- helpers ---

func requireBuilder(b *Builder) {
	if b == nil {
		panic("admin: nil builder")
	}
}

func requireGuard(g Guard, capName string) {
	if g == nil {
		panic("admin: " + capName + ": nil Guard")
	}
}

func requireTuning(t *tuning.Tuning, capName string) {
	if t == nil {
		panic("admin: " + capName + ": nil tuning.Tuning")
	}
}

// --- access (tuning/tasks allowlists) ---

func tuningReadOptionsOrPanic(a TuningAccessSpec) []ops.TuningOption {
	fn := tuningAccessFuncOrPanic(a)
	if fn == nil {
		return nil
	}
	return []ops.TuningOption{ops.WithTuningKeyGuard(fn)}
}

func tuningWriteOptionsOrPanic(a TuningAccessSpec) []ops.TuningOption {
	fn := tuningAccessFuncOrPanic(a)
	if fn == nil {
		// Fail-closed: empty access => deny all writes.
		return []ops.TuningOption{ops.WithTuningKeyGuard(func(string) bool { return false })}
	}
	return []ops.TuningOption{ops.WithTuningKeyGuard(fn)}
}

func tuningAccessFuncOrPanic(a TuningAccessSpec) func(key string) bool {
	// AllowFunc is exclusive to avoid semantic confusion.
	if a.AllowFunc != nil {
		if len(a.AllowPrefixes) != 0 || len(a.AllowKeys) != 0 {
			panic("admin: tuning access: AllowFunc conflicts with AllowPrefixes/AllowKeys")
		}
		return a.AllowFunc
	}
	prefixes, prefixesSpecified := trimNonEmpty(a.AllowPrefixes)
	keys, keysSpecified := trimNonEmpty(a.AllowKeys)

	// Fail-closed on invalid config: user specified a list but none valid.
	if prefixesSpecified && len(prefixes) == 0 {
		return func(string) bool { return false }
	}
	if keysSpecified && len(keys) == 0 {
		return func(string) bool { return false }
	}

	if len(prefixes) == 0 && len(keys) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		set[k] = struct{}{}
	}
	return func(key string) bool {
		if key == "" {
			return false
		}
		if _, ok := set[key]; ok {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(key, p) {
				return true
			}
		}
		return false
	}
}

func taskReadOptionsOrPanic(a TaskAccessSpec) []ops.TaskOption {
	fn := taskAccessFuncOrPanic(a)
	if fn == nil {
		return nil
	}
	return []ops.TaskOption{ops.WithTaskNameGuard(fn)}
}

func taskWriteOptionsOrPanic(a TaskAccessSpec) []ops.TaskOption {
	fn := taskAccessFuncOrPanic(a)
	if fn == nil {
		// Fail-closed: empty access => deny all writes.
		return []ops.TaskOption{ops.WithTaskNameGuard(func(string) bool { return false })}
	}
	return []ops.TaskOption{ops.WithTaskNameGuard(fn)}
}

func taskAccessFuncOrPanic(a TaskAccessSpec) func(name string) bool {
	if a.AllowFunc != nil {
		if len(a.AllowPrefixes) != 0 || len(a.AllowNames) != 0 {
			panic("admin: task access: AllowFunc conflicts with AllowPrefixes/AllowNames")
		}
		return a.AllowFunc
	}
	prefixes, prefixesSpecified := trimNonEmpty(a.AllowPrefixes)
	names, namesSpecified := trimNonEmpty(a.AllowNames)

	if prefixesSpecified && len(prefixes) == 0 {
		return func(string) bool { return false }
	}
	if namesSpecified && len(names) == 0 {
		return func(string) bool { return false }
	}

	if len(prefixes) == 0 && len(names) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		set[n] = struct{}{}
	}
	return func(name string) bool {
		if name == "" {
			return false
		}
		if _, ok := set[name]; ok {
			return true
		}
		for _, p := range prefixes {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		return false
	}
}

func trimNonEmpty(in []string) (out []string, specified bool) {
	if len(in) == 0 {
		return nil, false
	}
	specified = true
	out = make([]string, 0, len(in))
	for _, raw := range in {
		s := strings.TrimSpace(raw)
		if s == "" {
			continue
		}
		out = append(out, s)
	}
	return out, specified
}
