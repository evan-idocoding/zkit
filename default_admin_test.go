package zkit

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewDefaultAdmin_Provided_DefaultDisabled(t *testing.T) {
	h := NewDefaultAdmin(AdminSpec{
		ReadGuard: AllowAll(),
		// ProvidedItems nil => /provided not mounted
	})

	req := httptest.NewRequest(http.MethodGet, "http://admin.test/provided", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)

	// Not found: the endpoint is not mounted.
	if rw.Code != http.StatusNotFound {
		t.Fatalf("status=%d, want %d", rw.Code, http.StatusNotFound)
	}
}

func TestNewDefaultAdmin_LogLevelSet_RequiresVar(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatalf("expected panic")
		}
	}()
	_ = NewDefaultAdmin(AdminSpec{
		ReadGuard:          AllowAll(),
		WriteGuard:        AllowAll(),
		EnableLogLevelSet: true,
	})
}

func TestNewDefaultAdmin_LogLevelGet_Works(t *testing.T) {
	var lv slog.LevelVar
	lv.Set(slog.LevelDebug)

	h := NewDefaultAdmin(AdminSpec{
		ReadGuard:   AllowAll(),
		LogLevelVar: &lv,
	})

	req := httptest.NewRequest(http.MethodGet, "http://admin.test/log/level?format=json", nil)
	rw := httptest.NewRecorder()
	h.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("status=%d, want %d, body=%s", rw.Code, http.StatusOK, rw.Body.String())
	}
}
