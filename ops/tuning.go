package ops

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/evan-idocoding/zkit/rt/tuning"
)

type tuningConfig struct {
	format Format

	guards []func(key string) bool
	guard  func(key string) bool
}

// TuningOption configures tuning handlers.
type TuningOption func(*tuningConfig)

// WithTuningDefaultFormat sets the default response format for tuning handlers.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithTuningDefaultFormat(f Format) TuningOption {
	return func(c *tuningConfig) { c.format = f }
}

// WithTuningKeyGuard appends a key guard.
//
// All guards are combined with AND: a key is allowed only if all guards allow it.
// This applies to both read and write handlers.
func WithTuningKeyGuard(fn func(key string) bool) TuningOption {
	return func(c *tuningConfig) {
		if fn != nil {
			c.guards = append(c.guards, fn)
		}
	}
}

// WithTuningAllowPrefixes restricts keys to the provided prefixes.
//
// Safety note: if no non-empty prefix is provided, this option denies all keys.
func WithTuningAllowPrefixes(prefixes ...string) TuningOption {
	var ps []string
	for _, p := range prefixes {
		if p == "" {
			continue
		}
		ps = append(ps, p)
	}
	return WithTuningKeyGuard(func(key string) bool {
		if len(ps) == 0 {
			return false
		}
		for _, p := range ps {
			if strings.HasPrefix(key, p) {
				return true
			}
		}
		return false
	})
}

// WithTuningAllowKeys restricts keys to the provided explicit set.
//
// Safety note: if no non-empty key is provided, this option denies all keys.
func WithTuningAllowKeys(keys ...string) TuningOption {
	set := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		if k == "" {
			continue
		}
		set[k] = struct{}{}
	}
	return WithTuningKeyGuard(func(key string) bool {
		if len(set) == 0 {
			return false
		}
		_, ok := set[key]
		return ok
	})
}

func applyTuningOptions(opts []TuningOption) tuningConfig {
	cfg := tuningConfig{
		format: FormatText,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(&cfg)
		}
	}
	if cfg.format != FormatText && cfg.format != FormatJSON {
		cfg.format = FormatText
	}
	if len(cfg.guards) > 0 {
		cfg.guard = func(key string) bool { return true }
		for _, g := range cfg.guards {
			if g == nil {
				continue
			}
			prev := cfg.guard
			gg := g
			cfg.guard = func(key string) bool { return prev(key) && gg(key) }
		}
	}
	return cfg
}

// TuningSnapshotHandler returns a handler that outputs a tuning snapshot.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func TuningSnapshotHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeTuningSnapshot(w, r, format, http.StatusMethodNotAllowed, tuningSnapshotResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap := TuningSnapshot(t)
		if cfg.guard != nil {
			snap = filterSnapshot(snap, cfg.guard)
		}
		writeTuningSnapshot(w, r, format, http.StatusOK, tuningSnapshotResponse{
			OK:     true,
			Tuning: &snap,
		})
	})
}

// TuningOverridesHandler returns a handler that outputs ExportOverrides (Value != DefaultValue).
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func TuningOverridesHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeTuningOverrides(w, r, format, http.StatusMethodNotAllowed, tuningOverridesResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		ovs := TuningOverrides(t)
		if cfg.guard != nil {
			ovs = filterOverrides(ovs, cfg.guard)
		}
		writeTuningOverrides(w, r, format, http.StatusOK, tuningOverridesResponse{
			OK:        true,
			Overrides: ovs,
		})
	})
}

// TuningLookupHandler returns a handler that looks up a single key.
//
// Input:
//   - GET/HEAD only
//   - URL query: ?key=<tuning key>
func TuningLookupHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeTuningLookup(w, r, format, http.StatusMethodNotAllowed, tuningLookupResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		key, ok := getQueryRequired(r, "key")
		if !ok || key == "" {
			writeTuningLookup(w, r, format, http.StatusBadRequest, tuningLookupResponse{
				OK:    false,
				Error: "missing key",
			})
			return
		}
		if err := validateTuningKey(key); err != nil {
			writeTuningLookup(w, r, format, http.StatusBadRequest, tuningLookupResponse{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(key) {
			writeTuningLookup(w, r, format, http.StatusForbidden, tuningLookupResponse{
				OK:    false,
				Error: "key not allowed",
			})
			return
		}

		it, found := TuningLookup(t, key)
		if !found {
			writeTuningLookup(w, r, format, http.StatusNotFound, tuningLookupResponse{
				OK:    false,
				Error: "key not found",
			})
			return
		}

		writeTuningLookup(w, r, format, http.StatusOK, tuningLookupResponse{
			OK:   true,
			Item: &it,
		})
	})
}

// TuningSetHandler returns a handler that sets a tuning key from string.
//
// Input:
//   - POST only
//   - URL query: ?key=<tuning key>&value=<string representation>
//
// value can be empty string if the underlying variable type allows it.
func TuningSetHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTuningWrite(w, r, format, http.StatusMethodNotAllowed, tuningWriteResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		key, ok := getQueryRequired(r, "key")
		if !ok || key == "" {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: "missing key",
			})
			return
		}
		if err := validateTuningKey(key); err != nil {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(key) {
			writeTuningWrite(w, r, format, http.StatusForbidden, tuningWriteResponse{
				OK:    false,
				Error: "key not allowed",
			})
			return
		}

		value, hasValue := getQueryRaw(r, "value")
		if !hasValue {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: "missing value",
			})
			return
		}

		old, found := TuningLookup(t, key)
		if !found {
			writeTuningWrite(w, r, format, http.StatusNotFound, tuningWriteResponse{
				OK:    false,
				Error: "key not found",
			})
			return
		}

		if err := t.SetFromString(key, value); err != nil {
			code := mapTuningWriteErrorToStatus(err)
			errMsg := err.Error()
			if isRedactedItem(old) {
				errMsg = sanitizeTuningWriteError(err)
			}
			writeTuningWrite(w, r, format, code, tuningWriteResponse{
				OK:    false,
				Error: errMsg,
				Key:   key,
				Old:   &old,
			})
			return
		}

		newIt, _ := TuningLookup(t, key)
		writeTuningWrite(w, r, format, http.StatusOK, tuningWriteResponse{
			OK:  true,
			Key: key,
			Old: &old,
			New: &newIt,
		})
	})
}

// TuningResetToDefaultHandler returns a handler that resets a key to its default.
//
// Input:
//   - POST only
//   - URL query: ?key=<tuning key>
func TuningResetToDefaultHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTuningWrite(w, r, format, http.StatusMethodNotAllowed, tuningWriteResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		key, ok := getQueryRequired(r, "key")
		if !ok || key == "" {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: "missing key",
			})
			return
		}
		if err := validateTuningKey(key); err != nil {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(key) {
			writeTuningWrite(w, r, format, http.StatusForbidden, tuningWriteResponse{
				OK:    false,
				Error: "key not allowed",
			})
			return
		}

		old, found := TuningLookup(t, key)
		if !found {
			writeTuningWrite(w, r, format, http.StatusNotFound, tuningWriteResponse{
				OK:    false,
				Error: "key not found",
			})
			return
		}

		if err := t.ResetToDefault(key); err != nil {
			code := mapTuningWriteErrorToStatus(err)
			errMsg := err.Error()
			if isRedactedItem(old) {
				errMsg = sanitizeTuningWriteError(err)
			}
			writeTuningWrite(w, r, format, code, tuningWriteResponse{
				OK:    false,
				Error: errMsg,
				Key:   key,
				Old:   &old,
			})
			return
		}

		newIt, _ := TuningLookup(t, key)
		writeTuningWrite(w, r, format, http.StatusOK, tuningWriteResponse{
			OK:  true,
			Key: key,
			Old: &old,
			New: &newIt,
		})
	})
}

// TuningResetToLastValueHandler returns a handler that undoes one step for a key.
//
// Input:
//   - POST only
//   - URL query: ?key=<tuning key>
func TuningResetToLastValueHandler(t *tuning.Tuning, opts ...TuningOption) http.Handler {
	if t == nil {
		panic("ops: nil tuning.Tuning")
	}
	cfg := applyTuningOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeTuningWrite(w, r, format, http.StatusMethodNotAllowed, tuningWriteResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		key, ok := getQueryRequired(r, "key")
		if !ok || key == "" {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: "missing key",
			})
			return
		}
		if err := validateTuningKey(key); err != nil {
			writeTuningWrite(w, r, format, http.StatusBadRequest, tuningWriteResponse{
				OK:    false,
				Error: err.Error(),
			})
			return
		}
		if cfg.guard != nil && !cfg.guard(key) {
			writeTuningWrite(w, r, format, http.StatusForbidden, tuningWriteResponse{
				OK:    false,
				Error: "key not allowed",
			})
			return
		}

		old, found := TuningLookup(t, key)
		if !found {
			writeTuningWrite(w, r, format, http.StatusNotFound, tuningWriteResponse{
				OK:    false,
				Error: "key not found",
			})
			return
		}

		if err := t.ResetToLastValue(key); err != nil {
			code := mapTuningWriteErrorToStatus(err)
			errMsg := err.Error()
			if isRedactedItem(old) {
				errMsg = sanitizeTuningWriteError(err)
			}
			writeTuningWrite(w, r, format, code, tuningWriteResponse{
				OK:    false,
				Error: errMsg,
				Key:   key,
				Old:   &old,
			})
			return
		}

		newIt, _ := TuningLookup(t, key)
		writeTuningWrite(w, r, format, http.StatusOK, tuningWriteResponse{
			OK:  true,
			Key: key,
			Old: &old,
			New: &newIt,
		})
	})
}

// TuningSnapshot returns a point-in-time snapshot for ops usage.
func TuningSnapshot(t *tuning.Tuning) tuning.Snapshot {
	if t == nil {
		return tuning.Snapshot{}
	}
	return t.Snapshot()
}

// TuningOverrides returns current overrides (Value != DefaultValue) for ops usage.
func TuningOverrides(t *tuning.Tuning) []tuning.OverrideItem {
	if t == nil {
		return nil
	}
	return t.ExportOverrides()
}

// TuningLookup returns a point-in-time view for a single key (redaction rules apply).
func TuningLookup(t *tuning.Tuning, key string) (tuning.Item, bool) {
	if t == nil {
		return tuning.Item{}, false
	}
	return t.Lookup(key)
}

type tuningSnapshotResponse struct {
	OK     bool             `json:"ok"`
	Error  string           `json:"error,omitempty"`
	Tuning *tuning.Snapshot `json:"tuning,omitempty"`
}

type tuningOverridesResponse struct {
	OK        bool                  `json:"ok"`
	Error     string                `json:"error,omitempty"`
	Overrides []tuning.OverrideItem `json:"overrides,omitempty"`
}

type tuningLookupResponse struct {
	OK    bool         `json:"ok"`
	Error string       `json:"error,omitempty"`
	Item  *tuning.Item `json:"item,omitempty"`
}

type tuningWriteResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Key string       `json:"key,omitempty"`
	Old *tuning.Item `json:"old,omitempty"`
	New *tuning.Item `json:"new,omitempty"`
}

func writeTuningSnapshot(w http.ResponseWriter, r *http.Request, f Format, code int, resp tuningSnapshotResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		if !resp.OK {
			writeTextError(w, resp.Error)
			return
		}
		if resp.Tuning == nil {
			writeTextError(w, "error")
			return
		}
		_, _ = w.Write([]byte(renderTuningSnapshotText(*resp.Tuning)))
	}
}

func writeTuningOverrides(w http.ResponseWriter, r *http.Request, f Format, code int, resp tuningOverridesResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		if !resp.OK {
			writeTextError(w, resp.Error)
			return
		}
		_, _ = w.Write([]byte(renderTuningOverridesText(resp.Overrides)))
	}
}

func writeTuningLookup(w http.ResponseWriter, r *http.Request, f Format, code int, resp tuningLookupResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		if !resp.OK {
			writeTextError(w, resp.Error)
			return
		}
		if resp.Item == nil {
			writeTextError(w, "error")
			return
		}
		_, _ = w.Write([]byte(renderTuningItemText(*resp.Item)))
	}
}

func writeTuningWrite(w http.ResponseWriter, r *http.Request, f Format, code int, resp tuningWriteResponse) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(resp)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if !resp.OK {
			writeTextError(w, resp.Error)
			return
		}
		_, _ = w.Write([]byte(renderTuningWriteText(resp.Key, resp.Old, resp.New)))
	}
}

func writeTextError(w http.ResponseWriter, msg string) {
	if msg != "" {
		_, _ = w.Write([]byte(msg + "\n"))
		return
	}
	_, _ = w.Write([]byte("error\n"))
}

func renderTuningSnapshotText(s tuning.Snapshot) string {
	var b strings.Builder
	b.Grow(256)
	for _, it := range s.Items {
		appendTuningItemLines(&b, it)
	}
	return b.String()
}

func renderTuningOverridesText(ovs []tuning.OverrideItem) string {
	// Stable and greppable.
	// Format: tuning_override\t<key>\t<type>\t<value>\n
	var b strings.Builder
	b.Grow(128)
	for _, ov := range ovs {
		if ov.Key == "" {
			continue
		}
		b.WriteString("tuning_override")
		b.WriteByte('\t')
		b.WriteString(ov.Key)
		b.WriteByte('\t')
		b.WriteString(string(ov.Type))
		b.WriteByte('\t')
		b.WriteString(escapeTextField(ov.Value))
		b.WriteByte('\n')
	}
	return b.String()
}

func renderTuningItemText(it tuning.Item) string {
	var b strings.Builder
	b.Grow(128)
	appendTuningItemLines(&b, it)
	return b.String()
}

func appendTuningItemLines(b *strings.Builder, it tuning.Item) {
	// Keep it stable and greppable: one key per line, tab-separated fields.
	// Format: tuning\t<key>\t<field>\t<value>\n
	if b == nil || it.Key == "" {
		return
	}

	write := func(field, value string) {
		if field == "" {
			return
		}
		b.WriteString("tuning")
		b.WriteByte('\t')
		b.WriteString(it.Key)
		b.WriteByte('\t')
		b.WriteString(field)
		b.WriteByte('\t')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	write("type", string(it.Type))
	write("value", formatTuningAny(it.Value))
	write("default", formatTuningAny(it.DefaultValue))
	write("source", it.Source.String())
	if !it.LastUpdatedAt.IsZero() {
		write("last_updated_at", it.LastUpdatedAt.Format(time.RFC3339Nano))
	}
}

func renderTuningWriteText(key string, old, newIt *tuning.Item) string {
	var b strings.Builder
	b.Grow(256)

	// Unify with Snapshot/Lookup text shape:
	//   tuning\t<key>\t<field>\t<value>\n
	//
	// Use "old.*" / "new.*" as field namespaces.
	effectiveKey := key
	if effectiveKey == "" {
		if newIt != nil && newIt.Key != "" {
			effectiveKey = newIt.Key
		} else if old != nil && old.Key != "" {
			effectiveKey = old.Key
		}
	}
	if effectiveKey == "" {
		return ""
	}

	write := func(field, value string) {
		if field == "" {
			return
		}
		b.WriteString("tuning")
		b.WriteByte('\t')
		b.WriteString(effectiveKey)
		b.WriteByte('\t')
		b.WriteString(field)
		b.WriteByte('\t')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	if old != nil {
		write("old.type", string(old.Type))
		write("old.value", formatTuningAny(old.Value))
		write("old.default", formatTuningAny(old.DefaultValue))
		write("old.source", old.Source.String())
		if !old.LastUpdatedAt.IsZero() {
			write("old.last_updated_at", old.LastUpdatedAt.Format(time.RFC3339Nano))
		}
	}
	if newIt != nil {
		write("new.type", string(newIt.Type))
		write("new.value", formatTuningAny(newIt.Value))
		write("new.default", formatTuningAny(newIt.DefaultValue))
		write("new.source", newIt.Source.String())
		if !newIt.LastUpdatedAt.IsZero() {
			write("new.last_updated_at", newIt.LastUpdatedAt.Format(time.RFC3339Nano))
		}
	}
	return b.String()
}

func formatTuningAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return escapeTextField(x)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case time.Duration:
		return x.String()
	default:
		// Fallback (should rarely happen).
		return stringifyAny(v)
	}
}

func stringifyAny(v any) string {
	// Avoid fmt import on hot-ish paths; keep it simple and safe.
	// For unknown values, json is a reasonable representation.
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	// If it's a JSON string, drop quotes for readability.
	if len(b) >= 2 && b[0] == '"' && b[len(b)-1] == '"' {
		s, err := strconv.Unquote(string(b))
		if err == nil {
			return s
		}
	}
	return string(b)
}

func escapeTextField(s string) string {
	// Text outputs in ops are line-based and tab-separated.
	// Escape control characters to prevent output injection / parsing ambiguity.
	//
	// Rules:
	//   - '\'  => '\\'
	//   - '\t' => '\t'
	//   - '\r' => '\r'
	//   - '\n' => '\n'
	//   - other ASCII control chars (0x00-0x1f) => \u00XX
	//
	// If no escaping is needed, returns s as-is.
	if s == "" {
		return s
	}
	need := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '\\' || c == '\t' || c == '\r' || c == '\n' || c < 0x20 {
			need = true
			break
		}
	}
	if !need {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\':
			b.WriteString(`\\`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\n':
			b.WriteString(`\n`)
		default:
			if c < 0x20 {
				const hex = "0123456789abcdef"
				b.WriteString(`\u00`)
				b.WriteByte(hex[c>>4])
				b.WriteByte(hex[c&0x0f])
			} else {
				b.WriteByte(c)
			}
		}
	}
	return b.String()
}

func filterSnapshot(s tuning.Snapshot, guard func(string) bool) tuning.Snapshot {
	if guard == nil || len(s.Items) == 0 {
		return s
	}
	out := tuning.Snapshot{Items: make([]tuning.Item, 0, len(s.Items))}
	for _, it := range s.Items {
		if it.Key == "" {
			continue
		}
		if guard(it.Key) {
			out.Items = append(out.Items, it)
		}
	}
	return out
}

func filterOverrides(ovs []tuning.OverrideItem, guard func(string) bool) []tuning.OverrideItem {
	if guard == nil || len(ovs) == 0 {
		return ovs
	}
	out := make([]tuning.OverrideItem, 0, len(ovs))
	for _, ov := range ovs {
		if ov.Key == "" {
			continue
		}
		if guard(ov.Key) {
			out = append(out, ov)
		}
	}
	return out
}

func mapTuningWriteErrorToStatus(err error) int {
	switch {
	case err == nil:
		return http.StatusOK
	case errors.Is(err, tuning.ErrInvalidKey):
		return http.StatusBadRequest
	case errors.Is(err, tuning.ErrInvalidValue):
		return http.StatusBadRequest
	case errors.Is(err, tuning.ErrTypeMismatch):
		return http.StatusBadRequest
	case errors.Is(err, tuning.ErrNotFound):
		return http.StatusNotFound
	case errors.Is(err, tuning.ErrNoLastValue):
		return http.StatusConflict
	case errors.Is(err, tuning.ErrReentrantWrite):
		// Indicates a programming error (write API called from onChange).
		return http.StatusInternalServerError
	case errors.Is(err, tuning.ErrInvalidConfig):
		return http.StatusInternalServerError
	default:
		return http.StatusInternalServerError
	}
}

func getQueryRequired(r *http.Request, name string) (string, bool) {
	if r == nil || r.URL == nil {
		return "", false
	}
	q := r.URL.Query()
	if q == nil {
		return "", false
	}
	if _, ok := q[name]; !ok {
		return "", false
	}
	vs := q[name]
	if len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

func getQueryRaw(r *http.Request, name string) (string, bool) {
	// Like getQueryRequired, but allows empty string values.
	if r == nil || r.URL == nil {
		return "", false
	}
	q := r.URL.Query()
	if q == nil {
		return "", false
	}
	vs, ok := q[name]
	if !ok {
		return "", false
	}
	if len(vs) == 0 {
		return "", false
	}
	return vs[0], true
}

func validateTuningKey(key string) error {
	// Keep the same rule as rt/tuning: [A-Za-z0-9._-], non-empty.
	if key == "" {
		return errors.New("invalid key: empty")
	}
	for i := 0; i < len(key); i++ {
		c := key[i]
		switch {
		case c >= 'a' && c <= 'z':
		case c >= 'A' && c <= 'Z':
		case c >= '0' && c <= '9':
		case c == '.' || c == '_' || c == '-':
		default:
			if c == '/' {
				return errors.New("invalid key: contains '/' (not allowed)")
			}
			if strings.ContainsRune(" \t\r\n", rune(c)) {
				return errors.New("invalid key: contains whitespace (not allowed)")
			}
			return errors.New("invalid key: contains invalid char (allowed: [A-Za-z0-9._-])")
		}
	}
	return nil
}

func isRedactedItem(it tuning.Item) bool {
	// tuning exposes redaction via Snapshot/Lookup by replacing values with "<redacted>".
	// We treat either field as a redaction signal.
	return it.Value == "<redacted>" || it.DefaultValue == "<redacted>"
}

func sanitizeTuningWriteError(err error) string {
	// For redacted keys, avoid reflecting user-provided values in error messages.
	// Keep it stable and machine-actionable.
	switch {
	case err == nil:
		return ""
	case errors.Is(err, tuning.ErrInvalidValue):
		return "invalid value"
	case errors.Is(err, tuning.ErrTypeMismatch):
		return "type mismatch"
	case errors.Is(err, tuning.ErrInvalidKey):
		return "invalid key"
	case errors.Is(err, tuning.ErrNotFound):
		return "key not found"
	case errors.Is(err, tuning.ErrNoLastValue):
		return "no last value"
	case errors.Is(err, tuning.ErrReentrantWrite):
		return "re-entrant write"
	default:
		return "error"
	}
}
