// SPDX-License-Identifier: TODO

package cmd

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/alexedwards/scs/postgresstore"
	"github.com/alexedwards/scs/v2"
	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
	"github.com/m4schini/splitkauf/telemetry"
	"github.com/m4schini/splitkauf/telemetry/metrics"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"
)

const shutdownTimeout = 10 * time.Second

var serveCmd = &cobra.Command{
	Use:   "serve",
	Short: "Start the HTTP API server",
	RunE: func(_ *cobra.Command, _ []string) error {
		return serve()
	},
}

func init() {
	rootCmd.AddCommand(serveCmd)
}

// namedServer pairs an http.Server with a label for logs.
type namedServer struct {
	name string
	srv  *http.Server
}

// sessionStore is the pure decision behind the session-store choice: given
// whether OIDC is configured and whether the database was reachable at
// startup, it decides whether to use the Postgres-backed store.
//
//   - (oidcEnabled=true,  dbReachable=true)  -> postgres (usePostgres=true, nil)
//   - (oidcEnabled=true,  dbReachable=false) -> fatal: sessions must be
//     durable in OIDC mode (amnesiac sessions would silently break login
//     state — CSRF/PKCE state and the member's session are lost on any
//     process restart or DB blip)
//   - (oidcEnabled=false, dbReachable=true)  -> postgres (usePostgres=true, nil)
//     (dev-auth mode with the DB up still gets durable sessions, same as
//     OIDC mode)
//   - (oidcEnabled=false, dbReachable=false) -> memory fallback
//     (dev-auth mode; local development must keep serving with the DB down)
//
// Extracted as a pure function (no I/O) so the policy is unit-testable
// without exercising serve() itself.
func sessionStore(oidcEnabled, dbReachable bool) (usePostgres bool, err error) {
	if oidcEnabled && !dbReachable {
		return false, errors.New("sessions require a reachable database in OIDC mode")
	}
	return dbReachable, nil
}

// newSessionManager builds the scs session manager for the auth port. The
// store choice is made ONCE here at startup, driven by sessionStore: in OIDC
// mode a reachable database is required (see sessionStore) and startup fails
// otherwise, so this is only ever called with usePostgres=true in that mode.
// In dev-auth mode sessions persist to Postgres when the database is
// reachable, and fall back to scs's default in-memory store when it is not,
// so local development still serves; in that fallback case sessions are
// process-local and lost on restart. The session cookie is HttpOnly,
// SameSite=Lax, and Secure per config; it carries only the opaque session id.
func newSessionManager(log *zap.Logger, conn *sql.DB, usePostgres bool) *scs.SessionManager {
	sm := scs.New()
	if usePostgres {
		sm.Store = postgresstore.New(conn)
	} else {
		log.Warn("database unavailable; using in-memory session store (sessions are process-local)")
	}
	sm.Lifetime = config.C.Auth.Session.Lifetime
	sm.Cookie.HttpOnly = true
	sm.Cookie.Secure = config.C.Auth.Session.CookieSecure
	sm.Cookie.SameSite = http.SameSiteLaxMode
	return sm
}

func serve() error {
	log := telemetry.Logger("server")

	metrics.SetBuildInfo(config.C.App.Version, config.C.App.Environment)

	// Open the database handle before starting the server. A ping failure is
	// only a warning: the server still starts (health reports degraded) so it
	// does not crash-loop while the DB is briefly unavailable. NewSQL returns
	// the opened handle even on ping error, so it can recover once the DB is up.
	conn, dbErr := db.NewSQL(config.C.Database.DSN())
	if dbErr != nil {
		log.Warn("database not reachable at startup; serving with degraded health", zap.Error(dbErr))
	}

	// The lists service persists to Postgres via the db adapter. The handle may
	// be temporarily unreachable (health reports degraded); requests error until
	// it recovers.
	service := lists.NewService(db.NewListsRepository(conn))

	// Decide the session store before doing anything else network-facing: in
	// OIDC mode a reachable database is required for durable sessions, so this
	// gate must run BEFORE OIDC discovery below — an unreachable DB must fail
	// fast without ever contacting the issuer. In dev-auth mode the in-memory
	// fallback keeps local development serving with the DB down.
	usePostgres, err := sessionStore(config.C.IsOIDCEnabled(), dbErr == nil)
	if err != nil {
		log.Error("cannot start in OIDC mode", zap.Error(err))
		return err
	}

	// Session manager for the auth port: sessions persist to Postgres when the
	// DB is reachable, and fall back to an in-memory store when it is not, so
	// dev/degraded still serves. The cookie carries only an opaque session id.
	sm := newSessionManager(log, conn, usePostgres)

	// The authenticator runs the OIDC BFF flow when configured, else dev-auth.
	// OIDC construction discovers the provider over the network, so bound it with
	// a timeout: a hung or unreachable issuer must not block startup before the
	// HTTP server binds. On timeout auth.New returns the context error and serve
	// fails fast with a clear message.
	membersRepo := db.NewMemberRepository(conn)
	usersRepo := db.NewUserRepository(conn)
	discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancelDiscovery()
	authr, err := auth.New(discoveryCtx, config.C, sm, membersRepo, usersRepo)
	if err != nil {
		return fmt.Errorf("building authenticator: %w", err)
	}

	// In dev-auth mode the fixed dev user never signs in, so upsert it once at
	// startup so it exists in the members table like any OIDC account. Best
	// effort: a DB failure here must not stop the server from serving.
	if !config.C.IsOIDCEnabled() && conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := membersRepo.Upsert(ctx, auth.DevMember()); err != nil {
			log.Warn("could not upsert dev member at startup", zap.Error(err))
		}
		cancel()
	}

	// The event broker fans real-time reload hints from the mutating REST
	// handlers out to every connected SSE stream. It is the same instance the
	// handlers publish to (via v1.V1.Events) and the SSE endpoint subscribes to
	// (via rest.New).
	broker := events.NewBroker()

	servers := []namedServer{{
		name: "api",
		srv: &http.Server{
			Addr:              net.JoinHostPort(config.C.Server.Host, strconv.Itoa(config.C.Server.Port)),
			Handler:           rest.New(&v1.V1{DB: conn, Service: service, Events: broker}, sm, authr, broker),
			ReadHeaderTimeout: 5 * time.Second,
		},
	}}

	if config.C.Metrics.Enabled {
		servers = append(servers, namedServer{
			name: "metrics",
			srv:  metrics.NewServer(config.C.Metrics.Host, config.C.Metrics.Port, config.C.Metrics.Path),
		})
	}

	return runServers(log, servers)
}

// runServers starts every server concurrently and blocks until either one
// fails or a shutdown signal arrives, then gracefully stops them all.
func runServers(log *zap.Logger, servers []namedServer) error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	g, gctx := errgroup.WithContext(ctx)

	for _, s := range servers {
		g.Go(func() error {
			log.Info("starting server", zap.String("name", s.name), zap.String("addr", s.srv.Addr))
			if err := s.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("%s server: %w", s.name, err)
			}
			return nil
		})
	}

	g.Go(func() error {
		<-gctx.Done()
		log.Info("shutdown signal received, stopping servers")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		for _, s := range servers {
			if err := s.srv.Shutdown(shutdownCtx); err != nil {
				log.Warn("server shutdown error", zap.String("name", s.name), zap.Error(err))
			}
		}
		return nil
	})

	return g.Wait()
}
