package ops

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
)

type providedSnapshotConfig struct {
	format   Format
	maxBytes int
}

// ProvidedSnapshotOption configures ProvidedSnapshotHandler.
type ProvidedSnapshotOption func(*providedSnapshotConfig)

// WithProvidedSnapshotDefaultFormat sets the default response format.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithProvidedSnapshotDefaultFormat(f Format) ProvidedSnapshotOption {
	return func(c *providedSnapshotConfig) { c.format = f }
}

// WithProvidedSnapshotMaxBytes sets an upper bound for the response body size.
//
// This is a safety valve to avoid accidental huge dumps. If the response exceeds
// this limit, the handler responds with HTTP 413.
//
// Note: this limit is enforced on the final rendered body (JSON/text). It is not
// a guarantee on serialization CPU/memory cost.
//
// <= 0 means "no limit".
func WithProvidedSnapshotMaxBytes(n int) ProvidedSnapshotOption {
	return func(c *providedSnapshotConfig) { c.maxBytes = n }
}

func applyProvidedSnapshotOptions(opts []ProvidedSnapshotOption) providedSnapshotConfig {
	cfg := providedSnapshotConfig{
		format:   FormatText,
		maxBytes: 4 << 20, // 4 MiB (conservative default; can be increased or disabled).
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.format != FormatText && cfg.format != FormatJSON {
		cfg.format = FormatText
	}
	return cfg
}

// ProvidedSnapshotHandler returns a handler that outputs injected snapshots.
//
// It is read-only and intended for operational inspection (often high-sensitivity).
// It does not perform authn/authz decisions; protect it with your own middleware.
//
// Input:
//   - items: name -> any (static values; may include pointers / references)
//   - items is snapshotted at handler construction time: the map is copied and
//     key ordering is fixed. Mutating the original map after creating the handler
//     does not affect the output.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
//   - Best-effort safety: per-item marshal errors/panics do not crash the handler; they
//     are reported in the response (partial success).
//
// Notes on "live" references:
//   - Passing pointers/maps/slices may appear "live", but can introduce data races if the
//     underlying object is mutated concurrently.
//   - Prefer copy-on-write snapshots (e.g. via *atomic.Value) if you need runtime updates.
func ProvidedSnapshotHandler(items map[string]any, opts ...ProvidedSnapshotOption) http.Handler {
	cfg := applyProvidedSnapshotOptions(opts)

	// Snapshot names and values at handler construction time to avoid
	// concurrent-map hazards and keep output stable.
	names := make([]string, 0, len(items))
	vals := make(map[string]any, len(items))
	for name, v := range items {
		if name == "" {
			panic("ops: provided snapshot item has empty name")
		}
		names = append(names, name)
		vals[name] = v
	}
	sort.Strings(names)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeProvidedSnapshot(w, r, format, cfg.maxBytes, http.StatusMethodNotAllowed, providedSnapshotResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap, errs := buildProvidedSnapshot(names, vals)
		code := http.StatusOK
		ok := len(errs) == 0
		msg := ""
		if !ok {
			code = http.StatusInternalServerError
			msg = "one or more items failed"
		}
		writeProvidedSnapshot(w, r, format, cfg.maxBytes, code, providedSnapshotResponse{
			OK:        ok,
			Error:     msg,
			Snapshots: snap,
			Errors:    errs,
		})
	})
}

type providedSnapshotResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Snapshots map[string]json.RawMessage `json:"snapshots,omitempty"`
	Errors    []ProvidedSnapshotError    `json:"errors,omitempty"`
}

// ProvidedSnapshotError represents a per-item error.
type ProvidedSnapshotError struct {
	Name     string `json:"name"`
	Error    string `json:"error"`
	Panicked bool   `json:"panicked,omitempty"`
}

func buildProvidedSnapshot(names []string, vals map[string]any) (map[string]json.RawMessage, []ProvidedSnapshotError) {
	out := make(map[string]json.RawMessage, len(names))
	var errs []ProvidedSnapshotError
	for _, name := range names {
		v, ok := vals[name]
		if !ok {
			// Should not happen (names are derived from vals), but keep stable behavior.
			errs = append(errs, ProvidedSnapshotError{Name: name, Error: "missing value"})
			continue
		}
		raw, perr, panicked := safeMarshalSnapshotValue(v)
		if perr != "" {
			errs = append(errs, ProvidedSnapshotError{Name: name, Error: perr, Panicked: panicked})
			continue
		}
		out[name] = raw
	}
	if len(out) == 0 {
		out = nil
	}
	return out, errs
}

func safeMarshalSnapshotValue(v any) (raw json.RawMessage, errMsg string, panicked bool) {
	defer func() {
		if p := recover(); p != nil {
			panicked = true
			errMsg = fmt.Sprintf("panic: %v", p)
			raw = nil
		}
	}()

	// Best-effort support for copy-on-write containers without expanding the API.
	switch x := v.(type) {
	case atomic.Value:
		// atomic.Value must not be copied after first use. If it is passed as an interface
		// value, it is already copied. Require pointer to avoid subtle races/bugs.
		return nil, "atomic.Value must be passed as *atomic.Value", false
	case *atomic.Value:
		if x == nil {
			return nil, "nil atomic.Value", false
		}
		v = x.Load() // may panic if uninitialized
	}

	b, err := json.Marshal(v)
	if err != nil {
		return nil, err.Error(), false
	}
	return json.RawMessage(b), "", false
}

func writeProvidedSnapshot(w http.ResponseWriter, r *http.Request, f Format, maxBytes int, code int, resp providedSnapshotResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		if r.Method == http.MethodHead {
			w.WriteHeader(code)
			return
		}
		status := code
		body, err := json.Marshal(resp)
		if err != nil {
			// Best-effort fallback.
			status = http.StatusInternalServerError
			body = []byte(`{"ok":false,"error":"internal error"}`)
		}
		// Always end with newline for greppability.
		if maxBytes > 0 && len(body)+1 > maxBytes {
			status = http.StatusRequestEntityTooLarge
			body = []byte(`{"ok":false,"error":"response too large"}`)
		}
		w.WriteHeader(status)
		_, _ = w.Write(append(body, '\n'))
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if r.Method == http.MethodHead {
			w.WriteHeader(code)
			return
		}
		s := renderProvidedSnapshotText(resp)
		status := code
		if maxBytes > 0 && len(s) > maxBytes {
			status = http.StatusRequestEntityTooLarge
			s = "response too large\n"
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(s))
	}
}

func renderProvidedSnapshotText(resp providedSnapshotResponse) string {
	var b strings.Builder
	b.Grow(512)

	if resp.OK {
		_, _ = b.WriteString("ok\n")
	} else {
		if resp.Error != "" {
			_, _ = b.WriteString("error: ")
			_, _ = b.WriteString(resp.Error)
			_ = b.WriteByte('\n')
		} else {
			_, _ = b.WriteString("error\n")
		}
	}

	// Build error lookup for quick per-section rendering.
	errByName := make(map[string]ProvidedSnapshotError, len(resp.Errors))
	for _, e := range resp.Errors {
		if e.Name == "" {
			continue
		}
		errByName[e.Name] = e
	}

	// Stable order: sort names from snapshots+errors union.
	set := make(map[string]struct{}, len(resp.Snapshots)+len(errByName))
	for k := range resp.Snapshots {
		if k != "" {
			set[k] = struct{}{}
		}
	}
	for k := range errByName {
		if k != "" {
			set[k] = struct{}{}
		}
	}
	names := make([]string, 0, len(set))
	for k := range set {
		names = append(names, k)
	}
	sort.Strings(names)

	for _, name := range names {
		_ = b.WriteByte('\n')
		_, _ = b.WriteString("== ")
		_, _ = b.WriteString(name)
		_, _ = b.WriteString(" ==\n")

		if e, ok := errByName[name]; ok {
			_, _ = b.WriteString("error: ")
			if e.Error != "" {
				_, _ = b.WriteString(e.Error)
			} else {
				_, _ = b.WriteString("error")
			}
			_ = b.WriteByte('\n')
			continue
		}

		raw, ok := resp.Snapshots[name]
		if !ok || len(raw) == 0 {
			_, _ = b.WriteString("error\n")
			continue
		}

		// If it's a JSON string, show it without quotes for readability.
		// Otherwise, pretty-print JSON.
		if len(raw) >= 2 && raw[0] == '"' && raw[len(raw)-1] == '"' {
			var s string
			if err := json.Unmarshal(raw, &s); err == nil {
				_, _ = b.WriteString(s)
				if len(s) == 0 || s[len(s)-1] != '\n' {
					_ = b.WriteByte('\n')
				}
				continue
			}
		}

		var buf bytes.Buffer
		if err := json.Indent(&buf, raw, "", "  "); err == nil {
			_, _ = b.Write(buf.Bytes())
			_ = b.WriteByte('\n')
			continue
		}

		// Fallback: raw bytes.
		_, _ = b.Write(raw)
		_ = b.WriteByte('\n')
	}

	return b.String()
}
