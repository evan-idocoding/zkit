package zkit

import (
	"context"
	"errors"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func TestService_OnStartHookError_InitiatesShutdownAndWaitReturnsWrapped(t *testing.T) {
	sentinel := errors.New("boom")
	s := NewDefaultService(ServiceSpec{
		OnStart: []func(context.Context) error{
			func(context.Context) error { return sentinel },
		},
	})

	err := s.Start(context.Background())
	if err == nil || !errors.Is(err, sentinel) {
		t.Fatalf("Start err=%v, want sentinel", err)
	}

	werr := s.Wait()
	if werr == nil || !errors.Is(werr, sentinel) || !strings.Contains(werr.Error(), "OnStart[0]") {
		t.Fatalf("Wait err=%v, want wrapped OnStart[0] sentinel", werr)
	}
}

func TestService_Run_ContextCancel_TriggersShutdownAndReturns(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		},
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(ctx) }()

	_ = waitForBoundAddr(t, s, s.PrimaryServer) // ensure started
	cancel()

	select {
	case err := <-errCh:
		if err == nil || !errors.Is(err, context.Canceled) {
			t.Fatalf("Run err=%v, want context.Canceled (wrapped)", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Run to return")
	}
}

func TestService_OnShutdownHooks_RunAll_EvenWhenOnePanics(t *testing.T) {
	var h0, h2 atomic.Int32
	sentinel := errors.New("shutdown hook err")

	s := NewDefaultService(ServiceSpec{
		OnShutdown: []func(context.Context) error{
			func(context.Context) error {
				h0.Add(1)
				return nil
			},
			func(context.Context) error {
				panic("boom")
			},
			func(context.Context) error {
				h2.Add(1)
				return sentinel
			},
		},
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	serr := s.Shutdown(context.Background())
	if serr == nil {
		t.Fatalf("Shutdown err=nil, want non-nil")
	}
	if h0.Load() != 1 || h2.Load() != 1 {
		t.Fatalf("hooks called h0=%d h2=%d, want 1 each", h0.Load(), h2.Load())
	}
	if !strings.Contains(serr.Error(), "OnShutdown[1]") || !strings.Contains(serr.Error(), "panic") {
		t.Fatalf("Shutdown err=%v, want OnShutdown[1] panic", serr)
	}
	if !errors.Is(serr, sentinel) {
		t.Fatalf("Shutdown err=%v, want sentinel error joined", serr)
	}
	if werr := s.Wait(); werr == nil {
		t.Fatalf("Wait err=nil, want non-nil")
	}
}

func TestService_Shutdown_IdempotentAndConcurrentWaits(t *testing.T) {
	release := make(chan struct{})
	var hooks atomic.Int32

	s := NewDefaultService(ServiceSpec{
		OnShutdown: []func(context.Context) error{
			func(context.Context) error {
				hooks.Add(1)
				<-release
				return nil
			},
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	bgDone := make(chan error, 1)
	go func() { bgDone <- s.Shutdown(context.Background()) }()

	// Wait until shutdown is in progress (hook is blocking).
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		stopping := s.stopping
		s.mu.Unlock()
		if stopping {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := s.Shutdown(ctx); err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Shutdown(timeout) err=%v, want context.DeadlineExceeded", err)
	}

	close(release)

	select {
	case err := <-bgDone:
		if err != nil {
			t.Fatalf("Shutdown(background) err=%v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Shutdown(background)")
	}

	if hooks.Load() != 1 {
		t.Fatalf("OnShutdown hook calls=%d, want 1", hooks.Load())
	}
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait err=%v, want nil", err)
	}
}

func TestService_ExtraServers_ServeAndShutdown_OK(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("primary"))
			}),
		},
		Extra: []*HTTPServerSpec{
			{
				Addr: "127.0.0.1:0",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					_, _ = w.Write([]byte("extra"))
				}),
			},
		},
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	pAddr := waitForBoundAddr(t, s, s.PrimaryServer)
	eAddr := waitForBoundAddr(t, s, s.ExtraServers[0])

	if code, body := httpGetBody(t, "http://"+pAddr+"/"); code != http.StatusOK || body != "primary" {
		t.Fatalf("primary status=%d body=%q, want 200 primary", code, body)
	}
	if code, body := httpGetBody(t, "http://"+eAddr+"/"); code != http.StatusOK || body != "extra" {
		t.Fatalf("extra status=%d body=%q, want 200 extra", code, body)
	}

	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown err=%v", err)
	}
	if err := s.Wait(); err != nil {
		t.Fatalf("Wait err=%v", err)
	}
}

func TestService_AdminStandalone_ServesHealthz(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Admin: &AdminSpec{
			ReadGuard: AllowAll(),
		},
		AdminStandaloneServer: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			// Handler may be nil; zkit injects the admin handler.
		},
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	aAddr := waitForBoundAddr(t, s, s.AdminServer)
	if code, _ := httpGetBody(t, "http://"+aAddr+"/healthz"); code != http.StatusOK {
		t.Fatalf("admin healthz status=%d, want %d", code, http.StatusOK)
	}

	_ = s.Shutdown(context.Background())
	_ = s.Wait()
}

func TestService_AdminExpose_TasksAndTuning_MountedOnPrimary(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("biz"))
			}),
		},
		Admin: &AdminSpec{
			ReadGuard: AllowAll(),
		},
		// Default mount prefix "/-/".
		TasksExposeToAdmin:  true,
		TuningExposeToAdmin: true,
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}

	pAddr := waitForBoundAddr(t, s, s.PrimaryServer)
	if code, _ := httpGetBody(t, "http://"+pAddr+"/-/tasks/snapshot"); code != http.StatusOK {
		t.Fatalf("tasks snapshot status=%d, want %d", code, http.StatusOK)
	}
	if code, _ := httpGetBody(t, "http://"+pAddr+"/-/tuning/snapshot"); code != http.StatusOK {
		t.Fatalf("tuning snapshot status=%d, want %d", code, http.StatusOK)
	}

	_ = s.Shutdown(context.Background())
	_ = s.Wait()
}

func TestService_ServeError_Critical_TriggersGlobalShutdown(t *testing.T) {
	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		},
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	_ = waitForBoundAddr(t, s, s.PrimaryServer)

	// Simulate an unexpected Serve error by closing the bound listener while not stopping.
	s.mu.Lock()
	ln := s.listeners[s.PrimaryServer]
	s.mu.Unlock()
	if ln == nil {
		t.Fatalf("expected primary listener")
	}
	_ = ln.Close()

	select {
	case <-s.doneCh:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for global shutdown after critical serve error")
	}

	err := s.Wait()
	if err == nil || !strings.Contains(err.Error(), `server "primary"`) {
		t.Fatalf("Wait err=%v, want error mentioning primary server failure", err)
	}
}

func TestService_ServeError_NonCritical_DoesNotTriggerGlobalShutdown(t *testing.T) {
	nonCritical := false

	var (
		cbMu     sync.Mutex
		cbCalled int
		cbCrit   *bool
		cbName   string
	)
	cbCh := make(chan struct{}, 1)

	s := NewDefaultService(ServiceSpec{
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		},
		Extra: []*HTTPServerSpec{
			{
				Name:     "noncritical-extra",
				Critical: &nonCritical,
				Addr:     "127.0.0.1:0",
				Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.WriteHeader(http.StatusOK)
				}),
			},
		},
		OnServeError: func(name string, err error, critical bool) {
			cbMu.Lock()
			defer cbMu.Unlock()
			cbCalled++
			c := critical
			cbCrit = &c
			cbName = name
			select {
			case cbCh <- struct{}{}:
			default:
			}
		},
	})

	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start err=%v", err)
	}
	_ = waitForBoundAddr(t, s, s.PrimaryServer)
	_ = waitForBoundAddr(t, s, s.ExtraServers[0])

	// Cause the non-critical extra server to exit unexpectedly.
	s.mu.Lock()
	eln := s.listeners[s.ExtraServers[0]]
	s.mu.Unlock()
	if eln == nil {
		t.Fatalf("expected extra listener")
	}
	_ = eln.Close()

	select {
	case <-cbCh:
		// ok
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for OnServeError callback")
	}

	// Ensure global shutdown did NOT start automatically.
	select {
	case <-s.doneCh:
		t.Fatalf("service stopped unexpectedly after non-critical serve error")
	case <-time.After(200 * time.Millisecond):
		// ok
	}

	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		t.Fatalf("stopping=true, want false after non-critical serve error")
	}

	cbMu.Lock()
	called := cbCalled
	crit := cbCrit
	name := cbName
	cbMu.Unlock()
	if called == 0 {
		t.Fatalf("OnServeError was not called")
	}
	if crit == nil || *crit {
		t.Fatalf("OnServeError critical=%v, want false", crit)
	}
	if name != "noncritical-extra" {
		t.Fatalf("OnServeError name=%q, want %q", name, "noncritical-extra")
	}

	// Cleanup. Note: shutting down after a server has already died may join http.ErrServerClosed.
	_ = s.Shutdown(context.Background())
	_ = s.Wait()
}

func TestService_Run_Signal_TriggersGracefulShutdown(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals not supported on windows")
	}

	// Use a non-fatal signal by default to avoid terminating the test process if
	// signal.Notify wiring regresses.
	//
	// On macOS/Linux, SIGWINCH is ignored by default.
	s := NewDefaultService(ServiceSpec{
		Signals: []os.Signal{syscall.SIGWINCH},
		Primary: &HTTPServerSpec{
			Addr: "127.0.0.1:0",
			Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			}),
		},
	})

	// Ensure no other test has SIGWINCH ignored/reset unexpectedly.
	signal.Reset(syscall.SIGWINCH)
	t.Cleanup(func() { signal.Reset(syscall.SIGWINCH) })

	errCh := make(chan error, 1)
	go func() { errCh <- s.Run(context.Background()) }()

	_ = waitForBoundAddr(t, s, s.PrimaryServer) // ensure started and signal.Notify active
	_ = syscall.Kill(os.Getpid(), syscall.SIGWINCH)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("Run err=%v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("timeout waiting for Run to return after signal")
	}
}
