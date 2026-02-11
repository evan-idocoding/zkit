package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/evan-idocoding/zkit/rt/task"
)

type taskOpsConfig struct {
	format Format

	guards []func(name string) bool
	guard  func(name string) bool
}

// TaskOption configures task ops handlers.
type TaskOption func(*taskOpsConfig)

// WithTaskDefaultFormat sets the default response format for task handlers.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithTaskDefaultFormat(f Format) TaskOption {
	return func(c *taskOpsConfig) { c.format = f }
}

// WithTaskNameGuard appends a name guard.
//
// All guards are combined with AND: a name is allowed only if all guards allow it.
// This applies to both read and write handlers.
func WithTaskNameGuard(fn func(name string) bool) TaskOption {
	return func(c *taskOpsConfig) {
		if fn != nil {
			c.guards = append(c.guards, fn)
		}
	}
}

// WithTaskAllowPrefixes restricts task names to the provided prefixes.
//
// Safety note: if no non-empty prefix is provided, this option denies all names.
func WithTaskAllowPrefixes(prefixes ...string) TaskOption {
	var ps []string
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		ps = append(ps, p)
	}
	return WithTaskNameGuard(func(name string) bool {
		if len(ps) == 0 {
			return false
		}
		for _, p := range ps {
			if strings.HasPrefix(name, p) {
				return true
			}
		}
		return false
	})
}

// WithTaskAllowNames restricts task names to the provided explicit set.
//
// Safety note: if no non-empty name is provided, this option denies all names.
func WithTaskAllowNames(names ...string) TaskOption {
	set := make(map[string]struct{}, len(names))
	for _, n := range names {
		if n == "" {
			continue
		}
		set[n] = struct{}{}
	}
	return WithTaskNameGuard(func(name string) bool {
		if len(set) == 0 {
			return false
		}
		_, ok := set[name]
		return ok
	})
}

func applyTaskOptions(opts []TaskOption) taskOpsConfig {
	cfg := taskOpsConfig{
		format: FormatText,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.format != FormatText && cfg.format != FormatJSON {
		cfg.format = FormatText
	}
	if len(cfg.guards) > 0 {
		cfg.guard = func(name string) bool { return true }
		for _, g := range cfg.guards {
			if g == nil {
				continue
			}
			prev := cfg.guard
			gg := g
			cfg.guard = func(name string) bool { return prev(name) && gg(name) }
		}
	}
	return cfg
}

// TasksSnapshotHandler returns a handler that outputs a task manager snapshot.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func TasksSnapshotHandler(m *task.Manager, opts ...TaskOption) http.Handler {
	if m == nil {
		panic("ops: nil task.Manager")
	}
	cfg := applyTaskOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeTasksSnapshot(w, r, format, http.StatusMethodNotAllowed, tasksSnapshotResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap := m.Snapshot()
		items := toTaskStatusSnapshots(snap, cfg.guard)
		writeTasksSnapshot(w, r, format, http.StatusOK, tasksSnapshotResponse{
			OK:    true,
			Tasks: items,
		})
	})
}

// TaskTriggerHandler returns a handler that triggers a task once (fire-and-forget).
//
// Input:
//   - POST only
//   - URL query: ?name=<task name>
//
// Output:
//   - 200 if accepted
//   - 409 if not accepted (busy / not running / closed)
func TaskTriggerHandler(m *task.Manager, opts ...TaskOption) http.Handler {
	if m == nil {
		panic("ops: nil task.Manager")
	}
	cfg := applyTaskOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTaskTrigger(w, r, format, http.StatusMethodNotAllowed, taskTriggerResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		name, ok := getQueryRequired(r, "name")
		if !ok || name == "" {
			writeTaskTrigger(w, r, format, http.StatusBadRequest, taskTriggerResponse{
				OK:    false,
				Error: "missing name",
			})
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			writeTaskTrigger(w, r, format, http.StatusBadRequest, taskTriggerResponse{
				OK:    false,
				Error: "missing name",
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(name) {
			writeTaskTrigger(w, r, format, http.StatusForbidden, taskTriggerResponse{
				OK:    false,
				Error: "name not allowed",
				Name:  name,
			})
			return
		}

		h, found := m.Lookup(name)
		if !found || h == nil {
			writeTaskTrigger(w, r, format, http.StatusNotFound, taskTriggerResponse{
				OK:    false,
				Error: "task not found",
				Name:  name,
			})
			return
		}

		accepted := h.TryTrigger()
		if !accepted {
			msg := inferTriggerNotAcceptedReason(h.Status())
			if msg == "" {
				msg = "trigger not accepted"
			}
			writeTaskTrigger(w, r, format, http.StatusConflict, taskTriggerResponse{
				OK:       false,
				Error:    msg,
				Name:     name,
				Accepted: accepted,
			})
			return
		}
		writeTaskTrigger(w, r, format, http.StatusOK, taskTriggerResponse{
			OK:       true,
			Name:     name,
			Accepted: accepted,
		})
	})
}

// TaskTriggerAndWaitHandler returns a handler that triggers a task and waits for completion.
//
// Input:
//   - POST only
//   - URL query: ?name=<task name>&timeout=<go duration>
//
// timeout is optional; when provided, it must be a positive Go duration (e.g. 5s, 200ms).
func TaskTriggerAndWaitHandler(m *task.Manager, opts ...TaskOption) http.Handler {
	if m == nil {
		panic("ops: nil task.Manager")
	}
	cfg := applyTaskOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTaskTriggerAndWait(w, r, format, http.StatusMethodNotAllowed, taskTriggerAndWaitResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		name, ok := getQueryRequired(r, "name")
		if !ok || name == "" {
			writeTaskTriggerAndWait(w, r, format, http.StatusBadRequest, taskTriggerAndWaitResponse{
				OK:    false,
				Error: "missing name",
			})
			return
		}
		name = strings.TrimSpace(name)
		if name == "" {
			writeTaskTriggerAndWait(w, r, format, http.StatusBadRequest, taskTriggerAndWaitResponse{
				OK:    false,
				Error: "missing name",
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(name) {
			writeTaskTriggerAndWait(w, r, format, http.StatusForbidden, taskTriggerAndWaitResponse{
				OK:    false,
				Error: "name not allowed",
				Name:  name,
			})
			return
		}

		h, found := m.Lookup(name)
		if !found || h == nil {
			writeTaskTriggerAndWait(w, r, format, http.StatusNotFound, taskTriggerAndWaitResponse{
				OK:    false,
				Error: "task not found",
				Name:  name,
			})
			return
		}

		ctx := r.Context()
		cancel := func() {}
		if r.URL != nil {
			if raw, has := getQueryRaw(r, "timeout"); has {
				if raw == "" {
					writeTaskTriggerAndWait(w, r, format, http.StatusBadRequest, taskTriggerAndWaitResponse{
						OK:    false,
						Error: "invalid timeout",
						Name:  name,
					})
					return
				}
				d, err := time.ParseDuration(raw)
				if err != nil || d <= 0 {
					writeTaskTriggerAndWait(w, r, format, http.StatusBadRequest, taskTriggerAndWaitResponse{
						OK:    false,
						Error: "invalid timeout",
						Name:  name,
					})
					return
				}
				ctx, cancel = context.WithTimeout(ctx, d)
			}
		}
		defer cancel()

		start := time.Now()
		err := h.TriggerAndWait(ctx)
		dur := time.Since(start)

		if err == nil {
			writeTaskTriggerAndWait(w, r, format, http.StatusOK, taskTriggerAndWaitResponse{
				OK:       true,
				Name:     name,
				Duration: dur,
			})
			return
		}

		code := mapTriggerAndWaitErrorToStatus(err)
		resp := taskTriggerAndWaitResponse{
			OK:       false,
			Error:    err.Error(),
			Name:     name,
			Duration: dur,
		}
		if errors.Is(err, context.DeadlineExceeded) {
			resp.TimedOut = true
		}
		if errors.Is(err, context.Canceled) {
			resp.Canceled = true
		}
		writeTaskTriggerAndWait(w, r, format, code, resp)
	})
}

func mapTriggerAndWaitErrorToStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, task.ErrNotRunning):
		return http.StatusConflict
	case errors.Is(err, task.ErrClosed):
		return http.StatusServiceUnavailable
	case errors.Is(err, task.ErrSkipped):
		return http.StatusConflict
	case errors.Is(err, task.ErrPanicked):
		return http.StatusInternalServerError
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout
	default:
		return http.StatusInternalServerError
	}
}

func inferTriggerNotAcceptedReason(st task.Status) string {
	switch st.State {
	case task.StateNotStarted:
		return "manager not running"
	case task.StateStopping, task.StateStopped:
		return "task closed"
	case task.StateRunning:
		// For OverlapSkip, a common case is that the task is already at max concurrency.
		return "task busy"
	default:
		return ""
	}
}

type taskTag struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type taskStatusSnapshot struct {
	// Name is the configured task name (may be empty).
	Name string `json:"name"`
	// DisplayName is always non-empty. Unnamed tasks are rendered as "unnamed#<index>".
	//
	// Note: the index is derived from the current snapshot ordering and is intended
	// for display only; it is not a stable identifier.
	DisplayName string `json:"display_name"`

	Tags []taskTag `json:"tags,omitempty"`

	State   string `json:"state"`
	Running int    `json:"running"`
	Pending bool   `json:"pending"`

	RunCount      uint64 `json:"run_count"`
	FailCount     uint64 `json:"fail_count"`
	SuccessCount  uint64 `json:"success_count"`
	CanceledCount uint64 `json:"canceled_count"`

	LastStarted  time.Time     `json:"last_started,omitempty"`
	LastFinished time.Time     `json:"last_finished,omitempty"`
	LastSuccess  time.Time     `json:"last_success,omitempty"`
	LastDuration time.Duration `json:"last_duration"`
	LastError    string        `json:"last_error,omitempty"`

	NextRun time.Time `json:"next_run,omitempty"`
}

type tasksSnapshotResponse struct {
	OK    bool                 `json:"ok"`
	Error string               `json:"error,omitempty"`
	Tasks []taskStatusSnapshot `json:"tasks,omitempty"`
}

func toTaskStatusSnapshots(s task.Snapshot, guard func(name string) bool) []taskStatusSnapshot {
	if len(s.Tasks) == 0 {
		return nil
	}
	out := make([]taskStatusSnapshot, 0, len(s.Tasks))
	for i, st := range s.Tasks {
		// Apply guard to configured name only; if the task is unnamed and a guard is
		// configured, we exclude it (safe default: do not expose unnamed tasks when a
		// name allowlist is in effect).
		if guard != nil {
			if st.Name == "" || !guard(st.Name) {
				continue
			}
		}
		display := st.Name
		if display == "" {
			display = fmt.Sprintf("unnamed#%d", i)
		}
		item := taskStatusSnapshot{
			Name:          st.Name,
			DisplayName:   display,
			State:         st.State.String(),
			Running:       st.Running,
			Pending:       st.Pending,
			RunCount:      st.RunCount,
			FailCount:     st.FailCount,
			SuccessCount:  st.SuccessCount,
			CanceledCount: st.CanceledCount,
			LastStarted:   st.LastStarted,
			LastFinished:  st.LastFinished,
			LastSuccess:   st.LastSuccess,
			LastDuration:  st.LastDuration,
			LastError:     st.LastError,
			NextRun:       st.NextRun,
		}
		if len(st.Tags) > 0 {
			item.Tags = make([]taskTag, 0, len(st.Tags))
			for _, t := range st.Tags {
				if t.Key == "" && t.Value == "" {
					continue
				}
				item.Tags = append(item.Tags, taskTag{Key: t.Key, Value: t.Value})
			}
		}
		out = append(out, item)
	}
	return out
}

func writeTasksSnapshot(w http.ResponseWriter, r *http.Request, f Format, code int, resp tasksSnapshotResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		if !resp.OK {
			writeTextError(w, resp.Error)
			return
		}
		_, _ = w.Write([]byte(renderTasksSnapshotText(resp.Tasks)))
	}
}

func renderTasksSnapshotText(tasks []taskStatusSnapshot) string {
	// Stable and greppable.
	// Format: task\t<display_name>\t<field>\t<value>\n
	var b strings.Builder
	b.Grow(256)

	write := func(name, field, value string) {
		if name == "" || field == "" {
			return
		}
		b.WriteString("task")
		b.WriteByte('\t')
		b.WriteString(escapeTextField(name))
		b.WriteByte('\t')
		b.WriteString(field)
		b.WriteByte('\t')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	for _, st := range tasks {
		n := st.DisplayName
		if n == "" {
			// Should not happen.
			continue
		}
		write(n, "state", st.State)
		write(n, "running", strconv.Itoa(st.Running))
		write(n, "pending", strconv.FormatBool(st.Pending))
		write(n, "run_count", strconv.FormatUint(st.RunCount, 10))
		write(n, "fail_count", strconv.FormatUint(st.FailCount, 10))
		write(n, "success_count", strconv.FormatUint(st.SuccessCount, 10))
		write(n, "canceled_count", strconv.FormatUint(st.CanceledCount, 10))
		if !st.LastStarted.IsZero() {
			write(n, "last_started", st.LastStarted.Format(time.RFC3339Nano))
		}
		if !st.LastFinished.IsZero() {
			write(n, "last_finished", st.LastFinished.Format(time.RFC3339Nano))
		}
		if !st.LastSuccess.IsZero() {
			write(n, "last_success", st.LastSuccess.Format(time.RFC3339Nano))
		}
		if st.LastDuration != 0 {
			write(n, "last_duration", st.LastDuration.String())
		}
		if st.LastError != "" {
			write(n, "last_error", escapeTextField(st.LastError))
		}
		if !st.NextRun.IsZero() {
			write(n, "next_run", st.NextRun.Format(time.RFC3339Nano))
		}

		// Optional tags (compact): one line per tag.
		for _, tag := range st.Tags {
			if tag.Key == "" && tag.Value == "" {
				continue
			}
			// task_tag\t<display_name>\t<key>\t<value>\n
			b.WriteString("task_tag")
			b.WriteByte('\t')
			b.WriteString(escapeTextField(n))
			b.WriteByte('\t')
			b.WriteString(escapeTextField(tag.Key))
			b.WriteByte('\t')
			b.WriteString(escapeTextField(tag.Value))
			b.WriteByte('\n')
		}
	}
	return b.String()
}

type taskTriggerResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Name     string `json:"name,omitempty"`
	Accepted bool   `json:"accepted,omitempty"`
}

func writeTaskTrigger(w http.ResponseWriter, r *http.Request, f Format, code int, resp taskTriggerResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if !resp.OK {
			// Escape to keep text output stable/greppable and avoid output injection.
			writeTextError(w, escapeTextField(resp.Error))
			return
		}
		_, _ = w.Write([]byte(renderTaskTriggerText(resp.Name, resp.Accepted)))
	}
}

func renderTaskTriggerText(name string, accepted bool) string {
	// Stable and greppable:
	// task_trigger\t<name>\taccepted\ttrue|false\n
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(64)
	b.WriteString("task_trigger")
	b.WriteByte('\t')
	b.WriteString(escapeTextField(name))
	b.WriteByte('\t')
	b.WriteString("accepted")
	b.WriteByte('\t')
	b.WriteString(strconv.FormatBool(accepted))
	b.WriteByte('\n')
	return b.String()
}

type taskTriggerAndWaitResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Name     string        `json:"name,omitempty"`
	Duration time.Duration `json:"duration"`

	TimedOut bool `json:"timed_out,omitempty"`
	Canceled bool `json:"canceled,omitempty"`
}

func writeTaskTriggerAndWait(w http.ResponseWriter, r *http.Request, f Format, code int, resp taskTriggerAndWaitResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if !resp.OK {
			// Escape to keep text output stable/greppable and avoid output injection.
			writeTextError(w, escapeTextField(resp.Error))
			return
		}
		_, _ = w.Write([]byte(renderTaskTriggerAndWaitText(resp.Name, resp.Duration)))
	}
}

func renderTaskTriggerAndWaitText(name string, d time.Duration) string {
	// Stable and greppable:
	// task_trigger_and_wait\t<name>\tduration\t<duration>\n
	if name == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(96)
	b.WriteString("task_trigger_and_wait")
	b.WriteByte('\t')
	b.WriteString(escapeTextField(name))
	b.WriteByte('\t')
	b.WriteString("duration")
	b.WriteByte('\t')
	b.WriteString(d.String())
	b.WriteByte('\n')
	return b.String()
}
