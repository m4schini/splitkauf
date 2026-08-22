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
// tooling, not a domain port: "user ls"/"user merge" are its only callers.
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
	const query = `
SELECT u.id, u.username, COALESCE(u.name, ''), COALESCE(u.email, ''),
       m.subject, m.user_id, COALESCE(m.name, ''), COALESCE(m.email, ''),
       m.updated_at
FROM users u
FULL OUTER JOIN members m ON m.subject = u.id::text`

	rows, err := r.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("listing identities: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []Identity

	for rows.Next() {
		identity, err := scanIdentity(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, identity)
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

// scanIdentity reads one row of List's users/members full outer join and
// classifies its kind: a users row makes it local, the dev subject makes it
// dev, any other members-only row is oidc.
func scanIdentity(rows *sql.Rows) (Identity, error) {
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
		return Identity{}, fmt.Errorf("scanning identity: %w", err)
	}

	switch {
	case userID != nil:
		return Identity{
			Kind:       IdentityKindLocal,
			Identifier: *username,
			UserID:     *userID,
			Name:       userName,
			Email:      userEmail,
			LastLogin:  lastLogin,
		}, nil
	case subject != nil && *subject == auth.DevUser.ID.String():
		return Identity{
			Kind:       IdentityKindDev,
			Identifier: *subject,
			UserID:     *memberUserID,
			Name:       memberName,
			Email:      memberEmail,
			LastLogin:  lastLogin,
		}, nil
	default:
		return Identity{
			Kind:       IdentityKindOIDC,
			Identifier: *subject,
			UserID:     *memberUserID,
			Name:       memberName,
			Email:      memberEmail,
			LastLogin:  lastLogin,
		}, nil
	}
}

// ResolveUUID classifies a raw user id: a users row with that id makes it a
// local identity; otherwise a members row with that user_id makes it oidc (or
// dev, for the dev subject); otherwise the id matches nothing and the identity
// comes back with Kind "unknown" — still mergeable, since attribution columns
// may hold ids with no surviving account, and cleanup steps that match nothing
// are no-ops.
func (r *IdentityRepository) ResolveUUID(ctx context.Context, userID uuid.UUID) (Identity, error) {
	const userQuery = `
SELECT username, COALESCE(name, ''), COALESCE(email, '')
FROM users
WHERE id = $1`

	var ident Identity

	err := r.db.QueryRowContext(ctx, userQuery, userID).Scan(&ident.Identifier, &ident.Name, &ident.Email)
	if err == nil {
		ident.Kind = IdentityKindLocal
		ident.UserID = userID

		return ident, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, fmt.Errorf("resolving uuid against users: %w", err)
	}

	const memberQuery = `
SELECT subject, COALESCE(name, ''), COALESCE(email, ''), updated_at
FROM members
WHERE user_id = $1`

	var lastLogin time.Time

	err = r.db.QueryRowContext(ctx, memberQuery, userID).Scan(&ident.Identifier, &ident.Name, &ident.Email, &lastLogin)
	if err == nil {
		ident.Kind = IdentityKindOIDC
		if ident.Identifier == auth.DevUser.ID.String() {
			ident.Kind = IdentityKindDev
		}

		ident.UserID = userID
		ident.LastLogin = &lastLogin

		return ident, nil
	}

	if !errors.Is(err, sql.ErrNoRows) {
		return Identity{}, fmt.Errorf("resolving uuid against members: %w", err)
	}

	return Identity{
		Kind:       IdentityKindUnknown,
		Identifier: userID.String(),
		UserID:     userID,
		Name:       "",
		Email:      "",
		LastLogin:  nil,
	}, nil
}

// CountAttribution returns how many rows each attribution column holds for the
// given user id (lists created, items added, items bought), for the merge
// confirmation prompt.
func (r *IdentityRepository) CountAttribution(ctx context.Context, userID uuid.UUID) (int, int, int, error) {
	const query = `
SELECT (SELECT count(*) FROM lists WHERE created_by = $1),
       (SELECT count(*) FROM items WHERE added_by   = $1),
       (SELECT count(*) FROM items WHERE bought_by  = $1)`

	var lists, added, bought int
	if err := r.db.QueryRowContext(ctx, query, userID).Scan(&lists, &added, &bought); err != nil {
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
	transaction, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return MergeResult{}, fmt.Errorf("merge: begin: %w", err)
	}
	// Rolled back unless the commit below already ended the transaction, in
	// which case this is a no-op returning ErrTxDone.
	defer func() { _ = transaction.Rollback() }()

	result, err := rewriteAttribution(ctx, transaction, source, target)
	if err != nil {
		return MergeResult{}, err
	}

	if err := deleteSourceIdentity(ctx, transaction, source); err != nil {
		return MergeResult{}, err
	}

	if err := seedTargetMember(ctx, transaction, target); err != nil {
		return MergeResult{}, err
	}

	if err := transaction.Commit(); err != nil {
		return MergeResult{}, fmt.Errorf("merge: commit: %w", err)
	}

	return result, nil
}

// rewriteAttribution re-points the three attribution columns from the source
// user id to the target's, reporting how many rows each UPDATE rewrote.
func rewriteAttribution(ctx context.Context, transaction *sql.Tx, source, target Identity) (MergeResult, error) {
	var result MergeResult

	updates := []struct {
		query string
		dest  *int64
	}{
		{`UPDATE lists SET created_by = $1 WHERE created_by = $2`, &result.Lists},
		{`UPDATE items SET added_by   = $1 WHERE added_by   = $2`, &result.Added},
		{`UPDATE items SET bought_by  = $1 WHERE bought_by  = $2`, &result.Bought},
	}
	for _, update := range updates {
		res, err := transaction.ExecContext(ctx, update.query, target.UserID, source.UserID)
		if err != nil {
			return MergeResult{}, fmt.Errorf("merge: rewriting attribution: %w", err)
		}

		if *update.dest, err = res.RowsAffected(); err != nil {
			return MergeResult{}, fmt.Errorf("merge: rewriting attribution: %w", err)
		}
	}

	return result, nil
}

// deleteSourceIdentity removes the source's members row (all kinds) and, for a
// local source, its users row.
func deleteSourceIdentity(ctx context.Context, transaction *sql.Tx, source Identity) error {
	if _, err := transaction.ExecContext(ctx,
		`DELETE FROM members WHERE user_id = $1`, source.UserID); err != nil {
		return fmt.Errorf("merge: deleting source member: %w", err)
	}

	if source.Kind != IdentityKindLocal {
		return nil
	}

	if _, err := transaction.ExecContext(ctx,
		`DELETE FROM users WHERE id = $1`, source.UserID); err != nil {
		return fmt.Errorf("merge: deleting source user: %w", err)
	}

	return nil
}

// seedTargetMember gives a local merge target a members row keyed by its user
// UUID string, exactly as a password login writes one, so display names
// resolve immediately instead of on the target's next login. Non-local targets
// already have their members row and are left alone.
func seedTargetMember(ctx context.Context, transaction *sql.Tx, target Identity) error {
	if target.Kind != IdentityKindLocal {
		return nil
	}

	const seed = `
INSERT INTO members (subject, user_id, email, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (subject) DO NOTHING`
	if _, err := transaction.ExecContext(ctx, seed,
		target.UserID.String(), target.UserID, target.Email, target.Name); err != nil {
		return fmt.Errorf("merge: seeding target member: %w", err)
	}

	return nil
}
