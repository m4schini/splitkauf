// SPDX-License-Identifier: TODO

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/lists"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// fakeService is an in-memory ListService for hermetic full-stack handler
// tests. Each field, when set, overrides the corresponding method; unset
// methods return zero values. This lets a test wire only what it exercises.
type fakeService struct {
	createList  func(ctx context.Context, name string, actor uuid.UUID) (lists.List, error)
	listsFn     func(ctx context.Context) ([]lists.List, error)
	getList     func(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error)
	renameList  func(ctx context.Context, id uuid.UUID, name string) (lists.List, error)
	deleteList  func(ctx context.Context, id uuid.UUID) error
	copyList    func(ctx context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error)
	addItem     func(ctx context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool, actor uuid.UUID) (lists.Item, error)
	updateItem  func(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error)
	deleteItem  func(ctx context.Context, listID, itemID uuid.UUID) error
	restoreItem func(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
	checkItem   func(ctx context.Context, listID, itemID uuid.UUID, actor uuid.UUID) (lists.Item, error)
	uncheckItem func(ctx context.Context, listID, itemID uuid.UUID, actor uuid.UUID) (lists.Item, error)
}

func (f *fakeService) CreateList(ctx context.Context, name string, actor uuid.UUID) (lists.List, error) {
	return f.createList(ctx, name, actor)
}

func (f *fakeService) Lists(ctx context.Context) ([]lists.List, error) {
	return f.listsFn(ctx)
}

func (f *fakeService) GetList(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error) {
	return f.getList(ctx, id)
}

func (f *fakeService) RenameList(ctx context.Context, id uuid.UUID, name string) (lists.List, error) {
	return f.renameList(ctx, id, name)
}

func (f *fakeService) DeleteList(ctx context.Context, id uuid.UUID) error {
	return f.deleteList(ctx, id)
}

func (f *fakeService) CopyList(ctx context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error) {
	return f.copyList(ctx, id, name, actor)
}

func (f *fakeService) AddItem(ctx context.Context, listID uuid.UUID, name string, quantity int, unit string, note *string, checked bool, actor uuid.UUID) (lists.Item, error) {
	return f.addItem(ctx, listID, name, quantity, unit, note, checked, actor)
}

func (f *fakeService) UpdateItem(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error) {
	return f.updateItem(ctx, listID, itemID, update)
}

func (f *fakeService) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	return f.deleteItem(ctx, listID, itemID)
}

func (f *fakeService) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	return f.restoreItem(ctx, listID, itemID)
}

func (f *fakeService) CheckItem(ctx context.Context, listID, itemID uuid.UUID, actor uuid.UUID) (lists.Item, error) {
	return f.checkItem(ctx, listID, itemID, actor)
}

func (f *fakeService) UncheckItem(ctx context.Context, listID, itemID uuid.UUID, actor uuid.UUID) (lists.Item, error) {
	return f.uncheckItem(ctx, listID, itemID, actor)
}

// newServer starts a full-stack dev-mode REST server (via rest.New) backed by
// the given fake service, exercising the real router, session middleware,
// validator, and the dev authenticator (which injects the fixed dev user).
func newServer(t *testing.T, svc v1.ListService) *httptest.Server {
	t.Helper()
	return newServerWithEvents(t, svc, nil)
}

// newServerWithEvents is newServer with an events.Publisher wired into V1 so
// tests can assert the real-time hints handlers emit after a mutation.
func newServerWithEvents(t *testing.T, svc v1.ListService, pub events.Publisher) *httptest.Server {
	t.Helper()
	sm := scs.New()
	authr, err := auth.New(context.Background(), &config.Config{}, sm, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}
	srv := httptest.NewServer(rest.New(&v1.V1{Service: svc, Events: pub}, sm, authr, events.NewBroker()))
	t.Cleanup(srv.Close)
	return srv
}

// capturingPublisher records every published event for assertions. It is safe
// for concurrent use, though these tests publish synchronously in the handler.
type capturingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *capturingPublisher) Publish(e events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.events = append(c.events, e)
}

func (c *capturingPublisher) captured() []events.Event {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]events.Event, len(c.events))
	copy(out, c.events)
	return out
}

// noopMembers is a members.Repository that records nothing; sufficient for the
// hermetic handler tests, which never actually sign a user in.
type noopMembers struct{}

func (noopMembers) Upsert(context.Context, members.Member) error { return nil }
func (noopMembers) Get(context.Context, string) (members.Member, error) {
	return members.Member{}, members.ErrNotFound
}

func TestGetMe(t *testing.T) {
	srv := newServer(t, &fakeService{})

	resp, err := http.Get(srv.URL + "/api/v1/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	defer resp.Body.Close()

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
	id := uuid.New()

	t.Run("with email", func(t *testing.T) {
		v := &v1.V1{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{ID: id, Name: "Alice", Email: "alice@example.com"}))
		rec := httptest.NewRecorder()
		v.GetMe(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		var got v1.User
		if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
			t.Fatalf("decode: %v", err)
		}
		if got.Id != id || got.Name != "Alice" {
			t.Errorf("got %+v", got)
		}
		if got.Email == nil || string(*got.Email) != "alice@example.com" {
			t.Errorf("email = %v, want alice@example.com", got.Email)
		}
	})

	t.Run("without email", func(t *testing.T) {
		v := &v1.V1{}
		req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
		req = req.WithContext(auth.WithUser(req.Context(), auth.User{ID: id, Name: "Bob"}))
		rec := httptest.NewRecorder()
		v.GetMe(rec, req)

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

// TestOIDCUnauthenticated401 is the key auth-mode acceptance check: in OIDC
// mode, a request to a guarded /api/v1 endpoint with no session is rejected by
// RequireAuth with a 401 application/problem+json unauthorized problem — before
// the handler runs. The provider is a local discovery stub (no live IdP).
func TestOIDCUnauthenticated401(t *testing.T) {
	sm := scs.New()

	cfg := &config.Config{}
	cfg.Auth.OIDC.Issuer = newDiscoveryServer(t)
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"
	if !cfg.IsOIDCEnabled() {
		t.Fatal("test config is not OIDC-enabled")
	}

	authr, err := auth.New(context.Background(), cfg, sm, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (oidc): %v", err)
	}

	// The service must never be reached for an unauthenticated request.
	svc := &fakeService{listsFn: func(context.Context) ([]lists.List, error) {
		t.Fatal("service should not be called for an unauthenticated request")
		return nil, nil
	}}
	srv := httptest.NewServer(rest.New(&v1.V1{Service: svc}, sm, authr, events.NewBroker()))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/lists")
	if err != nil {
		t.Fatalf("GET /lists: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
	var prob struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if prob.Type != "/problems/unauthorized" {
		t.Errorf("type = %q, want /problems/unauthorized", prob.Type)
	}
	if prob.Status != http.StatusUnauthorized {
		t.Errorf("problem status = %d, want 401", prob.Status)
	}
}

// TestOIDCHealthPublic proves that even in OIDC mode the health endpoint stays
// publicly reachable: GET /api/v1/health returns 200 with no session, while a
// guarded route (GET /api/v1/lists) is still rejected with 401. This is the
// wiring-layer carve-out in rest.New that bypasses RequireAuth for health only.
func TestOIDCHealthPublic(t *testing.T) {
	sm := scs.New()

	cfg := &config.Config{}
	cfg.Auth.OIDC.Issuer = newDiscoveryServer(t)
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"
	if !cfg.IsOIDCEnabled() {
		t.Fatal("test config is not OIDC-enabled")
	}

	authr, err := auth.New(context.Background(), cfg, sm, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (oidc): %v", err)
	}

	svc := &fakeService{listsFn: func(context.Context) ([]lists.List, error) {
		t.Fatal("service should not be called for an unauthenticated request")
		return nil, nil
	}}
	srv := httptest.NewServer(rest.New(&v1.V1{Service: svc}, sm, authr, events.NewBroker()))
	t.Cleanup(srv.Close)

	// Health is public: 200 even with no session.
	health, err := http.Get(srv.URL + "/api/v1/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer health.Body.Close()
	if health.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", health.StatusCode)
	}

	// A guarded route is still rejected: 401.
	guarded, err := http.Get(srv.URL + "/api/v1/lists")
	if err != nil {
		t.Fatalf("GET /lists: %v", err)
	}
	defer guarded.Body.Close()
	if guarded.StatusCode != http.StatusUnauthorized {
		t.Fatalf("lists status = %d, want 401", guarded.StatusCode)
	}
}

// newDiscoveryServer starts an httptest server serving a minimal OIDC discovery
// document so auth.New's provider discovery succeeds without a live IdP.
func newDiscoveryServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	var issuer string
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, _ *http.Request) {
		doc := map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL
	return issuer
}

func TestCreateListHappyPath(t *testing.T) {
	want := lists.List{ID: uuid.New(), Name: "Groceries", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	svc := &fakeService{createList: func(_ context.Context, name string, actor uuid.UUID) (lists.List, error) {
		if name != "Groceries" {
			t.Errorf("service got name %q", name)
		}
		// The dev authenticator is the one wired into this server, so the
		// handler must have pulled its user out of the request context.
		if actor != auth.DevUser.ID {
			t.Errorf("service got actor %v, want the dev user %v", actor, auth.DevUser.ID)
		}
		return want, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got v1.List
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "Groceries" || got.Id != want.ID {
		t.Errorf("got %+v, want name Groceries id %v", got, want.ID)
	}
}

// TestCreateListValidationSurface is the key M1 acceptance criterion: an empty
// body is rejected by the OpenAPI request-validation middleware before the
// handler runs, yielding a 400 application/problem+json validation problem.
func TestCreateListValidationSurface(t *testing.T) {
	// The service must never be called for an invalid request.
	svc := &fakeService{createList: func(_ context.Context, _ string, _ uuid.UUID) (lists.List, error) {
		t.Fatal("service should not be called for invalid body")
		return lists.List{}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
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
	svc := &fakeService{listsFn: func(_ context.Context) ([]lists.List, error) {
		return []lists.List{
			{ID: uuid.New(), Name: "A", OpenItemCount: 2, CheckedItemCount: 1},
			{ID: uuid.New(), Name: "B"},
		}, nil
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists")
	if err != nil {
		t.Fatalf("GET /lists: %v", err)
	}
	defer resp.Body.Close()
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
	svc := &fakeService{getList: func(_ context.Context, _ uuid.UUID) (lists.List, []lists.Item, error) {
		return lists.List{}, nil, lists.ErrNotFound
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists/" + uuid.New().String())
	if err != nil {
		t.Fatalf("GET /lists/{id}: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
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
	listID := uuid.New()
	itemID := uuid.New()
	svc := &fakeService{getList: func(_ context.Context, id uuid.UUID) (lists.List, []lists.Item, error) {
		if id != listID {
			t.Errorf("service got id %v, want %v", id, listID)
		}
		return lists.List{ID: listID, Name: "Groceries", OpenItemCount: 1},
			[]lists.Item{{ID: itemID, ListID: listID, Name: "milk", Quantity: 1}}, nil
	}}
	srv := newServer(t, svc)

	resp, err := http.Get(srv.URL + "/api/v1/lists/" + listID.String())
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got v1.ListWithItems
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Id != listID || len(got.Items) != 1 || got.Items[0].Name != "milk" {
		t.Errorf("got %+v", got)
	}
}

func TestAddItemHappyPath(t *testing.T) {
	listID := uuid.New()
	svc := &fakeService{addItem: func(_ context.Context, lid uuid.UUID, name string, qty int, unit string, note *string, checked bool, _ uuid.UUID) (lists.Item, error) {
		if lid != listID || name != "milk" || qty != 2 || unit != "l" {
			t.Errorf("service got lid=%v name=%q qty=%d unit=%q", lid, name, qty, unit)
		}
		return lists.Item{ID: uuid.New(), ListID: listID, Name: name, Quantity: qty, Unit: unit, Note: note, Checked: checked}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","quantity":2,"unit":"l"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Name != "milk" || got.Quantity != 2 || got.Unit != v1.L {
		t.Errorf("got %+v", got)
	}
}

// TestAddItemInvalidUnit400 pins that an unrecognised unit token is rejected by
// the OpenAPI enum validator before the handler runs, yielding a 400
// application/problem+json validation problem. The service is never called.
func TestAddItemInvalidUnit400(t *testing.T) {
	listID := uuid.New()
	svc := &fakeService{addItem: func(_ context.Context, _ uuid.UUID, _ string, _ int, _ string, _ *string, _ bool, _ uuid.UUID) (lists.Item, error) {
		t.Fatal("service should not be called for an invalid unit")
		return lists.Item{}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","unit":"furlong"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
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
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{updateItem: func(_ context.Context, _, _ uuid.UUID, up lists.ItemUpdate) (lists.Item, error) {
		if up.Unit == nil || *up.Unit != "kg" {
			t.Errorf("service got unit = %v, want kg", up.Unit)
		}
		return lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1, Unit: *up.Unit}, nil
	}}
	srv := newServer(t, svc)

	req, _ := http.NewRequest(http.MethodPatch, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String(),
		bytes.NewBufferString(`{"unit":"kg"}`))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("PATCH: %v", err)
	}
	defer resp.Body.Close()
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
	listID, itemID := uuid.New(), uuid.New()
	now := time.Now()
	svc := &fakeService{checkItem: func(_ context.Context, lid, iid uuid.UUID, _ uuid.UUID) (lists.Item, error) {
		if lid != listID || iid != itemID {
			t.Errorf("service got lid=%v iid=%v", lid, iid)
		}
		return lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1, Checked: true, CheckedAt: &now}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/check", "")
	defer resp.Body.Close()
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
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{restoreItem: func(_ context.Context, lid, iid uuid.UUID) (lists.Item, error) {
		if lid != listID || iid != itemID {
			t.Errorf("service got lid=%v iid=%v", lid, iid)
		}
		return lists.Item{ID: itemID, ListID: listID, Name: "milk", Quantity: 1}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got v1.Item
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Id != itemID || got.Name != "milk" {
		t.Errorf("got %+v", got)
	}
}

func TestRestoreItemNotFound(t *testing.T) {
	listID, itemID := uuid.New(), uuid.New()
	svc := &fakeService{restoreItem: func(_ context.Context, _, _ uuid.UUID) (lists.Item, error) {
		return lists.Item{}, lists.ErrNotFound
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items/"+itemID.String()+"/restore", "")
	defer resp.Body.Close()
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
	listID := uuid.New()
	svc := &fakeService{addItem: func(_ context.Context, lid uuid.UUID, name string, qty int, unit string, note *string, checked bool, _ uuid.UUID) (lists.Item, error) {
		if !checked {
			t.Errorf("service got checked=false, want true")
		}
		now := time.Now()
		return lists.Item{ID: uuid.New(), ListID: lid, Name: name, Quantity: qty, Unit: unit, Note: note, Checked: checked, CheckedAt: &now}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+listID.String()+"/items", `{"name":"milk","checked":true}`)
	defer resp.Body.Close()
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

func TestDeleteListNoContent(t *testing.T) {
	svc := &fakeService{deleteList: func(_ context.Context, _ uuid.UUID) error { return nil }}
	srv := newServer(t, svc)

	req, _ := http.NewRequest(http.MethodDelete, srv.URL+"/api/v1/lists/"+uuid.New().String(), nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("DELETE: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", resp.StatusCode)
	}
}

// TestCopyListWithoutBody covers the body-less copy (the UI's one-tap case):
// the request carries no name, so the service is called with an empty one and
// derives the default itself.
func TestCopyListWithoutBody(t *testing.T) {
	sourceID := uuid.New()
	want := lists.List{ID: uuid.New(), Name: "Groceries (copy)", OpenItemCount: 3, CreatedAt: time.Now(), UpdatedAt: time.Now()}
	svc := &fakeService{copyList: func(_ context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error) {
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
	}}
	srv := newServer(t, svc)

	resp, err := http.Post(srv.URL+"/api/v1/lists/"+sourceID.String()+"/copy", "", nil) //nolint:gosec // url is test-controlled (httptest server)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

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
	sourceID := uuid.New()
	svc := &fakeService{copyList: func(_ context.Context, _ uuid.UUID, name string, _ uuid.UUID) (lists.List, error) {
		if name != "Party" {
			t.Errorf("service got name %q, want %q", name, "Party")
		}
		return lists.List{ID: uuid.New(), Name: name}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+sourceID.String()+"/copy", `{"name":"Party"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}

// TestCopyListNotFound maps a missing source list to a 404 problem.
func TestCopyListNotFound(t *testing.T) {
	svc := &fakeService{copyList: func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		return lists.List{}, lists.ErrNotFound
	}}
	srv := newServer(t, svc)

	resp, err := http.Post(srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", "", nil) //nolint:gosec // url is test-controlled (httptest server)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
}

// TestCopyListRejectsEmptyName pins that the OpenAPI validator (minLength: 1)
// rejects an explicitly empty name before the handler runs.
func TestCopyListRejectsEmptyName(t *testing.T) {
	svc := &fakeService{copyList: func(_ context.Context, _ uuid.UUID, _ string, _ uuid.UUID) (lists.List, error) {
		t.Fatal("service should not be called for invalid body")
		return lists.List{}, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists/"+uuid.New().String()+"/copy", `{"name":""}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", resp.StatusCode)
	}
}

// postJSON POSTs a JSON body and returns the response; the caller closes it.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()
	resp, err := http.Post(url, "application/json", bytes.NewBufferString(body)) //nolint:gosec // url is test-controlled (httptest server)
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	return resp
}
