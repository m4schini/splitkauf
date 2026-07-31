// SPDX-License-Identifier: TODO

// Package members is the minimal membership domain for M2. Every account that
// authenticates against the OIDC provider (or the dev user in dev-auth mode) is
// upserted into a members table keyed by the OIDC subject. There is no in-app
// membership administration: the identity provider is the source of truth and
// the table is populated just-in-time as accounts sign in.
package members

import (
	"context"
	"time"
)

// Member is one account known to the app, keyed by its OIDC subject. Email and
// Name are provider-derived and may be empty when the provider omits them. The
// timestamps are managed by the repository.
type Member struct {
	Subject   string
	Email     string
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// Repository is the persistence port for members. The Postgres adapter
// implements it; callers depend only on this interface.
type Repository interface {
	// Upsert inserts the member or, when a row with the same subject already
	// exists, updates its email and name. Subject is the primary key.
	Upsert(ctx context.Context, m Member) error
	// Get returns the member with the given subject. It returns ErrNotFound
	// when no such member exists.
	Get(ctx context.Context, subject string) (Member, error)
}
