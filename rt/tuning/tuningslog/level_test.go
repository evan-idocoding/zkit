package tuningslog

import (
	"log/slog"
	"testing"

	"github.com/evan-idocoding/zkit/rt/tuning"
)

func TestLevelVarCaseInsensitiveSetFromString(t *testing.T) {
	tu := tuning.New()
	_, lv, err := LevelVar(tu, "log.level", slog.LevelInfo)
	if err != nil {
		t.Fatal(err)
	}

	if err := tu.SetFromString("log.level", "ERROR"); err != nil {
		t.Fatal(err)
	}
	if got := lv.Level(); got != slog.LevelError {
		t.Fatalf("expected slog.LevelError, got %v", got)
	}

	if err := tu.SetFromString("log.level", "warning"); err != nil {
		t.Fatal(err)
	}
	if got := lv.Level(); got != slog.LevelWarn {
		t.Fatalf("expected slog.LevelWarn, got %v", got)
	}
}
