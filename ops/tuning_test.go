package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/evan-idocoding/zkit/rt/tuning"
)

func TestTuningSnapshot_Text_OK_Redaction(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	_, _ = tr.String("secret", "s3cr3t", tuning.WithRedactString())

	h := TuningSnapshotHandler(tr)
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot", nil)
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
	if !strings.Contains(body, "tuning\tfeature.x\ttype\tbool\n") {
		t.Fatalf("body=%q, want contain %q", body, "tuning\\tfeature.x\\ttype\\tbool\\n")
	}
	if !strings.Contains(body, "tuning\tfeature.x\tvalue\tfalse\n") {
		t.Fatalf("body=%q, want contain %q", body, "tuning\\tfeature.x\\tvalue\\tfalse\\n")
	}
	if !strings.Contains(body, "tuning\tsecret\tvalue\t<redacted>\n") {
		t.Fatalf("body=%q, want contain redacted value line", body)
	}
	if strings.Contains(body, "s3cr3t") {
		t.Fatalf("body leaks secret: %q", body)
	}
}

func TestTuningSnapshot_JSON_OK(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)

	h := TuningSnapshotHandler(tr, WithTuningDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got tuningSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Tuning == nil {
		t.Fatalf("got=%+v, want ok with tuning snapshot", got)
	}
	if len(got.Tuning.Items) != 1 || got.Tuning.Items[0].Key != "feature.x" {
		t.Fatalf("items=%+v, want one feature.x", got.Tuning.Items)
	}
}

func TestTuningSnapshot_QueryFormatOverridesOption(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSnapshotHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestTuningSnapshot_MethodNotAllowed(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSnapshotHandler(tr)

	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestTuningSnapshot_Head_NoBody(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSnapshotHandler(tr)

	r := httptest.NewRequest(http.MethodHead, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestTuningSnapshot_KeyGuard_Filters(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	_, _ = tr.Bool("other.y", true)

	h := TuningSnapshotHandler(tr, WithTuningAllowPrefixes("feature."))
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "tuning\tfeature.x\t") {
		t.Fatalf("body=%q, want contain feature.x", body)
	}
	if strings.Contains(body, "other.y") {
		t.Fatalf("body=%q, want not contain other.y", body)
	}
}

func TestTuningOverrides_Text_OK(t *testing.T) {
	tr := tuning.New()
	b, _ := tr.Bool("feature.x", false)
	s, _ := tr.String("secret", "a", tuning.WithRedactString())
	_ = b.Set(true)
	_ = s.Set("b")

	h := TuningOverridesHandler(tr)
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_overrides", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()
	if !strings.Contains(body, "tuning_override\tfeature.x\tbool\ttrue\n") {
		t.Fatalf("body=%q, want contain override line", body)
	}
	if !strings.Contains(body, "tuning_override\tsecret\tstring\t<redacted>\n") {
		t.Fatalf("body=%q, want contain redacted override line", body)
	}
	if strings.Contains(body, "\tsecret\tstring\tb\n") {
		t.Fatalf("body leaks secret override: %q", body)
	}
}

func TestTuningSnapshot_Text_EscapesControlChars(t *testing.T) {
	tr := tuning.New()
	s, _ := tr.String("msg", "x")
	_ = s.Set("a\tb\nc\r\\d")

	h := TuningSnapshotHandler(tr)
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	body := w.Body.String()
	// Ensure it is a single logical record line for "value" with escaped sequences.
	if !strings.Contains(body, "tuning\tmsg\tvalue\ta\\tb\\nc\\r\\\\d\n") {
		t.Fatalf("body=%q, want escaped value line", body)
	}
	// Ensure raw control characters are not present.
	if strings.Contains(body, "a\tb") || strings.Contains(body, "b\nc") || strings.Contains(body, "c\r") {
		t.Fatalf("body contains raw control chars: %q", body)
	}
}

func TestTuningOverrides_JSON_OK(t *testing.T) {
	tr := tuning.New()
	b, _ := tr.Bool("feature.x", false)
	_ = b.Set(true)

	h := TuningOverridesHandler(tr, WithTuningDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_overrides", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	var got tuningOverridesResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("ok=false, want true; got=%+v", got)
	}
	if len(got.Overrides) != 1 || got.Overrides[0].Key != "feature.x" {
		t.Fatalf("overrides=%+v, want one feature.x", got.Overrides)
	}
}

func TestTuningLookup_OK_Text(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Int64("limit", 10)

	h := TuningLookupHandler(tr)
	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_lookup?key=limit", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); !strings.Contains(body, "tuning\tlimit\tvalue\t10\n") {
		t.Fatalf("body=%q, want contain limit value line", body)
	}
}

func TestTuningLookup_MissingKey_400(t *testing.T) {
	tr := tuning.New()
	h := TuningLookupHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_lookup", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTuningLookup_InvalidKey_400(t *testing.T) {
	tr := tuning.New()
	h := TuningLookupHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_lookup?key=a/b", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTuningLookup_NotFound_404(t *testing.T) {
	tr := tuning.New()
	h := TuningLookupHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_lookup?key=missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusNotFound {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusNotFound)
	}
}

func TestTuningSet_OK_Text_AndAllowsEmptyValueWhenPresent(t *testing.T) {
	tr := tuning.New()
	s, _ := tr.String("msg", "hello")

	h := TuningSetHandler(tr)

	// Set to empty string: value param is present but empty.
	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_set?key=msg&value=", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if got := s.Get(); got != "" {
		t.Fatalf("msg=%q, want empty string", got)
	}
	if body := w.Body.String(); !strings.Contains(body, "tuning\tmsg\tnew.value\t\n") {
		t.Fatalf("body=%q, want contain new.value empty line", body)
	}
}

func TestTuningSet_MissingValue_400(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSetHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_set?key=feature.x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTuningSet_InvalidValue_400(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Int64("limit", 10)
	h := TuningSetHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_set?key=limit&value=abc", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
}

func TestTuningSet_RedactedKey_DoesNotEchoInputValueInError(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Int64("secret.limit", 10, tuning.WithRedactInt64())
	h := TuningSetHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_set?key=secret.limit&value=notanint", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusBadRequest {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusBadRequest)
	}
	var got tuningWriteResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("ok=true, want false; got=%+v", got)
	}
	if strings.Contains(got.Error, "notanint") {
		t.Fatalf("error=%q echoes input, want sanitized", got.Error)
	}
}

func TestTuningSet_KeyGuard_403(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSetHandler(tr, WithTuningAllowPrefixes("safe."))

	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_set?key=feature.x&value=true", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusForbidden {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusForbidden)
	}
}

func TestTuningSet_MethodNotAllowed(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSetHandler(tr, WithTuningDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_set?key=feature.x&value=true", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "POST" {
		t.Fatalf("Allow=%q, want %q", allow, "POST")
	}
}

func TestTuningResetToDefault_OK(t *testing.T) {
	tr := tuning.New()
	b, _ := tr.Bool("feature.x", false)
	_ = b.Set(true)

	h := TuningResetToDefaultHandler(tr)
	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_reset?key=feature.x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if got := b.Get(); got != false {
		t.Fatalf("feature.x=%v, want false", got)
	}
}

func TestTuningResetToLastValue_NoLast_409(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)

	h := TuningResetToLastValueHandler(tr, WithTuningDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_undo?key=feature.x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusConflict {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusConflict)
	}
}

func TestTuningResetToLastValue_OK(t *testing.T) {
	tr := tuning.New()
	d, _ := tr.Duration("timeout", 100*time.Millisecond)
	_ = d.Set(200 * time.Millisecond)

	h := TuningResetToLastValueHandler(tr)
	r := httptest.NewRequest(http.MethodPost, "http://example/tuning_undo?key=timeout", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if got := d.Get(); got != 100*time.Millisecond {
		t.Fatalf("timeout=%s, want %s", got, 100*time.Millisecond)
	}
}

func TestTuning_InvalidFormatFallsBackToText(t *testing.T) {
	tr := tuning.New()
	_, _ = tr.Bool("feature.x", false)
	h := TuningSnapshotHandler(tr, WithTuningDefaultFormat(Format(999)))

	r := httptest.NewRequest(http.MethodGet, "http://example/tuning_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}
