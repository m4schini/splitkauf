// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/lists"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// TestCopyListWithoutBody covers the body-less copy (the UI's one-tap case):
// the request carries no name, so the service is called with an empty one and
// derives the default itself.
func TestCopyListWithoutBody(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()
	want := makeList(uuid.New(), "Groceries (copy)", 3, 0)
	want.CreatedAt = time.Now()
	want.UpdatedAt = time.Now()

	var svc fakeService

	svc.copyList = func(_ context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error) {
		if id != sourceID {
			t.Errorf("service got list id %v, want %v", id, sourceID)
		}

		if name != "" {
			t.Errorf("service got name %q, want empty (no name supplied)", name)
		}

		if actor != auth.DevUser.ID {
			t.Errorf("service got actor %v, want the dev user %v", actor, auth.DevUser.ID)
		}

		return want, nil
	}

	srv := newServer(t, &svc)

	resp := postNoBody(t, srv.URL+"/api/v1/lists/"+sourceID.String()+"/copy")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var got v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Id != want.ID || got.Name != want.Name || got.OpenItemCount != 3 {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestCopyListWithName covers the optional body: a supplied name is forwarded
// to the service verbatim.
func TestCopyListWithName(t *testing.T) {
	t.Parallel()

	sourceID := uuid.New()

	var svc fakeService

	svc.copyList = func(_ context.Context, _ uuid.UUID, name string, _ uuid.UUID) (lists.List, error) {
		if name != "Party" {
			t.Errorf("service got name %q, want %q", name, "Party")
		}

		return makeList(uuid.New(), name, 0, 0), nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+sourceID.String()+"/copy", `{"name":"Party"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

// TestCopyListNotFound maps a missing source list to a 404 problem.
func TestCopyListNotFound(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.copyList = func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return lists.List{}, lists.ErrNotFound
	}

	srv := newServer(t, &svc)

	resp := postNoBody(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != contentTypeProblem {
		t.Errorf("content-type = %q, want %s", ct, contentTypeProblem)
	}
}

// TestCopyListRejectsEmptyName pins that the OpenAPI validator (minLength: 1)
// rejects an explicitly empty name before the handler runs.
func TestCopyListRejectsEmptyName(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.copyList = func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		t.Fatal("service should not be called for invalid body")

		return lists.List{
			ID:               uuid.UUID{},
			Name:             "",
			OpenItemCount:    0,
			CheckedItemCount: 0,
			CreatedBy:        nil,
			CreatedAt:        time.Time{},
			UpdatedAt:        time.Time{},
		}, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", `{"name":""}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}
