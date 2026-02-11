package ops

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLogLevelGet_Text_OK(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv)

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level", nil)
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
	if !strings.Contains(body, "log\tlevel\tinfo\n") {
		t.Fatalf("body=%q, want contain %q", body, "log\tlevel\tinfo\\n")
	}
	if !strings.Contains(body, "log\tlevel_value\t0\n") {
		t.Fatalf("body=%q, want contain %q", body, "log\tlevel_value\t0\\n")
	}
}

func TestLogLevelGet_JSON_OK(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelWarn)
	h := LogLevelGetHandler(lv, WithLogLevelDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level", nil)
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
	var got logLevelGetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Log == nil {
		t.Fatalf("got=%+v, want ok with log", got)
	}
	if got.Log.Level != "warn" {
		t.Fatalf("level=%q, want %q", got.Log.Level, "warn")
	}
	if got.Log.LevelValue != int(slog.LevelWarn) {
		t.Fatalf("level_value=%d, want %d", got.Log.LevelValue, int(slog.LevelWarn))
	}
}

func TestLogLevelGet_QueryFormatOverridesOption(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv, WithLogLevelDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "log\tlevel\tinfo\n") {
		t.Fatalf("body=%q, want contain %q", body, "log\tlevel\tinfo\\n")
	}
}

func TestLogLevelGet_QueryJSONOverridesDefaultText(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv)

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got logLevelGetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Log == nil {
		t.Fatalf("got=%+v, want ok with log", got)
	}
}

func TestLogLevelGet_MethodNotAllowed(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv)

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestLogLevelGet_Head_NoBody(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv)

	r := httptest.NewRequest(http.MethodHead, "http://example/log_level", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestLogLevelGet_CacheControl_NoStore(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv)

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestLogLevelGet_InvalidFormatFallsBackToText(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelGetHandler(lv, WithLogLevelDefaultFormat(Format(999)))

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestLogLevelSet_Text_OK_CaseInsensitiveAndAlias(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv)

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=ERROR", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "log\tnew_level\terror\n") {
		t.Fatalf("body=%q, want contain %q", body, "log\tnew_level\terror\\n")
	}
	if got := lv.Level(); got != slog.LevelError {
		t.Fatalf("lv.Level()=%v, want %v", got, slog.LevelError)
	}

	// Alias: warning -> warn
	r2 := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=warning", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w2.Result().StatusCode, http.StatusOK)
	}
	if got := lv.Level(); got != slog.LevelWarn {
		t.Fatalf("lv.Level()=%v, want %v", got, slog.LevelWarn)
	}
}

func TestLogLevelSet_JSON_OK(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv, WithLogLevelDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=warn", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got logLevelSetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Old == nil || got.New == nil {
		t.Fatalf("got=%+v, want ok with old/new", got)
	}
	if got.New.Level != "warn" {
		t.Fatalf("new.level=%q, want %q", got.New.Level, "warn")
	}
}

func TestLogLevelSet_QueryFormatOverridesOption(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv, WithLogLevelDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level?format=text&level=debug", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "log\tnew_level\tdebug\n") {
		t.Fatalf("body=%q, want contain %q", body, "log\tnew_level\tdebug\\n")
	}
}

func TestLogLevelSet_MissingOrInvalidLevel_400(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv)

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}

	r2 := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=verbose", nil)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w2.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestLogLevelSet_MethodNotAllowed(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv, WithLogLevelDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/log_level?level=debug", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want %q", allow, "POST")
	}
	var got logLevelSetResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("ok=true, want false; got=%+v", got)
	}
}

func TestLogLevelSet_CacheControl_NoStore(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv)

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=debug", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestLogLevelSet_InvalidFormatFallsBackToText(t *testing.T) {
	lv := new(slog.LevelVar)
	lv.Set(slog.LevelInfo)
	h := LogLevelSetHandler(lv, WithLogLevelDefaultFormat(Format(999)))

	r := httptest.NewRequest(http.MethodPost, "http://example/log_level?level=debug", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}
