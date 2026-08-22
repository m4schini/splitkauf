// SPDX-License-Identifier: CC0-1.0

package database

import (
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/telemetry"
)

//go:embed migrations
var migrationsFS embed.FS

const (
	// LatestSchema (0) migrates to the newest embedded version.
	LatestSchema = uint(0)
	// DoNotOverrideVersion sentinel disables the --force override.
	DoNotOverrideVersion = int(-1)
)

// ErrDirtySchema reports a database whose migration state was left dirty by a
// previously crashed migration. It is wrapped with the concrete version and
// the remedy before being returned.
var ErrDirtySchema = errors.New("database is dirty")

// OverrideDirty unconditionally sets the migration state to the given version.
// This recovers a database left dirty by a previously crashed migration.
func OverrideDirty(db *sql.DB, version int) error {
	log := telemetry.Logger("database", "migrate")

	migrator, err := newMigrator(db)
	if err != nil {
		return err
	}

	currentVersion, dirty, err := migrator.Version()
	if err != nil {
		return fmt.Errorf("reading schema version: %w", err)
	}

	log.Warn("OVERRIDING MIGRATION VERSION",
		zap.Int("force_version", version),
		zap.Uint("current_version", currentVersion),
		zap.Bool("current_dirty", dirty),
	)

	if err := migrator.Force(version); err != nil {
		log.Error("overriding migration version failed", zap.Error(err),
			zap.Int("force_version", version),
			zap.Uint("current_version", currentVersion),
		)

		return fmt.Errorf("forcing version %d: %w", version, err)
	}

	log.Warn("migration state overridden", zap.Int("version", version))

	return nil
}

// MigrateDown tears down the entire database schema.
func MigrateDown(db *sql.DB) error {
	log := telemetry.Logger("database", "migrate")
	log.Warn("destroying database schema")

	migrator, err := newMigrator(db)
	if err != nil {
		return err
	}

	currentVersion, dirty, err := migrator.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("reading schema version: %w", err)
	}

	if dirty {
		return fmt.Errorf("%w at version %d; re-run with --force <version> first",
			ErrDirtySchema, currentVersion)
	}

	err = migrator.Down()
	if errors.Is(err, migrate.ErrNoChange) {
		return nil
	}

	if err != nil {
		log.Error("migration sql failed", zap.Error(err))

		return fmt.Errorf("destroying database schema: %w", err)
	}

	log.Info("destroyed database schema")

	return nil
}

// Migrate migrates the database to the specified version. When version is 0 it
// migrates to the latest embedded version. It returns true when at least one
// migration was applied.
func Migrate(db *sql.DB, version uint) (bool, error) {
	log := telemetry.Logger("database", "migrate")
	log.Info("syncing database schema")

	migrator, err := newMigrator(db)
	if err != nil {
		return false, err
	}

	currentVersion, dirty, err := migrator.Version()
	log.Info("database schema version",
		zap.Uint("version", currentVersion), zap.Bool("dirty", dirty), zap.Error(err))

	if dirty {
		dirtyErr := fmt.Errorf("%w at version %d; re-run with --force <version> to set the state, "+
			"or fix manually with: UPDATE schema_migrations SET dirty = false WHERE version = %d",
			ErrDirtySchema, currentVersion, currentVersion)
		log.Error("migration state is dirty", zap.Error(dirtyErr),
			zap.Uint("schema_version", currentVersion),
			zap.Bool("dirty", true),
		)

		return false, dirtyErr
	}

	if version == 0 {
		err = migrator.Up()
	} else {
		err = migrator.Migrate(version)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}

	if err != nil {
		log.Error("migration sql failed", zap.Error(err),
			zap.Uint("target_version", version),
			zap.Uint("schema_version", currentVersion),
		)

		return false, fmt.Errorf("applying migrations: %w", err)
	}

	currentVersion, dirty, err = migrator.Version()
	log.Info("migrated database schema",
		zap.Uint("version", currentVersion), zap.Bool("dirty", dirty), zap.Error(err))

	return true, nil
}

func newMigrator(conn *sql.DB) (*migrate.Migrate, error) {
	sourceDriver, err := iofs.New(migrationsFS, "migrations")
	if err != nil {
		return nil, fmt.Errorf("opening embedded migrations: %w", err)
	}

	defer func() { _ = sourceDriver.Close() }()

	databaseDriver, err := postgres.WithInstance(conn, &postgres.Config{
		MigrationsTable:       "",
		MigrationsTableQuoted: false,
		MultiStatementEnabled: false,
		DatabaseName:          "",
		SchemaName:            "",
		StatementTimeout:      0,
		MultiStatementMaxSize: 0,
	})
	if err != nil {
		return nil, fmt.Errorf("initialising postgres migration driver: %w", err)
	}

	migrator, err := migrate.NewWithInstance(
		"iofs", sourceDriver,
		"postgres", databaseDriver)
	if err != nil {
		return nil, fmt.Errorf("building migrator: %w", err)
	}

	return migrator, nil
}
