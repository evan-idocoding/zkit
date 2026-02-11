package ops

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime/debug"
	"strings"
	"testing"
)

func TestBuildInfo_Text_OK(t *testing.T) {
	h := BuildInfoHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo", nil)
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
	if !strings.Contains(body, "mod\t") {
		t.Fatalf("body=%q, want contain %q", body, "mod\t")
	}
	if !strings.Contains(body, "go\t") {
		t.Fatalf("body=%q, want contain %q", body, "go\t")
	}
}

func TestBuildInfo_JSON_OK(t *testing.T) {
	h := BuildInfoHandler(WithBuildInfoDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo", nil)
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
	var got buildInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK {
		t.Fatalf("ok=false, want true; got=%+v", got)
	}
	if got.Build == nil {
		t.Fatalf("build=nil, want non-nil; got=%+v", got)
	}
	if got.Build.Runtime.Version == "" {
		t.Fatalf("runtime.version empty, want non-empty; got=%+v", got.Build.Runtime)
	}
}

func TestBuildInfo_QueryFormatOverridesOption(t *testing.T) {
	h := BuildInfoHandler(WithBuildInfoDefaultFormat(FormatJSON))
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo?format=text", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "mod\t") {
		t.Fatalf("body=%q, want contain %q", body, "mod\t")
	}
}

func TestBuildInfo_QueryJSONOverridesDefaultText(t *testing.T) {
	h := BuildInfoHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo?format=json", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if resp := w.Result(); resp.StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", resp.StatusCode, http.StatusOK)
	}
	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Fatalf("Content-Type=%q, want application/json", ct)
	}
	var got buildInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Build == nil {
		t.Fatalf("got=%+v, want ok with build", got)
	}
}

func TestBuildInfo_MethodNotAllowed(t *testing.T) {
	h := BuildInfoHandler()
	r := httptest.NewRequest(http.MethodPost, "http://example/buildinfo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusMethodNotAllowed)
	}
	if allow := w.Result().Header.Get("Allow"); allow != "GET, HEAD" {
		t.Fatalf("Allow=%q, want %q", allow, "GET, HEAD")
	}
}

func TestBuildInfo_Head_NoBody(t *testing.T) {
	h := BuildInfoHandler()
	r := httptest.NewRequest(http.MethodHead, "http://example/buildinfo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	if body := w.Body.String(); body != "" {
		t.Fatalf("body=%q, want empty", body)
	}
}

func TestBuildInfo_CacheControl_NoStore(t *testing.T) {
	h := BuildInfoHandler()
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if cc := w.Result().Header.Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control=%q, want %q", cc, "no-store")
	}
}

func TestBuildInfo_InvalidFormatFallsBackToText(t *testing.T) {
	h := BuildInfoHandler(WithBuildInfoDefaultFormat(Format(999)))
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if ct := w.Result().Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("Content-Type=%q, want text/plain", ct)
	}
}

func TestBuildInfo_IncludeSettings(t *testing.T) {
	// Build settings are not guaranteed to be present in all builds/environments.
	// In particular, some CI/test binaries may have an empty Settings list even when
	// debug.ReadBuildInfo() is available.
	if snap, ok := readBuildInfoSnapshot(); !ok || len(snap.settings) == 0 {
		t.Skip("build settings not available in this build")
	}

	h := BuildInfoHandler(
		WithBuildInfoDefaultFormat(FormatJSON),
		WithBuildInfoIncludeSettings(true),
	)
	r := httptest.NewRequest(http.MethodGet, "http://example/buildinfo", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if w.Result().StatusCode != http.StatusOK {
		t.Fatalf("status=%d, want=%d", w.Result().StatusCode, http.StatusOK)
	}
	var got buildInfoResponse
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !got.OK || got.Build == nil {
		t.Fatalf("got=%+v, want ok with build", got)
	}
	if len(got.Build.Settings) == 0 {
		t.Fatalf("settings empty, want non-empty")
	}
}

func TestExtractVCS_OK(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.revision", Value: "abc123"},
		{Key: "vcs.time", Value: "2026-01-01T00:00:00Z"},
		{Key: "vcs.modified", Value: "true"},
	}
	v, ok := extractVCS(settings)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if v.System != "git" || v.Revision != "abc123" || v.Time != "2026-01-01T00:00:00Z" {
		t.Fatalf("v=%+v, want system/revision/time set", v)
	}
	if v.Modified == nil || *v.Modified != true {
		t.Fatalf("modified=%v, want true pointer", v.Modified)
	}
}

func TestExtractVCS_None(t *testing.T) {
	if _, ok := extractVCS([]debug.BuildSetting{{Key: "foo", Value: "bar"}}); ok {
		t.Fatalf("ok=true, want false")
	}
}

func TestExtractVCS_UnparsableModifiedStillOK(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs", Value: "git"},
		{Key: "vcs.modified", Value: "notabool"},
	}
	v, ok := extractVCS(settings)
	if !ok {
		t.Fatalf("ok=false, want true")
	}
	if v.System != "git" {
		t.Fatalf("system=%q, want %q", v.System, "git")
	}
	if v.Modified != nil {
		t.Fatalf("modified=%v, want nil (unparsable)", v.Modified)
	}
}

func TestFormatModuleTextLong_WithReplace(t *testing.T) {
	m := BuildInfoModule{
		Path:    "example.com/a",
		Version: "v1.0.0",
		Sum:     "h1:sum",
		Replace: &BuildInfoModule{Path: "example.com/b", Version: "v2.0.0", Sum: "h1:sum2"},
	}
	s := formatModuleTextLong(m)
	if !strings.Contains(s, "=>") {
		t.Fatalf("s=%q, want contain %q", s, "=>")
	}
	if !strings.Contains(s, "example.com/a") || !strings.Contains(s, "example.com/b") {
		t.Fatalf("s=%q, want contain paths", s)
	}
}

func TestRenderBuildInfoText_IncludesVCSAndSettings(t *testing.T) {
	mod := true
	s := BuildInfoSnapshot{
		Module:  BuildInfoModule{Path: "example.com/app", Version: "v0.0.0"},
		Runtime: BuildInfoRuntime{Version: "go1.x", GOOS: "linux", GOARCH: "amd64", Compiler: "gc"},
		VCS: &BuildInfoVCS{
			System:   "git",
			Revision: "abc123",
			Time:     "2026-01-01T00:00:00Z",
			Modified: &mod,
		},
		Settings: []BuildInfoSetting{
			{Key: "vcs", Value: "git"},
			{Key: "CGO_ENABLED", Value: "0"},
			{Key: "", Value: "ignored"},
		},
	}
	out := renderBuildInfoText(s)
	if !strings.Contains(out, "build\tCGO_ENABLED\t0\n") {
		t.Fatalf("out=%q, want contain CGO_ENABLED line", out)
	}
	if strings.Contains(out, "build\t\tignored") {
		t.Fatalf("out=%q, want empty-key setting skipped", out)
	}
}

func TestRenderBuildInfoText_NoDuplicateVCSLinesWhenSettingsIncluded(t *testing.T) {
	mod := true
	s := BuildInfoSnapshot{
		Module:  BuildInfoModule{Path: "example.com/app", Version: "v0.0.0"},
		Runtime: BuildInfoRuntime{Version: "go1.x", GOOS: "linux", GOARCH: "amd64", Compiler: "gc"},
		VCS: &BuildInfoVCS{
			System:   "git",
			Revision: "abc123",
			Time:     "2026-01-01T00:00:00Z",
			Modified: &mod,
		},
		Settings: []BuildInfoSetting{
			{Key: "vcs.revision", Value: "abc123"},
		},
	}
	out := renderBuildInfoText(s)
	const line = "build\tvcs.revision\tabc123\n"
	if n := strings.Count(out, line); n != 1 {
		t.Fatalf("count=%d, want 1; out=%q", n, out)
	}
}
