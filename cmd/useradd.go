// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/users"
)

// Sentinel errors of interactive password entry, so callers can match them
// with errors.Is.
var (
	errNoTerminal       = errors.New("no terminal available for password entry; use --password-stdin")
	errPasswordMismatch = errors.New("passwords do not match")
)

// newUseraddCmd builds the "user add" command that creates a local
// username/password account.
func newUseraddCmd() *cobra.Command {
	var (
		passwordStdin bool
		displayName   string
		email         string
	)

	useraddCmd := new(cobra.Command)
	useraddCmd.Use = "add <username>"
	useraddCmd.Short = "Create a local username/password account"
	useraddCmd.Long = `Creates a local account for username/password authentication
(SPLITKAUF_AUTH_PASSWORD_ENABLED). The password is read from an interactive
no-echo prompt (entered twice) or, with --password-stdin, from standard input
for automation. Only the bcrypt hash is stored; the plaintext is never written
to the database, logs, or the process arguments.

There is no public sign-up: accounts exist only when created here.`
	useraddCmd.Args = cobra.ExactArgs(1)
	useraddCmd.RunE = func(cmd *cobra.Command, args []string) error {
		return runUseradd(cmd, args[0], passwordStdin, displayName, email)
	}

	useraddCmd.Flags().BoolVar(&passwordStdin, "password-stdin", false,
		"read the password from standard input instead of prompting")
	useraddCmd.Flags().StringVar(&displayName, "name", "", "display name (defaults to the username)")
	useraddCmd.Flags().StringVar(&email, "email", "", "optional email address")

	return useraddCmd
}

// runUseradd resolves the password, hashes it, and creates the local account
// in the database. displayName defaults to the username when empty.
func runUseradd(cmd *cobra.Command, usernameArg string, passwordStdin bool, displayName, email string) error {
	username := strings.TrimSpace(usernameArg)
	if username == "" {
		return users.ErrUsernameEmpty
	}

	password, err := resolvePassword(passwordStdin, cmd.InOrStdin(), cmd.OutOrStdout())
	if err != nil {
		return err
	}

	hash, err := users.HashPassword(password)
	if err != nil {
		return fmt.Errorf("hashing password: %w", err)
	}

	name := strings.TrimSpace(displayName)
	if name == "" {
		name = username
	}

	conn, err := db.NewSQL(config.C.Database.DSN())
	if err != nil {
		return fmt.Errorf("connecting to database: %w", err)
	}
	defer func() { _ = conn.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), userCmdTimeout)
	defer cancel()

	if _, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username:     username,
		PasswordHash: hash,
		Name:         name,
		Email:        strings.TrimSpace(email),
	}); err != nil {
		if errors.Is(err, users.ErrUsernameTaken) {
			return fmt.Errorf("user %q already exists: %w", username, err)
		}

		return fmt.Errorf("creating user: %w", err)
	}

	_, _ = fmt.Fprintf(cmd.OutOrStdout(), "Created user %q.\n", username)

	return nil
}

// resolvePassword obtains the new account's password. With --password-stdin it
// reads (and trims one trailing newline from) input. Interactively it prompts
// twice with echo disabled and requires the two entries to match. It never
// echoes or returns the password to logs.
func resolvePassword(fromStdin bool, input io.Reader, out io.Writer) (string, error) {
	if fromStdin {
		raw, err := io.ReadAll(input)
		if err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}

		return strings.TrimRight(string(raw), "\r\n"), nil
	}

	termFd, ok := terminalFd(input)
	if !ok {
		return "", errNoTerminal
	}

	_, _ = fmt.Fprint(out, "Password: ")

	first, err := term.ReadPassword(termFd)

	_, _ = fmt.Fprintln(out)

	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}

	_, _ = fmt.Fprint(out, "Confirm password: ")

	second, err := term.ReadPassword(termFd)

	_, _ = fmt.Fprintln(out)

	if err != nil {
		return "", fmt.Errorf("reading password confirmation: %w", err)
	}

	if string(first) != string(second) {
		return "", errPasswordMismatch
	}

	return string(first), nil
}

// terminalFd returns the file descriptor of in when it is a terminal, so
// ReadPassword can disable echo. It returns ok=false for a non-TTY (pipe/file),
// which the caller turns into a "use --password-stdin" error.
func terminalFd(in io.Reader) (int, bool) {
	f, ok := in.(*os.File)
	if !ok {
		return 0, false
	}

	fd := int(f.Fd())
	if !term.IsTerminal(fd) {
		return 0, false
	}

	return fd, true
}
