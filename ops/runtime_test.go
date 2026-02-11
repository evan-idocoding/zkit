package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRuntime_Text_OK(t *testing.T) {
	h := RuntimeHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime", nil)
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
	if !strings.Contains(body, "proc\tgoroutines\t") {
		t.Fatalf("body=%q, want contain %q", body, "proc\tgoroutines\t")
	}
	if !strings.Contains(body, "mem\talloc_bytes\t") {
		t.Fatalf("body=%q, want contain %q", body, "mem\talloc_bytes\t")
	}
}

func TestRuntime_JSON_OK(t *testing.T) {
	h := RuntimeHandler(WithRuntimeDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime", nil)
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
	var got runtimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Runtime == nil {
		t.Fatalf("got=%+v, want ok with runtime", got)
	}
	if got.Runtime.Runtime.Version == "" {
		t.Fatalf("runtime.version empty, want non-empty; got=%+v", got.Runtime.Runtime)
	}
	if got.Runtime.Goroutines < 1 {
		t.Fatalf("goroutines=%d, want >=1", got.Runtime.Goroutines)
	}
	if got.Runtime.PID <= 0 {
		t.Fatalf("pid=%d, want >0", got.Runtime.PID)
	}
}

func TestRuntime_QueryFormatOverridesOption(t *testing.T) {
	h := RuntimeHandler(WithRuntimeDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "proc\tpid\t") {
		t.Fatalf("body=%q, want contain %q", body, "proc\tpid\t")
	}
}

func TestRuntime_QueryJSONOverridesDefaultText(t *testing.T) {
	h := RuntimeHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got runtimeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Runtime == nil {
		t.Fatalf("got=%+v, want ok with runtime", got)
	}
}

func TestRuntime_MethodNotAllowed(t *testing.T) {
	h := RuntimeHandler()
	r := httptest.NewRequest(http.MethodPost, "http://example/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestRuntime_Head_NoBody(t *testing.T) {
	h := RuntimeHandler()
	r := httptest.NewRequest(http.MethodHead, "http://example/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestRuntime_CacheControl_NoStore(t *testing.T) {
	h := RuntimeHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestRuntime_InvalidFormatFallsBackToText(t *testing.T) {
	h := RuntimeHandler(WithRuntimeDefaultFormat(Format(999)))
	r := httptest.NewRequest(http.MethodGet, "http://example/runtime", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}
