// SPDX-License-Identifier: CC0-1.0

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
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"golang.org/x/sync/errgroup"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
	"github.com/m4schini/splitkauf/telemetry"
	"github.com/m4schini/splitkauf/telemetry/metrics"
)

const (
	// shutdownTimeout bounds the graceful stop of every server after a
	// shutdown signal.
	shutdownTimeout = 10 * time.Second

	// oidcDiscoveryTimeout bounds OIDC provider discovery at startup, so a
	// hung or unreachable issuer cannot block the process from failing fast.
	oidcDiscoveryTimeout = 15 * time.Second

	// devMemberUpsertTimeout bounds the best-effort dev-member upsert at
	// startup in dev-auth mode.
	devMemberUpsertTimeout = 5 * time.Second

	// readHeaderTimeout bounds reading a request's headers, guarding the API
	// server against slowloris-style connections.
	readHeaderTimeout = 5 * time.Second
)

// errOIDCNeedsDatabase is returned by sessionStore when OIDC mode is
// configured but the database is unreachable: durable sessions are mandatory
// in that mode.
var errOIDCNeedsDatabase = errors.New("sessions require a reachable database in OIDC mode")

// newServeCmd builds the "serve" command that runs the HTTP API server.
func newServeCmd() *cobra.Command {
	serveCmd := new(cobra.Command)
	serveCmd.Use = "serve"
	serveCmd.Short = "Start the HTTP API server"
	serveCmd.RunE = func(_ *cobra.Command, _ []string) error {
		return serve()
	}

	return serveCmd
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
//   - (oidcEnabled=true,  dbReachable=true)  -> postgres (true, nil)
//   - (oidcEnabled=true,  dbReachable=false) -> fatal: sessions must be
//     durable in OIDC mode (amnesiac sessions would silently break login
//     state — CSRF/PKCE state and the member's session are lost on any
//     process restart or DB blip)
//   - (oidcEnabled=false, dbReachable=true)  -> postgres (true, nil)
//     (dev-auth mode with the DB up still gets durable sessions, same as
//     OIDC mode)
//   - (oidcEnabled=false, dbReachable=false) -> memory fallback
//     (dev-auth mode; local development must keep serving with the DB down)
//
// Extracted as a pure function (no I/O) so the policy is unit-testable
// without exercising serve() itself.
func sessionStore(oidcEnabled, dbReachable bool) (bool, error) {
	if oidcEnabled && !dbReachable {
		return false, errOIDCNeedsDatabase
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
	manager := scs.New()
	if usePostgres {
		manager.Store = postgresstore.New(conn)
	} else {
		log.Warn("database unavailable; using in-memory session store (sessions are process-local)")
	}

	manager.Lifetime = config.C.Auth.Session.Lifetime
	manager.Cookie.HttpOnly = true
	manager.Cookie.Secure = config.C.Auth.Session.CookieSecure
	manager.Cookie.SameSite = http.SameSiteLaxMode

	return manager
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
	sessionManager := newSessionManager(log, conn, usePostgres)

	// The authenticator runs the OIDC BFF flow when configured, else dev-auth.
	// OIDC construction discovers the provider over the network, so bound it with
	// a timeout: a hung or unreachable issuer must not block startup before the
	// HTTP server binds. On timeout auth.New returns the context error and serve
	// fails fast with a clear message.
	membersRepo := db.NewMemberRepository(conn)
	usersRepo := db.NewUserRepository(conn)

	discoveryCtx, cancelDiscovery := context.WithTimeout(context.Background(), oidcDiscoveryTimeout)
	defer cancelDiscovery()

	authr, err := auth.New(discoveryCtx, config.C, sessionManager, membersRepo, usersRepo)
	if err != nil {
		return fmt.Errorf("building authenticator: %w", err)
	}

	// In dev-auth mode the fixed dev user never signs in, so upsert it once at
	// startup so it exists in the members table like any OIDC account. Best
	// effort: a DB failure here must not stop the server from serving.
	if !config.C.IsOIDCEnabled() && conn != nil {
		ctx, cancel := context.WithTimeout(context.Background(), devMemberUpsertTimeout)
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

	apiServer := new(http.Server)
	apiServer.Addr = net.JoinHostPort(config.C.Server.Host, strconv.Itoa(config.C.Server.Port))
	apiServer.Handler = rest.New(&v1.V1{DB: conn, Service: service, Events: broker}, sessionManager, authr, broker)
	apiServer.ReadHeaderTimeout = readHeaderTimeout

	servers := []namedServer{{name: "api", srv: apiServer}}

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

	group, groupCtx := errgroup.WithContext(ctx)

	for _, server := range servers {
		group.Go(func() error {
			log.Info("starting server", zap.String("name", server.name), zap.String("addr", server.srv.Addr))

			if err := server.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				return fmt.Errorf("%s server: %w", server.name, err)
			}

			return nil
		})
	}

	group.Go(func() error {
		<-groupCtx.Done()
		log.Info("shutdown signal received, stopping servers")

		// groupCtx is already cancelled on this path, so detach the shutdown
		// deadline from its cancellation while keeping its values.
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(groupCtx), shutdownTimeout)
		defer cancel()

		for _, server := range servers {
			if err := server.srv.Shutdown(shutdownCtx); err != nil {
				log.Warn("server shutdown error", zap.String("name", server.name), zap.Error(err))
			}
		}

		return nil
	})

	if err := group.Wait(); err != nil {
		return fmt.Errorf("running servers: %w", err)
	}

	return nil
}
