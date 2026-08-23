// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/ports/rest/problem"
	"github.com/m4schini/splitkauf/users"
)

// errRenewDelete is returned by the session store when deleting the previous
// token, the only way scs.RenewToken fails.
var errRenewDelete = errors.New("store delete failed")

// errMemberUpsert is returned by the members repository under test.
var errMemberUpsert = errors.New("members upsert failed")

// renewFailStore wraps a session store and can fail the Delete of the old
// token that RenewToken performs, driving the session-fixation failure branch.
type renewFailStore struct {
	scs.Store

	deleteErr error
}

func (s renewFailStore) Delete(token string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

	if err := s.Store.Delete(token); err != nil {
		return fmt.Errorf("delete %q: %w", token, err)
	}

	return nil
}

// recordingMembers (defined in oidc_test.go) is reused here as the
// members.Repository that records every upsert and can fail on demand.

func TestPasswordEstablishSession(t *testing.T) {
	t.Parallel()

	stamp := time.Date(2026, time.August, 22, 12, 0, 0, 0, time.UTC)
	account := users.User{
		ID:        uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		Username:  "alice",
		Name:      "Alice",
		Email:     "alice@example.com",
		CreatedAt: stamp,
		UpdatedAt: stamp,
	}

	cases := []struct {
		name string
		user users.User
		// storeDeleteErr fails RenewToken; upsertErr fails members.Upsert.
		storeDeleteErr error
		upsertErr      error
		want           bool
		wantRenewed    bool
		wantStored     bool
		wantUpserts    int
		// wantDetail names the failing step in the problem response; it is
		// empty when the call is expected to succeed.
		wantDetail string
	}{
		{
			name:           "success",
			user:           account,
			storeDeleteErr: nil,
			upsertErr:      nil,
			want:           true,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "",
		},
		{
			// The function does not validate the user: a nil-UUID account is
			// stored as-is (requireSession is what rejects it later).
			name: "zero-value user is stored unvalidated",
			user: users.User{
				ID:        uuid.Nil,
				Username:  "",
				Name:      "",
				Email:     "",
				CreatedAt: time.Time{},
				UpdatedAt: time.Time{},
			},
			storeDeleteErr: nil,
			upsertErr:      nil,
			want:           true,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "",
		},
		{
			// RenewToken fails first: nothing is stored and no member is upserted.
			name:           "renew token fails",
			user:           account,
			storeDeleteErr: errRenewDelete,
			upsertErr:      nil,
			want:           false,
			wantRenewed:    false,
			wantStored:     false,
			wantUpserts:    0,
			wantDetail:     "establishing session",
		},
		{
			name:           "member upsert fails after session data was stored",
			user:           account,
			storeDeleteErr: nil,
			upsertErr:      errMemberUpsert,
			want:           false,
			wantRenewed:    true,
			wantStored:     true,
			wantUpserts:    1,
			wantDetail:     "recording membership",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sessions := scs.New()
			sessions.Store = renewFailStore{Store: sessions.Store, deleteErr: testCase.storeDeleteErr}
			memberRepo := &recordingMembers{
				mu:    sync.Mutex{},
				err:   testCase.upsertErr,
				calls: 0,
				member: members.Member{
					Subject:   "",
					UserID:    uuid.Nil,
					Email:     "",
					Name:      "",
					CreatedAt: time.Time{},
					UpdatedAt: time.Time{},
				},
			}
			authenticator := &passwordAuthenticator{
				users:   stubUsers{},
				members: memberRepo,
				sm:      sessions,
				logger:  zap.NewNop(),
			}

			// A committed session gives the request an scs-loaded context with a
			// non-empty token, so RenewToken has an old token to delete.
			seeded := seedSession(t, sessions, func(context.Context) {})

			ctx, err := sessions.Load(t.Context(), seeded)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}

			req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/login", nil)
			rec := httptest.NewRecorder()

			if got := authenticator.establishSession(rec, req, testCase.user); got != testCase.want {
				t.Fatalf("establishSession = %v, want %v", got, testCase.want)
			}

			// Session fixation: the token is renewed before anything is stored.
			if renewed := sessions.Token(ctx) != seeded; renewed != testCase.wantRenewed {
				t.Errorf("session token renewed = %v, want %v", renewed, testCase.wantRenewed)
			}

			assertStoredSessionData(t, ctx, sessions, testCase.user, testCase.wantStored)
			assertUpsertedMember(t, memberRepo, testCase.user, testCase.wantUpserts)

			if testCase.want {
				assertNothingWritten(t, rec)

				return
			}

			assertInternalProblem(t, rec, req.URL.Path, testCase.wantDetail)
		})
	}
}

// assertStoredSessionData checks whether the session carries the SessionData
// derived from user, and that it does not when stored is false.
func assertStoredSessionData(
	t *testing.T,
	ctx context.Context,
	sessions *scs.SessionManager,
	user users.User,
	stored bool,
) {
	t.Helper()

	got, ok := getSessionData(ctx, sessions)
	if ok != stored {
		t.Fatalf("session data present = %v, want %v", ok, stored)
	}

	if !stored {
		return
	}

	// Password mode has no OIDC token, so IDToken stays empty.
	want := SessionData{
		UserID:  user.ID,
		IDToken: "",
		Subject: user.ID.String(),
		Email:   user.Email,
		Name:    user.Name,
	}
	if got != want {
		t.Errorf("stored session data = %+v, want %+v", got, want)
	}
}

// assertUpsertedMember checks how often the members repository was called and,
// when it was, that the member mirrors the user with repository-owned
// timestamps left zero.
func assertUpsertedMember(t *testing.T, repo *recordingMembers, user users.User, wantCalls int) {
	t.Helper()

	calls, last := repo.upserted()
	if calls != wantCalls {
		t.Errorf("members.Upsert called %d times, want %d", calls, wantCalls)
	}

	if wantCalls == 0 {
		return
	}

	// Subject is the string form of the same UUID as UserID.
	want := members.Member{
		Subject:   user.ID.String(),
		UserID:    user.ID,
		Email:     user.Email,
		Name:      user.Name,
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
	if last != want {
		t.Errorf("upserted member = %+v, want %+v", last, want)
	}
}

// assertNothingWritten checks the recorder is untouched: on success the caller
// (Login) writes the 204, not establishSession.
func assertNothingWritten(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusOK || rec.Body.Len() != 0 || rec.Header().Get("Content-Type") != "" {
		t.Errorf("response = %d %q (content-type %q), want an untouched recorder",
			rec.Code, rec.Body.String(), rec.Header().Get("Content-Type"))
	}
}

// assertInternalProblem checks the response is the RFC 9457 internal problem
// establishSession writes on failure.
func assertInternalProblem(t *testing.T, rec *httptest.ResponseRecorder, instance, detail string) {
	t.Helper()

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", rec.Code, http.StatusInternalServerError)
	}

	if contentType := rec.Header().Get("Content-Type"); contentType != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", contentType, problem.ContentType)
	}

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem body %q: %v", rec.Body.String(), err)
	}

	if got.Type != problem.Internal.URI() || got.Status != http.StatusInternalServerError {
		t.Errorf("problem type/status = %q/%d, want %q/%d",
			got.Type, got.Status, problem.Internal.URI(), http.StatusInternalServerError)
	}

	// The detail names the step that failed, so the branches stay distinguishable.
	if got.Detail != detail {
		t.Errorf("problem detail = %q, want %q", got.Detail, detail)
	}

	if got.Instance != instance {
		t.Errorf("problem instance = %q, want %q", got.Instance, instance)
	}
}

// errDestroyDelete is returned by the session store when Destroy deletes the
// session token, the only way scs.Destroy fails.
var errDestroyDelete = errors.New("store delete failed")

// destroyFailStore wraps a session store and can fail the delete that
// scs.Destroy performs, driving Logout's failure branch.
type destroyFailStore struct {
	scs.Store

	deleteErr error
}

func (s destroyFailStore) Delete(token string) error {
	if s.deleteErr != nil {
		return s.deleteErr
	}

	if err := s.Store.Delete(token); err != nil {
		return fmt.Errorf("delete %q: %w", token, err)
	}

	return nil
}

// logoutUserID is the account behind the seeded session under test.
func logoutUserID() uuid.UUID {
	return uuid.MustParse("55555555-5555-5555-5555-555555555555")
}

// newPasswordForTest builds the password authenticator over sessions, with
// repositories that no logout path touches.
func newPasswordForTest(sessions *scs.SessionManager) *passwordAuthenticator {
	return &passwordAuthenticator{
		users:   stubUsers{},
		members: noopMembers{},
		sm:      sessions,
		logger:  zap.NewNop(),
	}
}

// seedLogoutSession commits a password session (no ID token) and returns its
// token for use as the request cookie.
func seedLogoutSession(t *testing.T, sessions *scs.SessionManager) string {
	t.Helper()

	return seedSession(t, sessions, func(ctx context.Context) {
		data := SessionData{
			UserID:  logoutUserID(),
			IDToken: "",
			Subject: logoutUserID().String(),
			Email:   "alice@example.com",
			Name:    "Alice",
		}
		if err := putSessionData(ctx, sessions, data); err != nil {
			t.Fatalf("putSessionData: %v", err)
		}
	})
}

func TestPasswordLogout(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		// method pins that Logout, unlike Login, does not branch on it.
		method string
		// seeded sends a committed session cookie with the request.
		seeded bool
		// storeDeleteErr fails the store delete inside sm.Destroy.
		storeDeleteErr error
		wantStatus     int
		wantLocation   string
	}{
		{
			name:           "post destroys the session and redirects home",
			method:         http.MethodPost,
			seeded:         true,
			storeDeleteErr: nil,
			wantStatus:     http.StatusFound,
			wantLocation:   "/",
		},
		{
			// Method-agnostic: a GET logs out exactly like the form POST.
			name:           "get logs out the same way",
			method:         http.MethodGet,
			seeded:         true,
			storeDeleteErr: nil,
			wantStatus:     http.StatusFound,
			wantLocation:   "/",
		},
		{
			// Destroying the empty session of an anonymous visitor succeeds.
			name:           "anonymous visitor is redirected home",
			method:         http.MethodPost,
			seeded:         false,
			storeDeleteErr: nil,
			wantStatus:     http.StatusFound,
			wantLocation:   "/",
		},
		{
			name:           "session store delete fails",
			method:         http.MethodPost,
			seeded:         true,
			storeDeleteErr: errDestroyDelete,
			wantStatus:     http.StatusInternalServerError,
			wantLocation:   "",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sessions := scs.New()
			handler := sessions.LoadAndSave(http.HandlerFunc(newPasswordForTest(sessions).Logout))

			var token string
			if testCase.seeded {
				token = seedLogoutSession(t, sessions)
			}

			// Swapped in after seeding so only the Destroy delete can fail.
			sessions.Store = destroyFailStore{Store: sessions.Store, deleteErr: testCase.storeDeleteErr}

			req := httptest.NewRequestWithContext(t.Context(), testCase.method, "/api/auth/logout", nil)
			if testCase.seeded {
				req.AddCookie(sessionCookie(sessions.Cookie.Name, token))
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, testCase.wantStatus, rec.Body.String())
			}

			if got := rec.Header().Get("Location"); got != testCase.wantLocation {
				t.Errorf("Location = %q, want %q", got, testCase.wantLocation)
			}

			if testCase.wantStatus != http.StatusFound {
				assertDestroyProblem(t, rec, req.URL.Path)
				// The failed destroy left the session intact.
				assertSessionResolvable(t, sessions, token, true)

				return
			}

			if contentType := rec.Header().Get("Content-Type"); contentType == problem.ContentType {
				t.Errorf("Content-Type = %q, want no problem body on a successful logout", contentType)
			}

			assertClearedSessionCookie(t, rec, sessions.Cookie.Name)

			if testCase.seeded {
				assertSessionResolvable(t, sessions, token, false)
			}
		})
	}
}

// TestPasswordLogoutIsIdempotent replays the dead cookie: a second logout is
// still a plain redirect home, not an error.
func TestPasswordLogoutIsIdempotent(t *testing.T) {
	t.Parallel()

	sessions := scs.New()
	handler := sessions.LoadAndSave(http.HandlerFunc(newPasswordForTest(sessions).Logout))
	token := seedLogoutSession(t, sessions)

	for attempt := 1; attempt <= 2; attempt++ {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/logout", nil)
		req.AddCookie(sessionCookie(sessions.Cookie.Name, token))

		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/" {
			t.Fatalf("logout attempt %d = %d %q, want 302 /", attempt, rec.Code, rec.Header().Get("Location"))
		}

		assertSessionResolvable(t, sessions, token, false)
	}
}

// TestPasswordLogoutRequiresSessionMiddleware documents the wiring assumption:
// without scs.LoadAndSave there is no session data in the request context and
// scs panics rather than returning an error.
func TestPasswordLogoutRequiresSessionMiddleware(t *testing.T) {
	t.Parallel()

	defer func() {
		recovered := recover()
		if recovered == nil {
			t.Fatal("Logout without the session middleware did not panic, want scs to reject the bare context")
		}

		if got := fmt.Sprint(recovered); !strings.Contains(got, "no session data in context") {
			t.Errorf("panic = %q, want scs's missing-session-data panic", got)
		}
	}()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/logout", nil)
	newPasswordForTest(scs.New()).Logout(httptest.NewRecorder(), req)
}

// assertDestroyProblem checks the response is the RFC 9457 internal problem
// Logout writes when destroying the session fails.
func assertDestroyProblem(t *testing.T, rec *httptest.ResponseRecorder, instance string) {
	t.Helper()

	if contentType := rec.Header().Get("Content-Type"); contentType != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", contentType, problem.ContentType)
	}

	var got problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decoding problem body %q: %v", rec.Body.String(), err)
	}

	if got.Type != problem.Internal.URI() || got.Status != http.StatusInternalServerError {
		t.Errorf("problem type/status = %q/%d, want %q/%d",
			got.Type, got.Status, problem.Internal.URI(), http.StatusInternalServerError)
	}

	if !strings.Contains(got.Detail, "destroying session") {
		t.Errorf("problem detail = %q, want it to name the destroy step", got.Detail)
	}

	if got.Instance != instance {
		t.Errorf("problem instance = %q, want %q", got.Instance, instance)
	}
}

// assertClearedSessionCookie checks the response tells the browser to drop the
// session cookie: empty value and a negative max-age.
func assertClearedSessionCookie(t *testing.T, rec *httptest.ResponseRecorder, name string) {
	t.Helper()

	for _, cookie := range rec.Result().Cookies() {
		if cookie.Name != name {
			continue
		}

		if cookie.Value != "" || cookie.MaxAge >= 0 {
			t.Errorf("session cookie = %q (max-age %d), want an empty value and a negative max-age",
				cookie.Value, cookie.MaxAge)
		}

		return
	}

	t.Errorf("no Set-Cookie for %q, want the session cookie to be cleared", name)
}

// assertSessionResolvable checks whether the token still loads session data
// from the store, i.e. whether a replayed cookie would still authenticate.
func assertSessionResolvable(t *testing.T, sessions *scs.SessionManager, token string, want bool) {
	t.Helper()

	ctx, err := sessions.Load(context.Background(), token)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, ok := getSessionData(ctx, sessions); ok != want {
		t.Errorf("session data resolvable from token = %v, want %v", ok, want)
	}
}
