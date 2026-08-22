// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/database"
	"github.com/m4schini/splitkauf/telemetry"
)

// newMigrateCmd builds the "migrate" command that applies database migrations
// and exits.
func newMigrateCmd() *cobra.Command {
	var (
		schemaVersion   uint
		forceVersion    int
		destroyDatabase bool
	)

	migrateCmd := new(cobra.Command)
	migrateCmd.Use = "migrate"
	migrateCmd.Short = "Apply all pending database migrations and exit"
	migrateCmd.Long = `Applies every pending up-migration embedded in the binary and then
exits. This is useful for running migrations in an init container or CI
pipeline, separate from the application server.

Use --force <version> to unconditionally set the migration state to the given
version before migrating. This recovers a dirty database left behind by a
previously crashed migration.`
	migrateCmd.RunE = func(_ *cobra.Command, _ []string) error {
		return runMigrate(schemaVersion, forceVersion, destroyDatabase)
	}

	migrateCmd.Flags().UintVar(&schemaVersion, "version", database.LatestSchema,
		"target schema version (default 0 = latest)")
	migrateCmd.Flags().IntVar(&forceVersion, "force", database.DoNotOverrideVersion,
		"force migration state to this version before migrating")
	migrateCmd.Flags().BoolVar(&destroyDatabase, "dangerously-destroy-database", false,
		"completely deletes database and schema")

	return migrateCmd
}

// runMigrate connects to the database and applies (or, with destroyDatabase,
// tears down) the schema, forcing the migration state first when forceVersion
// is non-negative.
func runMigrate(schemaVersion uint, forceVersion int, destroyDatabase bool) error {
	log := telemetry.Logger("migrate")

	conn, err := db.NewSQL(config.C.Database.DSN())
	if err != nil {
		log.Error("database connection failed", zap.Error(err))

		return fmt.Errorf("connecting to database: %w", err)
	}

	if destroyDatabase {
		log.Warn("Destroying database and all data contained in it")

		if err := database.MigrateDown(conn); err != nil {
			return fmt.Errorf("destroying database: %w", err)
		}

		log.Warn("Destroyed database and all data contained in it")

		return nil
	}

	if forceVersion >= 0 {
		if err := database.OverrideDirty(conn, forceVersion); err != nil {
			log.Error("migration failed", zap.Error(err))

			return fmt.Errorf("forcing migration state to %d: %w", forceVersion, err)
		}
	}

	migrationDone, err := database.Migrate(conn, schemaVersion)
	if err != nil {
		log.Error("migration failed", zap.Error(err))

		return fmt.Errorf("applying migrations: %w", err)
	}

	if migrationDone {
		log.Info("migrations applied successfully")
	} else {
		log.Info("no migration necessary")
	}

	return nil
}
