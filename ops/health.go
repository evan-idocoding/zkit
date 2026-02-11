package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

// Format controls the response rendering format.
//
// This is shared across ops handlers that support multiple output formats.
type Format int

const (
	FormatText Format = iota
	FormatJSON
)

type healthConfig struct {
	format Format
}

// HealthOption configures HealthzHandler / ReadyzHandler.
type HealthOption func(*healthConfig)

// WithHealthDefaultFormat sets the default response format for health handlers.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithHealthDefaultFormat(f Format) HealthOption {
	return func(c *healthConfig) { c.format = f }
}

func applyHealthOptions(opts []HealthOption) healthConfig {
	cfg := healthConfig{
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

// HealthzHandler returns a liveness handler.
//
// It is designed to be fast, stable, and side-effect free. By default it always
// responds 200 OK for GET/HEAD.
func HealthzHandler(opts ...HealthOption) http.Handler {
	cfg := applyHealthOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeHealth(w, r, format, http.StatusMethodNotAllowed, healthResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}
		writeHealth(w, r, format, http.StatusOK, healthResponse{OK: true})
	})
}

// ReadyCheckFunc is a readiness check function.
//
// It should return nil when healthy. Implementations should be fast and must
// respect ctx cancellation.
type ReadyCheckFunc func(context.Context) error

// ReadyCheck is a named readiness check.
type ReadyCheck struct {
	Name    string
	Func    ReadyCheckFunc
	Timeout time.Duration // optional per-check timeout; <= 0 means "no extra timeout"
}

// ReadyCheckResult is a single check execution result.
type ReadyCheckResult struct {
	Name string `json:"name"`
	OK   bool   `json:"ok"`
	// Duration is encoded as an integer number of nanoseconds in JSON.
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
	TimedOut bool          `json:"timed_out,omitempty"`
}

// ReadyzReport is a point-in-time readiness execution report.
type ReadyzReport struct {
	OK bool `json:"ok"`
	// Duration is encoded as an integer number of nanoseconds in JSON.
	Duration time.Duration      `json:"duration"`
	Checks   []ReadyCheckResult `json:"checks,omitempty"`
}

// ReadyzHandler returns a readiness handler that runs checks sequentially.
//
// It responds:
//   - 200 OK if all checks pass
//   - 503 Service Unavailable if any check fails or times out
//
// GET/HEAD only; other methods return 405.
func ReadyzHandler(checks []ReadyCheck, opts ...HealthOption) http.Handler {
	for i, c := range checks {
		if c.Name == "" {
			panic(fmt.Sprintf("ops: ready check[%d] has empty Name", i))
		}
		if c.Func == nil {
			panic(fmt.Sprintf("ops: ready check[%d] %q has nil Func", i, c.Name))
		}
	}
	cfg := applyHealthOptions(opts)

	// Snapshot to keep handler stable if caller mutates the slice later.
	snapshot := append([]ReadyCheck(nil), checks...)

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			// Keep JSON shape consistent: always return ReadyzReport for ReadyzHandler.
			writeReady(w, r, format, http.StatusMethodNotAllowed, ReadyzReport{
				OK: false,
				Checks: []ReadyCheckResult{
					{Name: "method", OK: false, Error: "method not allowed"},
				},
			})
			return
		}

		rep := RunReadyzChecks(r.Context(), snapshot)
		code := http.StatusOK
		if !rep.OK {
			code = http.StatusServiceUnavailable
		}
		writeReady(w, r, format, code, rep)
	})
}

func formatFromRequest(r *http.Request, def Format) Format {
	if r == nil || r.URL == nil {
		return def
	}
	switch r.URL.Query().Get("format") {
	case "json":
		return FormatJSON
	case "text":
		return FormatText
	default:
		return def
	}
}

// RunReadyzChecks executes checks sequentially and returns a report.
func RunReadyzChecks(ctx context.Context, checks []ReadyCheck) ReadyzReport {
	if ctx == nil {
		ctx = context.Background()
	}
	start := time.Now()
	out := ReadyzReport{
		OK:     true,
		Checks: make([]ReadyCheckResult, 0, len(checks)),
	}

	for _, c := range checks {
		cr := runOneCheck(ctx, c)
		out.Checks = append(out.Checks, cr)
		if !cr.OK {
			out.OK = false
		}
	}

	out.Duration = time.Since(start)
	return out
}

func runOneCheck(parent context.Context, c ReadyCheck) (cr ReadyCheckResult) {
	cr.Name = c.Name

	start := time.Now()
	ctx := parent
	cancel := func() {}
	if c.Timeout > 0 {
		ctx, cancel = context.WithTimeout(parent, c.Timeout)
	}
	defer cancel()

	defer func() {
		cr.Duration = time.Since(start)

		if p := recover(); p != nil {
			cr.OK = false
			cr.Error = fmt.Sprintf("panic: %v", p)
		}

		if ctx.Err() == context.DeadlineExceeded {
			cr.TimedOut = true
			cr.OK = false
			if cr.Error == "" {
				cr.Error = "timeout"
			}
		}
	}()

	if c.Func == nil {
		cr.OK = false
		cr.Error = "nil check func"
		return cr
	}

	if err := c.Func(ctx); err != nil {
		cr.OK = false
		cr.Error = err.Error()
		return cr
	}
	cr.OK = true
	return cr
}

type healthResponse struct {
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
}

func writeHealth(w http.ResponseWriter, r *http.Request, f Format, code int, resp healthResponse) {
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
		if code == http.StatusOK && resp.OK {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		if resp.Error != "" {
			_, _ = w.Write([]byte(resp.Error + "\n"))
			return
		}
		_, _ = w.Write([]byte("error\n"))
	}
}

func writeReady(w http.ResponseWriter, r *http.Request, f Format, code int, rep ReadyzReport) {
	w.Header().Set("Cache-Control", "no-store")
	switch f {
	case FormatJSON:
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		_ = json.NewEncoder(w).Encode(rep)
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(code)
		if r.Method == http.MethodHead {
			return
		}
		if rep.OK {
			_, _ = w.Write([]byte("ok\n"))
			return
		}
		for _, c := range rep.Checks {
			if c.OK {
				continue
			}
			if c.Error != "" {
				_, _ = w.Write([]byte("fail " + c.Name + ": " + c.Error + "\n"))
			} else {
				_, _ = w.Write([]byte("fail " + c.Name + "\n"))
			}
		}
	}
}
