package ops

import (
	"encoding/json"
	"net/http"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
)

type buildInfoConfig struct {
	format          Format
	includeDeps     bool
	includeSettings bool
}

// BuildInfoOption configures BuildInfoHandler.
type BuildInfoOption func(*buildInfoConfig)

// WithBuildInfoDefaultFormat sets the default response format.
//
// This default can be overridden per request by URL query:
//   - ?format=json
//   - ?format=text
//
// Default is FormatText.
func WithBuildInfoDefaultFormat(f Format) BuildInfoOption {
	return func(c *buildInfoConfig) { c.format = f }
}

// WithBuildInfoIncludeDeps controls whether the response includes dependency modules.
//
// Default is false.
func WithBuildInfoIncludeDeps(v bool) BuildInfoOption {
	return func(c *buildInfoConfig) { c.includeDeps = v }
}

// WithBuildInfoIncludeSettings controls whether the response includes all build settings.
//
// Default is false.
func WithBuildInfoIncludeSettings(v bool) BuildInfoOption {
	return func(c *buildInfoConfig) { c.includeSettings = v }
}

func applyBuildInfoOptions(opts []BuildInfoOption) buildInfoConfig {
	cfg := buildInfoConfig{
		format:          FormatText,
		includeDeps:     false,
		includeSettings: false,
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

// BuildInfoHandler returns a handler that outputs build metadata.
//
// It is read-only and intended for operational inspection. It does not perform
// authn/authz decisions; protect it with your own middleware.
//
// Behavior:
//   - GET/HEAD only; other methods return 405.
//   - By default, it renders text. You can change the default with options.
//   - The response format can be overridden per request by URL query (?format=json|text).
func BuildInfoHandler(opts ...BuildInfoOption) http.Handler {
	cfg := applyBuildInfoOptions(opts)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil {
			panic("ops: nil request")
		}
		format := formatFromRequest(r, cfg.format)
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			writeBuildInfo(w, r, format, http.StatusMethodNotAllowed, buildInfoResponse{
				OK:    false,
				Error: "method not allowed",
			})
			return
		}

		snap, ok := readBuildInfoSnapshot()
		if !ok {
			writeBuildInfo(w, r, format, http.StatusInternalServerError, buildInfoResponse{
				OK:    false,
				Error: "build info not available",
			})
			return
		}

		out := buildInfoResponse{
			OK:    true,
			Build: snap.render(cfg.includeDeps, cfg.includeSettings),
		}
		writeBuildInfo(w, r, format, http.StatusOK, out)
	})
}

// BuildInfo returns a structured build info snapshot (compact).
//
// It does not include deps or build settings by default. Use BuildInfoFull if you
// want a complete snapshot including deps and settings.
func BuildInfo() (*BuildInfoSnapshot, bool) {
	snap, ok := readBuildInfoSnapshot()
	if !ok {
		return nil, false
	}
	return snap.render(false, false), true
}

// BuildInfoFull returns a structured build info snapshot including deps and build settings.
func BuildInfoFull() (*BuildInfoSnapshot, bool) {
	snap, ok := readBuildInfoSnapshot()
	if !ok {
		return nil, false
	}
	return snap.render(true, true), true
}

type buildInfoResponse struct {
	OK    bool               `json:"ok"`
	Error string             `json:"error,omitempty"`
	Build *BuildInfoSnapshot `json:"build,omitempty"`
}

// BuildInfoSnapshot is a structured snapshot of build info.
type BuildInfoSnapshot struct {
	Module   BuildInfoModule    `json:"module"`
	VCS      *BuildInfoVCS      `json:"vcs,omitempty"`
	Runtime  BuildInfoRuntime   `json:"runtime"`
	Deps     []BuildInfoModule  `json:"deps,omitempty"`
	Settings []BuildInfoSetting `json:"settings,omitempty"`
}

// BuildInfoRuntime describes the Go runtime/toolchain for the running binary.
type BuildInfoRuntime struct {
	Version  string `json:"version"`
	GOOS     string `json:"goos"`
	GOARCH   string `json:"goarch"`
	Compiler string `json:"compiler"`
}

// BuildInfoVCS describes version control metadata embedded by the Go toolchain.
type BuildInfoVCS struct {
	System   string `json:"system,omitempty"`
	Revision string `json:"revision,omitempty"`
	Time     string `json:"time,omitempty"`
	Modified *bool  `json:"modified,omitempty"`
}

// BuildInfoModule describes a module in the build graph.
//
// Replace, when present, indicates that this module was replaced by another module.
type BuildInfoModule struct {
	Path    string           `json:"path"`
	Version string           `json:"version,omitempty"`
	Sum     string           `json:"sum,omitempty"`
	Replace *BuildInfoModule `json:"replace,omitempty"`
}

// BuildInfoSetting is a key/value entry from debug.ReadBuildInfo().Settings.
type BuildInfoSetting struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

type buildInfoSnapshot struct {
	main     debug.Module
	deps     []*debug.Module
	settings []debug.BuildSetting
	vcs      BuildInfoVCS
	hasVCS   bool
}

func (s buildInfoSnapshot) render(includeDeps, includeSettings bool) *BuildInfoSnapshot {
	out := &BuildInfoSnapshot{
		Module:  convertModule(s.main),
		Runtime: runtimeSnapshot(),
	}
	if s.hasVCS {
		// Copy to avoid sharing internal pointers across requests.
		v := s.vcs
		out.VCS = &v
	}
	if includeDeps && len(s.deps) > 0 {
		out.Deps = make([]BuildInfoModule, 0, len(s.deps))
		for _, pm := range s.deps {
			if pm == nil {
				continue
			}
			out.Deps = append(out.Deps, convertModule(*pm))
		}
	}
	if includeSettings && len(s.settings) > 0 {
		out.Settings = make([]BuildInfoSetting, 0, len(s.settings))
		for _, kv := range s.settings {
			out.Settings = append(out.Settings, BuildInfoSetting{Key: kv.Key, Value: kv.Value})
		}
	}
	return out
}

func runtimeSnapshot() BuildInfoRuntime {
	return BuildInfoRuntime{
		Version:  runtime.Version(),
		GOOS:     runtime.GOOS,
		GOARCH:   runtime.GOARCH,
		Compiler: runtime.Compiler,
	}
}

func convertModule(m debug.Module) BuildInfoModule {
	out := BuildInfoModule{
		Path:    m.Path,
		Version: m.Version,
		Sum:     m.Sum,
	}
	if m.Replace != nil {
		r := convertModule(*m.Replace)
		out.Replace = &r
	}
	return out
}

var buildInfoOnce sync.Once
var cachedBuildInfo buildInfoSnapshot
var cachedBuildInfoOK bool

func readBuildInfoSnapshot() (*buildInfoSnapshot, bool) {
	buildInfoOnce.Do(func() {
		bi, ok := debug.ReadBuildInfo()
		if !ok || bi == nil {
			cachedBuildInfoOK = false
			return
		}
		// Cache it once: build info is effectively immutable for the lifetime of the process,
		// and caching avoids repeated parsing/allocations on each request.
		cachedBuildInfo.main = bi.Main
		cachedBuildInfo.deps = append([]*debug.Module(nil), bi.Deps...)
		cachedBuildInfo.settings = append([]debug.BuildSetting(nil), bi.Settings...)
		cachedBuildInfo.vcs, cachedBuildInfo.hasVCS = extractVCS(bi.Settings)
		cachedBuildInfoOK = true
	})
	if !cachedBuildInfoOK {
		return nil, false
	}
	return &cachedBuildInfo, true
}

func extractVCS(settings []debug.BuildSetting) (BuildInfoVCS, bool) {
	var out BuildInfoVCS
	var ok bool
	for _, kv := range settings {
		switch kv.Key {
		case "vcs":
			out.System = kv.Value
			ok = ok || kv.Value != ""
		case "vcs.revision":
			out.Revision = kv.Value
			ok = ok || kv.Value != ""
		case "vcs.time":
			out.Time = kv.Value
			ok = ok || kv.Value != ""
		case "vcs.modified":
			// Value is usually "true" or "false".
			if kv.Value == "" {
				continue
			}
			b, err := strconv.ParseBool(kv.Value)
			if err != nil {
				// Preserve the fact that the key exists even if unparsable:
				// omit Modified and still consider VCS present if other fields exist.
				continue
			}
			out.Modified = &b
			ok = true
		}
	}
	if !ok {
		return BuildInfoVCS{}, false
	}
	return out, true
}

func writeBuildInfo(w http.ResponseWriter, r *http.Request, f Format, code int, resp buildInfoResponse) {
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
		if resp.Build == nil {
			_, _ = w.Write([]byte("error\n"))
			return
		}
		_, _ = w.Write([]byte(renderBuildInfoText(*resp.Build)))
	}
}

func renderBuildInfoText(s BuildInfoSnapshot) string {
	var b strings.Builder
	b.Grow(256)

	// Standard-library flavored, `go version -m` inspired output.
	// Keep it stable and greppable: one key per line, tab-separated fields.
	writeKV := func(k, v string) {
		if v == "" {
			return
		}
		b.WriteString(k)
		b.WriteByte('\t')
		b.WriteString(v)
		b.WriteByte('\n')
	}

	// Path + main module.
	writeKV("path", s.Module.Path)
	b.WriteString("mod\t")
	b.WriteString(formatModuleTextLong(s.Module))
	b.WriteByte('\n')

	// Runtime.
	b.WriteString("go\t")
	b.WriteString(s.Runtime.Version)
	b.WriteString("\t")
	b.WriteString(s.Runtime.GOOS)
	b.WriteByte('/')
	b.WriteString(s.Runtime.GOARCH)
	if s.Runtime.Compiler != "" {
		b.WriteString("\t")
		b.WriteString(s.Runtime.Compiler)
	}
	b.WriteByte('\n')

	// VCS.
	// If build settings are included, they typically include vcs.* keys. Avoid
	// duplicate lines by not printing a separate VCS section in that case.
	if s.VCS != nil && len(s.Settings) == 0 {
		writeKV("build\tvcs", s.VCS.System)
		writeKV("build\tvcs.revision", s.VCS.Revision)
		writeKV("build\tvcs.time", s.VCS.Time)
		if s.VCS.Modified != nil {
			b.WriteString("build\tvcs.modified\t")
			b.WriteString(strconv.FormatBool(*s.VCS.Modified))
			b.WriteByte('\n')
		}
	}

	// Optional deps/settings.
	if len(s.Deps) > 0 {
		for _, m := range s.Deps {
			b.WriteString("dep\t")
			b.WriteString(formatModuleTextLong(m))
			b.WriteByte('\n')
		}
	}
	if len(s.Settings) > 0 {
		for _, kv := range s.Settings {
			if kv.Key == "" {
				continue
			}
			b.WriteString("build\t")
			b.WriteString(kv.Key)
			b.WriteByte('\t')
			b.WriteString(kv.Value)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func formatModuleTextLong(m BuildInfoModule) string {
	// Format inspired by `go version -m`:
	//   <path> <version> <sum> [=> <replace path> <replace version> <replace sum>]
	if m.Path == "" {
		return "(unknown)"
	}
	var b strings.Builder
	b.WriteString(m.Path)
	if m.Version != "" {
		b.WriteByte('\t')
		b.WriteString(m.Version)
	}
	if m.Sum != "" {
		b.WriteByte('\t')
		b.WriteString(m.Sum)
	}
	if m.Replace != nil {
		b.WriteString("\t=>\t")
		b.WriteString(m.Replace.Path)
		if m.Replace.Version != "" {
			b.WriteByte('\t')
			b.WriteString(m.Replace.Version)
		}
		if m.Replace.Sum != "" {
			b.WriteByte('\t')
			b.WriteString(m.Replace.Sum)
		}
	}
	return b.String()
}
