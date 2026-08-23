// SPDX-License-Identifier: CC0-1.0

package auth_test

import (
	"testing"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
)

// devUserIDString is the dev user's UUID. It is deliberately hardcoded here so
// that changing the constant in the implementation breaks this test: the value
// is baked into attribution columns and must stay stable.
const devUserIDString = "00000000-0000-0000-0000-000000000001"

func TestDevMember(t *testing.T) {
	t.Parallel()

	m := auth.DevMember()

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"subject is the dev uuid string", m.Subject, devUserIDString},
		{"user id is the dev uuid", m.UserID, uuid.MustParse(devUserIDString)},
		{"subject equals user id string", m.Subject, m.UserID.String()},
		{"name is the dev user name", m.Name, "Dev User"},
		{"email is empty by design", m.Email, ""},
		{"user id matches DevUser", m.UserID, auth.DevUser.ID},
		{"subject mirrors DevUser id", m.Subject, auth.DevUser.ID.String()},
		{"name matches DevUser", m.Name, auth.DevUser.Name},
		{"email matches DevUser", m.Email, auth.DevUser.Email},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if tt.got != tt.want {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}
}

func TestDevMemberTimestampsAreZero(t *testing.T) {
	t.Parallel()

	m := auth.DevMember()

	if !m.CreatedAt.IsZero() {
		t.Errorf("CreatedAt = %v, want zero time (the repository owns it)", m.CreatedAt)
	}

	if !m.UpdatedAt.IsZero() {
		t.Errorf("UpdatedAt = %v, want zero time (the repository owns it)", m.UpdatedAt)
	}
}

func TestDevMemberReturnsIndependentValue(t *testing.T) {
	t.Parallel()

	first := auth.DevMember()
	first.Subject = "mutated"
	first.Name = "mutated"
	first.Email = "mutated@example.test"
	first.UserID = uuid.Nil

	second := auth.DevMember()
	if second.Subject != devUserIDString {
		t.Errorf("Subject = %q after mutating an earlier value, want %q", second.Subject, devUserIDString)
	}

	if second.UserID != uuid.MustParse(devUserIDString) {
		t.Errorf("UserID = %v after mutating an earlier value, want %v", second.UserID, devUserIDString)
	}

	if second.Name != "Dev User" {
		t.Errorf("Name = %q after mutating an earlier value, want %q", second.Name, "Dev User")
	}

	if second.Email != "" {
		t.Errorf("Email = %q after mutating an earlier value, want empty", second.Email)
	}

	if auth.DevUser.ID != uuid.MustParse(devUserIDString) {
		t.Errorf("DevUser.ID = %v after mutating a returned Member, want %v", auth.DevUser.ID, devUserIDString)
	}

	if auth.DevUser.Name != "Dev User" {
		t.Errorf("DevUser.Name = %q after mutating a returned Member, want %q", auth.DevUser.Name, "Dev User")
	}

	if auth.DevUser.Email != "" {
		t.Errorf("DevUser.Email = %q after mutating a returned Member, want empty", auth.DevUser.Email)
	}
}
