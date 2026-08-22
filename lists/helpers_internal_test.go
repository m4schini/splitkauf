// SPDX-License-Identifier: CC0-1.0

package lists

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

// assertValidationError fails the test unless err is a *ValidationError on the
// expected field.
func assertValidationError(t *testing.T, err error, field string) {
	t.Helper()

	var validationErr *ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("expected *ValidationError, got %v", err)
	}

	if validationErr.Field != field {
		t.Fatalf("expected field %q, got %q", field, validationErr.Field)
	}
}

// newService builds a Service over a fresh fakeRepo, with the service clock
// pinned to the fake's fixed clock so timestamps are deterministic.
func newService() (*Service, *fakeRepo) {
	repo := newFakeRepo()
	svc := NewService(repo)
	svc.now = func() time.Time { return repo.clock }

	return svc, repo
}

// testActor stands in for the authenticated user the REST layer passes down.
func testActor() uuid.UUID {
	return uuid.MustParse("11111111-1111-1111-1111-111111111111")
}

// mustCreateList creates a list as testActor, failing the test on error.
func mustCreateList(t *testing.T, svc *Service, name string) List {
	t.Helper()

	list, err := svc.CreateList(context.Background(), name, testActor())
	if err != nil {
		t.Fatalf("CreateList: %v", err)
	}

	return list
}

// mustAddItem adds an open item (quantity 1, default unit, no note) to the
// list as testActor, failing the test on error.
func mustAddItem(t *testing.T, svc *Service, listID uuid.UUID, name string) Item {
	t.Helper()

	item, err := svc.AddItem(context.Background(), listID, name, 1, "", nil, false, testActor())
	if err != nil {
		t.Fatalf("AddItem: %v", err)
	}

	return item
}

// mustCheckItem checks an item off as actor, failing the test on error.
func mustCheckItem(t *testing.T, svc *Service, listID, itemID, actor uuid.UUID) Item {
	t.Helper()

	item, err := svc.CheckItem(context.Background(), listID, itemID, actor)
	if err != nil {
		t.Fatalf("CheckItem: %v", err)
	}

	return item
}
