package ops_test

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"

	"github.com/evan-idocoding/zkit/ops"
	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

func ExampleHealthzHandler() {
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ops.HealthzHandler().ServeHTTP(rr, req)

	fmt.Print(rr.Body.String())

	// Output:
	// ok
}

func ExampleLogLevelSetHandler() {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/?level=warn", nil)
	ops.LogLevelSetHandler(lv).ServeHTTP(rr, req)

	fmt.Print(rr.Body.String())

	// Output:
	// log	old_level	info
	// log	old_level_value	0
	// log	new_level	warn
	// log	new_level_value	4
}

func ExampleTuningSnapshotHandler() {
	tu := tuning.New()
	_, _ = tu.Bool("feature.x", false)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ops.TuningSnapshotHandler(tu).ServeHTTP(rr, req)

	fmt.Print(rr.Body.String())

	// Output:
	// tuning	feature.x	type	bool
	// tuning	feature.x	value	false
	// tuning	feature.x	default	false
	// tuning	feature.x	source	default
}

func ExampleTasksSnapshotHandler() {
	m := task.NewManager()
	_ = m.MustAdd(task.Trigger(func(context.Context) error { return nil }), task.WithName("job"))

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	ops.TasksSnapshotHandler(m).ServeHTTP(rr, req)

	fmt.Print(rr.Body.String())

	// Output:
	// task	job	state	not-started
	// task	job	running	0
	// task	job	pending	false
	// task	job	run_count	0
	// task	job	fail_count	0
	// task	job	success_count	0
	// task	job	canceled_count	0
}
