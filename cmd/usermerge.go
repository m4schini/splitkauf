// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"bufio"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

var usermergeYes bool

var usermergeCmd = &cobra.Command{
	Use:   "merge <source> <target>",
	Short: "Merge one user identity into another",
	Long: `Merges the source identity into the target: every attribution row
(lists.created_by, items.added_by, items.bought_by) is rewritten from the
source's user id to the target's, then the source identity is cleaned up (its
members row is deleted; a local source's account is deleted too).

Selectors: local:<username>, oidc:<subject>, uuid:<user_id>. Use "user ls" to
discover the values. An oidc: identity must have logged in at least once;
uuid: bypasses that check.

The primary use case is migrating a local-only account to an OIDC account
after an identity provider is introduced: the person logs in once via OIDC,
then "user merge local:<name> oidc:<subject>" moves their history over.

The merge prints its plan and asks for confirmation; --yes skips the prompt
(required on a non-interactive stdin). Live sessions of the source identity
are not invalidated; they last until session expiry.`,
	Args: cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		conn, err := db.NewSQL(config.C.Database.DSN())
		if err != nil {
			return fmt.Errorf("connecting to database: %w", err)
		}
		defer func() { _ = conn.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		identities := db.NewIdentityRepository(conn)

		source, err := resolveSelector(ctx, conn, args[0])
		if err != nil {
			return err
		}

		target, err := resolveSelector(ctx, conn, args[1])
		if err != nil {
			return err
		}

		if source.UserID == target.UserID {
			return fmt.Errorf("source and target resolve to the same user id %s", source.UserID)
		}

		listCount, added, bought, err := identities.CountAttribution(ctx, source.UserID)
		if err != nil {
			return err
		}

		out := cmd.OutOrStdout()
		fmt.Fprintln(out, "Merge plan:")
		fmt.Fprintf(out, "  source: %s (user_id %s, %q, %s)\n", args[0], source.UserID, source.Name, orDash(source.Email))
		fmt.Fprintf(out, "  target: %s (user_id %s, %q, %s)\n", args[1], target.UserID, target.Name, orDash(target.Email))
		fmt.Fprintf(out, "  lists.created_by: %d rows\n", listCount)
		fmt.Fprintf(out, "  items.added_by:   %d rows\n", added)
		fmt.Fprintf(out, "  items.bought_by:  %d rows\n", bought)

		for _, line := range cleanupLines(source, target) {
			fmt.Fprintf(out, "  %s\n", line)
		}

		if source.Kind != db.IdentityKindLocal {
			fmt.Fprintln(cmd.ErrOrStderr(), "warning: the source identity can still log in via the IdP; its next OIDC login re-derives the same user id and recreates the member row")
		}

		ok, err := confirmMerge(usermergeYes, cmd.InOrStdin(), out)
		if err != nil {
			return err
		}

		if !ok {
			fmt.Fprintln(out, "Aborted; nothing was changed.")

			return nil
		}

		result, err := identities.Merge(ctx, source, target)
		if err != nil {
			return err
		}

		fmt.Fprintf(out, "Merged %s into %s (lists: %d, added: %d, bought: %d).\n",
			args[0], args[1], result.Lists, result.Added, result.Bought)

		return nil
	},
}

// parseSelector splits a "user merge" selector into its kind prefix and value.
// Valid kinds are local, oidc and uuid; anything else (including a missing
// prefix or empty value) is an error naming the bad selector.
func parseSelector(s string) (kind, value string, err error) {
	kind, value, found := strings.Cut(s, ":")
	if !found || value == "" {
		return "", "", fmt.Errorf("invalid selector %q: want local:<username>, oidc:<subject>, or uuid:<user_id>", s)
	}

	switch kind {
	case "local", "oidc", "uuid":
		return kind, value, nil
	default:
		return "", "", fmt.Errorf("invalid selector %q: unknown prefix %q (want local, oidc, or uuid)", s, kind)
	}
}

// resolveSelector turns one selector into a classified Identity. local:
// requires an existing account; oidc: requires an existing members row, which
// proves the account has logged in at least once (guarding against a typoed
// subject merging into a void UUID); uuid: accepts any well-formed UUID and
// classifies it by what the database holds.
func resolveSelector(ctx context.Context, conn *sql.DB, selector string) (db.Identity, error) {
	kind, value, err := parseSelector(selector)
	if err != nil {
		return db.Identity{}, err
	}

	switch kind {
	case "local":
		u, _, err := db.NewUserRepository(conn).GetByUsername(ctx, value)
		if errors.Is(err, users.ErrNotFound) {
			return db.Identity{}, fmt.Errorf("selector %q: no local account with username %q", selector, value)
		}

		if err != nil {
			return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
		}

		return db.Identity{
			Kind:       db.IdentityKindLocal,
			Identifier: u.Username,
			UserID:     u.ID,
			Name:       u.Name,
			Email:      u.Email,
		}, nil

	case "oidc":
		m, err := db.NewMemberRepository(conn).Get(ctx, value)
		if errors.Is(err, members.ErrNotFound) {
			return db.Identity{}, fmt.Errorf("selector %q: no member with subject %q — the account must have logged in at least once (or use uuid:<user_id>)", selector, value)
		}

		if err != nil {
			return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
		}

		identityKind := db.IdentityKindOIDC
		if m.Subject == auth.DevUser.ID.String() {
			identityKind = db.IdentityKindDev
		}

		lastLogin := m.UpdatedAt

		return db.Identity{
			Kind:       identityKind,
			Identifier: m.Subject,
			UserID:     m.UserID,
			Name:       m.Name,
			Email:      m.Email,
			LastLogin:  &lastLogin,
		}, nil

	default: // "uuid"
		id, err := uuid.Parse(value)
		if err != nil {
			return db.Identity{}, fmt.Errorf("selector %q: not a valid UUID: %w", selector, err)
		}

		ident, err := db.NewIdentityRepository(conn).ResolveUUID(ctx, id)
		if err != nil {
			return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
		}

		return ident, nil
	}
}

// cleanupLines describes the cleanup the merge will perform, for the plan
// printout.
func cleanupLines(source, target db.Identity) []string {
	var lines []string

	switch source.Kind {
	case db.IdentityKindLocal:
		lines = append(lines, fmt.Sprintf("will delete: local account %q, members row of source", source.Identifier))
	case db.IdentityKindUnknown:
		lines = append(lines, "will delete: members row of source (if any)")
	default:
		lines = append(lines, "will delete: members row of source")
	}

	if target.Kind == db.IdentityKindLocal {
		lines = append(lines, fmt.Sprintf("will seed: members row for target %q (so display names resolve)", target.Identifier))
	}

	return lines
}

// confirmMerge gates the merge: --yes bypasses the prompt; otherwise stdin
// must be a terminal (mirroring "user add"'s --password-stdin guard) and the
// operator must answer y/Y to one "Proceed? [y/N]:" prompt.
func confirmMerge(yes bool, in io.Reader, out io.Writer) (bool, error) {
	if yes {
		return true, nil
	}

	if _, ok := terminalFd(in); !ok {
		return false, errors.New("confirmation required; use --yes")
	}

	fmt.Fprint(out, "Proceed? [y/N]: ")

	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}

	answer := strings.TrimSpace(line)

	return answer == "y" || answer == "Y", nil
}

func init() {
	userCmd.AddCommand(usermergeCmd)
	usermergeCmd.Flags().BoolVar(&usermergeYes, "yes", false, "skip the confirmation prompt")
}
