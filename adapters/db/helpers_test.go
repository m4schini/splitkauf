// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/adapters/db"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/members"
)

// Shared fixture values, hoisted to constants so every test file spells them
// identically.
const (
	usernameAlex     = "alex"
	nameAlex         = "Alex"
	emailAlex        = "alex@example.com"
	usernameMaria    = "maria"
	nameMaria        = "Maria"
	nameAlice        = "Alice"
	nameAliceRenamed = "Alice Cooper"
	subjectOIDC      = "oidc-subject-42"
	emailAlexIdp     = "alex@idp.example"
	nameAlexShort    = "Alex S."
	noteWhole        = "whole"
)

// testActor stands in for the authenticated user whose id the REST layer
// passes down to every attributing write.
//
//nolint:gochecknoglobals // shared fixture id used across every integration test file
var testActor = uuid.MustParse("11111111-1111-1111-1111-111111111111")

// newMember builds a fully populated members.Member fixture; the timestamps
// stay zero because the database stamps them on write.
func newMember(subject string, userID uuid.UUID, email, name string) members.Member {
	return members.Member{
		Subject:   subject,
		UserID:    userID,
		Email:     email,
		Name:      name,
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
}

// mustUpsertMember upserts the member, failing the test on error.
func mustUpsertMember(t *testing.T, ctx context.Context, repo *db.MemberRepository, member members.Member) {
	t.Helper()

	if err := repo.Upsert(ctx, member); err != nil {
		t.Fatalf("Upsert member %q: %v", member.Name, err)
	}
}

// mustCreateList creates a list, failing the test on error.
func mustCreateList(
	t *testing.T, ctx context.Context, repo *db.ListsRepository, name string, actor uuid.UUID,
) lists.List {
	t.Helper()

	list, err := repo.CreateList(ctx, name, actor)
	if err != nil {
		t.Fatalf("CreateList(%q): %v", name, err)
	}

	return list
}

// mustGetList reads a list, failing the test on error.
func mustGetList(t *testing.T, ctx context.Context, repo *db.ListsRepository, listID uuid.UUID) lists.List {
	t.Helper()

	list, err := repo.List(ctx, listID)
	if err != nil {
		t.Fatalf("List(%v): %v", listID, err)
	}

	return list
}

// mustLists reads the list overview, failing the test on error.
func mustLists(t *testing.T, ctx context.Context, repo *db.ListsRepository) []lists.List {
	t.Helper()

	all, err := repo.Lists(ctx)
	if err != nil {
		t.Fatalf("Lists: %v", err)
	}

	return all
}

// mustCopyList copies a list, failing the test on error.
func mustCopyList(
	t *testing.T, ctx context.Context, repo *db.ListsRepository, sourceID uuid.UUID, name string, actor uuid.UUID,
) lists.List {
	t.Helper()

	copied, err := repo.CopyList(ctx, sourceID, name, actor)
	if err != nil {
		t.Fatalf("CopyList(%q): %v", name, err)
	}

	return copied
}

// mustAddItem adds an item, failing the test on error.
func mustAddItem(
	t *testing.T,
	ctx context.Context,
	repo *db.ListsRepository,
	listID uuid.UUID,
	name string,
	quantity int,
	unit string,
	note *string,
	checked bool,
	actor uuid.UUID,
) lists.Item {
	t.Helper()

	item, err := repo.AddItem(ctx, listID, name, quantity, unit, note, checked, actor)
	if err != nil {
		t.Fatalf("AddItem(%q): %v", name, err)
	}

	return item
}

// mustGetItem reads an item, failing the test on error.
func mustGetItem(t *testing.T, ctx context.Context, repo *db.ListsRepository, listID, itemID uuid.UUID) lists.Item {
	t.Helper()

	item, err := repo.Item(ctx, listID, itemID)
	if err != nil {
		t.Fatalf("Item(%v): %v", itemID, err)
	}

	return item
}

// mustListItems reads a list's items, failing the test on error.
func mustListItems(t *testing.T, ctx context.Context, repo *db.ListsRepository, listID uuid.UUID) []lists.Item {
	t.Helper()

	items, err := repo.ListItems(ctx, listID)
	if err != nil {
		t.Fatalf("ListItems(%v): %v", listID, err)
	}

	return items
}

// mustUpdateItem applies a partial update, failing the test on error.
func mustUpdateItem(
	t *testing.T, ctx context.Context, repo *db.ListsRepository, listID, itemID uuid.UUID, update lists.ItemUpdate,
) lists.Item {
	t.Helper()

	item, err := repo.UpdateItem(ctx, listID, itemID, update)
	if err != nil {
		t.Fatalf("UpdateItem(%v): %v", itemID, err)
	}

	return item
}

// mustSetItemChecked writes an item's checked state, failing the test on error.
func mustSetItemChecked(
	t *testing.T,
	ctx context.Context,
	repo *db.ListsRepository,
	listID, itemID uuid.UUID,
	checked bool,
	checkedAt *time.Time,
	checkedBy *uuid.UUID,
) lists.Item {
	t.Helper()

	item, err := repo.SetItemChecked(ctx, listID, itemID, checked, checkedAt, checkedBy)
	if err != nil {
		t.Fatalf("SetItemChecked(%v, %v): %v", itemID, checked, err)
	}

	return item
}

// mustRestoreItem clears an item's soft delete, failing the test on error.
func mustRestoreItem(t *testing.T, ctx context.Context, repo *db.ListsRepository, listID, itemID uuid.UUID) lists.Item {
	t.Helper()

	item, err := repo.RestoreItem(ctx, listID, itemID)
	if err != nil {
		t.Fatalf("RestoreItem(%v): %v", itemID, err)
	}

	return item
}

// assertActor fails the test when got is nil or differs from the wanted
// attribution. An empty wantName asserts an attribution whose id has no
// member row to resolve a name from.
func assertActor(t *testing.T, label string, got *lists.Actor, wantID uuid.UUID, wantName string) {
	t.Helper()

	if got == nil {
		t.Fatalf("%s = nil, want {%v %q}", label, wantID, wantName)
	}

	if got.ID != wantID || got.Name != wantName {
		t.Errorf("%s = %+v, want {%v %q}", label, got, wantID, wantName)
	}
}
