// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"database/sql"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

// newTestIdentityRepo opens an IdentityRepository against the DSN in
// SPLITKAUF_TEST_DATABASE_DSN, skipping the test when running with -short or
// when the DSN is unset. It TRUNCATEs users, members, lists and items so each
// test starts clean, and returns the raw connection for companion
// repositories and direct assertions.
func newTestIdentityRepo(t *testing.T) (*db.IdentityRepository, *sql.DB, context.Context) {
	t.Helper()
	if testing.Short() {
		t.Skip("skipping integration test in -short mode")
	}
	dsn := os.Getenv("SPLITKAUF_TEST_DATABASE_DSN")
	if dsn == "" {
		t.Skip("SPLITKAUF_TEST_DATABASE_DSN not set; skipping integration test")
	}

	conn, err := db.NewSQL(dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := conn.ExecContext(context.Background(), `TRUNCATE TABLE users, members, lists, items`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db.NewIdentityRepository(conn), conn, context.Background()
}

func TestIdentityListLocalNeverLoggedIn(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	u, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username:     "maria",
		PasswordHash: "x",
		Name:         "Maria",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	ids, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List returned %d identities, want 1: %+v", len(ids), ids)
	}
	got := ids[0]
	if got.Kind != db.IdentityKindLocal || got.Identifier != "maria" || got.UserID != u.ID {
		t.Errorf("identity = %+v, want local maria %v", got, u.ID)
	}
	if got.Name != "Maria" || got.Email != "" {
		t.Errorf("name/email = %q/%q, want Maria/empty", got.Name, got.Email)
	}
	if got.LastLogin != nil {
		t.Errorf("LastLogin = %v, want nil (never logged in)", got.LastLogin)
	}
}

func TestIdentityListLocalWithMemberRowJoinsToOneEntry(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	u, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username:     "alex",
		PasswordHash: "x",
		Name:         "Alex",
		Email:        "alex@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	// A password login upserts a members row keyed by the user UUID string.
	if err := db.NewMemberRepository(conn).Upsert(ctx, members.Member{
		Subject: u.ID.String(),
		UserID:  u.ID,
		Email:   u.Email,
		Name:    u.Name,
	}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}

	ids, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List returned %d identities, want 1 (joined): %+v", len(ids), ids)
	}
	got := ids[0]
	if got.Kind != db.IdentityKindLocal || got.Identifier != "alex" || got.UserID != u.ID {
		t.Errorf("identity = %+v, want local alex %v", got, u.ID)
	}
	if got.LastLogin == nil {
		t.Errorf("LastLogin = nil, want the members updated_at")
	}
}

func TestIdentityListOIDCOnlyMember(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	userID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if err := db.NewMemberRepository(conn).Upsert(ctx, members.Member{
		Subject: "oidc-subject-42",
		UserID:  userID,
		Email:   "alex@idp.example",
		Name:    "Alex S.",
	}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}

	ids, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List returned %d identities, want 1: %+v", len(ids), ids)
	}
	got := ids[0]
	if got.Kind != db.IdentityKindOIDC || got.Identifier != "oidc-subject-42" || got.UserID != userID {
		t.Errorf("identity = %+v, want oidc oidc-subject-42 %v", got, userID)
	}
	if got.Name != "Alex S." || got.Email != "alex@idp.example" {
		t.Errorf("name/email = %q/%q, want provider values", got.Name, got.Email)
	}
	if got.LastLogin == nil {
		t.Errorf("LastLogin = nil, want the members updated_at")
	}
}

func TestIdentityListDevMemberClassifiedDev(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	if err := db.NewMemberRepository(conn).Upsert(ctx, auth.DevMember()); err != nil {
		t.Fatalf("upsert dev member: %v", err)
	}

	ids, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(ids) != 1 {
		t.Fatalf("List returned %d identities, want 1: %+v", len(ids), ids)
	}
	got := ids[0]
	if got.Kind != db.IdentityKindDev {
		t.Errorf("kind = %q, want dev", got.Kind)
	}
	if got.Identifier != auth.DevUser.ID.String() || got.UserID != auth.DevUser.ID {
		t.Errorf("identity = %+v, want the dev UUID", got)
	}
}
