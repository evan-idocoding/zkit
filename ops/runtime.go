package ops

import (
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type runtimeConfig struct {
	format Format
}

// RuntimeOption configures RuntimeHandler.
type RuntimeOption func(*runtimeConfig)

// WithRuntimeDefaultFormat sets the default response format.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithRuntimeDefaultFormat(f Format) RuntimeOption {
	return func(c *runtimeConfig) { c.format = f }
}

func applyRuntimeOptions(opts []RuntimeOption) runtimeConfig {
	cfg := runtimeConfig{
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

// startTime is captured once at package init time.
var startTime = time.Now()

// RuntimeHandler returns a handler that outputs a runtime overview snapshot.
//
// It is read-only and intended for operational inspection. It does not perform
// authn/authz decisions; protect it with your own middleware.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func RuntimeHandler(opts ...RuntimeOption) http.Handler {
	cfg := applyRuntimeOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeRuntime(w, r, format, http.StatusMethodNotAllowed, runtimeResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap := Runtime()
		writeRuntime(w, r, format, http.StatusOK, runtimeResponse{
			OK:      true,
			Runtime: &snap,
		})
	})
}

// Runtime returns a structured runtime snapshot (compact).
func Runtime() RuntimeSnapshot {
	now := time.Now()

	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)

	out := RuntimeSnapshot{
		Now:       now,
		StartTime: startTime,
		Uptime:    now.Sub(startTime),
		PID:       os.Getpid(),

		Runtime: runtimeSnapshot(),

		NumCPU:     runtime.NumCPU(),
		GOMAXPROCS: runtime.GOMAXPROCS(0),
		Goroutines: runtime.NumGoroutine(),
		CGOCalls:   runtime.NumCgoCall(),
	}

	out.Mem = RuntimeMemSnapshot{
		AllocBytes:      ms.Alloc,
		TotalAllocBytes: ms.TotalAlloc,
		SysBytes:        ms.Sys,

		HeapAllocBytes:    ms.HeapAlloc,
		HeapSysBytes:      ms.HeapSys,
		HeapInuseBytes:    ms.HeapInuse,
		HeapIdleBytes:     ms.HeapIdle,
		HeapReleasedBytes: ms.HeapReleased,

		StackInuseBytes: ms.StackInuse,
		StackSysBytes:   ms.StackSys,
	}

	out.GC = RuntimeGCSnapshot{
		NumGC:         ms.NumGC,
		PauseTotal:    time.Duration(ms.PauseTotalNs),
		NextGCBytes:   ms.NextGC,
		GCCPUFraction: ms.GCCPUFraction,
	}

	if ms.NumGC > 0 {
		// MemStats keeps a circular buffer of the last 256 pauses.
		idx := int((ms.NumGC - 1) % uint32(len(ms.PauseNs)))
		out.GC.LastPause = time.Duration(ms.PauseNs[idx])

		// Convert ns-since-epoch to time.Time.
		if end := ms.PauseEnd[idx]; end != 0 {
			t := time.Unix(0, int64(end))
			out.GC.LastPauseEndTime = &t
		}
		if ms.LastGC != 0 {
			t := time.Unix(0, int64(ms.LastGC))
			out.GC.LastGCTime = &t
		}
	}

	return out
}

type runtimeResponse struct {
	OK      bool             `json:"ok"`
	Error   string           `json:"error,omitempty"`
	Runtime *RuntimeSnapshot `json:"runtime,omitempty"`
}

// RuntimeSnapshot is a point-in-time runtime snapshot.
type RuntimeSnapshot struct {
	Now       time.Time `json:"now"`
	StartTime time.Time `json:"start_time"`
	// Uptime is encoded as an integer number of nanoseconds in JSON.
	Uptime time.Duration `json:"uptime"`

	PID int `json:"pid"`

	Runtime BuildInfoRuntime `json:"runtime"`

	NumCPU     int   `json:"num_cpu"`
	GOMAXPROCS int   `json:"gomaxprocs"`
	Goroutines int   `json:"goroutines"`
	CGOCalls   int64 `json:"cgo_calls"`

	Mem RuntimeMemSnapshot `json:"mem"`
	GC  RuntimeGCSnapshot  `json:"gc"`
}

// RuntimeMemSnapshot is a compact memory summary.
type RuntimeMemSnapshot struct {
	AllocBytes      uint64 `json:"alloc_bytes"`
	TotalAllocBytes uint64 `json:"total_alloc_bytes"`
	SysBytes        uint64 `json:"sys_bytes"`

	HeapAllocBytes    uint64 `json:"heap_alloc_bytes"`
	HeapSysBytes      uint64 `json:"heap_sys_bytes"`
	HeapInuseBytes    uint64 `json:"heap_inuse_bytes"`
	HeapIdleBytes     uint64 `json:"heap_idle_bytes"`
	HeapReleasedBytes uint64 `json:"heap_released_bytes"`

	StackInuseBytes uint64 `json:"stack_inuse_bytes"`
	StackSysBytes   uint64 `json:"stack_sys_bytes"`
}

// RuntimeGCSnapshot is a compact GC summary.
type RuntimeGCSnapshot struct {
	NumGC uint32 `json:"num_gc"`

	LastGCTime       *time.Time    `json:"last_gc_time,omitempty"`
	LastPause        time.Duration `json:"last_pause"`
	LastPauseEndTime *time.Time    `json:"last_pause_end_time,omitempty"`

	// PauseTotal is encoded as an integer number of nanoseconds in JSON.
	PauseTotal  time.Duration `json:"pause_total"`
	NextGCBytes uint64        `json:"next_gc_bytes"`

	GCCPUFraction float64 `json:"gc_cpu_fraction"`
}

func writeRuntime(w http.ResponseWriter, r *http.Request, f Format, code int, resp runtimeResponse) {
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
		if resp.Runtime == nil {
			_, _ = w.Write([]byte("error\n"))
			return
		}
		_, _ = w.Write([]byte(renderRuntimeText(*resp.Runtime)))
	}
}

func renderRuntimeText(s RuntimeSnapshot) string {
	// Keep it stable and greppable: one key per line, tab-separated fields.
	// Format: <section>\t<key>\t<value>\n
	var b strings.Builder
	b.Grow(512)

	write := func(section, key, value string) {
		if section == "" || key == "" || value == "" {
			return
		}
		b.WriteString(section)
		b.WriteByte('\t')
		b.WriteString(key)
		b.WriteByte('\t')
		b.WriteString(value)
		b.WriteByte('\n')
	}

	write("time", "start", s.StartTime.Format(time.RFC3339Nano))
	write("time", "now", s.Now.Format(time.RFC3339Nano))
	write("time", "uptime", s.Uptime.String())

	write("proc", "pid", strconv.Itoa(s.PID))
	write("proc", "num_cpu", strconv.Itoa(s.NumCPU))
	write("proc", "gomaxprocs", strconv.Itoa(s.GOMAXPROCS))
	write("proc", "goroutines", strconv.Itoa(s.Goroutines))
	write("proc", "cgo_calls", strconv.FormatInt(s.CGOCalls, 10))

	if s.Runtime.Version != "" {
		write("go", "version", s.Runtime.Version)
	}
	if s.Runtime.GOOS != "" || s.Runtime.GOARCH != "" {
		write("go", "osarch", s.Runtime.GOOS+"/"+s.Runtime.GOARCH)
	}
	if s.Runtime.Compiler != "" {
		write("go", "compiler", s.Runtime.Compiler)
	}

	write("mem", "alloc_bytes", strconv.FormatUint(s.Mem.AllocBytes, 10))
	write("mem", "total_alloc_bytes", strconv.FormatUint(s.Mem.TotalAllocBytes, 10))
	write("mem", "sys_bytes", strconv.FormatUint(s.Mem.SysBytes, 10))
	write("mem", "heap_alloc_bytes", strconv.FormatUint(s.Mem.HeapAllocBytes, 10))
	write("mem", "heap_sys_bytes", strconv.FormatUint(s.Mem.HeapSysBytes, 10))
	write("mem", "heap_inuse_bytes", strconv.FormatUint(s.Mem.HeapInuseBytes, 10))
	write("mem", "heap_idle_bytes", strconv.FormatUint(s.Mem.HeapIdleBytes, 10))
	write("mem", "heap_released_bytes", strconv.FormatUint(s.Mem.HeapReleasedBytes, 10))
	write("mem", "stack_inuse_bytes", strconv.FormatUint(s.Mem.StackInuseBytes, 10))
	write("mem", "stack_sys_bytes", strconv.FormatUint(s.Mem.StackSysBytes, 10))

	write("gc", "num_gc", strconv.FormatUint(uint64(s.GC.NumGC), 10))
	if s.GC.LastGCTime != nil {
		write("gc", "last_gc_time", s.GC.LastGCTime.Format(time.RFC3339Nano))
	}
	write("gc", "last_pause", s.GC.LastPause.String())
	if s.GC.LastPauseEndTime != nil {
		write("gc", "last_pause_end_time", s.GC.LastPauseEndTime.Format(time.RFC3339Nano))
	}
	write("gc", "pause_total", s.GC.PauseTotal.String())
	write("gc", "next_gc_bytes", strconv.FormatUint(s.GC.NextGCBytes, 10))
	write("gc", "gc_cpu_fraction", strconv.FormatFloat(s.GC.GCCPUFraction, 'g', -1, 64))

	return b.String()
}
