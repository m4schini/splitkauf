// SPDX-License-Identifier: TODO

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

// Upsert inserts the member or updates the email/name of the existing row with
// the same subject, refreshing updated_at. All values are bound as parameters.
func (r *MemberRepository) Upsert(ctx context.Context, m members.Member) error {
	const q = `
INSERT INTO members (subject, email, name)
VALUES ($1, $2, $3)
ON CONFLICT (subject) DO UPDATE
SET email      = EXCLUDED.email,
    name       = EXCLUDED.name,
    updated_at = now()`
	if _, err := r.db.ExecContext(ctx, q, m.Subject, m.Email, m.Name); err != nil {
		return fmt.Errorf("upserting member: %w", err)
	}
	return nil
}

// Get returns the member with the given subject, or members.ErrNotFound when no
// such row exists.
func (r *MemberRepository) Get(ctx context.Context, subject string) (members.Member, error) {
	const q = `
SELECT subject, email, name, created_at, updated_at
FROM members
WHERE subject = $1`
	var m members.Member
	err := r.db.QueryRowContext(ctx, q, subject).Scan(
		&m.Subject, &m.Email, &m.Name, &m.CreatedAt, &m.UpdatedAt,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return members.Member{}, members.ErrNotFound
	}
	if err != nil {
		return members.Member{}, fmt.Errorf("querying member: %w", err)
	}
	return m, nil
}
