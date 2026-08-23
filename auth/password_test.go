// SPDX-License-Identifier: CC0-1.0

package auth_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/members"
	"github.com/m4schini/splitkauf/users"
)

// fakeUsers is an in-memory users.Repository for the password-auth tests.
type fakeUsers struct {
	byName map[string]struct {
		user users.User
		hash string
	}
}

func (f *fakeUsers) Create(context.Context, users.NewUser) (users.User, error) {
	var none users.User

	return none, nil
}

func (f *fakeUsers) GetByUsername(_ context.Context, username string) (users.User, string, error) {
	rec, ok := f.byName[username]
	if !ok {
		return users.User{}, "", users.ErrNotFound
	}

	return rec.user, rec.hash, nil
}

// fakeMembers records upserts.
type fakeMembers struct{ upserted []members.Member }

func (f *fakeMembers) Upsert(_ context.Context, m members.Member) error {
	f.upserted = append(f.upserted, m)

	return nil
}

func (f *fakeMembers) Get(context.Context, string) (members.Member, error) {
	return members.Member{}, members.ErrNotFound
}

// passwordTestServer wires the password authenticator behind LoadAndSave with a
// /login (POST) and a /me (RequireAuth) route, and returns a client with a
// cookie jar so the session cookie flows between requests.
func passwordTestServer(t *testing.T) (*httptest.Server, *http.Client, *fakeMembers) {
	t.Helper()

	hash, err := users.HashPassword("correct horse")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}

	uid := uuid.MustParse("11111111-1111-1111-1111-111111111111")
	alex := users.User{
		ID:        uid,
		Username:  "alex",
		Name:      "Alex",
		Email:     "alex@example.com",
		CreatedAt: time.Time{},
		UpdatedAt: time.Time{},
	}
	usersRepo := &fakeUsers{byName: map[string]struct {
		user users.User
		hash string
	}{
		"alex": {user: alex, hash: hash},
	}}
	membersRec := &fakeMembers{upserted: nil}

	sessions := scs.New()

	var cfg config.Config

	cfg.Auth.Password.Enabled = true

	authr, err := auth.New(context.Background(), &cfg, sessions, membersRec, usersRepo)
	if err != nil {
		t.Fatalf("auth.New (password): %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login", authr.Login)
	mux.Handle("/me", authr.RequireAuth(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
		user, _ := auth.UserFrom(req.Context())
		_, _ = res.Write([]byte(user.ID.String()))
	})))
	srv := httptest.NewServer(sessions.LoadAndSave(mux))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	client := &http.Client{
		Transport: nil,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
		Jar:     jar,
		Timeout: 0,
	}

	return srv, client, membersRec
}

func postLogin(t *testing.T, client *http.Client, url, body string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url+"/login", strings.NewReader(body))
	if err != nil {
		t.Fatalf("building POST /login request: %v", err)
	}

	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}

	return resp
}

func get(t *testing.T, client *http.Client, url string) *http.Response {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("building GET %s request: %v", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}

	return resp
}

func TestPasswordLoginSuccess(t *testing.T) {
	t.Parallel()

	srv, client, membersRec := passwordTestServer(t)

	resp := postLogin(t, client, srv.URL, `{"username":"alex","password":"correct horse"}`)

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}

	// The session cookie now authenticates /me, and the injected user carries
	// the account UUID stored as the session's UserID at login.
	meResp := get(t, client, srv.URL+"/me")

	defer func() { _ = meResp.Body.Close() }()

	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("/me status = %d, want 200", meResp.StatusCode)
	}

	body, err := io.ReadAll(meResp.Body)
	if err != nil {
		t.Fatalf("reading /me body: %v", err)
	}

	if got, want := string(body), "11111111-1111-1111-1111-111111111111"; got != want {
		t.Errorf("/me user id = %q, want %q", got, want)
	}

	if len(membersRec.upserted) != 1 || membersRec.upserted[0].Name != "Alex" {
		t.Errorf("member upsert = %+v, want one upsert for Alex", membersRec.upserted)
	}
}

func TestPasswordLoginWrongPasswordAndUnknownUserAreIndistinguishable(t *testing.T) {
	t.Parallel()

	srv, client, _ := passwordTestServer(t)

	wrongPw := postLogin(t, client, srv.URL, `{"username":"alex","password":"nope nope nope"}`)

	defer func() { _ = wrongPw.Body.Close() }()

	unknown := postLogin(t, client, srv.URL, `{"username":"ghost","password":"nope nope nope"}`)

	defer func() { _ = unknown.Body.Close() }()

	if wrongPw.StatusCode != http.StatusUnauthorized || unknown.StatusCode != http.StatusUnauthorized {
		t.Fatalf("statuses: wrongPw=%d unknown=%d, both want 401", wrongPw.StatusCode, unknown.StatusCode)
	}

	if ct := wrongPw.Header.Get("Content-Type"); ct != unknown.Header.Get("Content-Type") {
		t.Errorf("content types differ: %q vs %q", ct, unknown.Header.Get("Content-Type"))
	}
	// The response bodies must be byte-for-byte identical so the problem detail
	// itself can't be used to tell a wrong password from an unknown user.
	wrongBody, _ := io.ReadAll(wrongPw.Body)

	unknownBody, _ := io.ReadAll(unknown.Body)
	if !bytes.Equal(wrongBody, unknownBody) {
		t.Errorf("response bodies differ:\n wrong-pw: %s\n unknown:  %s", wrongBody, unknownBody)
	}
}

func TestPasswordLoginRejectsUnauthenticatedMe(t *testing.T) {
	t.Parallel()

	srv, client, _ := passwordTestServer(t)

	resp := get(t, client, srv.URL+"/me")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/me without session = %d, want 401", resp.StatusCode)
	}
}

func TestPasswordLoginGetRedirects(t *testing.T) {
	t.Parallel()

	srv, client, _ := passwordTestServer(t)

	resp := get(t, client, srv.URL+"/login")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusFound {
		t.Errorf("GET /login status = %d, want 302", resp.StatusCode)
	}
}

func TestPasswordLoginRejectsMalformedBody(t *testing.T) {
	t.Parallel()

	srv, client, _ := passwordTestServer(t)

	resp := postLogin(t, client, srv.URL, `not json`)

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", resp.StatusCode)
	}
}

// TestPasswordLoginOversizedBodyIs400 proves the handler's own MaxBytesReader
// (these routes bypass the /api/v1 body cap) rejects an oversized body as a
// clean 400, not a 500.
func TestPasswordLoginOversizedBodyIs400(t *testing.T) {
	t.Parallel()

	srv, client, _ := passwordTestServer(t)
	big := `{"username":"alex","password":"` + strings.Repeat("a", 5000) + `"}`

	resp := postLogin(t, client, srv.URL, big)

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized login body status = %d, want 400", resp.StatusCode)
	}
}

// errBrokenSessionStore is what brokenSessionStore reports from Delete.
var errBrokenSessionStore = errors.New("session store unavailable")

// brokenSessionStore is an scs.Store whose Delete always fails, standing in for
// a session store (e.g. Postgres) that is unavailable when logout runs.
type brokenSessionStore struct{}

func (s brokenSessionStore) Delete(string) error { return errBrokenSessionStore }

func (s brokenSessionStore) Find(string) ([]byte, bool, error) { return nil, false, nil }

func (s brokenSessionStore) Commit(string, []byte, time.Time) error { return nil }

// passwordLogoutForTest builds the password authenticator and returns its Logout
// handler together with the session manager it destroys sessions in. A non-nil
// store replaces the default in-memory store.
func passwordLogoutForTest(t *testing.T, store scs.Store) (http.HandlerFunc, *scs.SessionManager) {
	t.Helper()

	sessions := scs.New()
	if store != nil {
		sessions.Store = store
	}

	var cfg config.Config

	cfg.Auth.Password.Enabled = true

	authr, err := auth.New(t.Context(), &cfg, sessions, &fakeMembers{upserted: nil}, &fakeUsers{byName: nil})
	if err != nil {
		t.Fatalf("auth.New (password): %v", err)
	}

	return authr.Logout, sessions
}

// TestPasswordLogoutRedirectsHome pins the success contract: whatever the method
// and whether or not a session was authenticated, Logout destroys the session and
// answers 302 -> "/". There is no method guard despite the doc comment naming a
// form POST, and logging out without a session is a safe no-op, not a 401.
func TestPasswordLogoutRedirectsHome(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		method        string
		authenticated bool
	}{
		{name: "post with authenticated session", method: http.MethodPost, authenticated: true},
		{name: "post without session", method: http.MethodPost, authenticated: false},
		{name: "get without session", method: http.MethodGet, authenticated: false},
		{name: "delete without session", method: http.MethodDelete, authenticated: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			logout, sessions := passwordLogoutForTest(t, nil)
			handler := sessions.LoadAndSave(http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				if testCase.authenticated {
					sessions.Put(req.Context(), "userID", uuid.NewString())
				}

				logout(res, req)
			}))

			req := httptest.NewRequestWithContext(t.Context(), testCase.method, "/auth/logout", nil)
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != http.StatusFound {
				t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusFound, rec.Body.String())
			}

			if got := rec.Header().Get("Location"); got != "/" {
				t.Errorf("Location = %q, want %q", got, "/")
			}
		})
	}
}

// TestPasswordLogoutExpiresSessionCookieAndIsIdempotent follows a real session
// through logout: the LoadAndSave middleware must emit a Set-Cookie clearing the
// session cookie once Destroy marked the session destroyed, and repeating the
// logout with the now-dead cookie must redirect again instead of failing.
func TestPasswordLogoutExpiresSessionCookieAndIsIdempotent(t *testing.T) {
	t.Parallel()

	logout, sessions := passwordLogoutForTest(t, nil)

	login := sessions.LoadAndSave(http.HandlerFunc(func(_ http.ResponseWriter, req *http.Request) {
		sessions.Put(req.Context(), "userID", uuid.NewString())
	}))

	loginRec := httptest.NewRecorder()
	login.ServeHTTP(loginRec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/login", nil))

	cookies := loginRec.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login set %d cookies, want the session cookie", len(cookies))
	}

	session := cookies[0]

	logoutHandler := sessions.LoadAndSave(logout)

	doLogout := func() *httptest.ResponseRecorder {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/logout", nil)
		req.AddCookie(session)

		rec := httptest.NewRecorder()
		logoutHandler.ServeHTTP(rec, req)

		return rec
	}

	first := doLogout()
	if first.Code != http.StatusFound || first.Header().Get("Location") != "/" {
		t.Fatalf("first logout = %d %q, want 302 \"/\"", first.Code, first.Header().Get("Location"))
	}

	cleared := first.Result().Cookies()
	if len(cleared) != 1 {
		t.Fatalf("logout set %d cookies, want the expiring session cookie", len(cleared))
	}

	if got := cleared[0]; got.Name != session.Name || got.Value != "" || got.MaxAge >= 0 {
		t.Errorf("cleared cookie = %+v, want %q with empty value and negative MaxAge", got, session.Name)
	}

	// Logging out again with the same (already destroyed) session still succeeds.
	second := doLogout()
	if second.Code != http.StatusFound || second.Header().Get("Location") != "/" {
		t.Errorf("second logout = %d %q, want 302 \"/\"", second.Code, second.Header().Get("Location"))
	}
}

// TestPasswordLogoutStoreDeleteFailureIsInternal covers the handler's only error
// path: when the session store cannot delete the session, the user must get the
// internal problem response ("destroying session") and no redirect.
func TestPasswordLogoutStoreDeleteFailureIsInternal(t *testing.T) {
	t.Parallel()

	logout, sessions := passwordLogoutForTest(t, brokenSessionStore{})
	handler := sessions.LoadAndSave(logout)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/logout", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d\nbody: %s", rec.Code, http.StatusInternalServerError, rec.Body.String())
	}

	if got := rec.Header().Get("Location"); got != "" {
		t.Errorf("Location = %q, want no redirect on a failed destroy", got)
	}

	if body := rec.Body.String(); !strings.Contains(body, "destroying session") {
		t.Errorf("body = %q, want it to mention %q", body, "destroying session")
	}
}

// TestPasswordLogoutWithoutSessionMiddlewarePanics documents the handler's
// precondition: it must be mounted inside the session manager's LoadAndSave
// middleware, otherwise scs panics instead of returning an error.
func TestPasswordLogoutWithoutSessionMiddlewarePanics(t *testing.T) {
	t.Parallel()

	logout, _ := passwordLogoutForTest(t, nil)

	defer func() {
		if recover() == nil {
			t.Error("Logout outside LoadAndSave did not panic, want a panic from scs")
		}
	}()

	logout(httptest.NewRecorder(), httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/auth/logout", nil))
}
