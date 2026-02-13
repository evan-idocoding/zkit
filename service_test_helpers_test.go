package zkit

import (
	"io"
	"net/http"
	"testing"
	"time"
)

func waitForBoundAddr(t *testing.T, s *Service, srv *http.Server) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		ln, ok := s.listeners[srv]
		s.mu.Unlock()
		if ok && ln != nil {
			return ln.Addr().String()
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timeout waiting for server listener to bind")
	return ""
}

func httpGetBody(t *testing.T, url string) (code int, body string) {
	t.Helper()
	c := &http.Client{Timeout: 2 * time.Second}
	resp, err := c.Get(url)
	if err != nil {
		t.Fatalf("GET %q err=%v", url, err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

