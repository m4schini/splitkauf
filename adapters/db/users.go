// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/m4schini/splitkauf/users"
)

// pgErrUniqueViolation is the SQLSTATE for a unique-constraint violation, used
// to map a duplicate username to users.ErrUsernameTaken.
const pgErrUniqueViolation = "23505"

// UserRepository is the Postgres implementation of users.Repository over a
// database/sql handle (pgx driver). It reads and writes the users table created
// by migration 000006.
type UserRepository struct {
	db *sql.DB
}

// NewUserRepository builds a UserRepository over the given *sql.DB.
func NewUserRepository(db *sql.DB) *UserRepository {
	return &UserRepository{db: db}
}

// Create inserts a new user and returns it. A unique-violation on username
// (SQLSTATE 23505) is mapped to users.ErrUsernameTaken. Email is stored as SQL
// NULL when empty. The password hash is bound as a parameter and never logged.
func (r *UserRepository) Create(ctx context.Context, newUser users.NewUser) (users.User, error) {
	const query = `
INSERT INTO users (username, password_hash, name, email)
VALUES ($1, $2, $3, $4)
RETURNING id, username, name, COALESCE(email, ''), created_at, updated_at`

	var user users.User

	err := r.db.QueryRowContext(ctx, query,
		newUser.Username, newUser.PasswordHash, newUser.Name, nullEmpty(newUser.Email)).Scan(
		&user.ID, &user.Username, &user.Name, &user.Email, &user.CreatedAt, &user.UpdatedAt,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgErrUniqueViolation {
			return users.User{}, users.ErrUsernameTaken
		}

		return users.User{}, fmt.Errorf("creating user: %w", err)
	}

	return user, nil
}

// GetByUsername returns the user and its bcrypt password hash, or
// users.ErrNotFound when no row matches.
func (r *UserRepository) GetByUsername(ctx context.Context, username string) (users.User, string, error) {
	const query = `
SELECT id, username, name, COALESCE(email, ''), password_hash, created_at, updated_at
FROM users
WHERE username = $1`

	var (
		user users.User
		hash string
	)

	err := r.db.QueryRowContext(ctx, query, username).Scan(
		&user.ID, &user.Username, &user.Name, &user.Email, &hash, &user.CreatedAt, &user.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return users.User{}, "", users.ErrNotFound
	}

	if err != nil {
		return users.User{}, "", fmt.Errorf("querying user: %w", err)
	}

	return user, hash, nil
}

// nullEmpty maps an empty string to a NULL-valued parameter so an omitted
// email is stored as SQL NULL rather than an empty string.
func nullEmpty(s string) any {
	if s == "" {
		return nil
	}

	return s
}
