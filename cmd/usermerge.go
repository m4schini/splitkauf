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

	"github.com/google/uuid"
	"github.com/spf13/cobra"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

// Selector kind prefixes accepted by "user merge".
const (
	selectorLocal = "local"
	selectorOIDC  = "oidc"
	selectorUUID  = "uuid"
)

// mergeSelectorArgs is the number of positional arguments "user merge" takes:
// a source selector and a target selector.
const mergeSelectorArgs = 2

// Sentinel errors of "user merge", so callers can match them with errors.Is.
var (
	errInvalidSelector      = errors.New("invalid selector")
	errSameUser             = errors.New("source and target resolve to the same user id")
	errNoLocalAccount       = errors.New("no local account")
	errNoMember             = errors.New("no member")
	errConfirmationRequired = errors.New("confirmation required; use --yes")
)

// newUsermergeCmd builds the "user merge" command that merges one user
// identity into another.
func newUsermergeCmd() *cobra.Command {
	var yes bool

	usermergeCmd := new(cobra.Command)
	usermergeCmd.Use = "merge <source> <target>"
	usermergeCmd.Short = "Merge one user identity into another"
	usermergeCmd.Long = `Merges the source identity into the target: every attribution row
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
are not invalidated; they last until session expiry.`
	usermergeCmd.Args = cobra.ExactArgs(mergeSelectorArgs)
	usermergeCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runUsermerge(cmd, args, yes)
	}

	usermergeCmd.Flags().BoolVar(&yes, "yes", false, "skip the confirmation prompt")

	return usermergeCmd
}

// runUsermerge resolves both selectors, prints the merge plan, asks for
// confirmation (unless yes is set), and performs the merge.
func runUsermerge(cmd *cobra.Command, args []string, yes bool) error {
	conn, err := db.NewSQL(config.C.Database.DSN())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), userCmdTimeout)
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
		return fmt.Errorf("%w %s", errSameUser, source.UserID)
	}

	listCount, added, bought, err := identities.CountAttribution(ctx, source.UserID)
	if err != nil {
		return fmt.Errorf("counting attribution rows: %w", err)
	}

	printMergePlan(cmd, args, source, target, listCount, added, bought)

	out := cmd.OutOrStdout()

	confirmed, err := confirmMerge(yes, cmd.InOrStdin(), out)
	if err != nil {
		return err
	}

	if !confirmed {
		_, _ = fmt.Fprintln(out, "Aborted; nothing was changed.")

		return nil
	}

	result, err := identities.Merge(ctx, source, target)
	if err != nil {
		return fmt.Errorf("merging identities: %w", err)
	}

	_, _ = fmt.Fprintf(out, "Merged %s into %s (lists: %d, added: %d, bought: %d).\n",
		args[0], args[1], result.Lists, result.Added, result.Bought)

	return nil
}

// printMergePlan writes the pre-confirmation summary of what the merge will
// rewrite and clean up, plus a warning when the source can log in again.
func printMergePlan(cmd *cobra.Command, args []string, source, target db.Identity, listCount, added, bought int) {
	out := cmd.OutOrStdout()

	_, _ = fmt.Fprintln(out, "Merge plan:")
	_, _ = fmt.Fprintf(out, "  source: %s (user_id %s, %q, %s)\n",
		args[0], source.UserID, source.Name, orDash(source.Email))
	_, _ = fmt.Fprintf(out, "  target: %s (user_id %s, %q, %s)\n",
		args[1], target.UserID, target.Name, orDash(target.Email))
	_, _ = fmt.Fprintf(out, "  lists.created_by: %d rows\n", listCount)
	_, _ = fmt.Fprintf(out, "  items.added_by:   %d rows\n", added)
	_, _ = fmt.Fprintf(out, "  items.bought_by:  %d rows\n", bought)

	for _, line := range cleanupLines(source, target) {
		_, _ = fmt.Fprintf(out, "  %s\n", line)
	}

	if source.Kind != db.IdentityKindLocal {
		_, _ = fmt.Fprintln(cmd.ErrOrStderr(),
			"warning: the source identity can still log in via the IdP; "+
				"its next OIDC login re-derives the same user id and recreates the member row")
	}
}

// parseSelector splits a "user merge" selector into its kind prefix and value.
// Valid kinds are local, oidc and uuid; anything else (including a missing
// prefix or empty value) is an error naming the bad selector.
func parseSelector(selector string) (string, string, error) {
	kind, value, found := strings.Cut(selector, ":")
	if !found || value == "" {
		return "", "", fmt.Errorf("%w %q: want local:<username>, oidc:<subject>, or uuid:<user_id>",
			errInvalidSelector, selector)
	}

	switch kind {
	case selectorLocal, selectorOIDC, selectorUUID:
		return kind, value, nil
	default:
		return "", "", fmt.Errorf("%w %q: unknown prefix %q (want local, oidc, or uuid)",
			errInvalidSelector, selector, kind)
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
	case selectorLocal:
		return resolveLocalSelector(ctx, conn, selector, value)
	case selectorOIDC:
		return resolveOIDCSelector(ctx, conn, selector, value)
	default: // selectorUUID
		return resolveUUIDSelector(ctx, conn, selector, value)
	}
}

// resolveLocalSelector resolves a local:<username> selector against the users
// table; the account must exist.
func resolveLocalSelector(ctx context.Context, conn *sql.DB, selector, username string) (db.Identity, error) {
	account, _, err := db.NewUserRepository(conn).GetByUsername(ctx, username)
	if errors.Is(err, users.ErrNotFound) {
		return db.Identity{}, fmt.Errorf("selector %q: %w with username %q", selector, errNoLocalAccount, username)
	}

	if err != nil {
		return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
	}

	return db.Identity{
		Kind:       db.IdentityKindLocal,
		Identifier: account.Username,
		UserID:     account.ID,
		Name:       account.Name,
		Email:      account.Email,
		LastLogin:  nil,
	}, nil
}

// resolveOIDCSelector resolves an oidc:<subject> selector against the members
// table; the members row proves the account has logged in at least once.
func resolveOIDCSelector(ctx context.Context, conn *sql.DB, selector, subject string) (db.Identity, error) {
	member, err := db.NewMemberRepository(conn).Get(ctx, subject)
	if errors.Is(err, members.ErrNotFound) {
		return db.Identity{}, fmt.Errorf(
			"selector %q: %w with subject %q — the account must have logged in at least once (or use uuid:<user_id>)",
			selector, errNoMember, subject)
	}

	if err != nil {
		return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
	}

	identityKind := db.IdentityKindOIDC
	if member.Subject == auth.DevUser.ID.String() {
		identityKind = db.IdentityKindDev
	}

	lastLogin := member.UpdatedAt

	return db.Identity{
		Kind:       identityKind,
		Identifier: member.Subject,
		UserID:     member.UserID,
		Name:       member.Name,
		Email:      member.Email,
		LastLogin:  &lastLogin,
	}, nil
}

// resolveUUIDSelector resolves a uuid:<user_id> selector; any well-formed
// UUID is accepted and classified by what the database holds.
func resolveUUIDSelector(ctx context.Context, conn *sql.DB, selector, value string) (db.Identity, error) {
	id, err := uuid.Parse(value)
	if err != nil {
		return db.Identity{}, fmt.Errorf("selector %q: not a valid UUID: %w", selector, err)
	}

	identity, err := db.NewIdentityRepository(conn).ResolveUUID(ctx, id)
	if err != nil {
		return db.Identity{}, fmt.Errorf("selector %q: %w", selector, err)
	}

	return identity, nil
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
		lines = append(lines,
			fmt.Sprintf("will seed: members row for target %q (so display names resolve)", target.Identifier))
	}

	return lines
}

// confirmMerge gates the merge: --yes bypasses the prompt; otherwise stdin
// must be a terminal (mirroring "user add"'s --password-stdin guard) and the
// operator must answer y/Y to one "Proceed? [y/N]:" prompt.
func confirmMerge(yes bool, input io.Reader, out io.Writer) (bool, error) {
	if yes {
		return true, nil
	}

	if _, ok := terminalFd(input); !ok {
		return false, errConfirmationRequired
	}

	_, _ = fmt.Fprint(out, "Proceed? [y/N]: ")

	line, err := bufio.NewReader(input).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return false, fmt.Errorf("reading confirmation: %w", err)
	}

	answer := strings.TrimSpace(line)

	return answer == "y" || answer == "Y", nil
}
