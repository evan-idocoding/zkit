package admin

import (
	"net/http"
	"strconv"
	"strings"
)

// --- small utilities ---

func itoa(i int) string { return strconv.Itoa(i) }

func resolvePath(specPath, def string) string {
	if strings.TrimSpace(specPath) == "" {
		return def
	}
	return specPath
}

// --- builder mount helpers ---

func (b *Builder) register(path string, h http.Handler) {
	if b == nil {
		panic("admin: nil builder")
	}
	path = normalizePathOrPanic(path)
	if h == nil {
		panic("admin: nil handler for path " + path)
	}
	if _, exists := b.paths[path]; exists {
		panic("admin: duplicated path handler: " + path)
	}
	b.paths[path] = h
}

func normalizePathOrPanic(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		panic("admin: empty path")
	}
	if !strings.HasPrefix(path, "/") {
		panic("admin: invalid path (must start with '/'): " + path)
	}
	// Avoid obvious ambiguity/injection.
	if strings.ContainsAny(path, " \t\r\n?#") {
		panic("admin: invalid path (contains whitespace or ?#): " + path)
	}
	// Keep it conservative: disallow '//' to avoid surprising ServeMux matches.
	if strings.Contains(path, "//") {
		panic("admin: invalid path (contains //): " + path)
	}
	return path
}

func mountRead(b *Builder, name, path string, g Guard, h http.Handler) http.Handler {
	if b == nil {
		panic("admin: nil builder")
	}
	if g == nil {
		panic("admin: " + name + ": nil Guard")
	}
	if h == nil {
		panic("admin: " + name + ": nil handler")
	}
	h = g.Middleware()(h)
	b.register(path, h)
	return h
}

func mountWrite(b *Builder, name, path string, g Guard, h http.Handler) http.Handler {
	if b == nil {
		panic("admin: nil builder")
	}
	if g == nil {
		panic("admin: " + name + ": nil Guard")
	}
	if h == nil {
		panic("admin: " + name + ": nil handler")
	}
	h = g.Middleware()(h)
	b.register(path, h)
	return h
}
