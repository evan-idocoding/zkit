package tuningslog

import (
	"log/slog"
	"strings"

	"github.com/evan-idocoding/zkit/rt/tuning"
)

// LevelVar registers a tuning enum and binds it to a slog.LevelVar.
//
// Accepted values are case-insensitive (normalized to lowercase):
//   - debug / info / warn / error
//
// and common aliases:
//   - warning -> warn
//   - err -> error
//
// defaultLevel is mapped to one of {debug,info,warn,error}.
//
// The returned EnumVar stores canonical values in lowercase.
func LevelVar(t *tuning.Tuning, key string, defaultLevel slog.Level, opts ...tuning.EnumOption) (*tuning.EnumVar, *slog.LevelVar, error) {
	if t == nil {
		t = tuning.Default()
	}

	defStr := levelToEnum(defaultLevel)
	lv := new(slog.LevelVar)
	lv.Set(enumToLevel(defStr))

	base := []tuning.EnumOption{
		tuning.WithEnumAllowed("debug", "info", "warn", "error"),
		tuning.WithEnumNormalize(normalizeLevelEnum),
		// Keep lv updated before user callbacks.
		tuning.WithOnChangeEnum(func(s string) {
			lv.Set(enumToLevel(s))
		}),
	}
	base = append(base, opts...)

	ev, err := t.Enum(key, defStr, base...)
	if err != nil {
		return nil, nil, err
	}
	// Initial sync (in case enum normalizer adjusts defStr).
	lv.Set(enumToLevel(ev.Get()))
	return ev, lv, nil
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
	// slog: Debug=-4, Info=0, Warn=4, Error=8
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
