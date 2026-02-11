package ops

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evan-idocoding/zkit/rt/safego"
	"github.com/evan-idocoding/zkit/rt/task"
)

func TestTasksSnapshot_Text_OK_UnnamedUsesSyntheticName(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }))
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("named"))

	h := TasksSnapshotHandler(m)
	r := httptest.NewRequest(http.MethodGet, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}

	body := w.Body.String()
	if !strings.Contains(body, "task\tunnamed#0\tstate\tnot-started\n") {
		t.Fatalf("body=%q, want contain unnamed#0 state line", body)
	}
	if !strings.Contains(body, "task\tnamed\tstate\tnot-started\n") {
		t.Fatalf("body=%q, want contain named state line", body)
	}
}

func TestTasksSnapshot_JSON_OK(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }))

	h := TasksSnapshotHandler(m, WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got tasksSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || len(got.Tasks) != 1 {
		t.Fatalf("got=%+v, want ok with 1 task", got)
	}
	if got.Tasks[0].Name != "" {
		t.Fatalf("name=%q, want empty", got.Tasks[0].Name)
	}
	if got.Tasks[0].DisplayName != "unnamed#0" {
		t.Fatalf("display_name=%q, want %q", got.Tasks[0].DisplayName, "unnamed#0")
	}
	if got.Tasks[0].State != "not-started" {
		t.Fatalf("state=%q, want %q", got.Tasks[0].State, "not-started")
	}
}

func TestTasksSnapshot_QueryFormatOverridesOption(t *testing.T) {
	m := task.NewManager()
	h := TasksSnapshotHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tasks?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestTasksSnapshot_MethodNotAllowed(t *testing.T) {
	m := task.NewManager()
	h := TasksSnapshotHandler(m)

	r := httptest.NewRequest(http.MethodPost, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestTasksSnapshot_Head_NoBody(t *testing.T) {
	m := task.NewManager()
	h := TasksSnapshotHandler(m)

	r := httptest.NewRequest(http.MethodHead, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestTasksSnapshot_InvalidFormatFallsBackToText(t *testing.T) {
	m := task.NewManager()
	h := TasksSnapshotHandler(m, WithTaskDefaultFormat(Format(999)))

	r := httptest.NewRequest(http.MethodGet, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestTaskTrigger_Text_OK_TriggersRun(t *testing.T) {
	m := task.NewManager()
	ran := make(chan struct{}, 1)
	h1, _ := m.Add(task.Trigger(func(ctx context.Context) error {
		ran <- struct{}{}
		return nil
	}), task.WithName("a"))

	_ = h1 // keep for clarity
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerHandler(m)

	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); !strings.Contains(body, "task_trigger\ta\taccepted\ttrue\n") {
		t.Fatalf("body=%q, want accepted line", body)
	}
	select {
	case <-ran:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("task did not run")
	}
}

func TestTaskTrigger_NotAccepted_409_JSON(t *testing.T) {
	m := task.NewManager()
	started := make(chan struct{})
	unblock := make(chan struct{})
	h1, _ := m.Add(task.Trigger(func(ctx context.Context) error {
		close(started)
		<-unblock
		return nil
	}), task.WithName("b"), task.WithOverlapPolicy(task.OverlapSkip))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		close(unblock)
		_ = m.Shutdown(context.Background())
	})

	// Start one run so the next TryTrigger is skipped.
	h1.Trigger()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("task did not start")
	}

	_ = h1 // keep for clarity
	h := TaskTriggerHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?name=b", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusConflict)
	}
	var got taskTriggerResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK || got.Accepted {
		t.Fatalf("got=%+v, want ok=false accepted=false", got)
	}
	if got.Name != "b" {
		t.Fatalf("name=%q, want %q", got.Name, "b")
	}
}

func TestTaskTrigger_Guard_403(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	h := TaskTriggerHandler(m, WithTaskAllowNames("x"), WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusForbidden)
	}
}

func TestTaskTrigger_MethodNotAllowed(t *testing.T) {
	h := TaskTriggerHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/task_trigger?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want %q", allow, "POST")
	}
}

func TestTaskTrigger_MissingName_400(t *testing.T) {
	h := TaskTriggerHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTaskTrigger_NotFound_404(t *testing.T) {
	h := TaskTriggerHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusNotFound)
	}
}

func TestTaskTrigger_QueryFormatOverridesOption(t *testing.T) {
	h := TaskTriggerHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?format=text&name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestTaskTrigger_CacheControl_NoStore(t *testing.T) {
	h := TaskTriggerHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_trigger?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestTaskTriggerAndWait_OK_JSON(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	var got taskTriggerAndWaitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Name != "a" {
		t.Fatalf("got=%+v, want ok=true name=a", got)
	}
}

func TestTaskTriggerAndWait_MethodNotAllowed(t *testing.T) {
	h := TaskTriggerAndWaitHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/task_wait?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want %q", allow, "POST")
	}
}

func TestTaskTriggerAndWait_MissingName_400(t *testing.T) {
	h := TaskTriggerAndWaitHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTaskTriggerAndWait_NotFound_404(t *testing.T) {
	h := TaskTriggerAndWaitHandler(task.NewManager(), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusNotFound)
	}
}

func TestTaskTriggerAndWait_Guard_403(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))

	h := TaskTriggerAndWaitHandler(m, WithTaskAllowNames("x"), WithTaskDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=10ms", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusForbidden)
	}
}

func TestTaskTriggerAndWait_QueryFormatOverridesOption(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?format=text&name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestTaskTriggerAndWait_CacheControl_NoStore(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestTaskTriggerAndWait_NotRunning_409(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=10ms", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusConflict)
	}
}

func TestTaskTriggerAndWait_Closed_503(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	_ = m.Shutdown(context.Background())

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=50ms", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
}

func TestTaskTriggerAndWait_Skipped_409(t *testing.T) {
	m := task.NewManager()
	started := make(chan struct{})
	unblock := make(chan struct{})
	h1, _ := m.Add(task.Trigger(func(ctx context.Context) error {
		close(started)
		<-unblock
		return nil
	}), task.WithName("a"), task.WithOverlapPolicy(task.OverlapSkip))

	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		close(unblock)
		_ = m.Shutdown(context.Background())
	})

	// Hold one run in-flight.
	go func() { _ = h1.TriggerAndWait(context.Background()) }()
	select {
	case <-started:
	case <-time.After(500 * time.Millisecond):
		t.Fatalf("task did not start")
	}

	_ = h1
	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusConflict)
	}
}

func TestTaskTriggerAndWait_Panicked_500(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { panic("boom") }), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusInternalServerError)
	}
}

func TestTaskTriggerAndWait_Timeout_408(t *testing.T) {
	m := task.NewManager()
	unblock := make(chan struct{})
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error {
		<-unblock
		return nil
	}), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		close(unblock)
		_ = m.Shutdown(context.Background())
	})

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=10ms", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusRequestTimeout {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusRequestTimeout)
	}
	var got taskTriggerAndWaitResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK || !got.TimedOut {
		t.Fatalf("got=%+v, want ok=false timed_out=true", got)
	}
}

func TestTaskTriggerAndWait_InvalidTimeout_400(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a"))
	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}

	r2 := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=abc", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w2.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTaskTriggerAndWait_Text_EscapesError(t *testing.T) {
	m := task.NewManager()
	_, _ = m.Add(task.Trigger(func(ctx context.Context) error {
		return errors.New("a\tb\nc\r\\d")
	}), task.WithName("a"))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	h := TaskTriggerAndWaitHandler(m, WithTaskDefaultFormat(FormatText))
	r := httptest.NewRequest(http.MethodPost, "http://example/task_wait?name=a&timeout=1s", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusInternalServerError)
	}
	body := w.Body.String()
	if !strings.Contains(body, "a\\tb\\nc\\r\\\\d\n") {
		t.Fatalf("body=%q, want escaped error line", body)
	}
	// Ensure raw control characters are not present.
	if strings.Contains(body, "a\tb") || strings.Contains(body, "b\nc") || strings.Contains(body, "c\r") {
		t.Fatalf("body contains raw control chars: %q", body)
	}
}

func TestTasksSnapshot_Text_EscapesLastErrorAndRendersTags(t *testing.T) {
	m := task.NewManager()
	h1, _ := m.Add(task.Trigger(func(ctx context.Context) error {
		return errors.New("a\tb\nc\r\\d")
	}), task.WithName("t1"), task.WithTags(
		safego.Tag{Key: "k", Value: "v\t1"},
	))
	if err := m.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() { _ = m.Shutdown(context.Background()) })

	// Run once to populate LastError and tags in snapshot.
	_ = h1.TriggerAndWait(context.Background())

	h := TasksSnapshotHandler(m)
	r := httptest.NewRequest(http.MethodGet, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "task\tt1\tlast_error\ta\\tb\\nc\\r\\\\d\n") {
		t.Fatalf("body=%q, want escaped last_error line", body)
	}
	if strings.Contains(body, "a\tb") || strings.Contains(body, "b\nc") || strings.Contains(body, "c\r") {
		t.Fatalf("body contains raw control chars: %q", body)
	}
	if !strings.Contains(body, "task_tag\tt1\tk\tv\\t1\n") {
		t.Fatalf("body=%q, want tag line", body)
	}
}

func TestTasksSnapshot_Text_EscapesTaskName(t *testing.T) {
	m := task.NewManager()
	_, err := m.Add(task.Trigger(func(ctx context.Context) error { return nil }), task.WithName("a\tb\nc\r\\d"))
	// task names are validated by rt/task; control characters are not allowed.
	if !errors.Is(err, task.ErrInvalidName) {
		t.Fatalf("Add err=%v, want ErrInvalidName", err)
	}

	h := TasksSnapshotHandler(m)
	r := httptest.NewRequest(http.MethodGet, "http://example/tasks", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()

	// No tasks were added due to invalid name.
	if body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}
