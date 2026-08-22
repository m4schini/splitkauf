// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"time"

	"github.com/spf13/cobra"
)

// userCmdTimeout bounds the database work of the operator user commands
// ("user add", "user ls", "user merge").
const userCmdTimeout = 10 * time.Second

// newUserCmd builds the "user" command that groups the operator commands for
// managing user identities: "user add", "user ls", "user merge". It only
// prints help; the behaviour lives in the subcommands.
func newUserCmd() *cobra.Command {
	userCmd := new(cobra.Command)
	userCmd.Use = "user"
	userCmd.Short = "Manage user identities (add, ls, merge)"
	userCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		return cmd.Help()
	}

	userCmd.AddCommand(newUseraddCmd(), newUserlsCmd(), newUsermergeCmd())

	return userCmd
}
