// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// seedSession loads a fresh scs session, runs seed against its context, and
// returns the committed session token for use as the request's cookie value.
func seedSession(t *testing.T, sessions *scs.SessionManager, seed func(ctx context.Context)) string {
	t.Helper()

	ctx, err := sessions.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	seed(ctx)

	token, _, err := sessions.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}

	return token
}

// sessionCookie builds the request cookie carrying a seeded session token. A
// request cookie only transmits name=value, but the security attributes are
// set anyway to model the production cookie shape.
func sessionCookie(name, token string) *http.Cookie {
	return &http.Cookie{
		Name:        name,
		Value:       token,
		Quoted:      false,
		Path:        "",
		Domain:      "",
		Expires:     time.Time{},
		RawExpires:  "",
		MaxAge:      0,
		Secure:      true,
		HttpOnly:    true,
		SameSite:    http.SameSiteLaxMode,
		Partitioned: false,
		Raw:         "",
		Unparsed:    nil,
	}
}

func TestRequireSession(t *testing.T) {
	t.Parallel()

	userID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	cases := []struct {
		name       string
		seed       func(ctx context.Context, sessions *scs.SessionManager) // nil = no session cookie
		wantStatus int
		wantUser   *User
	}{
		{
			name:       "no session",
			seed:       nil,
			wantStatus: http.StatusUnauthorized,
			wantUser:   nil,
		},
		{
			name: "pre-alignment session without user_id",
			seed: func(ctx context.Context, sessions *scs.SessionManager) {
				// Hand-built JSON as written before UserID existed in SessionData;
				// it unmarshals with UserID == uuid.Nil and must be rejected.
				sessions.Put(ctx, sessionDataKey, []byte(`{"subject":"sub-old","email":"old@example.com","name":"Old"}`))
			},
			wantStatus: http.StatusUnauthorized,
			wantUser:   nil,
		},
		{
			name: "valid session with user id",
			seed: func(ctx context.Context, sessions *scs.SessionManager) {
				if err := putSessionData(ctx, sessions, SessionData{
					UserID:  userID,
					IDToken: "",
					Subject: "sub-new",
					Email:   "new@example.com",
					Name:    "New",
				}); err != nil {
					t.Fatalf("putSessionData: %v", err)
				}
			},
			wantStatus: http.StatusOK,
			wantUser:   &User{ID: userID, Name: "New", Email: "new@example.com"},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			sessions := scs.New()

			var (
				got   User
				gotOK bool
			)

			next := http.HandlerFunc(func(res http.ResponseWriter, req *http.Request) {
				got, gotOK = UserFrom(req.Context())

				res.WriteHeader(http.StatusOK)
			})
			handler := sessions.LoadAndSave(requireSession(sessions, zap.NewNop())(next))

			req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/me", nil)

			if testCase.seed != nil {
				token := seedSession(t, sessions, func(ctx context.Context) { testCase.seed(ctx, sessions) })
				req.AddCookie(sessionCookie(sessions.Cookie.Name, token))
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != testCase.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, testCase.wantStatus)
			}

			if testCase.wantUser == nil {
				if gotOK {
					t.Errorf("next handler ran with user %+v, want request rejected", got)
				}

				return
			}

			if !gotOK {
				t.Fatal("next handler did not receive a user in the context")
			}

			if got != *testCase.wantUser {
				t.Errorf("injected user = %+v, want %+v", got, *testCase.wantUser)
			}
		})
	}
}
