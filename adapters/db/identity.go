// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"errors"
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

// ResolveUUID classifies a raw user id: a users row with that id makes it a
// local identity; otherwise a members row with that user_id makes it oidc (or
// dev, for the dev subject); otherwise the id matches nothing and the identity
// comes back with Kind "unknown" — still mergeable, since attribution columns
// may hold ids with no surviving account, and cleanup steps that match nothing
// are no-ops.
func (r *IdentityRepository) ResolveUUID(ctx context.Context, id uuid.UUID) (Identity, error) {
	const userQ = `
SELECT username, COALESCE(name, ''), COALESCE(email, '')
FROM users
WHERE id = $1`
	var ident Identity
	err := r.db.QueryRowContext(ctx, userQ, id).Scan(&ident.Identifier, &ident.Name, &ident.Email)
	if err == nil {
		ident.Kind = IdentityKindLocal
		ident.UserID = id
		return ident, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, fmt.Errorf("resolving uuid against users: %w", err)
	}

	const memberQ = `
SELECT subject, COALESCE(name, ''), COALESCE(email, ''), updated_at
FROM members
WHERE user_id = $1`
	var lastLogin time.Time
	err = r.db.QueryRowContext(ctx, memberQ, id).Scan(&ident.Identifier, &ident.Name, &ident.Email, &lastLogin)
	if err == nil {
		ident.Kind = IdentityKindOIDC
		if ident.Identifier == auth.DevUser.ID.String() {
			ident.Kind = IdentityKindDev
		}
		ident.UserID = id
		ident.LastLogin = &lastLogin
		return ident, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, fmt.Errorf("resolving uuid against members: %w", err)
	}

	return Identity{Kind: IdentityKindUnknown, Identifier: id.String(), UserID: id}, nil
}

// CountAttribution returns how many rows each attribution column holds for the
// given user id, for the merge confirmation prompt.
func (r *IdentityRepository) CountAttribution(ctx context.Context, userID uuid.UUID) (lists, added, bought int, err error) {
	const q = `
SELECT (SELECT count(*) FROM lists WHERE created_by = $1),
       (SELECT count(*) FROM items WHERE added_by   = $1),
       (SELECT count(*) FROM items WHERE bought_by  = $1)`
	if err := r.db.QueryRowContext(ctx, q, userID).Scan(&lists, &added, &bought); err != nil {
		return 0, 0, 0, fmt.Errorf("counting attribution: %w", err)
	}
	return lists, added, bought, nil
}

// MergeResult reports how many rows each attribution UPDATE rewrote.
type MergeResult struct {
	Lists  int64
	Added  int64
	Bought int64
}

// Merge rewrites all attribution from the source identity's user id to the
// target's and cleans up the source, in one transaction: the three attribution
// columns are re-pointed, the source's members row is deleted (all kinds), the
// source's users row is deleted when the source is local, and when the target
// is local without a members row one is seeded (subject = the user UUID
// string, exactly as a password login writes it) so display names resolve
// immediately instead of on the target's next login.
func (r *IdentityRepository) Merge(ctx context.Context, source, target Identity) (MergeResult, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MergeResult{}, fmt.Errorf("merge: begin: %w", err)
	}
	// Rolled back unless the commit below already ended the transaction, in
	// which case this is a no-op returning ErrTxDone.
	defer func() { _ = tx.Rollback() }()

	var result MergeResult
	updates := []struct {
		q    string
		dest *int64
	}{
		{`UPDATE lists SET created_by = $1 WHERE created_by = $2`, &result.Lists},
		{`UPDATE items SET added_by   = $1 WHERE added_by   = $2`, &result.Added},
		{`UPDATE items SET bought_by  = $1 WHERE bought_by  = $2`, &result.Bought},
	}
	for _, u := range updates {
		res, err := tx.ExecContext(ctx, u.q, target.UserID, source.UserID)
		if err != nil {
			return MergeResult{}, fmt.Errorf("merge: rewriting attribution: %w", err)
		}
		if *u.dest, err = res.RowsAffected(); err != nil {
			return MergeResult{}, fmt.Errorf("merge: rewriting attribution: %w", err)
		}
	}

	if _, err := tx.ExecContext(ctx, `DELETE FROM members WHERE user_id = $1`, source.UserID); err != nil {
		return MergeResult{}, fmt.Errorf("merge: deleting source member: %w", err)
	}
	if source.Kind == IdentityKindLocal {
		if _, err := tx.ExecContext(ctx, `DELETE FROM users WHERE id = $1`, source.UserID); err != nil {
			return MergeResult{}, fmt.Errorf("merge: deleting source user: %w", err)
		}
	}
	if target.Kind == IdentityKindLocal {
		const seed = `
INSERT INTO members (subject, user_id, email, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (subject) DO NOTHING`
		if _, err := tx.ExecContext(ctx, seed,
			target.UserID.String(), target.UserID, target.Email, target.Name); err != nil {
			return MergeResult{}, fmt.Errorf("merge: seeding target member: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return MergeResult{}, fmt.Errorf("merge: commit: %w", err)
	}
	return result, nil
}
