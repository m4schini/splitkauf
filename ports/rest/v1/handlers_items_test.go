// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

func TestAddItemHappyPath(t *testing.T) {
	t.Parallel()

	listID := uuid.New()

	var svc fakeService

	svc.addItem = func(
		_ context.Context, lid uuid.UUID, name string, qty int,
		unit string, note *string, checked bool, _ uuid.UUID,
	) (lists.Item, error) {
		if lid != listID || name != itemNameMilk || qty != 2 || unit != "l" {
			t.Errorf("service got lid=%v name=%q qty=%d unit=%q", lid, name, qty, unit)
		}

		item := makeItem(uuid.New(), listID, name, qty)
		item.Unit = unit
		item.Note = note
		item.Checked = checked

		return item, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","quantity":2,"unit":"l"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Name != itemNameMilk || got.Quantity != 2 || got.Unit != v1.L {
		t.Errorf("got %+v", got)
	}
}

// TestAddItemInvalidUnit400 pins that an unrecognised unit token is rejected by
// the OpenAPI enum validator before the handler runs, yielding a 400
// application/problem+json validation problem. The service is never called.
func TestAddItemInvalidUnit400(t *testing.T) {
	t.Parallel()

	listID := uuid.New()

	var svc fakeService

	svc.addItem = func(
		_ context.Context, _ uuid.UUID, _ string, _ int,
		_ string, _ *string, _ bool, _ uuid.UUID,
	) (lists.Item, error) {
		t.Fatal("service should not be called for an invalid unit")

		return lists.Item{
			ID:        uuid.UUID{},
			ListID:    uuid.UUID{},
			Name:      "",
			Quantity:  0,
			Unit:      "",
			Note:      nil,
			Checked:   false,
			CheckedAt: nil,
			AddedBy:   nil,
			BoughtBy:  nil,
			CreatedAt: time.Time{},
			UpdatedAt: time.Time{},
		}, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","unit":"furlong"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != contentTypeProblem {
		t.Errorf("content-type = %q, want %s", ct, contentTypeProblem)
	}

	var prob struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	if prob.Type != "/problems/validation" {
		t.Errorf("type = %q, want /problems/validation", prob.Type)
	}
}

// TestUpdateItemUnit maps a unit through the PATCH handler both directions: the
// request unit reaches the service's ItemUpdate, and the returned item's unit
// is serialised back.
func TestUpdateItemUnit(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.updateItem = func(_ context.Context, _, _ uuid.UUID, itemUpdate lists.ItemUpdate) (lists.Item, error) {
		if itemUpdate.Unit == nil || *itemUpdate.Unit != "kg" {
			t.Errorf("service got unit = %v, want kg", itemUpdate.Unit)
		}

		item := makeItem(itemID, listID, itemNameMilk, 1)
		item.Unit = *itemUpdate.Unit

		return item, nil
	}

	srv := newServer(t, &svc)

	resp := patchJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String(), `{"unit":"kg"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Unit != v1.Kg {
		t.Errorf("unit = %q, want kg", got.Unit)
	}
}

func TestCheckItemHappyPath(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()
	now := time.Now()

	var svc fakeService

	svc.checkItem = func(_ context.Context, lid, iid, _ uuid.UUID) (lists.Item, error) {
		if lid != listID || iid != itemID {
			t.Errorf("service got lid=%v iid=%v", lid, iid)
		}

		item := makeItem(itemID, listID, itemNameMilk, 1)
		item.Checked = true
		item.CheckedAt = &now

		return item, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !got.Checked || got.CheckedAt == nil {
		t.Errorf("got %+v, want checked", got)
	}
}

func TestRestoreItemHappyPath(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.restoreItem = func(_ context.Context, lid, iid uuid.UUID) (lists.Item, error) {
		if lid != listID || iid != itemID {
			t.Errorf("service got lid=%v iid=%v", lid, iid)
		}

		return makeItem(itemID, listID, itemNameMilk, 1), nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Id != itemID || got.Name != itemNameMilk {
		t.Errorf("got %+v", got)
	}
}

func TestRestoreItemNotFound(t *testing.T) {
	t.Parallel()

	listID, itemID := uuid.New(), uuid.New()

	var svc fakeService

	svc.restoreItem = func(_ context.Context, _, _ uuid.UUID) (lists.Item, error) {
		return lists.Item{}, lists.ErrNotFound
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	var prob struct {
		Type string `json:"type"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if prob.Type != "/problems/not-found" {
		t.Errorf("type = %q, want /problems/not-found", prob.Type)
	}
}

// TestAddItemChecked round-trips the checked flag: a create body with
// checked=true reaches the service, and the created item reports checked.
func TestAddItemChecked(t *testing.T) {
	t.Parallel()

	listID := uuid.New()

	var svc fakeService

	svc.addItem = func(
		_ context.Context, lid uuid.UUID, name string, qty int,
		unit string, note *string, checked bool, _ uuid.UUID,
	) (lists.Item, error) {
		if !checked {
			t.Errorf("service got checked=false, want true")
		}

		now := time.Now()

		item := makeItem(uuid.New(), lid, name, qty)
		item.Unit = unit
		item.Note = note
		item.Checked = checked
		item.CheckedAt = &now

		return item, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","checked":true}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if !got.Checked || got.CheckedAt == nil {
		t.Errorf("got %+v, want checked with CheckedAt", got)
	}
}
