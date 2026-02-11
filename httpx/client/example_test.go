package client_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/evan-idocoding/zkit/httpx/client"
)

func ExampleNew() {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	c := client.New(client.WithTimeout(2 * time.Second))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, err := c.Do(req)
	if err != nil {
		fmt.Println("error:", err)
		return
	}

	b, _ := client.ReadAllAndCloseLimit(resp.Body, 16)
	fmt.Println(string(b))

	// Output:
	// ok
}

func ExampleSetHeader() {
	got := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Test")
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := client.New(client.WithMiddlewares(client.SetHeader("X-Test", "v")))
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	resp, _ := c.Do(req)
	_ = client.DrainAndClose(resp.Body, 0)

	fmt.Println(got)

	// Output:
	// v
}

func ExampleReadAllAndCloseLimit_tooLarge() {
	body := io.NopCloser(io.LimitReader(&infiniteA{}, 100))
	_, err := client.ReadAllAndCloseLimit(body, 1)
	fmt.Println(err == client.ErrBodyTooLarge)

	// Output:
	// true
}

type infiniteA struct{}

func (infiniteA) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'a'
	}
	return len(p), nil
}
