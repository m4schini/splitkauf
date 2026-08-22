// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"context"
	"fmt"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/config"
)

// tablePadding is the number of padding cells between columns of the
// "user ls" table.
const tablePadding = 2

// newUserlsCmd builds the "user ls" command that lists all known user
// identities.
func newUserlsCmd() *cobra.Command {
	userlsCmd := new(cobra.Command)
	userlsCmd.Use = "ls"
	userlsCmd.Short = "List all known user identities"
	userlsCmd.Long = `Lists every identity known to the app: local username/password accounts,
OIDC members, and the dev user. IDENTIFIER is the username for local accounts
and the auth subject otherwise — it is the selector value for "user merge"
(local:<username> / oidc:<subject>). LAST_LOGIN is "never" for a local account
that has not signed in yet.`
	userlsCmd.Args = cobra.NoArgs
	userlsCmd.RunE = func(cmd *cobra.Command, _ []string) error {
		conn, err := db.NewSQL(config.C.Database.DSN())
		if err != nil {
			return fmt.Errorf("connecting to database: %w", err)
		}
		defer func() { _ = conn.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), userCmdTimeout)
		defer cancel()

		identities, err := db.NewIdentityRepository(conn).List(ctx)
		if err != nil {
			return fmt.Errorf("listing identities: %w", err)
		}

		table := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, tablePadding, ' ', 0)
		_, _ = fmt.Fprintln(table, "KIND\tIDENTIFIER\tUSER_ID\tNAME\tEMAIL\tLAST_LOGIN")

		for _, id := range identities {
			_, _ = fmt.Fprintf(table, "%s\t%s\t%s\t%s\t%s\t%s\n",
				id.Kind, id.Identifier, id.UserID,
				orDash(id.Name), orDash(id.Email), formatLastLogin(id.LastLogin))
		}

		return table.Flush()
	}

	return userlsCmd
}

// orDash substitutes an em dash for an empty value so blank columns stay
// visible in the table.
func orDash(s string) string {
	if s == "" {
		return "—"
	}

	return s
}

// formatLastLogin renders a last-login timestamp, or "never" for an account
// without one (a local account that has not signed in yet).
func formatLastLogin(t *time.Time) string {
	if t == nil {
		return "never"
	}

	return t.Format("2006-01-02 15:04")
}
