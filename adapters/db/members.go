// SPDX-License-Identifier: CC0-1.0

package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/m4schini/splitkauf/members"
)

// MemberRepository is the Postgres implementation of members.Repository over a
// database/sql handle (pgx driver). It writes the members table created by
// migration 000003.
type MemberRepository struct {
	db *sql.DB
}

// NewMemberRepository builds a MemberRepository over the given *sql.DB.
func NewMemberRepository(db *sql.DB) *MemberRepository {
	return &MemberRepository{db: db}
}

// Upsert inserts the member or updates the user id/email/name of the existing
// row with the same subject, refreshing updated_at. All values are bound as
// parameters.
//
// user_id is rewritten on every upsert, not just on insert: migration 000007
// backfilled it from the subject, and that backfill guesses wrong for a
// provider whose subjects are themselves UUID strings. Re-stamping it from the
// authenticated user heals such a row on the account's next login.
func (r *MemberRepository) Upsert(ctx context.Context, m members.Member) error {
	const q = `
INSERT INTO members (subject, user_id, email, name)
VALUES ($1, $2, $3, $4)
ON CONFLICT (subject) DO UPDATE
SET user_id    = EXCLUDED.user_id,
    email      = EXCLUDED.email,
    name       = EXCLUDED.name,
    updated_at = now()`
	if _, err := r.db.ExecContext(ctx, q, m.Subject, m.UserID, m.Email, m.Name); err != nil {
		return fmt.Errorf("upserting member: %w", err)
	}

	return nil
}

// Get returns the member with the given subject, or members.ErrNotFound when no
// such row exists.
func (r *MemberRepository) Get(ctx context.Context, subject string) (members.Member, error) {
	const q = `
SELECT subject, user_id, email, name, created_at, updated_at
FROM members
WHERE subject = $1`

	var m members.Member

	err := r.db.QueryRowContext(ctx, q, subject).Scan(
		&m.Subject, &m.UserID, &m.Email, &m.Name, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return members.Member{}, members.ErrNotFound
	}

	if err != nil {
		return members.Member{}, fmt.Errorf("querying member: %w", err)
	}

	return m, nil
}
