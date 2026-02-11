package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProvidedSnapshot_Text_OK_SortedAndStringUnquoted(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{
		"b": 1,
		"a": "hello",
	})
	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
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
	ai := strings.Index(body, "== a ==\n")
	bi := strings.Index(body, "== b ==\n")
	if ai < 0 || bi < 0 || ai > bi {
		t.Fatalf("sections not sorted: body=%q", body)
	}
	if !strings.Contains(body, "== a ==\nhello\n") {
		t.Fatalf("body=%q, want contain %q", body, "== a ==\\nhello\\n")
	}
	if !strings.Contains(body, "== b ==\n1\n") {
		t.Fatalf("body=%q, want contain %q", body, "== b ==\\n1\\n")
	}
}

func TestProvidedSnapshot_JSON_OK(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{
		"n": 1,
		"s": "hello",
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Error != "" || len(got.Errors) != 0 {
		t.Fatalf("got=%+v, want ok with no errors", got)
	}
	if string(got.Snapshots["n"]) != "1" {
		t.Fatalf("n=%q, want %q", string(got.Snapshots["n"]), "1")
	}
	if string(got.Snapshots["s"]) != `"hello"` {
		t.Fatalf("s=%q, want %q", string(got.Snapshots["s"]), `"hello"`)
	}
}

func TestProvidedSnapshot_MethodNotAllowed(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{"a": 1})
	r := httptest.NewRequest(http.MethodPost, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestProvidedSnapshot_QueryFormatOverridesOption(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{"a": 1}, WithProvidedSnapshotDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestProvidedSnapshot_Head_NoBody(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{"a": 1})
	r := httptest.NewRequest(http.MethodHead, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if w.Body.String() != "" {
		t.Fatalf("body=%q, want empty", w.Body.String())
	}
}

type marshalPanics struct{}

func (marshalPanics) MarshalJSON() ([]byte, error) {
	panic("boom")
}

func TestProvidedSnapshot_JSON_PanicInMarshal_IsolatedAndReported(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{
		"ok":  1,
		"bad": marshalPanics{},
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusInternalServerError)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("ok=true, want false; got=%+v", got)
	}
	if _, ok := got.Snapshots["ok"]; !ok {
		t.Fatalf("snapshots missing ok; got=%+v", got)
	}
	if _, ok := got.Snapshots["bad"]; ok {
		t.Fatalf("snapshots should not include bad; got=%+v", got)
	}
	if len(got.Errors) != 1 || got.Errors[0].Name != "bad" || !got.Errors[0].Panicked {
		t.Fatalf("errors=%+v, want one bad panicked error", got.Errors)
	}
}

func TestProvidedSnapshot_JSON_AtomicValue_Loaded(t *testing.T) {
	type snap struct {
		X int `json:"x"`
	}
	var av atomic.Value
	av.Store(snap{X: 1})

	h := ProvidedSnapshotHandler(map[string]any{
		"v": &av,
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	var decoded snap
	if err := json.Unmarshal(got.Snapshots["v"], &decoded); err != nil {
		t.Fatalf("unmarshal snapshot[v]: %v (raw=%q)", err, string(got.Snapshots["v"]))
	}
	if decoded.X != 1 {
		t.Fatalf("x=%d, want=1", decoded.X)
	}
}

func TestProvidedSnapshot_MaxBytes_TooLarge(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{"a": "0123456789"}, WithProvidedSnapshotMaxBytes(10))
	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusRequestEntityTooLarge)
	}
	if w.Body.String() != "response too large\n" {
		t.Fatalf("body=%q, want %q", w.Body.String(), "response too large\\n")
	}
}

func TestProvidedSnapshot_MaxBytes_TooLarge_JSON(t *testing.T) {
	h := ProvidedSnapshotHandler(
		map[string]any{"a": "0123456789"},
		WithProvidedSnapshotDefaultFormat(FormatJSON),
		WithProvidedSnapshotMaxBytes(10),
	)
	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusRequestEntityTooLarge)
	}
	if got := w.Body.String(); got != "{\"ok\":false,\"error\":\"response too large\"}\n" {
		t.Fatalf("body=%q, want %q", got, "{\"ok\":false,\"error\":\"response too large\"}\\n")
	}
}

func TestProvidedSnapshot_JSON_MarshalError_IsolatedAndReported(t *testing.T) {
	h := ProvidedSnapshotHandler(map[string]any{
		"ok":  1,
		"bad": make(chan int),
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusInternalServerError)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK {
		t.Fatalf("ok=true, want false; got=%+v", got)
	}
	if _, ok := got.Snapshots["ok"]; !ok {
		t.Fatalf("snapshots missing ok; got=%+v", got)
	}
	if _, ok := got.Snapshots["bad"]; ok {
		t.Fatalf("snapshots should not include bad; got=%+v", got)
	}
	if len(got.Errors) != 1 || got.Errors[0].Name != "bad" || got.Errors[0].Panicked {
		t.Fatalf("errors=%+v, want one bad non-panicked error", got.Errors)
	}
	if got.Errors[0].Error == "" {
		t.Fatalf("error message empty; got=%+v", got.Errors[0])
	}
}

func TestProvidedSnapshot_JSON_AtomicValue_Uninitialized_BestEffort(t *testing.T) {
	// Go's atomic.Value behavior for Load-before-Store is version-dependent:
	// some versions panic, some return nil. We accept either as "best-effort".
	var av atomic.Value
	h := ProvidedSnapshotHandler(map[string]any{
		"v": &av,
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	switch w.Result().StatusCode {
	case http.StatusOK:
		if !got.OK || len(got.Errors) != 0 {
			t.Fatalf("got=%+v, want ok with no errors", got)
		}
		if string(got.Snapshots["v"]) != "null" {
			t.Fatalf("v=%q, want %q", string(got.Snapshots["v"]), "null")
		}
	case http.StatusInternalServerError:
		if got.OK {
			t.Fatalf("ok=true, want false; got=%+v", got)
		}
		if _, ok := got.Snapshots["v"]; ok {
			t.Fatalf("snapshots should not include v; got=%+v", got)
		}
		if len(got.Errors) != 1 || got.Errors[0].Name != "v" || !got.Errors[0].Panicked {
			t.Fatalf("errors=%+v, want one v panicked error", got.Errors)
		}
	default:
		t.Fatalf("status=%d, want 200 or 500", w.Result().StatusCode)
	}
}

func TestProvidedSnapshot_ConstructionTimeSnapshot_MapMutationAfterCreateDoesNotAffectOutput(t *testing.T) {
	items := map[string]any{"a": 1}
	h := ProvidedSnapshotHandler(items, WithProvidedSnapshotDefaultFormat(FormatJSON))

	// Mutate original map after handler is created.
	items["a"] = 2

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("got=%+v, want ok", got)
	}
	if string(got.Snapshots["a"]) != "1" {
		t.Fatalf("a=%q, want %q", string(got.Snapshots["a"]), "1")
	}
}

func TestProvidedSnapshot_JSON_AtomicValue_ByValue_Forbidden(t *testing.T) {
	var av atomic.Value
	av.Store(1)

	h := ProvidedSnapshotHandler(map[string]any{
		"v": av, // passed by value (copied)
	}, WithProvidedSnapshotDefaultFormat(FormatJSON))

	r := httptest.NewRequest(http.MethodGet, "http://example/provided_snapshot", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusInternalServerError {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusInternalServerError)
	}
	var got providedSnapshotResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.OK || len(got.Errors) != 1 || got.Errors[0].Name != "v" {
		t.Fatalf("got=%+v, want ok=false with one v error", got)
	}
	if !strings.Contains(got.Errors[0].Error, "atomic.Value") {
		t.Fatalf("error=%q, want mention atomic.Value", got.Errors[0].Error)
	}
}
