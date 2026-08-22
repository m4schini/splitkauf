// SPDX-License-Identifier: CC0-1.0

package db_test

import (
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/members"
)

// TestListAttribution pins the read-time name resolution (US-L.11): the
// creator's id is stored on the list, the display name comes from the members
// join, and a rename of that member changes what past lists report — which is
// the whole reason the name is not snapshotted at write time.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestListAttribution(t *testing.T) {
	memberRepo, ctx := newTestMemberRepo(t)
	repo, _ := newTestRepo(t)

	subject := "subject-for-" + testActor.String()
	mustUpsertMember(t, ctx, memberRepo, newMember(subject, testActor, "", nameAlice))

	created := mustCreateList(t, ctx, repo, "Groceries", testActor)
	assertActor(t, "CreatedBy", created.CreatedBy, testActor, nameAlice)

	// A copy is credited to the copier, and the overview read resolves the name
	// through the same join.
	copied := mustCopyList(t, ctx, repo, created.ID, "Groceries (copy)", testActor)
	assertActor(t, "copy CreatedBy", copied.CreatedBy, testActor, nameAlice)

	// Rename the member: every past attribution must follow.
	mustUpsertMember(t, ctx, memberRepo, newMember(subject, testActor, "", nameAliceRenamed))

	all := mustLists(t, ctx, repo)
	if len(all) != 2 {
		t.Fatalf("Lists = %d, want 2", len(all))
	}

	for _, list := range all {
		assertActor(t, "list "+list.Name+" CreatedBy", list.CreatedBy, testActor, nameAliceRenamed)
	}

	// An actor with no member row still yields an attribution — the id is known,
	// only the name is not. The UI hides the line unless the id is the viewer's.
	stranger := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	orphan := mustCreateList(t, ctx, repo, "Orphan", stranger)
	assertActor(t, "orphan CreatedBy", orphan.CreatedBy, stranger, "")
}

// TestItemAttribution pins the item attributions against real SQL: the adder
// survives every subsequent write, the buyer is written on check and actually
// cleared (not merely hidden) on uncheck, and both names resolve through the
// members join.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestItemAttribution(t *testing.T) {
	memberRepo, ctx := newTestMemberRepo(t)
	repo, _ := newTestRepo(t)

	buyer := uuid.MustParse("22222222-2222-2222-2222-222222222222")
	for _, member := range []members.Member{
		newMember("adder", testActor, "", nameAlice),
		newMember("buyer", buyer, "", nameMaria),
	} {
		mustUpsertMember(t, ctx, memberRepo, member)
	}

	list := mustCreateList(t, ctx, repo, "Groceries", testActor)

	item := mustAddItem(t, ctx, repo, list.ID, "Milk", 1, "l", nil, false, testActor)
	assertActor(t, "AddedBy", item.AddedBy, testActor, nameAlice)

	if item.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v on a new open item, want nil", item.BoughtBy)
	}

	now := time.Now()

	checked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, true, &now, &buyer)
	assertActor(t, "BoughtBy", checked.BoughtBy, buyer, nameMaria)
	assertActor(t, "AddedBy after check", checked.AddedBy, testActor, nameAlice)

	// An edit must not disturb either attribution — the write path re-reads the
	// row rather than returning a RETURNING projection that cannot join.
	updated := mustUpdateItem(t, ctx, repo, list.ID, item.ID, lists.ItemUpdate{
		Name:     new("Oat milk"),
		Quantity: nil,
		Unit:     nil,
		NoteSet:  false,
		Note:     nil,
	})
	assertActor(t, "AddedBy after update", updated.AddedBy, testActor, nameAlice)
	assertActor(t, "BoughtBy after update", updated.BoughtBy, buyer, nameMaria)

	unchecked := mustSetItemChecked(t, ctx, repo, list.ID, item.ID, false, nil, nil)
	if unchecked.BoughtBy != nil {
		t.Errorf("BoughtBy = %+v after uncheck, want nil", unchecked.BoughtBy)
	}
	// Re-read independently: the buyer must be gone from the row itself, not
	// just absent from the response the write happened to build.
	reread := mustGetItem(t, ctx, repo, list.ID, item.ID)
	if reread.BoughtBy != nil {
		t.Errorf("bought_by still set in the database: %+v", reread.BoughtBy)
	}

	assertActor(t, "AddedBy after uncheck", reread.AddedBy, testActor, nameAlice)
}

// TestAddItemCheckedStampsBuyer covers the offline fold: a create that arrives
// already checked credits its adder as the buyer too, because no separate check
// call will ever follow to record one.
//
//nolint:paralleltest // integration tests share one database and truncate tables between tests
func TestAddItemCheckedStampsBuyer(t *testing.T) {
	repo, ctx := newTestRepo(t)

	list := mustCreateList(t, ctx, repo, "Groceries", testActor)

	item := mustAddItem(t, ctx, repo, list.ID, "Milk", 1, "l", nil, true, testActor)
	assertActor(t, "AddedBy", item.AddedBy, testActor, "")
	assertActor(t, "BoughtBy", item.BoughtBy, testActor, "")
}
