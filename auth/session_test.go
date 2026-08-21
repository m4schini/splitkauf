// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/google/uuid"
	"go.uber.org/zap"
)

// seedSession loads a fresh scs session, runs seed against its context, and
// returns the committed session token for use as the request's cookie value.
func seedSession(t *testing.T, sm *scs.SessionManager, seed func(ctx context.Context)) string {
	t.Helper()
	ctx, err := sm.Load(context.Background(), "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	seed(ctx)
	token, _, err := sm.Commit(ctx)
	if err != nil {
		t.Fatalf("Commit: %v", err)
	}
	return token
}

func TestRequireSession(t *testing.T) {
	userID := uuid.MustParse("33333333-3333-3333-3333-333333333333")

	cases := []struct {
		name       string
		seed       func(ctx context.Context, sm *scs.SessionManager) // nil = no session cookie
		wantStatus int
		wantUser   *User
	}{
		{
			name:       "no session",
			seed:       nil,
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "pre-alignment session without user_id",
			seed: func(ctx context.Context, sm *scs.SessionManager) {
				// Hand-built JSON as written before UserID existed in SessionData;
				// it unmarshals with UserID == uuid.Nil and must be rejected.
				sm.Put(ctx, sessionDataKey, []byte(`{"subject":"sub-old","email":"old@example.com","name":"Old"}`))
			},
			wantStatus: http.StatusUnauthorized,
		},
		{
			name: "valid session with user id",
			seed: func(ctx context.Context, sm *scs.SessionManager) {
				if err := putSessionData(ctx, sm, SessionData{
					UserID:  userID,
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

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sm := scs.New()

			var got User
			var gotOK bool
			handler := sm.LoadAndSave(requireSession(sm, zap.NewNop())(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				got, gotOK = UserFrom(r.Context())
				w.WriteHeader(http.StatusOK)
			})))

			req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			if tc.seed != nil {
				token := seedSession(t, sm, func(ctx context.Context) { tc.seed(ctx, sm) })
				req.AddCookie(&http.Cookie{Name: sm.Cookie.Name, Value: token})
			}
			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantUser == nil {
				if gotOK {
					t.Errorf("next handler ran with user %+v, want request rejected", got)
				}
				return
			}
			if !gotOK {
				t.Fatal("next handler did not receive a user in the context")
			}
			if got != *tc.wantUser {
				t.Errorf("injected user = %+v, want %+v", got, *tc.wantUser)
			}
		})
	}
}
