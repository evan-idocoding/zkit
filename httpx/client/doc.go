// Package client provides a small, stable and standard-library flavored HTTP client builder.
//
// Design goals:
//   - Thin: it should not become a framework or a REST DSL.
//   - Non-invasive: it should not change users' standard net/http habits.
//   - No business decisions: no retries / circuit breakers / rate limits built-in.
//   - No global pollution: it never mutates http.DefaultClient or http.DefaultTransport.
//
// # Timeouts
//
// By default, New does not set http.Client.Timeout (it remains 0). In production code,
// you should usually enforce timeouts by either setting WithTimeout(...) or using a
// context with deadline/cancel on each request (or both).
//
// WithTimeout sets http.Client.Timeout, which is a total deadline covering:
// connection, redirects, and reading the response body.
//
// # Transports and connection reuse
//
// If no base transport is provided, client.New builds an independent transport cloned from
// http.DefaultTransport (when it is *http.Transport). This avoids global shared mutable state
// while inheriting standard library defaults.
//
// To improve connection reuse, always close response bodies. If you do not need the body,
// consider draining a small amount before closing (DrainAndClose).
//
// # Example: basic client
//
//	c := client.New(
//		client.WithTimeout(2*time.Second),
//	)
//	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
//	resp, err := c.Do(req)
//	if err != nil {
//		// handle error
//	}
//	defer resp.Body.Close()
//
// # Example: middleware + I/O guards
//
//	c := client.New(
//		client.WithMiddlewares(client.SetHeader("User-Agent", "my-app/1.0")),
//	)
//
//	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.com", nil)
//	resp, err := c.Do(req)
//	if err != nil {
//		// handle error
//	}
//
//	// Option A: read with a limit and always close.
//	_, _ = client.ReadAllAndCloseLimit(resp.Body, 1<<20) // 1 MiB
//
//	// Option B: if you don't need the body, drain a bit and close to improve reuse.
//	_ = client.DrainAndClose(resp.Body, 8<<10) // 8 KiB
//
// What it provides:
//   - A RoundTripper middleware chain (func(http.RoundTripper) http.RoundTripper).
//   - A New(...) helper to build a *http.Client with an independent base transport.
//   - A few I/O guard helpers (ReadAllAndCloseLimit, DrainAndClose).
package client
