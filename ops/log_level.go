package ops

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
)

type logLevelConfig struct {
	format Format
}

// LogLevelOption configures LogLevelGetHandler / LogLevelSetHandler.
type LogLevelOption func(*logLevelConfig)

// WithLogLevelDefaultFormat sets the default response format for log level handlers.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithLogLevelDefaultFormat(f Format) LogLevelOption {
	return func(c *logLevelConfig) { c.format = f }
}

func applyLogLevelOptions(opts []LogLevelOption) logLevelConfig {
	cfg := logLevelConfig{
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
	return cfg
}

// LogLevelSnapshot is a point-in-time snapshot of a slog.LevelVar.
type LogLevelSnapshot struct {
	// Level is a canonical string derived from LevelValue. It is always one of:
	// debug/info/warn/error.
	Level string `json:"level"`
	// LevelValue is the underlying numeric slog level (e.g. Debug=-4, Info=0, Warn=4, Error=8).
	LevelValue int `json:"level_value"`
}

// LogLevel returns a snapshot of lv.
func LogLevel(lv *slog.LevelVar) LogLevelSnapshot {
	if lv == nil {
		return LogLevelSnapshot{}
	}
	l := lv.Level()
	return LogLevelSnapshot{
		Level:      levelToEnum(l),
		LevelValue: int(l),
	}
}

type logLevelGetResponse struct {
	OK    bool              `json:"ok"`
	Error string            `json:"error,omitempty"`
	Log   *LogLevelSnapshot `json:"log,omitempty"`
}

type logLevelSetResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`

	Old *LogLevelSnapshot `json:"old,omitempty"`
	New *LogLevelSnapshot `json:"new,omitempty"`
}

// LogLevelGetHandler returns a handler that outputs the current slog log level.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func LogLevelGetHandler(lv *slog.LevelVar, opts ...LogLevelOption) http.Handler {
	if lv == nil {
		panic("ops: nil slog.LevelVar")
	}
	cfg := applyLogLevelOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeLogLevelGet(w, r, format, http.StatusMethodNotAllowed, logLevelGetResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap := LogLevel(lv)
		writeLogLevelGet(w, r, format, http.StatusOK, logLevelGetResponse{
			OK:  true,
			Log: &snap,
		})
	})
}

// LogLevelSetHandler returns a handler that sets slog log level.
//
// Input:
//   - POST only
//   - URL query: ?level=debug|info|warn|error (case-insensitive; also supports "warning"->"warn", "err"->"error")
//
// Output:
//   - Text or JSON (controlled by option or ?format=)
func LogLevelSetHandler(lv *slog.LevelVar, opts ...LogLevelOption) http.Handler {
	if lv == nil {
		panic("ops: nil slog.LevelVar")
	}
	cfg := applyLogLevelOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			writeLogLevelSet(w, r, format, http.StatusMethodNotAllowed, logLevelSetResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		levelStr := ""
		if r.URL != nil {
			levelStr = r.URL.Query().Get("level")
		}
		enum, ok := normalizeLevelEnum(levelStr)
		if !ok {
			writeLogLevelSet(w, r, format, http.StatusBadRequest, logLevelSetResponse{
				OK:    false,
				Error: "invalid level (want one of: debug, info, warn, error)",
			})
			return
		}

		old := LogLevel(lv)
		lv.Set(enumToLevel(enum))
		newSnap := LogLevel(lv)
		writeLogLevelSet(w, r, format, http.StatusOK, logLevelSetResponse{
			OK:  true,
			Old: &old,
			New: &newSnap,
		})
	})
}

func writeLogLevelGet(w http.ResponseWriter, r *http.Request, f Format, code int, resp logLevelGetResponse) {
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
			if resp.Error != "" {
				_, _ = w.Write([]byte(resp.Error + "\n"))
			} else {
				_, _ = w.Write([]byte("error\n"))
			}
			return
		}
		if resp.Log == nil || resp.Log.Level == "" {
			_, _ = w.Write([]byte("error\n"))
			return
		}
		_, _ = w.Write([]byte(renderLogLevelGetText(*resp.Log)))
	}
}

func writeLogLevelSet(w http.ResponseWriter, r *http.Request, f Format, code int, resp logLevelSetResponse) {
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
			if resp.Error != "" {
				_, _ = w.Write([]byte(resp.Error + "\n"))
			} else {
				_, _ = w.Write([]byte("error\n"))
			}
			return
		}
		if resp.New == nil || resp.New.Level == "" {
			_, _ = w.Write([]byte("error\n"))
			return
		}
		_, _ = w.Write([]byte(renderLogLevelSetText(resp.Old, resp.New)))
	}
}

func renderLogLevelGetText(s LogLevelSnapshot) string {
	// Stable and greppable: <section>\t<key>\t<value>\n
	var b strings.Builder
	b.Grow(64)
	b.WriteString("log\tlevel\t")
	b.WriteString(s.Level)
	b.WriteByte('\n')
	b.WriteString("log\tlevel_value\t")
	b.WriteString(strconv.Itoa(s.LevelValue))
	b.WriteByte('\n')
	return b.String()
}

func renderLogLevelSetText(old, newSnap *LogLevelSnapshot) string {
	var b strings.Builder
	b.Grow(128)
	if old != nil && old.Level != "" {
		b.WriteString("log\told_level\t")
		b.WriteString(old.Level)
		b.WriteByte('\n')
		b.WriteString("log\told_level_value\t")
		b.WriteString(strconv.Itoa(old.LevelValue))
		b.WriteByte('\n')
	}
	b.WriteString("log\tnew_level\t")
	b.WriteString(newSnap.Level)
	b.WriteByte('\n')
	b.WriteString("log\tnew_level_value\t")
	b.WriteString(strconv.Itoa(newSnap.LevelValue))
	b.WriteByte('\n')
	return b.String()
}

func normalizeLevelEnum(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	s = strings.ToLower(s)
	switch s {
	case "warning":
		s = "warn"
	case "err":
		s = "error"
	}
	switch s {
	case "debug", "info", "warn", "error":
		return s, true
	default:
		return "", false
	}
}

func levelToEnum(l slog.Level) string {
	// Map an arbitrary slog.Level into our *canonical* string set:
	//   debug / info / warn / error
	//
	// Why bucketize?
	// - The handler API intentionally keeps a low mental model: most ops usage only
	//   needs the four common severities.
	// - slog.Level is an integer and callers may use custom values between/around
	//   the defaults. To keep output stable, we classify by ranges.
	//
	// Classification rule (using slog defaults as boundaries):
	//   <  info(0)  => debug
	//   <  warn(4)  => info
	//   <  error(8) => warn
	//   >= error(8) => error
	//
	// Note: LogLevelSnapshot also includes LevelValue (the raw numeric level), so
	// users can still observe fine-grained custom levels if needed.
	if l < slog.LevelInfo {
		return "debug"
	}
	if l < slog.LevelWarn {
		return "info"
	}
	if l < slog.LevelError {
		return "warn"
	}
	return "error"
}

func enumToLevel(s string) slog.Level {
	switch s {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		// Should not happen; keep it safe.
		return slog.LevelInfo
	}
}
