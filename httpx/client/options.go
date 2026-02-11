package client

import (
	"net/http"
	"time"
)

type config struct {
	timeout       time.Duration
	transport     *http.Transport
	roundTripper  http.RoundTripper
	middlewares   []Middleware
	checkRedirect func(req *http.Request, via []*http.Request) error
	jar           http.CookieJar
}

// Option configures a New(...) call.
type Option func(*config)

func defaultConfig() config {
	return config{
		timeout: 0,
	}
}

// WithTimeout sets http.Client.Timeout (total timeout).
//
// Default is 0, which means no client-level timeout (caller may still use context).
func WithTimeout(d time.Duration) Option {
	return func(c *config) { c.timeout = d }
}

// WithTransport sets the base *http.Transport.
//
// New(...) will clone the provided transport (t.Clone()) to avoid shared mutable state.
func WithTransport(t *http.Transport) Option {
	return func(c *config) { c.transport = t }
}

// WithRoundTripper sets the base http.RoundTripper.
//
// If set, it takes precedence over WithTransport.
func WithRoundTripper(rt http.RoundTripper) Option {
	return func(c *config) { c.roundTripper = rt }
}

// WithMiddlewares appends middlewares for the RoundTripper chain.
func WithMiddlewares(mws ...Middleware) Option {
	return func(c *config) {
		if len(mws) == 0 {
			return
		}
		c.middlewares = append(c.middlewares, mws...)
	}
}

// WithCheckRedirect sets http.Client.CheckRedirect.
func WithCheckRedirect(fn func(req *http.Request, via []*http.Request) error) Option {
	return func(c *config) { c.checkRedirect = fn }
}

// WithCookieJar sets http.Client.Jar.
func WithCookieJar(jar http.CookieJar) Option {
	return func(c *config) { c.jar = jar }
}
