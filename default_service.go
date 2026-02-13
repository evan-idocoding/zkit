package zkit

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/evan-idocoding/zkit/rt/task"
	"github.com/evan-idocoding/zkit/rt/tuning"
)

var (
	// ErrAlreadyStarted indicates Start/Run was called more than once.
	ErrAlreadyStarted = errors.New("zkit: service already started")
	// ErrNotStarted indicates Wait was called before Start.
	ErrNotStarted = errors.New("zkit: service not started")
)

// Service is a runnable, shutdownable assembled service.
type Service struct {
	// Assembly outputs (optional depending on spec).
	PrimaryServer *http.Server
	ExtraServers  []*http.Server
	AdminServer   *http.Server
	AdminHandler  http.Handler

	TaskManager *task.Manager
	Tuning      *tuning.Tuning
	LogLevelVar *slog.LevelVar

	// --- internals ---

	onStart         []func(context.Context) error
	onShutdown      []func(context.Context) error
	onServeError    func(name string, err error, critical bool)
	signalsDisable  bool
	signalsList     []os.Signal
	shutdownTimeout time.Duration

	tasksEnabled bool

	servers       []managedServer // primary + extra + (admin standalone if present)
	adminOnlySrv  *http.Server
	adminOnlyName string

	mu        sync.Mutex
	started   bool
	startCtx  context.Context
	startStop context.CancelFunc
	stopping  bool

	// listeners tracks successfully bound listeners (per server). It is used to:
	//   - shut down only servers that actually bound successfully
	//   - avoid races where Shutdown happens before Serve starts tracking listeners
	listeners map[*http.Server]net.Listener

	primaryErr error

	shutdownOnce sync.Once
	shutdownCh   chan struct{}
	shutdownErr  error

	doneCh  chan struct{}
	waitErr error
}

type managedServer struct {
	name     string
	critical bool
	srv      *http.Server
}

// NewDefaultService assembles a default-safe runnable Service.
//
// Assembly errors are fail-fast and will panic.
// Runtime errors are returned from Start/Wait/Run/Shutdown.
func NewDefaultService(spec ServiceSpec) *Service {
	s := &Service{
		onStart:         spec.OnStart,
		onShutdown:      spec.OnShutdown,
		onServeError:    spec.OnServeError,
		signalsDisable:  spec.SignalsDisable,
		signalsList:     spec.Signals,
		shutdownTimeout: resolveDuration(spec.ShutdownTimeout, 30*time.Second),
		shutdownCh:      make(chan struct{}),
		doneCh:          make(chan struct{}),
		listeners:       make(map[*http.Server]net.Listener),
	}

	// ---- validate & assemble optional managed components ----

	// log level var: enabled when LevelVar != nil; create when admin needs it and ExposeToAdmin
	var lv *slog.LevelVar
	if spec.LogLevelVar != nil {
		lv = spec.LogLevelVar
	}

	// tasks manager: enabled when Manager != nil; create when admin needs it
	var mgr *task.Manager
	tasksEnabled := spec.TasksManager != nil
	if spec.TasksManager != nil {
		mgr = spec.TasksManager
	}

	// tuning: enabled when Tuning != nil; create when admin needs it
	var tu *tuning.Tuning
	if spec.Tuning != nil {
		tu = spec.Tuning
	}

	adminEnabled := spec.Admin != nil
	if adminEnabled {
		if spec.Admin.LogLevelVar != nil {
			lv = spec.Admin.LogLevelVar
		}
		if spec.Admin.Tuning != nil {
			tu = spec.Admin.Tuning
		}
		if spec.Admin.TaskManager != nil {
			mgr = spec.Admin.TaskManager
		}
	}

	adminWrites := adminEnabled && spec.Admin.WriteGuard != nil
	// Fail fast when a write group is enabled but no source was provided (do not auto-create for writes).
	if adminWrites && spec.Admin.EnableLogLevelSet && lv == nil {
		panic("zkit: ServiceSpec: EnableLogLevelSet requires LogLevelVar or Admin.LogLevelVar (or LogExposeToAdmin)")
	}
	if adminWrites && tuningWritesEnabled(*spec.Admin) && tu == nil {
		panic("zkit: ServiceSpec: TuningWritesEnabled requires Tuning or Admin.Tuning (or TuningExposeToAdmin)")
	}
	if adminWrites && taskWritesEnabled(*spec.Admin) && mgr == nil {
		panic("zkit: ServiceSpec: TaskWritesEnabled requires TasksManager or Admin.TaskManager (or TasksExposeToAdmin)")
	}

	adminNeedsLog := adminEnabled && (spec.Admin.LogLevelVar != nil ||
		spec.LogExposeToAdmin ||
		(adminWrites && spec.Admin.EnableLogLevelSet))
	if adminNeedsLog && lv == nil {
		lv = &slog.LevelVar{}
	}

	adminNeedsTuning := adminEnabled && (spec.Admin.Tuning != nil ||
		spec.TuningExposeToAdmin ||
		(adminWrites && tuningWritesEnabled(*spec.Admin)))
	if adminNeedsTuning && tu == nil {
		tu = tuning.New()
	}

	adminNeedsTasks := adminEnabled && (spec.Admin.TaskManager != nil ||
		spec.TasksExposeToAdmin ||
		(adminWrites && taskWritesEnabled(*spec.Admin)))
	if adminNeedsTasks && mgr == nil {
		mgr = task.NewManager()
		if adminWrites && taskWritesEnabled(*spec.Admin) {
			tasksEnabled = true
		}
	}

	s.tasksEnabled = tasksEnabled && mgr != nil
	s.TaskManager = mgr
	s.Tuning = tu
	s.LogLevelVar = lv

	// ---- assemble admin handler & admin mode ----

	var adminHandler http.Handler
	var adminMountPrefix string
	var adminStandaloneSpec *HTTPServerSpec
	if adminEnabled {
		if spec.AdminStandaloneServer != nil && strings.TrimSpace(spec.AdminMountPrefix) != "" {
			panic("zkit: ServiceSpec: AdminStandaloneServer and AdminMountPrefix are mutually exclusive")
		}
		if spec.AdminStandaloneServer == nil && spec.Primary == nil {
			panic("zkit: ServiceSpec: worker-only requires AdminStandaloneServer")
		}

		adminSpec := *spec.Admin
		if adminSpec.ReadGuard == nil {
			panic("zkit: ServiceSpec.Admin: nil ReadGuard")
		}
		if adminSpec.LogLevelVar == nil && spec.LogExposeToAdmin {
			adminSpec.LogLevelVar = lv
		}
		if adminSpec.Tuning == nil && spec.TuningExposeToAdmin {
			adminSpec.Tuning = tu
		}
		if adminSpec.TaskManager == nil && spec.TasksExposeToAdmin {
			adminSpec.TaskManager = mgr
		}
		if adminSpec.WriteGuard != nil && adminSpec.EnableLogLevelSet && adminSpec.LogLevelVar == nil {
			adminSpec.LogLevelVar = lv
		}
		if adminSpec.WriteGuard != nil && tuningWritesEnabled(adminSpec) && adminSpec.Tuning == nil {
			adminSpec.Tuning = tu
		}
		if adminSpec.WriteGuard != nil && taskWritesEnabled(adminSpec) && adminSpec.TaskManager == nil {
			adminSpec.TaskManager = mgr
		}

		adminHandler = NewDefaultAdmin(adminSpec)
		s.AdminHandler = adminHandler

		if spec.AdminStandaloneServer == nil {
			adminMountPrefix = strings.TrimSpace(spec.AdminMountPrefix)
			if adminMountPrefix == "" {
				adminMountPrefix = "/-/"
			}
		} else {
			// Standalone admin: no primary required, use admin server only.
			adminStandaloneSpec = spec.AdminStandaloneServer
		}
	}

	// ---- assemble primary/extra servers ----

	if spec.Primary != nil {
		srv, name, critical := assembleHTTPServerOrPanic(*spec.Primary, "primary", true, nil)
		if adminEnabled && adminStandaloneSpec == nil {
			bh := srv.Handler
			if bh == nil {
				bh = http.DefaultServeMux
			}
			mh := mountPrefix(adminMountPrefix, adminHandler, bh)
			srv.Handler = mh
		}
		s.PrimaryServer = srv
		s.servers = append(s.servers, managedServer{name: name, critical: critical, srv: srv})
	} else if adminEnabled && adminStandaloneSpec == nil {
		panic("zkit: ServiceSpec: Admin mount requires Primary")
	}

	if len(spec.Extra) != 0 {
		for i, sp := range spec.Extra {
			if sp == nil {
				panic(fmt.Sprintf("zkit: ServiceSpec.Extra[%d] is nil", i))
			}
			defName := fmt.Sprintf("extra#%d", i)
			srv, name, critical := assembleHTTPServerOrPanic(*sp, defName, true, nil)
			s.ExtraServers = append(s.ExtraServers, srv)
			s.servers = append(s.servers, managedServer{name: name, critical: critical, srv: srv})
		}
	}

	// ---- assemble standalone admin server (if configured) ----

	if adminEnabled && adminStandaloneSpec != nil {
		srv, name, critical := assembleHTTPServerOrPanic(*adminStandaloneSpec, "admin", true, adminHandler)
		s.AdminServer = srv
		s.adminOnlySrv = srv
		s.adminOnlyName = name
		s.servers = append(s.servers, managedServer{name: name, critical: critical, srv: srv})
	}

	return s
}

// Run is equivalent to Start → wait for exit condition → Shutdown → return.
//
// It is NOT idempotent. If called after Start, it returns ErrAlreadyStarted.
func (s *Service) Run(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := s.Start(ctx); err != nil {
		return err
	}

	sigCh, stopSignals := s.runSignalWatcher()
	defer stopSignals()

	select {
	case <-s.doneCh:
		return s.Wait()
	case <-ctx.Done():
		s.recordPrimary(ctx.Err())
		_ = s.Shutdown(context.Background())
		return s.Wait()
	case <-sigCh:
		_ = s.Shutdown(context.Background())
		return s.Wait()
	}
}

// Start starts managed components. It is NOT idempotent.
func (s *Service) Start(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.started {
		s.mu.Unlock()
		return ErrAlreadyStarted
	}
	s.started = true
	s.startCtx, s.startStop = context.WithCancel(ctx)
	s.mu.Unlock()

	// 1) OnStart hooks.
	for i, h := range s.onStart {
		if h == nil {
			continue
		}
		if err := safeCallHook(s.startCtx, h); err != nil {
			s.recordPrimary(fmt.Errorf("zkit: OnStart[%d]: %w", i, err))
			s.initiateShutdown()
			return err
		}
	}

	// 2) tasks
	if s.tasksEnabled && s.TaskManager != nil {
		if err := s.TaskManager.Start(s.startCtx); err != nil {
			s.recordPrimary(err)
			s.initiateShutdown()
			return err
		}
	}

	// 3) servers (primary + extra + standalone admin)
	for i := range s.servers {
		ms := s.servers[i]
		if ms.srv == nil {
			continue
		}
		if err := s.startOneServer(ms); err != nil {
			s.recordPrimary(err)
			s.initiateShutdown()
			return err
		}
	}
	return nil
}

// Wait waits until the service fully stops.
//
// It is idempotent. If Start was never called, it returns ErrNotStarted.
func (s *Service) Wait() error {
	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return ErrNotStarted
	}
	ch := s.doneCh
	s.mu.Unlock()

	<-ch

	s.mu.Lock()
	err := s.waitErr
	s.mu.Unlock()
	return err
}

// Shutdown triggers shutdown. It is idempotent.
//
// If Start was never called, Shutdown returns nil.
// If Shutdown is already in progress, calling it again waits again using the new ctx.
func (s *Service) Shutdown(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if !s.started {
		s.mu.Unlock()
		return nil
	}
	shutdownCh := s.shutdownCh
	s.mu.Unlock()

	s.initiateShutdown()

	select {
	case <-shutdownCh:
		s.mu.Lock()
		err := s.shutdownErr
		s.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Service) startOneServer(ms managedServer) error {
	addr := ""
	if ms.srv != nil {
		addr = ms.srv.Addr
	}
	if addr == "" {
		return fmt.Errorf("zkit: server %q has empty Addr", ms.name)
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("zkit: server %q listen %q: %w", ms.name, addr, err)
	}

	// Record the bound listener first, so shutdown can close it even if shutdown
	// begins before Serve starts tracking listeners.
	s.mu.Lock()
	if s.listeners != nil && ms.srv != nil {
		s.listeners[ms.srv] = ln
	}
	s.mu.Unlock()

	go func() {
		err := ms.srv.Serve(ln)
		s.onServeExit(ms, err)
	}()
	return nil
}

func (s *Service) onServeExit(ms managedServer, err error) {
	if err == nil || errors.Is(err, http.ErrServerClosed) {
		return
	}
	// During shutdown, servers may exit with non-ErrServerClosed errors (e.g. listener close races).
	// Do not treat them as failures.
	s.mu.Lock()
	stopping := s.stopping
	s.mu.Unlock()
	if stopping {
		return
	}
	if ms.critical {
		s.recordPrimary(fmt.Errorf("zkit: server %q: %w", ms.name, err))
		if s.onServeError != nil {
			s.onServeError(ms.name, err, true)
		}
		s.initiateShutdown()
		return
	}
	if s.onServeError != nil {
		s.onServeError(ms.name, err, false)
	}
}

func (s *Service) recordPrimary(err error) {
	if err == nil {
		return
	}
	s.mu.Lock()
	if s.primaryErr == nil {
		s.primaryErr = err
	}
	s.mu.Unlock()
}

func (s *Service) initiateShutdown() {
	s.shutdownOnce.Do(func() {
		go s.doShutdown()
	})
}

func (s *Service) doShutdown() {
	// Make shutdown observable to in-flight contexts ASAP.
	s.mu.Lock()
	stop := s.startStop
	s.stopping = true
	// Snapshot listeners for this shutdown.
	listeners := make(map[*http.Server]net.Listener, len(s.listeners))
	for srv, ln := range s.listeners {
		listeners[srv] = ln
	}
	s.mu.Unlock()
	if stop != nil {
		stop()
	}

	ctx := context.Background()
	cancel := func() {}
	if s.shutdownTimeout > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), s.shutdownTimeout)
	}
	defer cancel()

	var errs []error

	// 1) shutdown primary + extra servers in parallel; admin standalone is handled last.
	var wg sync.WaitGroup
	var mu sync.Mutex

	shutdownOne := func(ms managedServer) {
		defer wg.Done()
		if ms.srv == nil {
			return
		}
		// Only shut down servers that successfully bound.
		ln, ok := listeners[ms.srv]
		if !ok {
			return
		}
		if s.adminOnlySrv != nil && ms.srv == s.adminOnlySrv {
			return
		}
		if err := ms.srv.Shutdown(ctx); err != nil {
			// Best-effort force close if graceful shutdown failed due to timeout/cancel.
			_ = ms.srv.Close()
			mu.Lock()
			errs = append(errs, fmt.Errorf("server %q shutdown: %w", ms.name, err))
			mu.Unlock()
		}
		// Best-effort: close the listener to cover the race where Shutdown happened
		// before Serve started tracking listeners.
		_ = ln.Close()
	}

	for _, ms := range s.servers {
		if ms.srv == nil {
			continue
		}
		wg.Add(1)
		go shutdownOne(ms)
	}
	wg.Wait()

	// 2) shutdown tasks
	if s.tasksEnabled && s.TaskManager != nil {
		if err := s.TaskManager.Shutdown(ctx); err != nil {
			errs = append(errs, fmt.Errorf("tasks shutdown: %w", err))
		}
	}

	// 3) OnShutdown hooks (sequential; best-effort run all)
	for i, h := range s.onShutdown {
		if h == nil {
			continue
		}
		if err := safeCallHook(ctx, h); err != nil {
			errs = append(errs, fmt.Errorf("OnShutdown[%d]: %w", i, err))
		}
	}

	// 4) shutdown standalone admin last
	if s.adminOnlySrv != nil {
		ln, ok := listeners[s.adminOnlySrv]
		if !ok {
			// Admin server never bound; nothing to shut down.
		} else {
			if err := s.adminOnlySrv.Shutdown(ctx); err != nil {
				_ = s.adminOnlySrv.Close()
				name := s.adminOnlyName
				if strings.TrimSpace(name) == "" {
					name = "admin"
				}
				errs = append(errs, fmt.Errorf("admin server %q shutdown: %w", name, err))
			}
			_ = ln.Close()
		}
	}

	shutdownErr := errors.Join(errs...)

	s.mu.Lock()
	s.shutdownErr = shutdownErr
	primary := s.primaryErr
	s.waitErr = errors.Join(primary, shutdownErr)
	s.mu.Unlock()

	close(s.shutdownCh)
	close(s.doneCh)
}

func (s *Service) runSignalWatcher() (<-chan os.Signal, func()) {
	// No signals requested.
	if s.signalsDisable {
		return nil, func() {}
	}
	sigs := s.signalsList
	if len(sigs) == 0 {
		sigs = defaultSignals()
	}
	if len(sigs) == 0 {
		return nil, func() {}
	}
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sigs...)
	stop := func() {
		signal.Stop(ch)
	}
	return ch, stop
}

// --- spec types ---

// ServiceSpec configures NewDefaultService. All fields are optional.
//
// Admin: nil = admin disabled. When non-nil, admin is either mounted on Primary (AdminMountPrefix, default "/-/")
// or served on its own server (AdminStandaloneServer). Mount and Standalone are mutually exclusive.
//
// Tasks/Tuning/Log: non-nil Manager/Tuning/LevelVar = enabled and started/wired; nil + ExposeToAdmin or admin
// write need causes Service to create a default instance.
//
// Override & ownership rules (when both ServiceSpec.<X> and ServiceSpec.Admin.<X> are set):
//   - Admin wiring uses Admin.<X> (Admin overrides Service-level source for the admin subtree).
//   - Service lifecycle ownership is controlled by ServiceSpec fields:
//       - TasksManager: when non-nil, Service starts/shuts down the task manager; when nil, Service does not manage it
//         (even if Admin.TaskManager is set to expose tasks endpoints).
//       - Tuning/LogLevelVar: not started/stopped by Service; they are data sources only.
type ServiceSpec struct {
	// Signals: SignalsDisable true = Run() does not listen for OS signals. Signals nil/empty = default set (SIGINT+SIGTERM on Unix).
	SignalsDisable bool
	Signals        []os.Signal

	// ShutdownTimeout: <= 0 means default (30s).
	ShutdownTimeout time.Duration

	// Primary: optional. Required if admin is mounted (Admin != nil and AdminStandaloneServer == nil).
	Primary *HTTPServerSpec

	// Extra: additional servers; admin is not mounted onto them.
	Extra []*HTTPServerSpec

	// Admin: nil = admin disabled. Non-nil = admin enabled; use AdminMountPrefix or AdminStandaloneServer (mutually exclusive).
	Admin *AdminSpec

	// AdminMountPrefix: used when Admin != nil and AdminStandaloneServer == nil. Empty => "/-/".
	AdminMountPrefix string

	// AdminStandaloneServer: when set, admin is served on this server only (worker-only). Mutually exclusive with mount.
	AdminStandaloneServer *HTTPServerSpec

	// Tasks: Manager != nil = enabled and started. ExposeToAdmin = wire into admin when admin enabled.
	TasksManager    *task.Manager
	TasksExposeToAdmin bool

	// Tuning: non-nil = enabled. ExposeToAdmin = wire into admin when admin enabled.
	Tuning             *tuning.Tuning
	TuningExposeToAdmin bool

	// Log: LogLevelVar != nil = enabled. ExposeToAdmin = wire into admin when admin enabled.
	LogLevelVar     *slog.LevelVar
	LogExposeToAdmin bool

	// Lifecycle hooks and serve error observer.
	OnStart      []func(context.Context) error
	OnShutdown   []func(context.Context) error
	OnServeError func(name string, err error, critical bool)
}

// HTTPServerSpec describes a managed http.Server.
type HTTPServerSpec struct {
	// Name is used for error reporting (OnServeError) and diagnostics.
	// Empty name will be replaced by a default name (primary / extra#N / admin).
	Name string

	// Critical controls fail-fast behavior.
	//
	// When Critical is true (default), an unexpected Serve error triggers a global shutdown.
	// When Critical is false, unexpected errors do NOT trigger shutdown but are reported via OnServeError.
	//
	// If nil, it defaults to true.
	Critical *bool

	// Provide either:
	//   - Server: a fully configured *http.Server (zkit will not override timeouts, BaseContext, ErrorLog, etc)
	//   - or Addr + Handler: zkit will build a server with conservative defaults (ReadHeaderTimeout, IdleTimeout).
	//
	// Assembly rules:
	//   - If Server != nil, Addr/Handler fields are ignored.
	//   - If Server == nil, Addr must be non-empty and Handler must be non-nil (when used for Primary or Extra;
	//     when used as AdminStandaloneServer, Handler may be nil and zkit injects the admin handler).
	Server  *http.Server
	Addr    string
	Handler http.Handler
}


// --- helpers ---

func assembleHTTPServerOrPanic(spec HTTPServerSpec, defaultName string, defaultCritical bool, forceHandler http.Handler) (*http.Server, string, bool) {
	name := strings.TrimSpace(spec.Name)
	if name == "" {
		name = defaultName
	}
	critical := defaultCritical
	if spec.Critical != nil {
		critical = *spec.Critical
	}

	if spec.Server != nil {
		srv := spec.Server
		if srv.Addr == "" {
			panic("zkit: server " + name + ": empty http.Server.Addr")
		}
		if forceHandler != nil {
			if srv.Handler != nil && srv.Handler != forceHandler {
				panic("zkit: server " + name + ": custom http.Server.Handler conflicts with required handler (use Addr+Handler spec, or assemble manually)")
			}
			srv.Handler = forceHandler
		}
		return srv, name, critical
	}

	addr := strings.TrimSpace(spec.Addr)
	if addr == "" {
		panic("zkit: server " + name + ": empty Addr")
	}
	h := spec.Handler
	if forceHandler != nil {
		h = forceHandler
	}
	if h == nil {
		panic("zkit: server " + name + ": nil Handler")
	}
	srv := newHTTPServerWithDefaults(addr, h)
	return srv, name, critical
}

func resolveDuration(v, def time.Duration) time.Duration {
	if v <= 0 {
		return def
	}
	return v
}

func safeCallHook(ctx context.Context, fn func(context.Context) error) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return fn(ctx)
}

// --- net/http assembly internals ---

// Conservative default timeouts used only when zkit builds the *http.Server
// (not when the user provides a fully configured server).
const (
	defaultReadHeaderTimeout = 5 * time.Second
	defaultIdleTimeout       = 60 * time.Second
)

func newHTTPServerWithDefaults(addr string, handler http.Handler) *http.Server {
	if strings.TrimSpace(addr) == "" {
		panic("zkit: newHTTPServerWithDefaults: empty addr")
	}
	if handler == nil {
		panic("zkit: newHTTPServerWithDefaults: nil handler")
	}
	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}
}

// mountPrefix routes requests to subtree when the URL path has the given prefix,
// otherwise it routes to fallback.
//
// Behavior:
//   - If prefix is empty, it panics.
//   - If prefix does not start with "/", it panics.
//   - If prefix does not end with "/", it is normalized by appending "/".
//   - Requests to the base path without the trailing slash (e.g. "/-") are
//     redirected to the normalized prefix (e.g. "/-/") using HTTP 307.
func mountPrefix(prefix string, subtree, fallback http.Handler) http.Handler {
	prefix = normalizeMountPrefixOrPanic(prefix)
	base := strings.TrimSuffix(prefix, "/")

	if subtree == nil {
		panic("zkit: mountPrefix: nil subtree handler")
	}
	if fallback == nil {
		panic("zkit: mountPrefix: nil fallback handler")
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r == nil || r.URL == nil {
			fallback.ServeHTTP(w, r)
			return
		}
		path := r.URL.Path
		if path == base {
			// Preserve query string.
			target := prefix
			if r.URL.RawQuery != "" {
				target = target + "?" + r.URL.RawQuery
			}
			http.Redirect(w, r, target, http.StatusTemporaryRedirect)
			return
		}
		if strings.HasPrefix(path, prefix) {
			// Strip prefix but keep a leading "/" so net/http muxes do not redirect.
			r2 := new(http.Request)
			*r2 = *r
			u2 := *r.URL
			r2.URL = &u2
			p := strings.TrimPrefix(path, prefix)
			if p == "" {
				p = "/"
			} else if p[0] != '/' {
				p = "/" + p
			}
			r2.URL.Path = p
			// RawPath is optional; clear to avoid inconsistencies.
			r2.URL.RawPath = ""
			subtree.ServeHTTP(w, r2)
			return
		}
		fallback.ServeHTTP(w, r)
	})
}

func normalizeMountPrefixOrPanic(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		panic("zkit: mountPrefix: empty prefix")
	}
	if !strings.HasPrefix(prefix, "/") {
		panic("zkit: mountPrefix: invalid prefix (must start with '/'): " + prefix)
	}
	// Keep it conservative: disallow obvious ambiguity/injection.
	if strings.ContainsAny(prefix, " \t\r\n?#") {
		panic("zkit: mountPrefix: invalid prefix (contains whitespace or ?#): " + prefix)
	}
	if strings.Contains(prefix, "//") {
		panic("zkit: mountPrefix: invalid prefix (contains //): " + prefix)
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix = prefix + "/"
	}
	return prefix
}
