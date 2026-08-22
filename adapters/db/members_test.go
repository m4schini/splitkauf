// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"

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

// mustGetMember reads a member by subject, failing the test on error.
func mustGetMember(t *testing.T, ctx context.Context, repo *db.MemberRepository, subject string) members.Member {
	t.Helper()

	member, err := repo.Get(ctx, subject)
	if err != nil {
		t.Fatalf("Get(%q): %v", subject, err)
	}

	return member
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestMemberUpsertInsertThenUpdate(t *testing.T) {
	repo, ctx := newTestMemberRepo(t)

	const subject = "oidc-subject-123"

	userID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")

	// First Upsert inserts a brand-new member.
	mustUpsertMember(t, ctx, repo, newMember(subject, userID, "alice@example.com", nameAlice))

	inserted := mustGetMember(t, ctx, repo, subject)
	assertInsertedMember(t, inserted, userID)

	// Ensure a measurable clock gap so the refreshed updated_at (now()) differs.
	time.Sleep(10 * time.Millisecond)

	// Second Upsert with the SAME subject but changed email/name must UPDATE the
	// existing row (ON CONFLICT) rather than insert a duplicate.
	// The user id is re-stamped too: migration 000007's backfill guesses it from
	// the subject, and a login is what corrects a wrong guess (US-L.11).
	corrected := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	mustUpsertMember(t, ctx, repo, newMember(subject, corrected, "alice.new@example.com", nameAliceRenamed))

	updated := mustGetMember(t, ctx, repo, subject)
	assertUpdatedMember(t, inserted, updated, corrected, subject)
}

// assertInsertedMember verifies the freshly inserted row round-tripped Alice's
// values and got both timestamps stamped.
func assertInsertedMember(t *testing.T, inserted members.Member, userID uuid.UUID) {
	t.Helper()

	if inserted.Email != "alice@example.com" || inserted.Name != nameAlice {
		t.Errorf("inserted = %+v, want email alice@example.com name Alice", inserted)
	}

	if inserted.UserID != userID {
		t.Errorf("user_id = %v, want %v", inserted.UserID, userID)
	}

	if inserted.CreatedAt.IsZero() || inserted.UpdatedAt.IsZero() {
		t.Errorf("timestamps not set: created=%v updated=%v", inserted.CreatedAt, inserted.UpdatedAt)
	}
}

// assertUpdatedMember verifies the ON CONFLICT update rewrote email, name and
// user id while preserving created_at and advancing updated_at.
func assertUpdatedMember(t *testing.T, inserted, updated members.Member, corrected uuid.UUID, subject string) {
	t.Helper()

	if updated.Email != "alice.new@example.com" || updated.Name != nameAliceRenamed {
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

	if updated.UserID != corrected {
		t.Errorf("user_id = %v, want the re-stamped %v", updated.UserID, corrected)
	}

	if updated.Subject != subject {
		t.Errorf("subject = %q, want %q", updated.Subject, subject)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestMemberGetNotFound(t *testing.T) {
	repo, ctx := newTestMemberRepo(t)

	if _, err := repo.Get(ctx, "does-not-exist"); !errors.Is(err, members.ErrNotFound) {
		t.Errorf("Get err = %v, want ErrNotFound", err)
	}
}
