package httpx

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
)

func reportAccessGuardHookPanicToStderr(r *http.Request, p any) {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "httpx: AccessGuard OnDeny hook panicked")
	if r != nil {
		if r.Method != "" {
			fmt.Fprintf(&buf, " method=%s", r.Method)
		}
		if r.URL != nil {
			fmt.Fprintf(&buf, " url=%q", r.URL.String())
		}
	}
	fmt.Fprintf(&buf, " value=%v\n", p)

	// Serialize stderr writes (reuse the same package-level mutex used by Recover/Timeout).
	recoverStderrMu.Lock()
	_, _ = os.Stderr.Write(buf.Bytes())
	recoverStderrMu.Unlock()
}
