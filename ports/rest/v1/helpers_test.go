// SPDX-License-Identifier: CC0-1.0

package v1_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
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

// Shared fixture strings, named so goconst is satisfied and a typo in one test
// cannot silently diverge from the others.
const (
	contentTypeJSON    = "application/json"
	contentTypeProblem = "application/problem+json"
	listNameGroceries  = "Groceries"
	itemNameMilk       = "milk"
)

// fakeService is an in-memory ListService for hermetic full-stack handler
// tests. Each field, when set, overrides the corresponding method; unset
// methods return zero values. This lets a test wire only what it exercises.
// Tests build it as a zero value and assign only the overrides they need.
type fakeService struct {
	createList func(ctx context.Context, name string, actor uuid.UUID) (lists.List, error)
	listsFn    func(ctx context.Context) ([]lists.List, error)
	getList    func(ctx context.Context, id uuid.UUID) (lists.List, []lists.Item, error)
	renameList func(ctx context.Context, id uuid.UUID, name string) (lists.List, error)
	deleteList func(ctx context.Context, id uuid.UUID) error
	copyList   func(ctx context.Context, id uuid.UUID, name string, actor uuid.UUID) (lists.List, error)
	addItem    func(
		ctx context.Context, listID uuid.UUID, name string, quantity int,
		unit string, note *string, checked bool, actor uuid.UUID,
	) (lists.Item, error)
	updateItem  func(ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate) (lists.Item, error)
	deleteItem  func(ctx context.Context, listID, itemID uuid.UUID) error
	restoreItem func(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error)
	checkItem   func(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error)
	uncheckItem func(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error)
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

func (f *fakeService) AddItem(
	ctx context.Context, listID uuid.UUID, name string, quantity int,
	unit string, note *string, checked bool, actor uuid.UUID,
) (lists.Item, error) {
	return f.addItem(ctx, listID, name, quantity, unit, note, checked, actor)
}

func (f *fakeService) UpdateItem(
	ctx context.Context, listID, itemID uuid.UUID, update lists.ItemUpdate,
) (lists.Item, error) {
	return f.updateItem(ctx, listID, itemID, update)
}

func (f *fakeService) DeleteItem(ctx context.Context, listID, itemID uuid.UUID) error {
	return f.deleteItem(ctx, listID, itemID)
}

func (f *fakeService) RestoreItem(ctx context.Context, listID, itemID uuid.UUID) (lists.Item, error) {
	return f.restoreItem(ctx, listID, itemID)
}

func (f *fakeService) CheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error) {
	return f.checkItem(ctx, listID, itemID, actor)
}

func (f *fakeService) UncheckItem(ctx context.Context, listID, itemID, actor uuid.UUID) (lists.Item, error) {
	return f.uncheckItem(ctx, listID, itemID, actor)
}

// makeList builds a fully-populated (zero where irrelevant) domain list for
// fixtures; callers tweak the fields a test cares about afterwards.
func makeList(listID uuid.UUID, name string, open, checked int) lists.List {
	return lists.List{
		ID:               listID,
		Name:             name,
		OpenItemCount:    open,
		CheckedItemCount: checked,
		CreatedBy:        nil,
		CreatedAt:        time.Time{},
		UpdatedAt:        time.Time{},
	}
}

// makeItem builds a fully-populated (zero where irrelevant) domain item for
// fixtures; callers tweak the fields a test cares about afterwards.
func makeItem(itemID, listID uuid.UUID, name string, quantity int) lists.Item {
	return lists.Item{
		ID:        itemID,
		ListID:    listID,
		Name:      name,
		Quantity:  quantity,
		Unit:      "",
		Note:      nil,
		Checked:   false,
		CheckedAt: nil,
		AddedBy:   nil,
		BoughtBy:  nil,
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
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

	sessions := scs.New()

	var cfg config.Config

	authr, err := auth.New(t.Context(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}

	srv := httptest.NewServer(rest.New(&v1.V1{DB: nil, Service: svc, Events: pub}, sessions, authr, events.NewBroker()))
	t.Cleanup(srv.Close)

	return srv
}

// capturingPublisher records every published event for assertions. It is safe
// for concurrent use, though these tests publish synchronously in the handler.
type capturingPublisher struct {
	mu     sync.Mutex
	events []events.Event
}

func (c *capturingPublisher) Publish(event events.Event) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.events = append(c.events, event)
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

// doRequest builds a request carrying the test's context, sends it, and
// returns the response; the caller closes the body (see closeBody). An empty
// contentType leaves the header unset; the body is sent verbatim.
func doRequest(t *testing.T, method, url, contentType, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	if err != nil {
		t.Fatalf("new %s request: %v", method, err)
	}

	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}

	return resp
}

// getURL GETs a URL and returns the response; the caller closes the body.
func getURL(t *testing.T, url string) *http.Response {
	t.Helper()

	return doRequest(t, http.MethodGet, url, "", "")
}

// postJSON POSTs a JSON body and returns the response; the caller closes it.
func postJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()

	return doRequest(t, http.MethodPost, url, contentTypeJSON, body)
}

// postNoBody POSTs with neither body nor Content-Type (the "one-tap" client
// case) and returns the response; the caller closes it.
func postNoBody(t *testing.T, url string) *http.Response {
	t.Helper()

	return doRequest(t, http.MethodPost, url, "", "")
}

// patchJSON PATCHes a JSON body and returns the response; the caller closes it.
func patchJSON(t *testing.T, url, body string) *http.Response {
	t.Helper()

	return doRequest(t, http.MethodPatch, url, contentTypeJSON, body)
}

// closeBody drains nothing and closes an HTTP response body, reporting a close
// failure through the test instead of ignoring it.
func closeBody(tb testing.TB, resp *http.Response) {
	tb.Helper()

	if err := resp.Body.Close(); err != nil {
		tb.Errorf("closing response body: %v", err)
	}
}
