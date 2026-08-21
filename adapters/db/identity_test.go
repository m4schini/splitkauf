// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"database/sql"
	"errors"
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

// seedAttribution creates one list and two items credited to actor: both items
// added by them, the second also checked (bought) by them. It returns the list
// id for later assertions.
func seedAttribution(t *testing.T, conn *sql.DB, ctx context.Context, actor uuid.UUID) uuid.UUID {
	t.Helper()
	listsRepo := db.NewListsRepository(conn)
	l, err := listsRepo.CreateList(ctx, "groceries", actor)
	if err != nil {
		t.Fatalf("create list: %v", err)
	}
	if _, err := listsRepo.AddItem(ctx, l.ID, "milk", 1, "amount", nil, false, actor); err != nil {
		t.Fatalf("add item: %v", err)
	}
	// checked=true folds the buyer in: bought_by = added_by = actor.
	if _, err := listsRepo.AddItem(ctx, l.ID, "bread", 1, "amount", nil, true, actor); err != nil {
		t.Fatalf("add checked item: %v", err)
	}
	return l.ID
}

func TestIdentityMergeLocalToOIDC(t *testing.T) {
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
	memberRepo := db.NewMemberRepository(conn)
	// The source has logged in via password at least once.
	if err := memberRepo.Upsert(ctx, members.Member{
		Subject: u.ID.String(), UserID: u.ID, Email: u.Email, Name: u.Name,
	}); err != nil {
		t.Fatalf("upsert source member: %v", err)
	}
	targetID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if err := memberRepo.Upsert(ctx, members.Member{
		Subject: "oidc-subject-42", UserID: targetID, Email: "alex@idp.example", Name: "Alex S.",
	}); err != nil {
		t.Fatalf("upsert target member: %v", err)
	}
	seedAttribution(t, conn, ctx, u.ID)

	source := db.Identity{Kind: db.IdentityKindLocal, Identifier: "alex", UserID: u.ID, Name: u.Name, Email: u.Email}
	target := db.Identity{Kind: db.IdentityKindOIDC, Identifier: "oidc-subject-42", UserID: targetID, Name: "Alex S.", Email: "alex@idp.example"}

	result, err := repo.Merge(ctx, source, target)
	if err != nil {
		t.Fatalf("Merge: %v", err)
	}
	if result.Lists != 1 || result.Added != 2 || result.Bought != 1 {
		t.Errorf("MergeResult = %+v, want lists 1 added 2 bought 1", result)
	}

	// All attribution now points at the target...
	var stale int
	if err := conn.QueryRowContext(ctx, `
SELECT (SELECT count(*) FROM lists WHERE created_by = $1)
     + (SELECT count(*) FROM items WHERE added_by = $1)
     + (SELECT count(*) FROM items WHERE bought_by = $1)`, u.ID).Scan(&stale); err != nil {
		t.Fatalf("count stale attribution: %v", err)
	}
	if stale != 0 {
		t.Errorf("%d attribution rows still reference the source", stale)
	}
	gotLists, gotAdded, gotBought, err := repo.CountAttribution(ctx, targetID)
	if err != nil {
		t.Fatalf("CountAttribution: %v", err)
	}
	if gotLists != 1 || gotAdded != 2 || gotBought != 1 {
		t.Errorf("target attribution = %d/%d/%d, want 1/2/1", gotLists, gotAdded, gotBought)
	}

	// ...the local account and its members row are gone...
	if _, _, err := db.NewUserRepository(conn).GetByUsername(ctx, "alex"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername after merge = %v, want ErrNotFound", err)
	}
	if _, err := memberRepo.Get(ctx, u.ID.String()); !errors.Is(err, members.ErrNotFound) {
		t.Errorf("source member after merge = %v, want ErrNotFound", err)
	}
	// ...and the target member row is intact.
	if m, err := memberRepo.Get(ctx, "oidc-subject-42"); err != nil || m.UserID != targetID {
		t.Errorf("target member after merge = %+v, %v; want intact row for %v", m, err, targetID)
	}
}

func TestIdentityMergeOIDCToLocalSeedsTargetMember(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	sourceID := uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb")
	memberRepo := db.NewMemberRepository(conn)
	if err := memberRepo.Upsert(ctx, members.Member{
		Subject: "oidc-subject-77", UserID: sourceID, Email: "old@idp.example", Name: "Old OIDC",
	}); err != nil {
		t.Fatalf("upsert source member: %v", err)
	}
	// The local target has never logged in: no members row.
	u, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username:     "maria",
		PasswordHash: "x",
		Name:         "Maria",
		Email:        "maria@example.com",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	listID := seedAttribution(t, conn, ctx, sourceID)

	source := db.Identity{Kind: db.IdentityKindOIDC, Identifier: "oidc-subject-77", UserID: sourceID, Name: "Old OIDC", Email: "old@idp.example"}
	target := db.Identity{Kind: db.IdentityKindLocal, Identifier: "maria", UserID: u.ID, Name: u.Name, Email: u.Email}

	if _, err := repo.Merge(ctx, source, target); err != nil {
		t.Fatalf("Merge: %v", err)
	}

	// The target got a seeded members row keyed by its UUID string...
	m, err := memberRepo.Get(ctx, u.ID.String())
	if err != nil {
		t.Fatalf("seeded target member: %v", err)
	}
	if m.UserID != u.ID || m.Name != "Maria" || m.Email != "maria@example.com" {
		t.Errorf("seeded member = %+v, want Maria's values", m)
	}
	// ...so the display-name JOIN resolves right away...
	l, err := db.NewListsRepository(conn).List(ctx, listID)
	if err != nil {
		t.Fatalf("read list: %v", err)
	}
	if l.CreatedBy == nil || l.CreatedBy.ID != u.ID || l.CreatedBy.Name != "Maria" {
		t.Errorf("list CreatedBy = %+v, want Maria (%v)", l.CreatedBy, u.ID)
	}
	// ...and the source member row is gone (the users table was never touched).
	if _, err := memberRepo.Get(ctx, "oidc-subject-77"); !errors.Is(err, members.ErrNotFound) {
		t.Errorf("source member after merge = %v, want ErrNotFound", err)
	}
}

func TestIdentityResolveUUID(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	u, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username: "alex", PasswordHash: "x", Name: "Alex",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	memberID := uuid.MustParse("cccccccc-cccc-cccc-cccc-cccccccccccc")
	if err := db.NewMemberRepository(conn).Upsert(ctx, members.Member{
		Subject: "oidc-subject-9", UserID: memberID, Name: "Member Only",
	}); err != nil {
		t.Fatalf("upsert member: %v", err)
	}

	local, err := repo.ResolveUUID(ctx, u.ID)
	if err != nil {
		t.Fatalf("ResolveUUID(local): %v", err)
	}
	if local.Kind != db.IdentityKindLocal || local.Identifier != "alex" || local.UserID != u.ID {
		t.Errorf("local = %+v, want local alex %v", local, u.ID)
	}

	member, err := repo.ResolveUUID(ctx, memberID)
	if err != nil {
		t.Fatalf("ResolveUUID(member): %v", err)
	}
	if member.Kind != db.IdentityKindOIDC || member.Identifier != "oidc-subject-9" {
		t.Errorf("member = %+v, want oidc oidc-subject-9", member)
	}

	unknownID := uuid.MustParse("dddddddd-dddd-dddd-dddd-dddddddddddd")
	unknown, err := repo.ResolveUUID(ctx, unknownID)
	if err != nil {
		t.Fatalf("ResolveUUID(unknown): %v", err)
	}
	if unknown.Kind != db.IdentityKindUnknown || unknown.UserID != unknownID {
		t.Errorf("unknown = %+v, want kind unknown with the id set", unknown)
	}
}

func TestIdentityCountAttribution(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	actor := uuid.MustParse("eeeeeeee-eeee-eeee-eeee-eeeeeeeeeeee")
	seedAttribution(t, conn, ctx, actor)

	lists, added, bought, err := repo.CountAttribution(ctx, actor)
	if err != nil {
		t.Fatalf("CountAttribution: %v", err)
	}
	if lists != 1 || added != 2 || bought != 1 {
		t.Errorf("counts = %d/%d/%d, want 1/2/1", lists, added, bought)
	}

	other := uuid.MustParse("ffffffff-ffff-ffff-ffff-ffffffffffff")
	lists, added, bought, err = repo.CountAttribution(ctx, other)
	if err != nil {
		t.Fatalf("CountAttribution(other): %v", err)
	}
	if lists != 0 || added != 0 || bought != 0 {
		t.Errorf("counts for uninvolved id = %d/%d/%d, want zeros", lists, added, bought)
	}
}

func TestIdentityMergeWithZeroAttributionRows(t *testing.T) {
	repo, conn, ctx := newTestIdentityRepo(t)

	u, err := db.NewUserRepository(conn).Create(ctx, users.NewUser{
		Username: "ghost", PasswordHash: "x", Name: "Ghost",
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	targetID := uuid.MustParse("aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa")
	if err := db.NewMemberRepository(conn).Upsert(ctx, members.Member{
		Subject: "oidc-subject-1", UserID: targetID, Name: "Target",
	}); err != nil {
		t.Fatalf("upsert target member: %v", err)
	}

	source := db.Identity{Kind: db.IdentityKindLocal, Identifier: "ghost", UserID: u.ID, Name: u.Name}
	target := db.Identity{Kind: db.IdentityKindOIDC, Identifier: "oidc-subject-1", UserID: targetID, Name: "Target"}

	result, err := repo.Merge(ctx, source, target)
	if err != nil {
		t.Fatalf("Merge with zero attribution: %v", err)
	}
	if result.Lists != 0 || result.Added != 0 || result.Bought != 0 {
		t.Errorf("MergeResult = %+v, want all zeros", result)
	}
	if _, _, err := db.NewUserRepository(conn).GetByUsername(ctx, "ghost"); !errors.Is(err, users.ErrNotFound) {
		t.Errorf("GetByUsername after merge = %v, want ErrNotFound", err)
	}
}
