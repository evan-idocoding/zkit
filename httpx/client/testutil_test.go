package client

import (
	"net/http"
	"net/url"
	"testing"
)

type staticStatusRT struct {
	status int
}

func (rt *staticStatusRT) RoundTrip(r *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: rt.status, Body: http.NoBody}, nil
}

type staticJar struct{}

func (j *staticJar) SetCookies(u *url.URL, cookies []*http.Cookie) {}
func (j *staticJar) Cookies(u *url.URL) []*http.Cookie             { return nil }

func mustURL(t *testing.T, s string) *url.URL {
	t.Helper()
	u, err := url.Parse(s)
	if err != nil {
		t.Fatalf("url.Parse(%q): %v", s, err)
	}
	return u
}
