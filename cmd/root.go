// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"os"

	"github.com/spf13/cobra"

	"github.com/m4schini/splitkauf/config"
)

// newRootCmd builds the base command and wires every subcommand under it in
// one place. The root itself only prints help; runnable behaviour lives in
// subcommands (e.g. serve).
func newRootCmd() *cobra.Command {
	rootCmd := new(cobra.Command)
	rootCmd.Use = config.ServiceName
	rootCmd.Short = "Splitkauf service"
	rootCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}

	rootCmd.AddCommand(newServeCmd(), newMigrateCmd(), newUserCmd())

	return rootCmd
}

// Execute builds the command tree, loads configuration and runs the command
// named on the command line. This is called by main.main().
func Execute() {
	cobra.OnInitialize(func() {
		cobra.CheckErr(config.Load())
	})

	if err := newRootCmd().Execute(); err != nil {
		os.Exit(1)
	}
}
