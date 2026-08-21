// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
)

// Identity kinds as reported by IdentityRepository. "local" is a
// username/password account (users table), "oidc" a provider-backed member,
// "dev" the fixed dev user, and "unknown" a raw UUID that matches neither
// table (still mergeable; cleanup steps that match nothing are no-ops).
const (
	IdentityKindLocal   = "local"
	IdentityKindOIDC    = "oidc"
	IdentityKindDev     = "dev"
	IdentityKindUnknown = "unknown"
)

// Identity is one known account as seen by the operator CLI: a local
// username/password user, an OIDC member, or the dev user. Identifier is the
// username for local accounts and the auth subject otherwise. LastLogin is
// members.updated_at (refreshed by the JIT upsert on every login) and nil for
// an account that never logged in.
type Identity struct {
	Kind       string
	Identifier string
	UserID     uuid.UUID
	Name       string
	Email      string
	LastLogin  *time.Time
}

// IdentityRepository answers the operator CLI's cross-table questions about
// account identities (users + members + attribution columns). It is operator
// tooling, not a domain port: userls/usermerge are its only callers.
type IdentityRepository struct {
	db *sql.DB
}

// NewIdentityRepository builds an IdentityRepository over the given *sql.DB.
func NewIdentityRepository(db *sql.DB) *IdentityRepository {
	return &IdentityRepository{db: db}
}

// List returns every known identity: local accounts (with or without a
// members row) and members without a local account (OIDC or dev). The kind is
// classified in Go so the dev UUID constant stays single-sourced in auth.
// Results are ordered by kind, then identifier.
func (r *IdentityRepository) List(ctx context.Context) ([]Identity, error) {
	// A local account's members row has the user UUID string as its subject,
	// so joining on that string pairs each users row with its login record
	// while leaving provider-backed members (OIDC subjects) on their own row.
	const q = `
SELECT u.id, u.username, COALESCE(u.name, ''), COALESCE(u.email, ''),
       m.subject, m.user_id, COALESCE(m.name, ''), COALESCE(m.email, ''),
       m.updated_at
FROM users u
FULL OUTER JOIN members m ON m.subject = u.id::text`
	rows, err := r.db.QueryContext(ctx, q)
	if err != nil {
		return nil, fmt.Errorf("listing identities: %w", err)
	}
	defer func() { _ = rows.Close() }()

	devSubject := auth.DevUser.ID.String()
	var out []Identity
	for rows.Next() {
		var (
			userID       *uuid.UUID
			username     *string
			userName     string
			userEmail    string
			subject      *string
			memberUserID *uuid.UUID
			memberName   string
			memberEmail  string
			lastLogin    *time.Time
		)
		if err := rows.Scan(&userID, &username, &userName, &userEmail,
			&subject, &memberUserID, &memberName, &memberEmail, &lastLogin); err != nil {
			return nil, fmt.Errorf("scanning identity: %w", err)
		}

		var id Identity
		switch {
		case userID != nil:
			id = Identity{
				Kind:       IdentityKindLocal,
				Identifier: *username,
				UserID:     *userID,
				Name:       userName,
				Email:      userEmail,
				LastLogin:  lastLogin,
			}
		case subject != nil && *subject == devSubject:
			id = Identity{
				Kind:       IdentityKindDev,
				Identifier: *subject,
				UserID:     *memberUserID,
				Name:       memberName,
				Email:      memberEmail,
				LastLogin:  lastLogin,
			}
		default:
			id = Identity{
				Kind:       IdentityKindOIDC,
				Identifier: *subject,
				UserID:     *memberUserID,
				Name:       memberName,
				Email:      memberEmail,
				LastLogin:  lastLogin,
			}
		}
		out = append(out, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing identities: %w", err)
	}

	// Kind classification happens in Go, so the kind-then-identifier ordering
	// must too.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Identifier < out[j].Identifier
	})
	return out, nil
}
