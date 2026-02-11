package safego

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"sync"
)

var stderrMu sync.Mutex

func reportPanicToStderr(info PanicInfo) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "safego: panic")
	if info.Name != "" {
		fmt.Fprintf(&buf, " name=%q", info.Name)
	}
	if len(info.Tags) > 0 {
		fmt.Fprintf(&buf, " tags=%s", formatTags(info.Tags))
	}
	fmt.Fprintf(&buf, " value=%v\n", info.Value)
	if len(info.Stack) > 0 {
		_, _ = buf.Write(info.Stack)
		// Avoid []byte->string allocation: check last byte.
		if info.Stack[len(info.Stack)-1] != '\n' {
			_ = buf.WriteByte('\n')
		}
	}

	stderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	stderrMu.Unlock()
}

func reportErrorToStderr(info ErrorInfo) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "safego: error")
	if info.Name != "" {
		fmt.Fprintf(&buf, " name=%q", info.Name)
	}
	if len(info.Tags) > 0 {
		fmt.Fprintf(&buf, " tags=%s", formatTags(info.Tags))
	}
	fmt.Fprintf(&buf, " err=%v\n", info.Err)

	stderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	stderrMu.Unlock()
}

func formatTags(tags []Tag) string {
	// Keep insertion order for stable output.
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
