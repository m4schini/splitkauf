// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/users"
)

// testPasswordHash is an opaque bcrypt-shaped placeholder; nothing here
// verifies it.
const testPasswordHash = "$2a$10$abcdefghijklmnopqrstuv"

// newTestUserRepo opens a UserRepository against SPLITKAUF_TEST_DATABASE_DSN,
// skipping under -short or when the DSN is unset. It TRUNCATEs the users table
// so each test starts clean.
func newTestUserRepo(t *testing.T) (*db.UserRepository, context.Context) {
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

	if _, err := conn.ExecContext(context.Background(), `TRUNCATE TABLE users`); err != nil {
		t.Fatalf("truncate: %v", err)
	}

	return db.NewUserRepository(conn), context.Background()
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestUserCreateAndGet(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	created, err := repo.Create(ctx, users.NewUser{
		Username:     usernameAlex,
		PasswordHash: testPasswordHash,
		Name:         nameAlex,
		Email:        emailAlex,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("Create returned a zero id")
	}

	if created.Username != usernameAlex || created.Name != nameAlex || created.Email != emailAlex {
		t.Errorf("Create returned %+v", created)
	}

	got, hash, err := repo.GetByUsername(ctx, usernameAlex)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("GetByUsername id = %s, want %s", got.ID, created.ID)
	}

	if hash != testPasswordHash {
		t.Errorf("GetByUsername returned hash %q", hash)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestUserCreateDuplicateUsername(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	first := users.NewUser{Username: usernameAlex, PasswordHash: "h", Name: "", Email: ""}
	if _, err := repo.Create(ctx, first); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	second := users.NewUser{Username: usernameAlex, PasswordHash: "h2", Name: "", Email: ""}

	_, err := repo.Create(ctx, second)
	if !errors.Is(err, users.ErrUsernameTaken) {
		t.Errorf("duplicate Create err = %v, want ErrUsernameTaken", err)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestUserGetByUsernameNotFound(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	_, _, err := repo.GetByUsername(ctx, "nobody")
	if !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername(nobody) err = %v, want ErrNotFound", err)
	}
}

// TestUserCreateEmptyEmailStoredAsNull confirms an omitted email round-trips as
// an empty string (stored NULL, read back via COALESCE).
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestUserCreateEmptyEmailStoredAsNull(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	created, err := repo.Create(ctx, users.NewUser{Username: "noemail", PasswordHash: "h", Name: "", Email: ""})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.Email != "" {
		t.Errorf("Email = %q, want empty", created.Email)
	}

	got, _, err := repo.GetByUsername(ctx, "noemail")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}

	if got.Email != "" {
		t.Errorf("read-back Email = %q, want empty", got.Email)
	}
}
