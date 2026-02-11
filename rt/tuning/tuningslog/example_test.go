package tuningslog_test

import (
	"fmt"
	"log/slog"

	"github.com/evan-idocoding/zkit/rt/tuning"
	"github.com/evan-idocoding/zkit/rt/tuning/tuningslog"
)

func ExampleLevelVar() {
	tu := tuning.New()
	ev, lv, _ := tuningslog.LevelVar(tu, "log.level", slog.LevelInfo)

	_ = tu.SetFromString("log.level", "warning")
	fmt.Println(ev.Get(), lv.Level())

	// Output:
	// warn WARN
}
