// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/lists"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

func TestGetMe(t *testing.T) {
	t.Parallel()

	var svc fakeService

	srv := newServer(t, &svc)

	resp := getURL(t, srv.URL+"/api/v1/me")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got v1.User
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Name != "Dev User" {
		t.Errorf("name = %q, want %q", got.Name, "Dev User")
	}

	if got.Id == (uuid.UUID{}) {
		t.Error("dev user id is zero")
	}
	// Dev mode has no email; the field is omitted.
	if got.Email != nil {
		t.Errorf("email = %v, want nil (omitted) in dev mode", *got.Email)
	}
}

// TestGetMeShape checks the /me payload shape for both the with-email and
// without-email cases by driving the handler directly with a context user (the
// authenticator is exercised end-to-end elsewhere).
func TestGetMeShape(t *testing.T) {
	t.Parallel()

	userID := uuid.New()

	t.Run("with email", func(t *testing.T) {
		t.Parallel()

		api := &v1.V1{DB: nil, Service: nil, Events: nil}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
		user := auth.User{ID: userID, Name: "Alice", Email: "alice@example.com"}
		req = req.WithContext(auth.WithUser(req.Context(), user))
		rec := httptest.NewRecorder()
		api.GetMe(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var got v1.User
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got.Id != userID || got.Name != "Alice" {
			t.Errorf("got %+v", got)
		}

		if got.Email == nil || string(*got.Email) != "alice@example.com" {
			t.Errorf("email = %v, want alice@example.com", got.Email)
		}
	})

	t.Run("without email", func(t *testing.T) {
		t.Parallel()

		api := &v1.V1{DB: nil, Service: nil, Events: nil}
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{ID: userID, Name: "Bob", Email: ""}))
		rec := httptest.NewRecorder()
		api.GetMe(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}

		var got v1.User
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}

		if got.Email != nil {
			t.Errorf("email = %v, want nil (omitted)", *got.Email)
		}
	})
}

func TestCreateListHappyPath(t *testing.T) {
	t.Parallel()

	want := makeList(uuid.New(), listNameGroceries, 0, 0)
	want.CreatedAt = time.Now()
	want.UpdatedAt = time.Now()

	var svc fakeService

	svc.createList = func(_ context.Context, name string, actor uuid.UUID) (lists.List, error) {
		if name != listNameGroceries {
			t.Errorf("service got name %q", name)
		}
		// The dev authenticator is the one wired into this server, so the
		// handler must have pulled its user out of the request context.
		if actor != auth.DevUser.ID {
			t.Errorf("service got actor %v, want the dev user %v", actor, auth.DevUser.ID)
		}

		return want, nil
	}

	srv := newServer(t, &svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}

	var got v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Name != listNameGroceries || got.Id != want.ID {
		t.Errorf("got %+v, want name Groceries id %v", got, want.ID)
	}
}

// TestCreateListValidationSurface is the key M1 acceptance criterion: an empty
// body is rejected by the OpenAPI request-validation middleware before the
// handler runs, yielding a 400 application/problem+json validation problem.
func TestCreateListValidationSurface(t *testing.T) {
	t.Parallel()

	var svc fakeService

	// The service must never be called for an invalid request.
	svc.createList = func(_ context.Context, _ string, _ uuid.UUID) (lists.List, error) {
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

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{}`)
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != contentTypeProblem {
		t.Errorf("content-type = %q, want %s", ct, contentTypeProblem)
	}

	var prob struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}

	if prob.Type != "/problems/validation" {
		t.Errorf("type = %q, want /problems/validation", prob.Type)
	}

	if prob.Status != http.StatusBadRequest {
		t.Errorf("problem status = %d, want 400", prob.Status)
	}
}

func TestListLists(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.listsFn = func(_ context.Context) ([]lists.List, error) {
		return []lists.List{
			makeList(uuid.New(), "A", 2, 1),
			makeList(uuid.New(), "B", 0, 0),
		}, nil
	}

	srv := newServer(t, &svc)

	resp := getURL(t, srv.URL+"/api/v1/lists")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got []v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if len(got) != 2 || got[0].OpenItemCount != 2 || got[0].CheckedItemCount != 1 {
		t.Errorf("got %+v", got)
	}
}

func TestGetListNotFound(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.getList = func(_ context.Context, _ uuid.UUID) (lists.List, []lists.Item, error) {
		return lists.List{}, nil, lists.ErrNotFound
	}

	srv := newServer(t, &svc)

	resp := getURL(t, srv.URL+"/api/v1/lists/"+uuid.New().String())
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != contentTypeProblem {
		t.Errorf("content-type = %q, want %s", ct, contentTypeProblem)
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

func TestGetListHappyPath(t *testing.T) {
	t.Parallel()

	listID := uuid.New()
	itemID := uuid.New()

	var svc fakeService

	svc.getList = func(_ context.Context, id uuid.UUID) (lists.List, []lists.Item, error) {
		if id != listID {
			t.Errorf("service got id %v, want %v", id, listID)
		}

		return makeList(listID, listNameGroceries, 1, 0),
			[]lists.Item{makeItem(itemID, listID, itemNameMilk, 1)}, nil
	}

	srv := newServer(t, &svc)

	resp := getURL(t, srv.URL+"/api/v1/lists/"+listID.String())
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	var got v1.ListWithItems
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if got.Id != listID || len(got.Items) != 1 || got.Items[0].Name != itemNameMilk {
		t.Errorf("got %+v", got)
	}
}

func TestDeleteListNoContent(t *testing.T) {
	t.Parallel()

	var svc fakeService

	svc.deleteList = func(_ context.Context, _ uuid.UUID) error { return nil }

	srv := newServer(t, &svc)

	resp := doRequest(t, http.MethodDelete, srv.URL+"/api/v1/lists/"+uuid.New().String(), "", "")
	defer closeBody(t, resp)

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}
