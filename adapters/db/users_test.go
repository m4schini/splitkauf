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

func TestUserCreateAndGet(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	created, err := repo.Create(ctx, users.NewUser{
		Username:     "alex",
		PasswordHash: "$2a$10$abcdefghijklmnopqrstuv", // opaque; not verified here
		Name:         "Alex",
		Email:        "alex@example.com",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	if created.ID.String() == "00000000-0000-0000-0000-000000000000" {
		t.Error("Create returned a zero id")
	}

	if created.Username != "alex" || created.Name != "Alex" || created.Email != "alex@example.com" {
		t.Errorf("Create returned %+v", created)
	}

	got, hash, err := repo.GetByUsername(ctx, "alex")
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("GetByUsername id = %s, want %s", got.ID, created.ID)
	}

	if hash != "$2a$10$abcdefghijklmnopqrstuv" {
		t.Errorf("GetByUsername returned hash %q", hash)
	}
}

func TestUserCreateDuplicateUsername(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	if _, err := repo.Create(ctx, users.NewUser{Username: "alex", PasswordHash: "h"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}

	_, err := repo.Create(ctx, users.NewUser{Username: "alex", PasswordHash: "h2"})
	if !errors.Is(err, users.ErrUsernameTaken) {
		t.Errorf("duplicate Create err = %v, want ErrUsernameTaken", err)
	}
}

func TestUserGetByUsernameNotFound(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	_, _, err := repo.GetByUsername(ctx, "nobody")
	if !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername(nobody) err = %v, want ErrNotFound", err)
	}
}

// TestUserCreateEmptyEmailStoredAsNull confirms an omitted email round-trips as
// an empty string (stored NULL, read back via COALESCE).
func TestUserCreateEmptyEmailStoredAsNull(t *testing.T) {
	repo, ctx := newTestUserRepo(t)

	created, err := repo.Create(ctx, users.NewUser{Username: "noemail", PasswordHash: "h"})
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
