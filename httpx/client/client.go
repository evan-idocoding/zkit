package client

import "net/http"

// New builds a *http.Client with an independent base transport and optional RoundTripper middlewares.
//
// It never mutates http.DefaultClient or http.DefaultTransport.
//
// Transport selection:
//   - If WithRoundTripper is provided, it is used as the base RoundTripper.
//   - Else if WithTransport is provided, New clones it (t.Clone()) and uses the clone as base.
//   - Else New clones http.DefaultTransport (when it is *http.Transport) and uses the clone as base.
func New(opts ...Option) *http.Client {
	cfg := defaultConfig()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		opt(&cfg)
	}

	var base http.RoundTripper
	switch {
	case cfg.roundTripper != nil:
		base = cfg.roundTripper
	case cfg.transport != nil:
		base = cfg.transport.Clone()
	default:
		base = nil // Chain will clone from http.DefaultTransport.
	}

	rt := Chain(base, cfg.middlewares...)

	return &http.Client{
		Transport:     rt,
		Timeout:       cfg.timeout,
		CheckRedirect: cfg.checkRedirect,
		Jar:           cfg.jar,
	}
}
