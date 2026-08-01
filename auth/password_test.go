// SPDX-License-Identifier: TODO

package auth_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"

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
	return users.User{}, nil
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
	fu := &fakeUsers{byName: map[string]struct {
		user users.User
		hash string
	}{
		"alex": {user: users.User{ID: uid, Username: "alex", Name: "Alex", Email: "alex@example.com"}, hash: hash},
	}}
	fm := &fakeMembers{}

	sm := scs.New()
	authr, err := auth.New(context.Background(),
		&config.Config{Auth: config.AuthConfig{Password: config.PasswordConfig{Enabled: true}}},
		sm, fm, fu)
	if err != nil {
		t.Fatalf("auth.New (password): %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/login", authr.Login)
	mux.Handle("/me", authr.RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, _ := auth.UserFrom(r.Context())
		_, _ = w.Write([]byte(u.ID.String()))
	})))
	srv := httptest.NewServer(sm.LoadAndSave(mux))
	t.Cleanup(srv.Close)

	jar, _ := cookiejar.New(nil)
	return srv, &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}}, fm
}

func postLogin(t *testing.T, client *http.Client, url, body string) *http.Response {
	t.Helper()
	resp, err := client.Post(url+"/login", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("POST /login: %v", err)
	}
	return resp
}

func TestPasswordLoginSuccess(t *testing.T) {
	srv, client, fm := passwordTestServer(t)

	resp := postLogin(t, client, srv.URL, `{"username":"alex","password":"correct horse"}`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("login status = %d, want 204", resp.StatusCode)
	}

	// The session cookie now authenticates /me.
	meResp, err := client.Get(srv.URL + "/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	defer meResp.Body.Close()
	if meResp.StatusCode != http.StatusOK {
		t.Fatalf("/me status = %d, want 200", meResp.StatusCode)
	}
	if len(fm.upserted) != 1 || fm.upserted[0].Name != "Alex" {
		t.Errorf("member upsert = %+v, want one upsert for Alex", fm.upserted)
	}
}

func TestPasswordLoginWrongPasswordAndUnknownUserAreIndistinguishable(t *testing.T) {
	srv, client, _ := passwordTestServer(t)

	wrongPw := postLogin(t, client, srv.URL, `{"username":"alex","password":"nope nope nope"}`)
	defer wrongPw.Body.Close()
	unknown := postLogin(t, client, srv.URL, `{"username":"ghost","password":"nope nope nope"}`)
	defer unknown.Body.Close()

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
	srv, client, _ := passwordTestServer(t)
	resp, err := client.Get(srv.URL + "/me")
	if err != nil {
		t.Fatalf("GET /me: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("/me without session = %d, want 401", resp.StatusCode)
	}
}

func TestPasswordLoginGetRedirects(t *testing.T) {
	srv, client, _ := passwordTestServer(t)
	resp, err := client.Get(srv.URL + "/login")
	if err != nil {
		t.Fatalf("GET /login: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("GET /login status = %d, want 302", resp.StatusCode)
	}
}

func TestPasswordLoginRejectsMalformedBody(t *testing.T) {
	srv, client, _ := passwordTestServer(t)
	resp := postLogin(t, client, srv.URL, `not json`)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("malformed body status = %d, want 400", resp.StatusCode)
	}
}

// TestPasswordLoginOversizedBodyIs400 proves the handler's own MaxBytesReader
// (these routes bypass the /api/v1 body cap) rejects an oversized body as a
// clean 400, not a 500.
func TestPasswordLoginOversizedBodyIs400(t *testing.T) {
	srv, client, _ := passwordTestServer(t)
	big := `{"username":"alex","password":"` + strings.Repeat("a", 5000) + `"}`
	resp := postLogin(t, client, srv.URL, big)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("oversized login body status = %d, want 400", resp.StatusCode)
	}
}
