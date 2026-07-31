// SPDX-License-Identifier: TODO

package db_test

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/members"
)

// newTestMemberRepo opens a MemberRepository against the DSN in
// SPLITKAUF_TEST_DATABASE_DSN, skipping the test when running with -short or when
// the DSN is unset. It TRUNCATEs the members table so each test starts clean
// without dropping or recreating the schema.
func newTestMemberRepo(t *testing.T) (*db.MemberRepository, context.Context) {
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

	if _, err := conn.ExecContext(context.Background(), `TRUNCATE TABLE members`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	return db.NewMemberRepository(conn), context.Background()
}

func TestMemberUpsertInsertThenUpdate(t *testing.T) {
	repo, ctx := newTestMemberRepo(t)

	const subject = "oidc-subject-123"

	// First Upsert inserts a brand-new member.
	if err := repo.Upsert(ctx, members.Member{
		Subject: subject,
		Email:   "alice@example.com",
		Name:    "Alice",
	}); err != nil {
		t.Fatalf("Upsert (insert): %v", err)
	}

	inserted, err := repo.Get(ctx, subject)
	if err != nil {
		t.Fatalf("Get after insert: %v", err)
	}
	if inserted.Email != "alice@example.com" || inserted.Name != "Alice" {
		t.Errorf("inserted = %+v, want email alice@example.com name Alice", inserted)
	}
	if inserted.CreatedAt.IsZero() || inserted.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: created=%v updated=%v", inserted.CreatedAt, inserted.UpdatedAt)
	}

	// Ensure a measurable clock gap so the refreshed updated_at (now()) differs.
	time.Sleep(10 * time.Millisecond)

	// Second Upsert with the SAME subject but changed email/name must UPDATE the
	// existing row (ON CONFLICT) rather than insert a duplicate.
	if err := repo.Upsert(ctx, members.Member{
		Subject: subject,
		Email:   "alice.new@example.com",
		Name:    "Alice Cooper",
	}); err != nil {
		t.Fatalf("Upsert (update): %v", err)
	}

	updated, err := repo.Get(ctx, subject)
	if err != nil {
		t.Fatalf("Get after update: %v", err)
	}
	if updated.Email != "alice.new@example.com" || updated.Name != "Alice Cooper" {
		t.Errorf("updated = %+v, want email alice.new@example.com name Alice Cooper", updated)
	}
	// created_at must be preserved across the ON CONFLICT update...
	if !updated.CreatedAt.Equal(inserted.CreatedAt) {
		t.Errorf("created_at changed on update: was %v, now %v", inserted.CreatedAt, updated.CreatedAt)
	}
	// ...while updated_at must advance.
	if !updated.UpdatedAt.After(inserted.UpdatedAt) {
		t.Errorf("updated_at did not advance: was %v, now %v", inserted.UpdatedAt, updated.UpdatedAt)
	}
	if updated.Subject != subject {
		t.Errorf("subject = %q, want %q", updated.Subject, subject)
	}
}

func TestMemberGetNotFound(t *testing.T) {
	repo, ctx := newTestMemberRepo(t)

	if _, err := repo.Get(ctx, "does-not-exist"); err != members.ErrNotFound {
		t.Errorf("Get err = %v, want ErrNotFound", err)
	}
}
