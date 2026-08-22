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

// mustCreateUser inserts a local account, failing the test on error. The
// password hash is an opaque placeholder; nothing here verifies it.
func mustCreateUser(t *testing.T, ctx context.Context, conn *sql.DB, username, name, email string) users.User {
	t.Helper()

	user, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username:     username,
		PasswordHash: "x",
		Name:         name,
		Email:        email,
	})
	if err != nil {
		t.Fatalf("create user %q: %v", username, err)
	}

	return user
}

// mustListOneIdentity lists all identities and fails the test unless exactly
// one came back, returning it.
func mustListOneIdentity(t *testing.T, ctx context.Context, repo *db.IdentityRepository) db.Identity {
	t.Helper()

	ids, err := repo.List(ctx)
	if err != nil {
		t.Fatalf("List: %v", err)
	}

	if len(ids) != 1 {
		t.Fatalf("List returned %d identities, want 1: %+v", len(ids), ids)
	}

	return ids[0]
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityListLocalNeverLoggedIn(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	user := mustCreateUser(t, ctx, conn, usernameMaria, nameMaria, "")

	got := mustListOneIdentity(t, ctx, repo)
	if got.Kind != db.IdentityKindLocal || got.Identifier != usernameMaria || got.UserID != user.ID {
		t.Errorf("identity = %+v, want local maria %v", got, user.ID)
	}

	if got.Name != nameMaria || got.Email != "" {
		t.Errorf("name/email = %q/%q, want Maria/empty", got.Name, got.Email)
	}

	if got.LastLogin != nil {
		t.Errorf("LastLogin = %v, want nil (never logged in)", got.LastLogin)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityListLocalWithMemberRowJoinsToOneEntry(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	user := mustCreateUser(t, ctx, conn, usernameAlex, nameAlex, emailAlex)
	// A password login upserts a members row keyed by the user UUID string.
	mustUpsertMember(t, ctx, db.NewMemberRepository(conn),
		newMember(user.ID.String(), user.ID, user.Email, user.Name))

	got := mustListOneIdentity(t, ctx, repo)
	if got.Kind != db.IdentityKindLocal || got.Identifier != usernameAlex || got.UserID != user.ID {
		t.Errorf("identity = %+v, want local alex %v", got, user.ID)
	}

	if got.LastLogin == nil {
		t.Errorf("LastLogin = nil, want the members updated_at")
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityListOIDCOnlyMember(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	userID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mustUpsertMember(t, ctx, db.NewMemberRepository(conn),
		newMember(subjectOIDC, userID, emailAlexIdp, nameAlexShort))

	got := mustListOneIdentity(t, ctx, repo)
	if got.Kind != db.IdentityKindOIDC || got.Identifier != subjectOIDC || got.UserID != userID {
		t.Errorf("identity = %+v, want oidc oidc-subject-42 %v", got, userID)
	}

	if got.Name != nameAlexShort || got.Email != emailAlexIdp {
		t.Errorf("name/email = %q/%q, want provider values", got.Name, got.Email)
	}

	if got.LastLogin == nil {
		t.Errorf("LastLogin = nil, want the members updated_at")
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityListDevMemberClassifiedDev(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	mustUpsertMember(t, ctx, db.NewMemberRepository(conn), auth.DevMember())

	got := mustListOneIdentity(t, ctx, repo)
	if got.Kind != db.IdentityKindDev {
		t.Errorf("kind = %q, want dev", got.Kind)
	}

	if got.Identifier != auth.DevUser.ID.String() || got.UserID != auth.DevUser.ID {
		t.Errorf("identity = %+v, want the dev UUID", got)
	}
}

// mustResolveUUID resolves a raw user id, failing the test on error.
func mustResolveUUID(t *testing.T, ctx context.Context, repo *db.IdentityRepository, userID uuid.UUID) db.Identity {
	t.Helper()

	identity, err := repo.ResolveUUID(ctx, userID)
	if err != nil {
		t.Fatalf("ResolveUUID(%v): %v", userID, err)
	}

	return identity
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityResolveUUID(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	user := mustCreateUser(t, ctx, conn, usernameAlex, nameAlex, "")

	memberID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	mustUpsertMember(t, ctx, db.NewMemberRepository(conn),
		newMember("oidc-subject-9", memberID, "", "Member Only"))

	local := mustResolveUUID(t, ctx, repo, user.ID)
	if local.Kind != db.IdentityKindLocal || local.Identifier != usernameAlex || local.UserID != user.ID {
		t.Errorf("local = %+v, want local alex %v", local, user.ID)
	}

	member := mustResolveUUID(t, ctx, repo, memberID)
	if member.Kind != db.IdentityKindOIDC || member.Identifier != "oidc-subject-9" {
		t.Errorf("member = %+v, want oidc oidc-subject-9", member)
	}

	unknownID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")

	unknown := mustResolveUUID(t, ctx, repo, unknownID)
	if unknown.Kind != db.IdentityKindUnknown || unknown.UserID != unknownID {
		t.Errorf("unknown = %+v, want kind unknown with the id set", unknown)
	}
}
