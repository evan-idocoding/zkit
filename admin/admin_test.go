package admin

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

func TestNilGuardPanics(t *testing.T) {
	assertPanics(t, func() {
		_ = New(
			EnableHealthz(HealthzSpec{}), // Guard nil
		)
	})
}

func TestSamePathSameMethodPanics(t *testing.T) {
	assertPanics(t, func() {
		_ = New(
			EnableHealthz(HealthzSpec{Guard: AllowAll(), Path: "/x"}),
			EnableRuntime(RuntimeSpec{Guard: AllowAll(), Path: "/x"}), // duplicates path
		)
	})
}

func TestLogLevel_GetAndSet_DefaultPathsDoNotCollide(t *testing.T) {
	lv := &slog.LevelVar{}
	h := New(
		EnableLogLevelGet(LogLevelGetSpec{Guard: AllowAll(), Var: lv}), // default "/log/level"
		EnableLogLevelSet(LogLevelSetSpec{Guard: AllowAll(), Var: lv}), // default "/log/level/set"
	)

	// GET
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/log/level?format=json", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET expected %d, got %d", http.StatusOK, rr.Code)
	}

	// POST
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodPost, "http://admin.test/log/level/set?format=json&level=debug", nil)
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("POST expected %d, got %d", http.StatusOK, rr2.Code)
	}
}

func TestSamePathDifferentMethodNowPanics(t *testing.T) {
	lv := &slog.LevelVar{}
	assertPanics(t, func() {
		_ = New(
			EnableLogLevelGet(LogLevelGetSpec{Guard: AllowAll(), Var: lv, Path: "/log/level"}),
			EnableLogLevelSet(LogLevelSetSpec{Guard: AllowAll(), Var: lv, Path: "/log/level"}), // duplicates path
		)
	})
}

func TestTuningWrite_EmptyAccessDeniesAll(t *testing.T) {
	tu := tuning.New()
	if _, err := tu.Bool("feature.a", false); err != nil {
		t.Fatalf("register tuning: %v", err)
	}

	h := New(
		EnableTuningSet(TuningSetSpec{
			Guard:  AllowAll(),
			T:      tu,
			Access: TuningAccessSpec{}, // empty => deny-all for writes
		}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://admin.test/tuning/set?key=feature.a&value=true", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestTokensOrIPAllowList_AllowsTokenOrIP(t *testing.T) {
	g := TokensOrIPAllowList([]string{"p1"}, []string{"10.0.0.0/8"})
	h := New(
		EnableHealthz(HealthzSpec{Guard: g}),
	)

	// Missing token + non-allowlisted IP: denied.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}

	// With token: allowed.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req2.RemoteAddr = "203.0.113.1:1234"
	req2.Header.Set(DefaultTokenHeader, "p1")
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr2.Code)
	}

	// With allowlisted IP: allowed.
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req3.RemoteAddr = "10.1.2.3:1234"
	h.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr3.Code)
	}
}

func TestTokensAndIPAllowList_RequiresBoth(t *testing.T) {
	g := TokensAndIPAllowList([]string{"p1"}, []string{"10.0.0.0/8"})
	h := New(
		EnableHealthz(HealthzSpec{Guard: g}),
	)

	// Token ok, IP not ok => denied.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req.RemoteAddr = "203.0.113.1:1234"
	req.Header.Set(DefaultTokenHeader, "p1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}

	// IP ok, token missing => denied.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req2.RemoteAddr = "10.1.2.3:1234"
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr2.Code)
	}

	// Both ok => allowed.
	rr3 := httptest.NewRecorder()
	req3 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req3.RemoteAddr = "10.1.2.3:1234"
	req3.Header.Set(DefaultTokenHeader, "p1")
	h.ServeHTTP(rr3, req3)
	if rr3.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr3.Code)
	}
}

func TestTokens_WithTokenHeader(t *testing.T) {
	g := Tokens([]string{"p1"}, WithTokenHeader("X-Admin-Token"))
	h := New(
		EnableHealthz(HealthzSpec{Guard: g}),
	)

	// Default header should not work.
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req.Header.Set(DefaultTokenHeader, "p1")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}

	// Custom header should work.
	rr2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://admin.test/healthz", nil)
	req2.Header.Set("X-Admin-Token", "p1")
	h.ServeHTTP(rr2, req2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr2.Code)
	}
}

func TestHealthz_Head(t *testing.T) {
	h := New(
		EnableHealthz(HealthzSpec{Guard: AllowAll()}),
	)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "http://admin.test/healthz", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	if rr.Body.Len() != 0 {
		t.Fatalf("expected empty body for HEAD, got %q", rr.Body.String())
	}
}

func TestTaskWrite_EmptyAccessDeniesAll(t *testing.T) {
	mgr := task.NewManager()

	h := New(
		EnableTaskTrigger(TaskTriggerSpec{
			Guard:  AllowAll(),
			Mgr:    mgr,
			Access: TaskAccessSpec{}, // empty => deny-all for writes
		}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://admin.test/tasks/trigger?name=x", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusForbidden {
		t.Fatalf("expected %d, got %d", http.StatusForbidden, rr.Code)
	}
}

func TestReport_IncludesOnlyEnabledSections(t *testing.T) {
	tu := tuning.New()
	if _, err := tu.Bool("feature.a", false); err != nil {
		t.Fatalf("register tuning: %v", err)
	}
	mgr := task.NewManager()

	h := New(
		EnableRuntime(RuntimeSpec{Guard: AllowAll()}),
		EnableTuningSnapshot(TuningSnapshotSpec{Guard: AllowAll(), T: tu}),
		EnableTasksSnapshot(TasksSnapshotSpec{Guard: AllowAll(), Mgr: mgr}),
		EnableProvidedSnapshot(ProvidedSnapshotSpec{Guard: AllowAll(), Items: map[string]any{"x": "y"}}),
		EnableReport(ReportSpec{Guard: AllowAll()}),
	)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/report", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	if got := rr.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("expected Cache-Control no-store, got %q", got)
	}
	body := rr.Body.String()

	// Header lists enabled sections.
	if !strings.Contains(body, "enabled sections:") {
		t.Fatalf("expected report to list enabled sections, got:\n%s", body)
	}
	if !strings.Contains(body, "runtime") || !strings.Contains(body, "tuning.snapshot") || !strings.Contains(body, "tasks.snapshot") || !strings.Contains(body, "provided") {
		t.Fatalf("expected report to list key sections, got:\n%s", body)
	}

	// Sections exist.
	if !strings.Contains(body, "=== runtime ===") {
		t.Fatalf("expected runtime section, got:\n%s", body)
	}
	if !strings.Contains(body, "=== tuning.snapshot ===") {
		t.Fatalf("expected tuning.snapshot section, got:\n%s", body)
	}
	if !strings.Contains(body, "=== tasks.snapshot ===") {
		t.Fatalf("expected tasks.snapshot section, got:\n%s", body)
	}
	if !strings.Contains(body, "=== provided ===") {
		t.Fatalf("expected provided section, got:\n%s", body)
	}
	if !strings.Contains(body, "note: below are user-provided snapshots") {
		t.Fatalf("expected provided note, got:\n%s", body)
	}

	// Not enabled => not included.
	if strings.Contains(body, "=== buildinfo ===") {
		t.Fatalf("did not expect buildinfo section, got:\n%s", body)
	}
}

func TestReport_ProvidedTruncation(t *testing.T) {
	// Make provided output large enough to trigger report truncation.
	huge := strings.Repeat("a", reportProvidedMaxBytes+1024)
	h := New(
		EnableProvidedSnapshot(ProvidedSnapshotSpec{Guard: AllowAll(), Items: map[string]any{"huge": huge}}),
		EnableReport(ReportSpec{Guard: AllowAll()}),
	)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://admin.test/report", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("expected %d, got %d", http.StatusOK, rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "=== provided ===") {
		t.Fatalf("expected provided section, got:\n%s", body)
	}
	if !strings.Contains(body, "(truncated)") {
		t.Fatalf("expected truncation marker, got:\n%s", body)
	}
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	fn()
}
