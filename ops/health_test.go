package ops

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHealthz_Text_OK(t *testing.T) {
	h := HealthzHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	resp := w.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	body := w.Body.String()
	if body != "ok\n" {
		t.Fatalf("body=%q, want %q", body, "ok\n")
	}
}

func TestHealthz_JSON_OK(t *testing.T) {
	h := HealthzHandler(WithHealthDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	resp := w.Result()
	t.Cleanup(func() { _ = resp.Body.Close() })

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ok, _ := got["ok"].(bool); !ok {
		t.Fatalf("json ok=%v, want true; got=%v", got["ok"], got)
	}
}

func TestHealthz_QueryFormatOverridesOption(t *testing.T) {
	h := HealthzHandler(WithHealthDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if body := w.Body.String(); body != "ok\n" {
		t.Fatalf("body=%q, want %q", body, "ok\n")
	}
}

func TestHealthz_QueryJSONOverridesDefaultText(t *testing.T) {
	h := HealthzHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if ok, _ := got["ok"].(bool); !ok {
		t.Fatalf("json ok=%v, want true; got=%v", got["ok"], got)
	}
}

func TestHealthz_MethodNotAllowed(t *testing.T) {
	h := HealthzHandler()
	r := httptest.NewRequest(http.MethodPost, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestHealthz_Head_NoBody(t *testing.T) {
	h := HealthzHandler()
	r := httptest.NewRequest(http.MethodHead, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestHealthz_CacheControl_NoStore(t *testing.T) {
	h := HealthzHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestHealthz_InvalidFormatFallsBackToText(t *testing.T) {
	h := HealthzHandler(WithHealthDefaultFormat(Format(999)))
	r := httptest.NewRequest(http.MethodGet, "http://example/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestReadyz_NoChecks_OK(t *testing.T) {
	h := ReadyzHandler(nil)
	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "ok\n" {
		t.Fatalf("body=%q, want %q", body, "ok\n")
	}
}

func TestReadyz_Fail_503_Text(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{
			Name: "dep-a",
			Func: func(ctx context.Context) error { return context.Canceled },
		},
	})
	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	if body := w.Body.String(); !strings.Contains(body, "fail dep-a") {
		t.Fatalf("body=%q, want contain %q", body, "fail dep-a")
	}
}

func TestReadyz_Fail_503_JSON(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{
			Name: "dep-a",
			Func: func(ctx context.Context) error { return context.Canceled },
		},
	}, WithHealthDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	var rep ReadyzReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rep.OK {
		t.Fatalf("rep.OK=true, want false")
	}
	if len(rep.Checks) != 1 {
		t.Fatalf("len(checks)=%d, want 1", len(rep.Checks))
	}
	if rep.Checks[0].Name != "dep-a" || rep.Checks[0].OK {
		t.Fatalf("check=%+v, want name=dep-a ok=false", rep.Checks[0])
	}
}

func TestReadyz_CheckTimeout(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{
			Name:    "slow",
			Timeout: 50 * time.Millisecond,
			Func: func(ctx context.Context) error {
				<-ctx.Done()
				return ctx.Err()
			},
		},
	}, WithHealthDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	var rep ReadyzReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(rep.Checks) != 1 || !rep.Checks[0].TimedOut {
		t.Fatalf("checks=%+v, want timed_out true", rep.Checks)
	}
}

func TestReadyz_CheckPanicBecomesFailure(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{
			Name: "boom",
			Func: func(ctx context.Context) error { panic("x") },
		},
	})
	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	if body := w.Body.String(); !strings.Contains(body, "panic: x") {
		t.Fatalf("body=%q, want contain %q", body, "panic: x")
	}
}

func TestReadyz_MethodNotAllowed_JSONShapeConsistent(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{Name: "ok", Func: func(ctx context.Context) error { return nil }},
	})

	r := httptest.NewRequest(http.MethodPost, "http://example/readyz?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}

	var rep ReadyzReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rep.OK {
		t.Fatalf("rep.OK=true, want false")
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Name != "method" || rep.Checks[0].OK {
		t.Fatalf("checks=%+v, want one method failure", rep.Checks)
	}
}

func TestReadyz_QueryFormatOverridesOption(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{Name: "dep-a", Func: func(ctx context.Context) error { return context.Canceled }},
	}, WithHealthDefaultFormat(FormatText))

	r := httptest.NewRequest(http.MethodGet, "http://example/readyz?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var rep ReadyzReport
	if err := json.Unmarshal(w.Body.Bytes(), &rep); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if rep.OK {
		t.Fatalf("rep.OK=true, want false")
	}
	if len(rep.Checks) != 1 || rep.Checks[0].Name != "dep-a" || rep.Checks[0].OK {
		t.Fatalf("checks=%+v, want dep-a failure", rep.Checks)
	}
}

func TestReadyz_Head_NoBody(t *testing.T) {
	h := ReadyzHandler([]ReadyCheck{
		{Name: "ok", Func: func(ctx context.Context) error { return nil }},
	})
	r := httptest.NewRequest(http.MethodHead, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestReadyz_CacheControl_NoStore(t *testing.T) {
	h := ReadyzHandler(nil)
	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestReadyz_InvalidFormatFallsBackToText(t *testing.T) {
	h := ReadyzHandler(nil, WithHealthDefaultFormat(Format(999)))
	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestReadyz_InvalidCheckPanics(t *testing.T) {
	defer func() {
		if p := recover(); p == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = ReadyzHandler([]ReadyCheck{{Name: "", Func: func(ctx context.Context) error { return nil }}})
}

func TestReadyz_CheckSliceIsSnapshotted(t *testing.T) {
	checks := []ReadyCheck{
		{Name: "a", Func: func(ctx context.Context) error { return context.Canceled }},
	}
	h := ReadyzHandler(checks)
	checks[0].Name = "b"

	r := httptest.NewRequest(http.MethodGet, "http://example/readyz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusServiceUnavailable)
	}
	body := w.Body.String()
	if !strings.Contains(body, "fail a") {
		t.Fatalf("body=%q, want contain %q", body, "fail a")
	}
	if strings.Contains(body, "fail b") {
		t.Fatalf("body=%q, want not contain %q", body, "fail b")
	}
}
