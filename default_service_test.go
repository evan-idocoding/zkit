package zkit

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/evan-idocoding/zkit/admin"
)

func TestService_WaitBeforeStart_ErrNotStarted(t *testing.T) {
	s := NewDefaultService(ServiceSpec{})
	if err := s.Wait(); err != ErrNotStarted {
		t.Fatalf("err=%v, want %v", err, ErrNotStarted)
	}
}

func TestService_ShutdownBeforeStart_OK(t *testing.T) {
	s := NewDefaultService(ServiceSpec{})
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("err=%v, want nil", err)
	}
}

func TestService_StartShutdownWait_OK(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr:    "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait err=%v", err)
	}
	if err := s.Start(context.Background()); err != ErrAlreadyStarted {
		t.Fatalf("Start(again) err=%v, want %v", err, ErrAlreadyStarted)
	}
}

func TestNewDefaultService_AdminMount_RoutesPrefixToAdmin(t *testing.T) {
	biz := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("biz"))
	})

	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr:    "127.0.0.1:12345",
			Handler: biz,
		},
		Admin: &ServiceAdminSpec{
			Spec: AdminSpec{
				ReadGuard: admin.AllowAll(),
			},
			Mount: &AdminMountSpec{Prefix: "/-/"},
		},
	})

	if s.PrimaryServer == nil || s.PrimaryServer.Handler == nil {
		t.Fatalf("expected primary server handler")
	}

	// Admin path.
	rw := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://svc.test/-/healthz", nil)
	s.PrimaryServer.Handler.ServeHTTP(rw, req)
	if rw.Code != http.StatusOK {
		t.Fatalf("admin status=%d, want %d, body=%s", rw.Code, http.StatusOK, rw.Body.String())
	}

	// Primary path.
	rw2 := httptest.NewRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "http://svc.test/x", nil)
	s.PrimaryServer.Handler.ServeHTTP(rw2, req2)
	if rw2.Code != http.StatusOK || rw2.Body.String() != "biz" {
		t.Fatalf("biz status=%d body=%q, want 200 and %q", rw2.Code, rw2.Body.String(), "biz")
	}
}

func TestService_StartRollback_DoesNotShutdownUnstartedServers(t *testing.T) {
	// First server should bind successfully.
	// Second server uses an invalid port and should fail at net.Listen.
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr:    "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		},
		Extra: []*HTTPServerSpec{
			{
				Addr:    "127.0.0.1:-1",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
			},
		},
	})

	err := s.Start(context.Background())
	if err == nil {
		t.Fatalf("expected Start error")
	}
	// Wait should return the primary Start error only (no extra shutdown noise).
	if werr := s.Wait(); werr == nil {
		t.Fatalf("expected Wait error")
	} else if werr.Error() != err.Error() {
		t.Fatalf("Wait err=%v, want %v", werr, err)
	}
}
