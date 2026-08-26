// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/m4schini/splitkauf/config"
)

func TestSessionStore(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		oidcEnabled  bool
		dbReachable  bool
		wantPostgres bool
		wantErr      string
	}{
		{
			name:         "oidc with reachable db uses postgres",
			oidcEnabled:  true,
			dbReachable:  true,
			wantPostgres: true,
			wantErr:      "",
		},
		{
			name:         "oidc with unreachable db fails fast",
			oidcEnabled:  true,
			dbReachable:  false,
			wantPostgres: false,
			wantErr:      "sessions require a reachable database in OIDC mode",
		},
		{
			name:         "dev-auth with reachable db uses postgres",
			oidcEnabled:  false,
			dbReachable:  true,
			wantPostgres: true,
			wantErr:      "",
		},
		{
			name:         "dev-auth with unreachable db falls back to memory",
			oidcEnabled:  false,
			dbReachable:  false,
			wantPostgres: false,
			wantErr:      "",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			usePostgres, err := sessionStore(tst.oidcEnabled, tst.dbReachable)

			if tst.wantErr != "" {
				if err == nil {
					t.Fatalf("sessionStore() error = nil, want %q", tst.wantErr)
				}

				if err.Error() != tst.wantErr {
					t.Fatalf("sessionStore() error = %q, want %q", err.Error(), tst.wantErr)
				}

				return
			}

			if err != nil {
				t.Fatalf("sessionStore() unexpected error = %v", err)
			}

			if usePostgres != tst.wantPostgres {
				t.Errorf("sessionStore() usePostgres = %v, want %v", usePostgres, tst.wantPostgres)
			}
		})
	}
}

// memoryFallbackWarn is the warning newSessionManager emits when it leaves scs's
// default in-memory store in place instead of wiring the Postgres one.
const memoryFallbackWarn = "database unavailable; using in-memory session store (sessions are process-local)"

// setSessionConfig installs a session config snapshot in the process-wide
// config.C singleton and restores the previous value when the test ends.
// newSessionManager reads that global directly, so tests using this helper must
// not run in parallel.
func setSessionConfig(t *testing.T, lifetime time.Duration, cookieSecure bool) {
	t.Helper()

	prev := config.C

	var cfg config.Config

	cfg.Auth.Session = config.SessionConfig{Lifetime: lifetime, CookieSecure: cookieSecure}
	config.C = &cfg

	t.Cleanup(func() { config.C = prev })
}

// observedLogger returns a logger writing into an in-memory observer, so the
// single Warn of the in-memory branch can be asserted on.
func observedLogger(t *testing.T) (*zap.Logger, *observer.ObservedLogs) {
	t.Helper()

	core, logs := observer.New(zapcore.DebugLevel)

	return zap.New(core), logs
}

// openDB hands back a *sql.DB that is never connected to anything.
// newSessionManager does no I/O, so an unusable handle is enough to exercise the
// Postgres branch.
func openDB(t *testing.T) *sql.DB {
	t.Helper()

	conn := sql.OpenDB(nil)

	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

// stopCleanup shuts down the background cleanup ticker postgresstore.New starts,
// so the Postgres cases do not leak a goroutine past the test.
func stopCleanup(t *testing.T, store scs.Store) {
	t.Helper()

	if pg, ok := store.(*postgresstore.PostgresStore); ok {
		t.Cleanup(pg.StopCleanup)
	}
}

// assertStore checks which of the two stores the constructor picked.
func assertStore(t *testing.T, store scs.Store, wantPostgres bool) {
	t.Helper()

	if store == nil {
		t.Fatal("newSessionManager() Store = nil, want a session store")
	}

	if _, gotPostgres := store.(*postgresstore.PostgresStore); gotPostgres != wantPostgres {
		t.Errorf("newSessionManager() Store = %T (postgres = %v), want postgres = %v",
			store, gotPostgres, wantPostgres)
	}
}

// assertCookiePolicy pins the cookie policy: HttpOnly and SameSite are
// hardcoded, only Secure follows the config.
func assertCookiePolicy(t *testing.T, cookie scs.SessionCookie, wantSecure bool) {
	t.Helper()

	if !cookie.HttpOnly {
		t.Error("newSessionManager() Cookie.HttpOnly = false, want true")
	}

	if cookie.Secure != wantSecure {
		t.Errorf("newSessionManager() Cookie.Secure = %v, want %v", cookie.Secure, wantSecure)
	}

	if cookie.SameSite != http.SameSiteLaxMode {
		t.Errorf("newSessionManager() Cookie.SameSite = %v, want %v (Lax)",
			cookie.SameSite, http.SameSiteLaxMode)
	}
}

// assertWarnings checks the only externally observable side effect: the single
// Warn of the in-memory fallback.
func assertWarnings(t *testing.T, logs *observer.ObservedLogs, want int) {
	t.Helper()

	warns := logs.FilterLevelExact(zapcore.WarnLevel).All()
	if len(warns) != want {
		t.Fatalf("newSessionManager() logged %d warnings, want %d", len(warns), want)
	}

	if want > 0 && warns[0].Message != memoryFallbackWarn {
		t.Errorf("newSessionManager() warning = %q, want %q", warns[0].Message, memoryFallbackWarn)
	}
}

//nolint:paralleltest // mutates the process-wide config.C singleton
func TestNewSessionManager(t *testing.T) {
	tests := []struct {
		name         string
		usePostgres  bool
		nilConn      bool
		lifetime     time.Duration
		cookieSecure bool
		wantPostgres bool
		wantWarns    int
	}{
		{
			name:         "postgres store with secure cookie",
			usePostgres:  true,
			nilConn:      false,
			lifetime:     2 * time.Hour,
			cookieSecure: true,
			wantPostgres: true,
			wantWarns:    0,
		},
		{
			name:         "postgres store with insecure cookie",
			usePostgres:  true,
			nilConn:      false,
			lifetime:     30 * time.Minute,
			cookieSecure: false,
			wantPostgres: true,
			wantWarns:    0,
		},
		{
			// No I/O at construction: even a nil handle wires up fine,
			// connection errors only surface once the store is used.
			name:         "postgres store with nil database handle",
			usePostgres:  true,
			nilConn:      true,
			lifetime:     time.Hour,
			cookieSecure: true,
			wantPostgres: true,
			wantWarns:    0,
		},
		{
			name:         "in-memory fallback warns once",
			usePostgres:  false,
			nilConn:      false,
			lifetime:     time.Hour,
			cookieSecure: false,
			wantPostgres: false,
			wantWarns:    1,
		},
		{
			name:         "in-memory fallback still honours secure cookie",
			usePostgres:  false,
			nilConn:      false,
			lifetime:     15 * time.Minute,
			cookieSecure: true,
			wantPostgres: false,
			wantWarns:    1,
		},
		{
			// The zero lifetime of an unset config is copied verbatim,
			// overwriting scs's own 24h default.
			name:         "zero lifetime overwrites the scs default",
			usePostgres:  true,
			nilConn:      false,
			lifetime:     0,
			cookieSecure: false,
			wantPostgres: true,
			wantWarns:    0,
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			setSessionConfig(t, tst.lifetime, tst.cookieSecure)

			log, logs := observedLogger(t)

			var conn *sql.DB
			if !tst.nilConn {
				conn = openDB(t)
			}

			manager := newSessionManager(log, conn, tst.usePostgres)
			if manager == nil {
				t.Fatal("newSessionManager() = nil, want a session manager")
			}

			stopCleanup(t, manager.Store)
			assertStore(t, manager.Store, tst.wantPostgres)

			if manager.Lifetime != tst.lifetime {
				t.Errorf("newSessionManager() Lifetime = %v, want %v", manager.Lifetime, tst.lifetime)
			}

			assertCookiePolicy(t, manager.Cookie, tst.cookieSecure)
			assertWarnings(t, logs, tst.wantWarns)
		})
	}
}

// TestNewSessionManagerNilLogger pins the undocumented contract that a logger is
// only required on the in-memory fallback path, since that is the only branch
// that logs.
//
//nolint:paralleltest // mutates the process-wide config.C singleton
func TestNewSessionManagerNilLogger(t *testing.T) {
	tests := []struct {
		name        string
		usePostgres bool
		wantPanic   bool
	}{
		{name: "postgres branch never touches the logger", usePostgres: true, wantPanic: false},
		{name: "fallback branch nil-derefs the logger", usePostgres: false, wantPanic: true},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			setSessionConfig(t, time.Hour, false)

			conn := openDB(t)

			var manager *scs.SessionManager

			func() {
				defer func() {
					if got := recover(); (got != nil) != tst.wantPanic {
						t.Errorf("newSessionManager(nil, conn, %v) panic = %v, want panic = %v",
							tst.usePostgres, got, tst.wantPanic)
					}
				}()

				manager = newSessionManager(nil, conn, tst.usePostgres)
				stopCleanup(t, manager.Store)
			}()

			if !tst.wantPanic && manager == nil {
				t.Errorf("newSessionManager(nil, conn, %v) = nil, want a session manager", tst.usePostgres)
			}
		})
	}
}

// TestNewSessionManagerReadsConfigPerCall shows the constructor snapshots the
// global config on every call instead of caching the first one.
//
//nolint:paralleltest // mutates the process-wide config.C singleton
func TestNewSessionManagerReadsConfigPerCall(t *testing.T) {
	log, _ := observedLogger(t)
	conn := openDB(t)

	setSessionConfig(t, time.Hour, false)

	first := newSessionManager(log, conn, true)
	stopCleanup(t, first.Store)

	setSessionConfig(t, 3*time.Hour, true)

	second := newSessionManager(log, conn, true)
	stopCleanup(t, second.Store)

	if first.Lifetime != time.Hour || first.Cookie.Secure {
		t.Errorf("first newSessionManager() = {Lifetime: %v, Secure: %v}, want {1h0m0s, false}",
			first.Lifetime, first.Cookie.Secure)
	}

	if second.Lifetime != 3*time.Hour || !second.Cookie.Secure {
		t.Errorf("second newSessionManager() = {Lifetime: %v, Secure: %v}, want {3h0m0s, true}",
			second.Lifetime, second.Cookie.Secure)
	}

	if first.Store == second.Store {
		t.Error("newSessionManager() reused one store across calls, want a fresh store per call")
	}
}

// guardShutdownSignals keeps SIGINT/SIGTERM from terminating the test binary
// for the duration of the test. runServers installs its own handler via
// signal.NotifyContext and removes it again when it returns, so without this
// guard a signal sent slightly too early or too late would kill the process.
func guardShutdownSignals(t *testing.T) {
	t.Helper()

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	t.Cleanup(func() { signal.Stop(ch) })
}

// sendShutdownSignals repeatedly sends SIGTERM to this process until the test
// finishes. Repeating is required because there is no way to observe when
// runServers has installed its signal handler; a signal delivered before that
// point is simply dropped and the call would block forever. Call
// guardShutdownSignals first — cleanups run last-registered-first, so the
// sender stops before the guard is removed.
func sendShutdownSignals(t *testing.T) {
	t.Helper()

	done := make(chan struct{})

	var wg sync.WaitGroup

	wg.Go(func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(20 * time.Millisecond):
				_ = syscall.Kill(os.Getpid(), syscall.SIGTERM)
			}
		}
	})

	t.Cleanup(func() {
		close(done)
		wg.Wait()
	})
}

// occupiedAddr returns an address that is bound for the whole test, so a
// server told to listen on it fails with "address already in use".
func occupiedAddr(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() unexpected error = %v", err)
	}

	t.Cleanup(func() { _ = ln.Close() })

	return ln.Addr().String()
}

// newTestServer builds a loopback server on an ephemeral port together with a
// channel that is closed when Shutdown is called on it, which is how the tests
// below observe that every server really was stopped.
func newTestServer(t *testing.T, addr string, h http.Handler) (*http.Server, <-chan struct{}) {
	t.Helper()

	if h == nil {
		h = http.NotFoundHandler()
	}

	srv := &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: time.Second,
	}

	stopped := make(chan struct{})

	var once sync.Once

	srv.RegisterOnShutdown(func() { once.Do(func() { close(stopped) }) })

	return srv, stopped
}

func TestRunServersReturnsListenError(t *testing.T) {
	t.Parallel()

	type serverSpec struct {
		name string
		bad  bool
	}

	tests := []struct {
		name         string
		specs        []serverSpec
		wantContains string
		wantMissing  string
	}{
		{
			name:         "single server that cannot bind",
			specs:        []serverSpec{{name: "api", bad: true}},
			wantContains: "running servers: api server:",
			wantMissing:  "",
		},
		{
			name:         "healthy server is stopped and only the failing one is named",
			specs:        []serverSpec{{name: "api", bad: false}, {name: "metrics", bad: true}},
			wantContains: "running servers: metrics server:",
			wantMissing:  "api server:",
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			servers := make([]namedServer, 0, len(tst.specs))
			stopped := make([]<-chan struct{}, 0, len(tst.specs))

			for _, spec := range tst.specs {
				addr := "127.0.0.1:0"
				if spec.bad {
					addr = occupiedAddr(t)
				}

				srv, ch := newTestServer(t, addr, nil)
				servers = append(servers, namedServer{name: spec.name, srv: srv})
				stopped = append(stopped, ch)
			}

			errCh := make(chan error, 1)
			go func() { errCh <- runServers(zap.NewNop(), servers) }()

			// No signal is sent: a listen failure must make runServers
			// return on its own.
			var err error

			select {
			case err = <-errCh:
			case <-time.After(30 * time.Second):
				t.Fatal("runServers() did not return after a listen failure")
			}

			if err == nil {
				t.Fatalf("runServers() error = nil, want error containing %q", tst.wantContains)
			}

			if !strings.Contains(err.Error(), tst.wantContains) {
				t.Errorf("runServers() error = %q, want it to contain %q", err.Error(), tst.wantContains)
			}

			if tst.wantMissing != "" && strings.Contains(err.Error(), tst.wantMissing) {
				t.Errorf("runServers() error = %q, want it to not mention %q", err.Error(), tst.wantMissing)
			}

			if _, ok := errors.AsType[*net.OpError](err); !ok {
				t.Errorf("runServers() error = %v, want the underlying *net.OpError to stay unwrappable", err)
			}

			for i, ch := range stopped {
				select {
				case <-ch:
				case <-time.After(10 * time.Second):
					t.Errorf("server %q was not shut down after the group failed", tst.specs[i].name)
				}
			}
		})
	}
}

// TestRunServersShutsDownOnSignal pins the normal path: runServers blocks
// until SIGTERM, then stops every server and returns nil. It must not be
// parallel — it signals the whole test process.
//
//nolint:paralleltest // signals the whole test process; see guardShutdownSignals
func TestRunServersShutsDownOnSignal(t *testing.T) {
	tests := []struct {
		name  string
		names []string
	}{
		{
			name:  "empty server slice still blocks until signalled",
			names: nil,
		},
		{
			name:  "single server",
			names: []string{"api"},
		},
		{
			name:  "api and metrics servers are both stopped",
			names: []string{"api", "metrics"},
		},
	}

	//nolint:paralleltest // each case sends a real SIGTERM to the test process via guardShutdownSignals
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			guardShutdownSignals(t)

			servers := make([]namedServer, 0, len(tst.names))
			stopped := make([]<-chan struct{}, 0, len(tst.names))

			for _, name := range tst.names {
				srv, ch := newTestServer(t, "127.0.0.1:0", nil)
				servers = append(servers, namedServer{name: name, srv: srv})
				stopped = append(stopped, ch)
			}

			errCh := make(chan error, 1)
			go func() { errCh <- runServers(zap.NewNop(), servers) }()

			select {
			case err := <-errCh:
				t.Fatalf("runServers() returned (error = %v) before any shutdown signal", err)
			case <-time.After(100 * time.Millisecond):
			}

			sendShutdownSignals(t)

			select {
			case err := <-errCh:
				if err != nil {
					t.Fatalf("runServers() error = %v, want nil", err)
				}
			case <-time.After(30 * time.Second):
				t.Fatal("runServers() did not return after SIGTERM")
			}

			for i, ch := range stopped {
				select {
				case <-ch:
				case <-time.After(10 * time.Second):
					t.Errorf("server %q was not shut down after SIGTERM", tst.names[i])
				}
			}
		})
	}
}

// TestRunServersCompletesInFlightRequestOnSignal checks the graceful part of
// the graceful shutdown: a request already being served finishes, while the
// listener stops accepting new connections.
//
//nolint:paralleltest // signals the whole test process; see guardShutdownSignals
func TestRunServersCompletesInFlightRequestOnSignal(t *testing.T) {
	guardShutdownSignals(t)

	entered := make(chan struct{})
	addrCh := make(chan string, 1)

	var once sync.Once

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(entered) })
		time.Sleep(300 * time.Millisecond)

		_, _ = io.WriteString(w, "done")
	})

	srv, _ := newTestServer(t, "127.0.0.1:0", handler)
	// BaseContext receives the real listener, which is the only way to learn
	// the ephemeral port the server actually bound.
	srv.BaseContext = func(l net.Listener) context.Context {
		select {
		case addrCh <- l.Addr().String():
		default:
		}

		return context.Background()
	}

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(zap.NewNop(), []namedServer{{name: "api", srv: srv}}) }()

	var addr string

	select {
	case addr = <-addrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("server never started listening")
	}

	bodyCh := make(chan string, 1)
	reqErrCh := make(chan error, 1)

	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			reqErrCh <- err

			return
		}
		defer func() { _ = resp.Body.Close() }()

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			reqErrCh <- err

			return
		}

		bodyCh <- string(body)
	}()

	select {
	case <-entered:
	case err := <-reqErrCh:
		t.Fatalf("request failed before the handler ran: %v", err)
	case <-time.After(10 * time.Second):
		t.Fatal("handler was never reached")
	}

	sendShutdownSignals(t)

	select {
	case body := <-bodyCh:
		if body != "done" {
			t.Errorf("in-flight response body = %q, want %q", body, "done")
		}
	case err := <-reqErrCh:
		t.Fatalf("in-flight request failed during graceful shutdown: %v", err)
	case <-time.After(20 * time.Second):
		t.Fatal("in-flight request never completed")
	}

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServers() error = %v, want nil", err)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("runServers() did not return after SIGTERM")
	}

	dialCtx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()

	conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)
	if err == nil {
		_ = conn.Close()

		t.Errorf("server at %s still accepts connections after shutdown", addr)
	}
}

// TestRunServersIgnoresShutdownTimeout pins the deliberate contract point that
// a failing or timing-out Shutdown is only logged, never returned: the handler
// below outlives the 10s shutdown deadline, yet runServers still returns nil.
//
//nolint:paralleltest // signals the whole test process; see guardShutdownSignals
func TestRunServersIgnoresShutdownTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("waits out the 10s shutdown deadline")
	}

	guardShutdownSignals(t)

	release := make(chan struct{})

	t.Cleanup(func() { close(release) })

	entered := make(chan struct{})
	addrCh := make(chan string, 1)

	var once sync.Once

	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		once.Do(func() { close(entered) })

		select {
		case <-release:
		case <-time.After(60 * time.Second):
		}

		_, _ = io.WriteString(w, "late")
	})

	srv, _ := newTestServer(t, "127.0.0.1:0", handler)
	srv.BaseContext = func(l net.Listener) context.Context {
		select {
		case addrCh <- l.Addr().String():
		default:
		}

		return context.Background()
	}

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(zap.NewNop(), []namedServer{{name: "api", srv: srv}}) }()

	var addr string

	select {
	case addr = <-addrCh:
	case <-time.After(10 * time.Second):
		t.Fatal("server never started listening")
	}

	go func() {
		resp, err := http.Get("http://" + addr + "/")
		if err != nil {
			return
		}

		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()

	select {
	case <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("handler was never reached")
	}

	sendShutdownSignals(t)

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("runServers() error = %v, want nil even when Shutdown times out", err)
		}
	case <-time.After(45 * time.Second):
		t.Fatal("runServers() did not return after the shutdown deadline expired")
	}
}

// freeAddr reserves a loopback port, releases it again and returns the
// address, so a server can bind it successfully.
func freeAddr(t *testing.T) string {
	t.Helper()

	ln, err := (&net.ListenConfig{}).Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("net.Listen() unexpected error = %v", err)
	}

	addr := ln.Addr().String()

	if err := ln.Close(); err != nil {
		t.Fatalf("listener.Close() unexpected error = %v", err)
	}

	return addr
}

// waitListening blocks until something accepts connections on addr.
func waitListening(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		dialCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)

		cancel()

		if err == nil {
			_ = conn.Close()

			return
		}

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("nothing listening on %s, want a running server", addr)
}

// waitClosed blocks until nothing accepts connections on addr any more.
func waitClosed(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		dialCtx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
		conn, err := (&net.Dialer{}).DialContext(dialCtx, "tcp", addr)

		cancel()

		if err != nil {
			return
		}

		_ = conn.Close()

		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("%s still accepts connections, want it shut down", addr)
}

// signalUntilDone sends SIGTERM to the test process until runServers reports
// back on errCh, so the test does not depend on when signal.NotifyContext is
// installed inside the function.
func signalUntilDone(t *testing.T, errCh <-chan error) error {
	t.Helper()

	deadline := time.After(30 * time.Second)

	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()

	for {
		if err := syscall.Kill(os.Getpid(), syscall.SIGTERM); err != nil {
			t.Fatalf("syscall.Kill() unexpected error = %v", err)
		}

		select {
		case err := <-errCh:
			return err
		case <-ticker.C:
		case <-deadline:
			t.Fatal("runServers() did not return after SIGTERM")
		}
	}
}

// newSimpleServer builds a minimal http.Server bound to addr, for tests that
// only care about listen/shutdown behaviour and not about request handling.
// Distinct from newTestServer above, which also wires up a shutdown-observed
// channel.
func newSimpleServer(addr string) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           http.NewServeMux(),
		ReadHeaderTimeout: time.Second,
	}
}

func TestRunServersListenError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// servers builds the input and the addresses that must be shut down
		// by the time runServers returns.
		servers func(t *testing.T) ([]namedServer, []string)
		// wantNames are the acceptable "<name> server:" labels in the error.
		wantNames []string
	}{
		{
			name: "single server fails to start",
			servers: func(t *testing.T) ([]namedServer, []string) {
				t.Helper()

				return []namedServer{
					{name: "api", srv: newSimpleServer(occupiedAddr(t))},
				}, nil
			},
			wantNames: []string{"api"},
		},
		{
			name: "failing server shuts down the healthy one",
			servers: func(t *testing.T) ([]namedServer, []string) {
				t.Helper()

				healthy := freeAddr(t)

				return []namedServer{
					{name: "api", srv: newSimpleServer(healthy)},
					{name: "metrics", srv: newSimpleServer(occupiedAddr(t))},
				}, []string{healthy}
			},
			wantNames: []string{"metrics"},
		},
		{
			name: "duplicate addr makes one server fail",
			servers: func(t *testing.T) ([]namedServer, []string) {
				t.Helper()

				addr := freeAddr(t)

				return []namedServer{
					{name: "api", srv: newSimpleServer(addr)},
					{name: "metrics", srv: newSimpleServer(addr)},
				}, []string{addr}
			},
			wantNames: []string{"api", "metrics"},
		},
	}

	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			t.Parallel()

			servers, wantClosed := tst.servers(t)

			err := runServers(zap.NewNop(), servers)
			if err == nil {
				t.Fatal("runServers() error = nil, want a listen error")
			}

			const prefix = "running servers: "

			matched := false

			for _, name := range tst.wantNames {
				if strings.HasPrefix(err.Error(), prefix+name+" server: ") {
					matched = true
				}
			}

			if !matched {
				t.Errorf("runServers() error = %q, want prefix %q with one of the names %v",
					err.Error(), prefix, tst.wantNames)
			}

			if _, ok := errors.AsType[*net.OpError](err); !ok {
				t.Errorf("errors.AsType[*net.OpError](%v) = false, want the underlying net error to stay reachable", err)
			}

			if errors.Is(err, http.ErrServerClosed) {
				t.Errorf("errors.Is(%v, http.ErrServerClosed) = true, want a real listen failure", err)
			}

			for _, addr := range wantClosed {
				waitClosed(t, addr)
			}
		})
	}
}

// TestRunServersShutsDownBothServersOnSignal pins the normal signal-driven
// shutdown path using address dial/close checks, complementing
// TestRunServersShutsDownOnSignal above which asserts via shutdown-hook
// channels instead.
//
//nolint:paralleltest // sends a real SIGTERM to the test process via guardShutdownSignals
func TestRunServersShutsDownBothServersOnSignal(t *testing.T) {
	guardShutdownSignals(t)

	apiAddr := freeAddr(t)
	metricsAddr := freeAddr(t)

	servers := []namedServer{
		{name: "api", srv: newSimpleServer(apiAddr)},
		{name: "metrics", srv: newSimpleServer(metricsAddr)},
	}

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(zap.NewNop(), servers) }()

	waitListening(t, apiAddr)
	waitListening(t, metricsAddr)

	// http.ErrServerClosed from the Shutdown of a healthy server counts as
	// success, so the signal path must return nil.
	if err := signalUntilDone(t, errCh); err != nil {
		t.Fatalf("runServers() error = %v, want nil after SIGTERM", err)
	}

	waitClosed(t, apiAddr)
	waitClosed(t, metricsAddr)
}

//nolint:paralleltest // sends a real SIGTERM to the test process via guardShutdownSignals
func TestRunServersEmptyServersBlocksUntilSignal(t *testing.T) {
	guardShutdownSignals(t)

	errCh := make(chan error, 1)
	go func() { errCh <- runServers(zap.NewNop(), nil) }()

	select {
	case err := <-errCh:
		t.Fatalf("runServers() returned early with error = %v, want it to block until a signal arrives", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := signalUntilDone(t, errCh); err != nil {
		t.Fatalf("runServers() error = %v, want nil after SIGTERM", err)
	}
}
