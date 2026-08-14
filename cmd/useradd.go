// SPDX-License-Identifier: CC0-1.0

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/users"
)

var (
	useraddPasswordStdin bool
	useraddName          string
	useraddEmail         string
)

var useraddCmd = &cobra.Command{
	Use:   "useradd <username>",
	Short: "Create a local username/password account",
	Long: `Creates a local account for username/password authentication
(SPLITKAUF_AUTH_PASSWORD_ENABLED). The password is read from an interactive
no-echo prompt (entered twice) or, with --password-stdin, from standard input
for automation. Only the bcrypt hash is stored; the plaintext is never written
to the database, logs, or the process arguments.

There is no public sign-up: accounts exist only when created here.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		username := strings.TrimSpace(args[0])
		if username == "" {
			return users.ErrUsernameEmpty
		}

		password, err := resolvePassword(useraddPasswordStdin, cmd.InOrStdin(), cmd.OutOrStdout())
		if err != nil {
			return err
		}
		hash, err := users.HashPassword(password)
		if err != nil {
			return err
		}

		name := strings.TrimSpace(useraddName)
		if name == "" {
			name = username
		}

		conn, err := db.NewSQL(config.C.Database.DSN())
		if err != nil {
			return fmt.Errorf("connecting to database: %w", err)
		}
		defer func() { _ = conn.Close() }()

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if _, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
			Username:     username,
			PasswordHash: hash,
			Name:         name,
			Email:        strings.TrimSpace(useraddEmail),
		}); err != nil {
			if errors.Is(err, users.ErrUsernameTaken) {
				return fmt.Errorf("user %q already exists", username)
			}
			return fmt.Errorf("creating user: %w", err)
		}

		fmt.Fprintf(cmd.OutOrStdout(), "Created user %q.\n", username)
		return nil
	},
}

// resolvePassword obtains the new account's password. With --password-stdin it
// reads (and trims one trailing newline from) stdin. Interactively it prompts
// twice with echo disabled and requires the two entries to match. It never
// echoes or returns the password to logs.
func resolvePassword(fromStdin bool, in io.Reader, out io.Writer) (string, error) {
	if fromStdin {
		raw, err := io.ReadAll(in)
		if err != nil {
			return "", fmt.Errorf("reading password from stdin: %w", err)
		}
		return strings.TrimRight(string(raw), "\r\n"), nil
	}

	fd, ok := terminalFd(in)
	if !ok {
		return "", errors.New("no terminal available for password entry; use --password-stdin")
	}

	fmt.Fprint(out, "Password: ")
	first, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("reading password: %w", err)
	}
	fmt.Fprint(out, "Confirm password: ")
	second, err := term.ReadPassword(fd)
	fmt.Fprintln(out)
	if err != nil {
		return "", fmt.Errorf("reading password confirmation: %w", err)
	}
	if string(first) != string(second) {
		return "", errors.New("passwords do not match")
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

func init() {
	rootCmd.AddCommand(useraddCmd)
	useraddCmd.Flags().BoolVar(&useraddPasswordStdin, "password-stdin", false, "read the password from standard input instead of prompting")
	useraddCmd.Flags().StringVar(&useraddName, "name", "", "display name (defaults to the username)")
	useraddCmd.Flags().StringVar(&useraddEmail, "email", "", "optional email address")
}
