// SPDX-License-Identifier: TODO

package cmd

import (
	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/database"
	"github.com/m4schini/splitkauf/telemetry"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	schemaVersion   uint
	forceVersion    int
	destroyDatabase bool
)

var migrateCmd = &cobra.Command{
	Use:   "migrate",
	Short: "Apply all pending database migrations and exit",
	Long: `Applies every pending up-migration embedded in the binary and then
exits. This is useful for running migrations in an init container or CI
pipeline, separate from the application server.

Use --force <version> to unconditionally set the migration state to the given
version before migrating. This recovers a dirty database left behind by a
previously crashed migration.`,
	RunE: func(_ *cobra.Command, _ []string) error {
		log := telemetry.Logger("migrate")

		conn, err := db.NewSQL(config.C.Database.DSN())
		if err != nil {
			log.Error("database connection failed", zap.Error(err))
			return err
		}

		if destroyDatabase {
			log.Warn("Destroying database and all data contained in it")
			err = database.MigrateDown(conn)
			if err == nil {
				log.Warn("Destroyed database and all data contained in it")
			}
			return err
		}

		if forceVersion >= 0 {
			if err := database.OverrideDirty(conn, forceVersion); err != nil {
				log.Error("migration failed", zap.Error(err))
				return err
			}
		}

		migrationDone, err := database.Migrate(conn, schemaVersion)
		if err != nil {
			log.Error("migration failed", zap.Error(err))
			return err
		}

		if migrationDone {
			log.Info("migrations applied successfully")
		} else {
			log.Info("no migration necessary")
		}

		return nil
	},
}

func init() {
	rootCmd.AddCommand(migrateCmd)
	migrateCmd.Flags().UintVar(&schemaVersion, "version", database.LatestSchema, "target schema version (default 0 = latest)")
	migrateCmd.Flags().IntVar(&forceVersion, "force", database.DoNotOverrideVersion, "force migration state to this version before migrating")
	migrateCmd.Flags().BoolVar(&destroyDatabase, "dangerously-destroy-database", false, "completely deletes database and schema")
}
