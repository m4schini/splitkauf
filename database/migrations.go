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
var fs embed.FS

const (
	// LatestSchema (0) migrates to the newest embedded version.
	LatestSchema = uint(0)
	// DoNotOverrideVersion sentinel disables the --force override.
	DoNotOverrideVersion = int(-1)
)

// OverrideDirty unconditionally sets the migration state to the given version.
// This recovers a database left dirty by a previously crashed migration.
func OverrideDirty(db *sql.DB, version int) error {
	log := telemetry.Logger("database", "migrate")

	m, err := newMigrator(db)
	if err != nil {
		return err
	}

	v, dirty, err := m.Version()
	if err != nil {
		return err
	}

	log.Warn("OVERRIDING MIGRATION VERSION",
		zap.Int("force_version", version),
		zap.Uint("current_version", v),
		zap.Bool("current_dirty", dirty),
	)

	if err := m.Force(version); err != nil {
		log.Error("overriding migration version failed", zap.Error(err),
			zap.Int("force_version", version),
			zap.Uint("current_version", v),
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

	m, err := newMigrator(db)
	if err != nil {
		return err
	}

	v, dirty, err := m.Version()
	if err != nil && !errors.Is(err, migrate.ErrNilVersion) {
		return err
	}

	if dirty {
		return fmt.Errorf("database is dirty at version %d; re-run with --force <version> first", v)
	}

	err = m.Down()
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

	m, err := newMigrator(db)
	if err != nil {
		return false, err
	}

	v, dirty, err := m.Version()
	log.Info("database schema version", zap.Uint("version", v), zap.Bool("dirty", dirty), zap.Error(err))

	if dirty {
		dirtyErr := fmt.Errorf("database is dirty at version %d; re-run with --force <version> to set the state, "+
			"or fix manually with: UPDATE schema_migrations SET dirty = false WHERE version = %d", v, v)
		log.Error("migration state is dirty", zap.Error(dirtyErr),
			zap.Uint("schema_version", v),
			zap.Bool("dirty", true),
		)

		return false, dirtyErr
	}

	if version == 0 {
		err = m.Up()
	} else {
		err = m.Migrate(version)
	}

	if errors.Is(err, migrate.ErrNoChange) {
		return false, nil
	}

	if err != nil {
		log.Error("migration sql failed", zap.Error(err),
			zap.Uint("target_version", version),
			zap.Uint("schema_version", v),
		)

		return false, fmt.Errorf("applying migrations: %w", err)
	}

	v, dirty, err = m.Version()
	log.Info("migrated database schema", zap.Uint("version", v), zap.Bool("dirty", dirty), zap.Error(err))

	return true, nil
}

func newMigrator(db *sql.DB) (*migrate.Migrate, error) {
	migrationFs, err := iofs.New(fs, "migrations")
	if err != nil {
		return nil, err
	}
	defer migrationFs.Close()

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return nil, err
	}

	return migrate.NewWithInstance(
		"iofs", migrationFs,
		"postgres", driver)
}
