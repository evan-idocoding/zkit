package httpx

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
)

func TestChain_Order(t *testing.T) {
	var got []string

	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, name+"<")
				next.ServeHTTP(w, r)
				got = append(got, ">"+name)
			})
		}
	}

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, "h")
		w.WriteHeader(http.StatusOK)
	})

	h := Chain(mw("a"), mw("b"), mw("c")).Handler(endpoint)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	want := []string{"a<", "b<", "c<", "h", ">c", ">b", ">a"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("order mismatch:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestChain_IgnoresNilMiddleware(t *testing.T) {
	var got []string

	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, "h")
		w.WriteHeader(http.StatusOK)
	})

	h := Chain(mw("a"), nil, mw("b"), nil).Handler(endpoint)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	want := []string{"a", "b", "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected execution:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestChain_EmptyChain_ReturnsObservableHandler(t *testing.T) {
	var got []string

	endpoint := &endpointHandler{got: &got}
	h := Chain().Handler(endpoint)

	ch, ok := h.(*ChainHandler)
	if !ok {
		t.Fatalf("expected *ChainHandler, got %T", h)
	}
	if ch.Endpoint != endpoint {
		t.Fatalf("Endpoint mismatch")
	}
	if len(ch.Middlewares) != 0 {
		t.Fatalf("expected empty middleware snapshot, got %d", len(ch.Middlewares))
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	want := []string{"h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected execution:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestChain_AllNil_ReturnsNilChainButHandlerStillWorks(t *testing.T) {
	var got []string

	endpoint := &endpointHandler{got: &got}
	mws := Chain(nil, nil)
	if mws != nil {
		t.Fatalf("expected nil chain, got %#v", mws)
	}
	h := mws.Handler(endpoint)

	ch, ok := h.(*ChainHandler)
	if !ok {
		t.Fatalf("expected *ChainHandler, got %T", h)
	}
	if len(ch.Middlewares) != 0 {
		t.Fatalf("expected empty middleware snapshot, got %d", len(ch.Middlewares))
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	want := []string{"h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("unexpected execution:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestMiddlewares_With_DoesNotMutateReceiver(t *testing.T) {
	var got []string

	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got = append(got, name)
				next.ServeHTTP(w, r)
			})
		}
	}

	endpoint := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, "h")
		w.WriteHeader(http.StatusOK)
	})

	base := Chain(mw("a"))
	derived := base.With(mw("b"))

	{
		got = got[:0]
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		base.Handler(endpoint).ServeHTTP(rr, req)

		want := []string{"a", "h"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("base chain changed:\n got: %#v\nwant: %#v", got, want)
		}
	}

	{
		got = got[:0]
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
		derived.Handler(endpoint).ServeHTTP(rr, req)

		want := []string{"a", "b", "h"}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("derived chain mismatch:\n got: %#v\nwant: %#v", got, want)
		}
	}
}

func TestChainHandler_ObservableAndStableSnapshot(t *testing.T) {
	var got []string

	mwA := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = append(got, "a")
			next.ServeHTTP(w, r)
		})
	}
	mwB := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = append(got, "b")
			next.ServeHTTP(w, r)
		})
	}

	endpoint := &endpointHandler{got: &got}

	mws := Chain(mwA, nil)
	h := mws.Handler(endpoint)

	ch, ok := h.(*ChainHandler)
	if !ok {
		t.Fatalf("expected *ChainHandler, got %T", h)
	}
	if ch.Endpoint != endpoint {
		t.Fatalf("Endpoint mismatch")
	}
	if len(ch.Middlewares) != 1 {
		t.Fatalf("expected nil-filtered snapshot, got %d", len(ch.Middlewares))
	}

	// Mutate the original slice after Handler() is built.
	mws[0] = mwB

	got = got[:0]
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://example.test/", nil)
	h.ServeHTTP(rr, req)

	// The built handler should stay stable (still uses mwA).
	want := []string{"a", "h"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("built handler changed after mws mutation:\n got: %#v\nwant: %#v", got, want)
	}
}

func TestNilEndpointPanics(t *testing.T) {
	assertPanics(t, func() { Chain().Handler(nil) })
	assertPanics(t, func() { Chain().HandlerFunc(nil) })
	assertPanics(t, func() { Wrap(nil) })
}

func assertPanics(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if r := recover(); r == nil {
			t.Fatalf("expected panic")
		}
	}()
	fn()
}

type endpointHandler struct {
	got *[]string
}

func (h *endpointHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	*h.got = append(*h.got, "h")
	w.WriteHeader(http.StatusOK)
}
