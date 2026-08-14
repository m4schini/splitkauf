// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"os"

	"github.com/m4schini/splitkauf/config"
	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands.
// It only prints help; runnable behaviour lives in subcommands (e.g. serve).
var rootCmd = &cobra.Command{
	Use:   config.ServiceName,
	Short: "Splitkauf service",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(func() {
		cobra.CheckErr(config.Load())
	})
}
