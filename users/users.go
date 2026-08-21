// SPDX-License-Identifier: CC0-1.0

// Package users is the local-account domain for password authentication
// (US-A.6/US-A.7). A user is a credential record — a unique username and a
// bcrypt password hash — provisioned by the operator via the `user add` CLI.
// There is no self-registration: the Repository only creates accounts on
// explicit operator action, and reads them back for the login flow. Password
// hashing lives here so the plaintext never travels beyond this package's
// HashPassword/VerifyPassword helpers.
package users

import (
	"context"
	"errors"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

// ErrNotFound is returned by Repository.GetByUsername when no user has the
// requested username.
var ErrNotFound = errors.New("user not found")

// ErrUsernameTaken is returned by Repository.Create when a user with the same
// username already exists.
var ErrUsernameTaken = errors.New("username already taken")

// Password policy. bcrypt hashes at most the first 72 bytes of its input, so a
// longer password would be silently truncated; reject it instead. The minimum
// is a light guard against trivially weak passwords.
const (
	MinPasswordLen = 8
	MaxPasswordLen = 72 // bcrypt's hard limit, in bytes
)

// ErrPasswordTooShort and ErrPasswordTooLong report a password outside the
// accepted length range.
var (
	ErrPasswordTooShort = errors.New("password too short")
	ErrPasswordTooLong  = errors.New("password too long")
	ErrUsernameEmpty    = errors.New("username must not be empty")
)

// User is one local account. It never carries the password hash — the hash is
// returned separately by GetByUsername and only to the authenticator.
type User struct {
	ID        uuid.UUID
	Username  string
	Name      string
	Email     string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewUser is the input to Repository.Create: a validated username plus an
// already-hashed password. Name/Email are optional.
type NewUser struct {
	Username     string
	PasswordHash string
	Name         string
	Email        string
}

// Repository is the persistence port for local accounts. The Postgres adapter
// implements it; callers depend only on this interface.
type Repository interface {
	// Create inserts a new user. It returns ErrUsernameTaken when the username
	// already exists.
	Create(ctx context.Context, in NewUser) (User, error)
	// GetByUsername returns the user and its bcrypt password hash, or
	// ErrNotFound when no such user exists. The hash is returned separately so
	// it never rides on the domain User.
	GetByUsername(ctx context.Context, username string) (User, string, error)
}

// ValidatePassword reports whether plain is an acceptable password to hash. The
// minimum is measured in characters (runes) so a short multibyte password can't
// slip past a byte-length check; the maximum is bcrypt's 72-byte ceiling
// (measured in bytes, the unit bcrypt actually truncates on).
func ValidatePassword(plain string) error {
	if utf8.RuneCountInString(plain) < MinPasswordLen {
		return ErrPasswordTooShort
	}
	if len(plain) > MaxPasswordLen {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword validates plain and returns its bcrypt hash at the default cost.
func HashPassword(plain string) (string, error) {
	if err := ValidatePassword(plain); err != nil {
		return "", err
	}
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(h), nil
}

// VerifyPassword reports whether plain matches the bcrypt hash. It runs in
// constant time relative to the hash (bcrypt.CompareHashAndPassword) and returns
// false for any mismatch or malformed hash — callers must not distinguish the
// two, to avoid leaking whether a hash was well-formed.
func VerifyPassword(hash, plain string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}
