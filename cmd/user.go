// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"github.com/spf13/cobra"
)

// userCmd groups the operator commands for managing user identities:
// "user add", "user ls", "user merge". It only prints help; the behaviour
// lives in the subcommands.
var userCmd = &cobra.Command{
	Use:   "user",
	Short: "Manage user identities (add, ls, merge)",
	RunE: func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	},
}

func init() {
	rootCmd.AddCommand(userCmd)
}
