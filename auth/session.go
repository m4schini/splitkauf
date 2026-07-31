// SPDX-License-Identifier: TODO

package auth

import (
	"context"
	"encoding/json"
	"time"

	"github.com/alexedwards/scs/v2"
)

// sessionDataKey is the single scs key under which the authenticated session's
// tokens and claims are stored as JSON. The browser cookie carries only the
// opaque session id; the SessionData never leaves the server.
const sessionDataKey = "auth_session"

// Pre-login scs keys. These transient values are written before redirecting to
// the identity provider and cleared on callback. They are kept out of
// SessionData because they only exist for the duration of one login round-trip.
const (
	stateKey    = "auth_state"
	nonceKey    = "auth_nonce"
	verifierKey = "auth_pkce_verifier"
	returnToKey = "auth_return_to"
)

// SessionData is the complete server-side authentication state for one signed-in
// session: the OAuth2/OIDC tokens, the access-token expiry used to drive
// proactive refresh, and the subject/email/name claims used to build the
// request-context User and the member upsert. It is stored as JSON under a
// single scs key and is never exposed to the browser.
type SessionData struct {
	AccessToken  string    `json:"access_token"`
	RefreshToken string    `json:"refresh_token"`
	IDToken      string    `json:"id_token"`
	Expiry       time.Time `json:"expiry"`
	Subject      string    `json:"subject"`
	Email        string    `json:"email"`
	Name         string    `json:"name"`
}

// getSessionData returns the SessionData stored in the current session and true,
// or the zero value and false when the session holds none (or holds an
// unparseable value). ctx must carry the scs session (via LoadAndSave).
func getSessionData(ctx context.Context, sm *scs.SessionManager) (SessionData, bool) {
	raw := sm.GetBytes(ctx, sessionDataKey)
	if len(raw) == 0 {
		return SessionData{}, false
	}
	var d SessionData
	if err := json.Unmarshal(raw, &d); err != nil {
		return SessionData{}, false
	}
	return d, true
}

// putSessionData stores d as JSON under the session-data key, replacing any
// previous value.
func putSessionData(ctx context.Context, sm *scs.SessionManager, d SessionData) error {
	raw, err := json.Marshal(d)
	if err != nil {
		return err
	}
	sm.Put(ctx, sessionDataKey, raw)
	return nil
}
