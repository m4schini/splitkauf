// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

// seedAttribution creates one list and two items credited to actor: both items
// added by them, the second also checked (bought) by them. It returns the list
// id for later assertions.
func seedAttribution(t *testing.T, ctx context.Context, conn *sql.DB, actor uuid.UUID) uuid.UUID {
	t.Helper()

	listsRepo := db.NewListsRepository(conn)
	list := mustCreateList(t, ctx, listsRepo, "groceries", actor)

	mustAddItem(t, ctx, listsRepo, list.ID, "milk", 1, "amount", nil, false, actor)
	// checked=true folds the buyer in: bought_by = added_by = actor.
	mustAddItem(t, ctx, listsRepo, list.ID, "bread", 1, "amount", nil, true, actor)

	return list.ID
}

// identityFixture builds a fully populated db.Identity for merge calls;
// LastLogin stays nil because Merge never reads it.
func identityFixture(kind, identifier string, userID uuid.UUID, name, email string) db.Identity {
	return db.Identity{
		Kind:       kind,
		Identifier: identifier,
		UserID:     userID,
		Name:       name,
		Email:      email,
		LastLogin:  nil,
	}
}

// assertMergeResult compares the row counts a Merge reported.
func assertMergeResult(t *testing.T, result db.MergeResult, wantLists, wantAdded, wantBought int64) {
	t.Helper()

	if result.Lists != wantLists || result.Added != wantAdded || result.Bought != wantBought {
		t.Errorf("MergeResult = %+v, want lists %d added %d bought %d", result, wantLists, wantAdded, wantBought)
	}
}

// assertAttributionCounts compares what CountAttribution reports for userID.
func assertAttributionCounts(
	t *testing.T, ctx context.Context, repo *db.IdentityRepository, userID uuid.UUID,
	wantLists, wantAdded, wantBought int,
) {
	t.Helper()

	gotLists, gotAdded, gotBought, err := repo.CountAttribution(ctx, userID)
	if err != nil {
		t.Fatalf("CountAttribution: %v", err)
	}

	if gotLists != wantLists || gotAdded != wantAdded || gotBought != wantBought {
		t.Errorf("attribution = %d/%d/%d, want %d/%d/%d",
			gotLists, gotAdded, gotBought, wantLists, wantAdded, wantBought)
	}
}

// assertNoStaleAttribution fails when any attribution column still references
// the given user id.
func assertNoStaleAttribution(t *testing.T, ctx context.Context, conn *sql.DB, userID uuid.UUID) {
	t.Helper()

	var stale int
	if err := conn.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM lists WHERE created_by = $1)
     + (SELECT count(*) FROM items WHERE added_by = $1)
     + (SELECT count(*) FROM items WHERE bought_by = $1)`, userID).Scan(&stale); err != nil {
		t.Fatalf("count stale attribution: %v", err)
	}

	if stale != 0 {
		t.Errorf("%d attribution rows still reference the source", stale)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityMergeLocalToOIDC(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	user := mustCreateUser(t, ctx, conn, usernameAlex, nameAlex, emailAlex)

	memberRepo := db.NewMemberRepository(conn)
	// The source has logged in via password at least once.
	mustUpsertMember(t, ctx, memberRepo, newMember(user.ID.String(), user.ID, user.Email, user.Name))

	targetID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mustUpsertMember(t, ctx, memberRepo, newMember(subjectOIDC, targetID, emailAlexIdp, nameAlexShort))

	seedAttribution(t, ctx, conn, user.ID)

	source := identityFixture(db.IdentityKindLocal, usernameAlex, user.ID, user.Name, user.Email)
	target := identityFixture(db.IdentityKindOIDC, subjectOIDC, targetID, nameAlexShort, emailAlexIdp)

	result, err := repo.Merge(ctx, source, target)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}

	assertMergeResult(t, result, 1, 2, 1)

	// All attribution now points at the target...
	assertNoStaleAttribution(t, ctx, conn, user.ID)
	assertAttributionCounts(t, ctx, repo, targetID, 1, 2, 1)

	// ...the local account and its members row are gone...
	if _, _, err := db.NewUserRepository(conn).GetByUsername(ctx, usernameAlex); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername after merge = %v, want ErrNotFound", err)
	}

	if _, err := memberRepo.Get(ctx, user.ID.String()); !errors.Is(err, members.ErrNotFound) {
		t.Errorf("source member after merge = %v, want ErrNotFound", err)
	}
	// ...and the target member row is intact.
	if member, err := memberRepo.Get(ctx, subjectOIDC); err != nil || member.UserID != targetID {
		t.Errorf("target member after merge = %+v, %v; want intact row for %v", member, err, targetID)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityMergeOIDCToLocalSeedsTargetMember(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	sourceID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")

	memberRepo := db.NewMemberRepository(conn)
	mustUpsertMember(t, ctx, memberRepo, newMember("oidc-subject-77", sourceID, "old@idp.example", "Old OIDC"))

	// The local target has never logged in: no members row.
	user := mustCreateUser(t, ctx, conn, usernameMaria, nameMaria, "maria@example.com")

	listID := seedAttribution(t, ctx, conn, sourceID)

	source := identityFixture(db.IdentityKindOIDC, "oidc-subject-77", sourceID, "Old OIDC", "old@idp.example")
	target := identityFixture(db.IdentityKindLocal, usernameMaria, user.ID, user.Name, user.Email)

	if _, err := repo.Merge(ctx, source, target); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// The target got a seeded members row keyed by its UUID string...
	seeded, err := memberRepo.Get(ctx, user.ID.String())
	if err != nil {
		t.Fatalf("seeded target member: %v", err)
	}

	if seeded.UserID != user.ID || seeded.Name != nameMaria || seeded.Email != "maria@example.com" {
		t.Errorf("seeded member = %+v, want Maria's values", seeded)
	}
	// ...so the display-name JOIN resolves right away...
	list := mustGetList(t, ctx, db.NewListsRepository(conn), listID)
	assertActor(t, "list CreatedBy", list.CreatedBy, user.ID, nameMaria)

	// ...and the source member row is gone (the users table was never touched).
	if _, err := memberRepo.Get(ctx, "oidc-subject-77"); !errors.Is(err, members.ErrNotFound) {
		t.Errorf("source member after merge = %v, want ErrNotFound", err)
	}
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityCountAttribution(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	actor := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	seedAttribution(t, ctx, conn, actor)

	assertAttributionCounts(t, ctx, repo, actor, 1, 2, 1)

	other := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	assertAttributionCounts(t, ctx, repo, other, 0, 0, 0)
}

//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestIdentityMergeWithZeroAttributionRows(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	user := mustCreateUser(t, ctx, conn, "ghost", "Ghost", "")

	targetID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	mustUpsertMember(t, ctx, db.NewMemberRepository(conn), newMember("oidc-subject-1", targetID, "", "Target"))

	source := identityFixture(db.IdentityKindLocal, "ghost", user.ID, user.Name, "")
	target := identityFixture(db.IdentityKindOIDC, "oidc-subject-1", targetID, "Target", "")

	result, err := repo.Merge(ctx, source, target)
	if err != nil {
		t.Fatalf("Merge with zero attribution: %v", err)
	}

	assertMergeResult(t, result, 0, 0, 0)

	if _, _, err := db.NewUserRepository(conn).GetByUsername(ctx, "ghost"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername after merge = %v, want ErrNotFound", err)
	}
}
