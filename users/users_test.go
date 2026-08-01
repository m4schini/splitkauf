// SPDX-License-Identifier: TODO

package users_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/m4schini/splitkauf/users"
)

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name  string
		plain string
		want  error
	}{
		{"ok", "correct horse", nil},
		{"minimum length exactly", "12345678", nil},
		{"too short", "short", users.ErrPasswordTooShort},
		{"empty", "", users.ErrPasswordTooShort},
		{"too long", strings.Repeat("a", users.MaxPasswordLen+1), users.ErrPasswordTooLong},
		{"max length exactly", strings.Repeat("a", users.MaxPasswordLen), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := users.ValidatePassword(tt.plain); !errors.Is(got, tt.want) {
				t.Errorf("ValidatePassword(%q) = %v, want %v", tt.plain, got, tt.want)
			}
		})
	}
}

func TestHashAndVerifyPassword(t *testing.T) {
	const pw = "correct horse battery staple"
	hash, err := users.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if hash == pw || hash == "" {
		t.Fatalf("hash is empty or equals the plaintext: %q", hash)
	}
	if !users.VerifyPassword(hash, pw) {
		t.Error("VerifyPassword rejected the correct password")
	}
	if users.VerifyPassword(hash, "wrong password") {
		t.Error("VerifyPassword accepted a wrong password")
	}
	if users.VerifyPassword("not a bcrypt hash", pw) {
		t.Error("VerifyPassword accepted a malformed hash")
	}
}

func TestHashPasswordRejectsInvalid(t *testing.T) {
	if _, err := users.HashPassword("short"); !errors.Is(err, users.ErrPasswordTooShort) {
		t.Errorf("HashPassword(short) err = %v, want ErrPasswordTooShort", err)
	}
	if _, err := users.HashPassword(strings.Repeat("a", users.MaxPasswordLen+1)); !errors.Is(err, users.ErrPasswordTooLong) {
		t.Errorf("HashPassword(too long) err = %v, want ErrPasswordTooLong", err)
	}
}

// TestHashPasswordSaltsEachHash proves two hashes of the same password differ
// (bcrypt salts), so identical passwords are not detectable by equal hashes.
func TestHashPasswordSaltsEachHash(t *testing.T) {
	const pw = "correct horse battery staple"
	h1, err := users.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	h2, err := users.HashPassword(pw)
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if h1 == h2 {
		t.Error("two hashes of the same password are identical (no salt?)")
	}
}
