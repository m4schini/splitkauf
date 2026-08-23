// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"context"
	"errors"
	"net"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/database"
)

const (
	// migrateConnectPrefix is the wrap runMigrate puts on every db.NewSQL failure.
	migrateConnectPrefix = "connecting to database: "
	// migrateTestDBIdent is the throwaway role/database name used in the test DSNs.
	migrateTestDBIdent = "splitkauf"
)

// migrateLaterStagePrefixes are the wraps runMigrate can only produce once it holds a
// connection; none of them may appear when connecting already failed.
var migrateLaterStagePrefixes = []string{
	"destroying database",
	"forcing migration state",
	"applying migrations",
}

// useMigrateConfig points the process-wide config.C singleton at cfg for the duration
// of the test and restores the previous value afterwards. Tests using it must
// not call t.Parallel(): config.C is shared mutable global state.
func useMigrateConfig(t *testing.T, cfg *config.Config) {
	t.Helper()

	prev := config.C
	config.C = cfg

	t.Cleanup(func() { config.C = prev })
}

// migrateClosedTCPPort returns a loopback port that nothing is listening on, so a
// connection attempt is refused immediately instead of waiting out the 5s ping
// timeout.
func migrateClosedTCPPort(t *testing.T) int {
	t.Helper()

	var listenCfg net.ListenConfig

	listener, err := listenCfg.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("listener address is %T, want *net.TCPAddr", listener.Addr())
	}

	port := addr.Port

	if err := listener.Close(); err != nil {
		t.Fatalf("closing probe listener: %v", err)
	}

	return port
}

// migrateDBConfig builds a config.Config whose DSN targets the given loopback port
// with the given sslmode.
func migrateDBConfig(port int, sslMode string) *config.Config {
	var cfg config.Config

	cfg.Database = config.DatabaseConfig{
		Host:     "127.0.0.1",
		Port:     port,
		User:     migrateTestDBIdent,
		Password: migrateTestDBIdent,
		Name:     migrateTestDBIdent,
		SSLMode:  sslMode,
	}

	return &cfg
}

// TestRunMigrateConnectFailure pins the one branch of runMigrate that is
// reachable without a live PostgreSQL: every flag combination must fail with
// the "connecting to database" wrap before any migration work happens. Both
// failing DSN shapes are covered — a refused connection and a DSN the driver
// rejects — and in both db.NewSQL hands back a non-nil *sql.DB alongside the
// error, which runMigrate must still treat as fatal.
//
//nolint:paralleltest // config.C is a process-wide global; these cases must not run in parallel
func TestRunMigrateConnectFailure(t *testing.T) {
	refusedPort := migrateClosedTCPPort(t)

	tests := []struct {
		name            string
		cfg             *config.Config
		schemaVersion   uint
		forceVersion    int
		destroyDatabase bool
	}{
		{
			name:            "latest schema without force",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   database.LatestSchema,
			forceVersion:    database.DoNotOverrideVersion,
			destroyDatabase: false,
		},
		{
			name:            "pinned schema version",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   3,
			forceVersion:    database.DoNotOverrideVersion,
			destroyDatabase: false,
		},
		{
			name:            "force to version zero",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   database.LatestSchema,
			forceVersion:    0,
			destroyDatabase: false,
		},
		{
			name:            "force to positive version",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   database.LatestSchema,
			forceVersion:    7,
			destroyDatabase: false,
		},
		{
			name:            "destroy database",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   database.LatestSchema,
			forceVersion:    database.DoNotOverrideVersion,
			destroyDatabase: true,
		},
		{
			name:            "destroy database with force and pinned version",
			cfg:             migrateDBConfig(refusedPort, "disable"),
			schemaVersion:   5,
			forceVersion:    2,
			destroyDatabase: true,
		},
		{
			name:            "dsn the driver rejects",
			cfg:             migrateDBConfig(refusedPort, "not-a-real-sslmode"),
			schemaVersion:   database.LatestSchema,
			forceVersion:    database.DoNotOverrideVersion,
			destroyDatabase: false,
		},
	}
	//nolint:paralleltest // see the note on the enclosing test
	for _, tst := range tests {
		t.Run(tst.name, func(t *testing.T) {
			useMigrateConfig(t, tst.cfg)

			err := runMigrate(tst.schemaVersion, tst.forceVersion, tst.destroyDatabase)
			if err == nil {
				t.Fatalf("runMigrate(%d, %d, %t) = nil, want error",
					tst.schemaVersion, tst.forceVersion, tst.destroyDatabase)
			}

			if !strings.HasPrefix(err.Error(), migrateConnectPrefix) {
				t.Errorf("runMigrate(%d, %d, %t) = %q, want prefix %q",
					tst.schemaVersion, tst.forceVersion, tst.destroyDatabase, err, migrateConnectPrefix)
			}

			for _, prefix := range migrateLaterStagePrefixes {
				if strings.Contains(err.Error(), prefix) {
					t.Errorf("runMigrate(%d, %d, %t) = %q, must not reach the %q stage",
						tst.schemaVersion, tst.forceVersion, tst.destroyDatabase, err, prefix)
				}
			}
		})
	}
}

// TestRunMigrateConnectErrorIsWrapped complements TestRunMigrateConnectFailure by
// pinning the error's identity rather than its text: db.NewSQL's failure must be
// joined with %w so a caller can unwrap the driver error, and the connect failure
// must never satisfy errors.Is(err, database.ErrDirtySchema) — that sentinel only
// travels through the later "destroying database"/"applying migrations" wraps, and
// callers branch on it to decide whether a force/repair is needed.
//
//nolint:paralleltest // config.C is a process-wide global; see useMigrateConfig
func TestRunMigrateConnectErrorIsWrapped(t *testing.T) {
	useMigrateConfig(t, migrateDBConfig(migrateClosedTCPPort(t), "disable"))

	err := runMigrate(database.LatestSchema, database.DoNotOverrideVersion, false)
	if err == nil {
		t.Fatal("runMigrate(LatestSchema, DoNotOverrideVersion, false) = nil, want error")
	}

	if cause := errors.Unwrap(err); cause == nil {
		t.Errorf("errors.Unwrap(%q) = nil, want the wrapped db.NewSQL error", err)
	}

	if errors.Is(err, database.ErrDirtySchema) {
		t.Errorf("runMigrate(LatestSchema, DoNotOverrideVersion, false) = %q, "+
			"must not report a connect failure as a dirty schema", err)
	}
}
