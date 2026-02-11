package admin

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

// reportProvidedMaxBytes is the max bytes we include for the provided section in /report.
//
// This is a best-effort guardrail for accidental huge dumps. It does NOT cap the CPU/memory
// cost of serializing the provided values (those costs happen inside ops.ProvidedSnapshotHandler).
const reportProvidedMaxBytes = 256 << 10 // 256 KiB

type reportSource struct {
	path string
	h    http.Handler
}

type reportState struct {
	buildInfo        reportSource
	runtime          reportSource
	logLevelGet      reportSource
	tuningSnapshot   reportSource
	tuningOverrides  reportSource
	tasksSnapshot    reportSource
	providedSnapshot reportSource
}

type reportSection struct {
	name  string
	src   reportSource
	limit int // 0 => no limit
}

func (s reportSource) enabled() bool { return s.h != nil }

func (b *Builder) assembleReport() {
	if b == nil || b.report == nil {
		return
	}

	spec := *b.report
	path := spec.Path
	if path == "" {
		path = "/report"
	}
	path = normalizePathOrPanic(path)

	sections := make([]reportSection, 0, 8)
	add := func(name string, src reportSource, limit int) {
		if !src.enabled() {
			return
		}
		sections = append(sections, reportSection{name: name, src: src, limit: limit})
	}

	// Stable order. Keep it human-oriented.
	add("buildinfo", b.reportState.buildInfo, 0)
	add("runtime", b.reportState.runtime, 0)
	add("log.level", b.reportState.logLevelGet, 0)
	add("tuning.snapshot", b.reportState.tuningSnapshot, 0)
	add("tuning.overrides", b.reportState.tuningOverrides, 0)
	add("tasks.snapshot", b.reportState.tasksSnapshot, 0)
	add("provided", b.reportState.providedSnapshot, reportProvidedMaxBytes)

	var h http.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("admin: nil request")
		}
		// Align with ops: avoid caching operational responses.
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		// For HEAD, avoid doing any work.
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusOK)
			return
		}

		_, body := renderReport(r.Context(), sections)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	})

	h = spec.Guard.Middleware()(h)
	b.register(path, h)
}

func renderReport(ctx context.Context, sections []reportSection) (ok bool, body string) {
	now := time.Now()

	enabledNames := make([]string, 0, len(sections))
	for _, s := range sections {
		enabledNames = append(enabledNames, s.name)
	}

	// Render sections first so we can decide the top-level status line.
	ok = true
	var out strings.Builder
	out.Grow(4096)

	const (
		sectionHeaderPrefix = "=== "
		sectionHeaderSuffix = " ===\n"
		indentPrefix        = "| "
	)

	for _, sec := range sections {
		out.WriteByte('\n')
		out.WriteString(sectionHeaderPrefix)
		out.WriteString(sec.name)
		out.WriteString(sectionHeaderSuffix)

		if sec.name == "provided" {
			out.WriteString(indentPrefix)
			out.WriteString("note: below are user-provided snapshots (not built-in report sections)\n")
		}

		code, text, truncated := callHandlerTextCaptured(ctx, sec.src, sec.limit)
		if code < 200 || code >= 300 {
			ok = false
			out.WriteString(indentPrefix)
			out.WriteString("error: status ")
			out.WriteString(itoa(code))
			out.WriteByte('\n')
		}
		if text == "" {
			out.WriteString(indentPrefix)
			out.WriteString("(empty)\n")
		} else {
			appendIndented(&out, text, indentPrefix)
			if text[len(text)-1] != '\n' {
				out.WriteByte('\n')
			}
		}
		if truncated {
			out.WriteString(indentPrefix)
			out.WriteString("(truncated)\n")
		}
	}

	var header strings.Builder
	header.Grow(256)
	if ok {
		header.WriteString("ok\n")
	} else {
		header.WriteString("error: one or more sections failed\n")
	}
	header.WriteString("generated_at: ")
	header.WriteString(now.Format(time.RFC3339Nano))
	header.WriteByte('\n')
	if len(enabledNames) == 0 {
		header.WriteString("enabled sections: (none)\n")
	} else {
		header.WriteString("enabled sections: ")
		header.WriteString(strings.Join(enabledNames, ", "))
		header.WriteByte('\n')
	}

	// Ensure a blank line between header and first section.
	header.WriteByte('\n')
	header.WriteString(out.String())
	return ok, header.String()
}

func appendIndented(b *strings.Builder, s string, prefix string) {
	if b == nil || s == "" {
		return
	}
	if prefix == "" {
		b.WriteString(s)
		return
	}
	i := 0
	for {
		b.WriteString(prefix)
		j := strings.IndexByte(s[i:], '\n')
		if j < 0 {
			b.WriteString(s[i:])
			return
		}
		j += i
		b.WriteString(s[i : j+1])
		i = j + 1
		if i >= len(s) {
			return
		}
	}
}

func callHandlerTextCaptured(ctx context.Context, src reportSource, limit int) (status int, text string, truncated bool) {
	if src.h == nil {
		return http.StatusInternalServerError, "nil handler\n", false
	}

	path := src.path
	if path == "" {
		path = "/"
	}

	req := httptest.NewRequest(http.MethodGet, "http://admin.report.invalid"+path, nil).WithContext(ctx)

	rw := newTextCapture(limit)
	src.h.ServeHTTP(rw, req)
	status = rw.status
	if status == 0 {
		status = http.StatusOK
	}

	// Ignore any Content-Type produced by the sub-handler; /report is text-only.
	b := rw.buf.Bytes()
	if len(b) == 0 {
		return status, "", rw.truncated
	}
	return status, string(b), rw.truncated
}

type textCapture struct {
	hdr       http.Header
	status    int
	buf       bytes.Buffer
	limit     int
	truncated bool
}

func newTextCapture(limit int) *textCapture {
	return &textCapture{
		hdr:   make(http.Header),
		limit: limit,
	}
}

func (w *textCapture) Header() http.Header { return w.hdr }

func (w *textCapture) WriteHeader(statusCode int) {
	if w.status == 0 {
		w.status = statusCode
	}
}

func (w *textCapture) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	if w.limit <= 0 {
		return w.buf.Write(p)
	}
	remain := w.limit - w.buf.Len()
	if remain <= 0 {
		w.truncated = true
		// Discard but report as written.
		return len(p), nil
	}
	if len(p) <= remain {
		_, _ = w.buf.Write(p)
		return len(p), nil
	}
	_, _ = w.buf.Write(p[:remain])
	w.truncated = true
	return len(p), nil
}
