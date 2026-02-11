package task

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/evan-idocoding/zkit/rt/safego"
)

var stderrMu sync.Mutex

func reportPanicToStderr(name string, tags []safego.Tag, v any, stack []byte) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "task: panic")
	if name != "" {
		fmt.Fprintf(&buf, " name=%q", name)
	}
	if len(tags) > 0 {
		fmt.Fprintf(&buf, " tags=%s", formatTags(tags))
	}
	fmt.Fprintf(&buf, " value=%v\n", v)
	if len(stack) > 0 {
		_, _ = buf.Write(stack)
		if stack[len(stack)-1] != '\n' {
			_ = buf.WriteByte('\n')
		}
	}

	stderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	stderrMu.Unlock()
}

func reportErrorToStderr(name string, tags []safego.Tag, err error) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "task: error")
	if name != "" {
		fmt.Fprintf(&buf, " name=%q", name)
	}
	if len(tags) > 0 {
		fmt.Fprintf(&buf, " tags=%s", formatTags(tags))
	}
	fmt.Fprintf(&buf, " err=%v\n", err)

	stderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	stderrMu.Unlock()
}

func formatTags(tags []safego.Tag) string {
	var b strings.Builder
	b.WriteByte('{')
	for i, t := range tags {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s=%q", t.Key, t.Value)
	}
	b.WriteByte('}')
	return b.String()
}
