// SPDX-License-Identifier: TODO

package cmd

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/m4schini/splitkauf/config"
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

func serve() error {
	log := telemetry.Logger("server")

	metrics.SetBuildInfo(config.C.App.Version, config.C.App.Environment)

	servers := []namedServer{{
		name: "api",
		srv: &http.Server{
			Addr:              net.JoinHostPort(config.C.Server.Host, strconv.Itoa(config.C.Server.Port)),
			Handler:           rest.New(&v1.V1{}),
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
