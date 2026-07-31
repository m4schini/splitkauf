# Research: OIDC Integration for Go Backend + PWA

*Based on training knowledge (cutoff August 2025). Focused on self-hosted providers (Zitadel, Keycloak).*

---

## Architecture: Backend for Frontend (BFF)

The Go backend owns all tokens — the PWA **never** sees an access token, refresh token, or ID token. The browser holds only an opaque session cookie (`HttpOnly`, `Secure`, `SameSite=Lax`). This is the current industry standard per RFC 9700 (OAuth 2.0 Security BCP, February 2025).

```
Browser (PWA)          Go Backend (BFF)           OIDC Provider
     |                       |                    (Zitadel/Keycloak)
     |-- GET /api/auth/login →|                         |
     |                       |-- redirect_uri, PKCE ──→ |
     |<──────── 302 to IdP ──|                         |
     |────────────────────────────────── browser navigates to IdP |
     |<───────── code=... ──────────────────────────── |
     |-- GET /api/auth/callback?code=... →|             |
     |                       |── code exchange (PKCE) →|
     |                       |<── access_token, refresh_token, id_token |
     |                       | stores tokens in server-side session      |
     |<── 302 + Set-Cookie: session=<opaque> ─|        |
     |                       |                         |
     |-- GET /api/lists ─────→|                         |
     |   Cookie: session=...  | read session → token    |
     |                       |── API call with access_token →  (resource server)
```

---

## Go Library Recommendations

### Option A: `coreos/go-oidc/v3` + `golang.org/x/oauth2`
Best for provider-agnostic setups or Keycloak.

```
github.com/coreos/go-oidc/v3          # provider discovery + ID token verification
golang.org/x/oauth2                    # authorization code flow, PKCE, token refresh
github.com/alexedwards/scs/v2          # session manager (preferred over gorilla/sessions)
```

### Option B: `zitadel/oidc/v3/pkg/client/rp`
Higher-level; official Zitadel SDK. Includes handler helpers for login/callback, built-in PKCE, works with Keycloak too.

```
github.com/zitadel/oidc/v3/pkg/client/rp   # RelyingParty with Login/CodeExchange handlers
github.com/alexedwards/scs/v2               # session manager
```

---

## PKCE (Mandatory per RFC 9700)

```go
verifier := oauth2.GenerateVerifier()
// store verifier in session before redirect

// auth URL
url := config.AuthCodeURL(state,
    oauth2.S256ChallengeOption(verifier),
    oauth2.SetAuthURLParam("nonce", nonce),
)

// token exchange in callback
token, err := config.Exchange(ctx, code,
    oauth2.VerifierOption(verifier),
)
```

Always generate a fresh verifier and nonce per login. Validate `nonce` in the ID token claims.

---

## Session Management

Use `alexedwards/scs/v2` with a Redis or PostgreSQL store:

```go
sessionManager = scs.New()
sessionManager.Store = postgresstore.New(db)  // or redisstore.New(pool)
sessionManager.Lifetime = 24 * time.Hour
sessionManager.Cookie.HttpOnly = true
sessionManager.Cookie.Secure = true   // HTTPS only
sessionManager.Cookie.SameSite = http.SameSiteLaxMode
```

Store in session:
```go
sessionManager.Put(r.Context(), "access_token", token.AccessToken)
sessionManager.Put(r.Context(), "refresh_token", token.RefreshToken)
sessionManager.Put(r.Context(), "id_token", rawIDToken)
sessionManager.Put(r.Context(), "token_expiry", token.Expiry)
sessionManager.Put(r.Context(), "user_id", claims.Subject)
```

---

## RequireAuth Middleware

```go
func RequireAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        userID := sessionManager.GetString(r.Context(), "user_id")
        if userID == "" {
            http.Redirect(w, r, "/api/auth/login?return_to="+r.URL.Path, http.StatusFound)
            return
        }
        // proactive token refresh
        expiry := sessionManager.GetTime(r.Context(), "token_expiry")
        if time.Until(expiry) < 30*time.Second {
            if err := refreshToken(r.Context(), w, r); err != nil {
                sessionManager.Destroy(r.Context())
                http.Redirect(w, r, "/api/auth/login", http.StatusFound)
                return
            }
        }
        ctx := context.WithValue(r.Context(), ctxUserID, userID)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}
```

---

## Token Refresh

```go
func refreshToken(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
    refreshToken := sessionManager.GetString(r.Context(), "refresh_token")
    ts := oauth2Config.TokenSource(ctx, &oauth2.Token{
        RefreshToken: refreshToken,
        Expiry:       time.Now().Add(-time.Second), // force refresh
    })
    newToken, err := ts.Token()
    if err != nil {
        // invalid_grant = session expired; destroy and redirect
        return err
    }
    sessionManager.Put(r.Context(), "access_token", newToken.AccessToken)
    sessionManager.Put(r.Context(), "token_expiry", newToken.Expiry)
    if newToken.RefreshToken != "" {
        // persist rotated refresh token
        sessionManager.Put(r.Context(), "refresh_token", newToken.RefreshToken)
    }
    return nil
}
```

---

## Logout (RP-Initiated)

```go
func LogoutHandler(w http.ResponseWriter, r *http.Request) {
    idToken := sessionManager.GetString(r.Context(), "id_token")
    sessionManager.Destroy(r.Context())  // destroy server-side session first

    // construct end_session_endpoint URL
    endSessionURL, _ := url.Parse(provider.Endpoint().AuthURL)
    endSessionURL.Path = strings.Replace(endSessionURL.Path, "/authorize", "/logout", 1)
    // better: use provider.Endpoint() discovery metadata end_session_endpoint

    q := endSessionURL.Query()
    q.Set("id_token_hint", idToken)
    q.Set("client_id", clientID)
    q.Set("post_logout_redirect_uri", "https://app.splitkauf.de/")
    endSessionURL.RawQuery = q.Encode()

    http.Redirect(w, r, endSessionURL.String(), http.StatusFound)
}
```

---

## PWA Integration

```js
// No tokens in the browser. All auth state lives in the server-side session.

// Initiate login (full page navigation, NOT fetch)
function login(returnTo = window.location.pathname) {
  window.location.href = `/api/auth/login?return_to=${encodeURIComponent(returnTo)}`
}

// Load current user after login redirect
async function loadUser() {
  const res = await apiFetch('/api/me')
  if (res.ok) store.user = await res.json()
}

// Wrapper that handles 401 transparently
async function apiFetch(url, options = {}) {
  const res = await fetch(url, { ...options, credentials: 'same-origin' })
  if (res.status === 401) {
    login(window.location.pathname)
    return  // navigation in progress
  }
  return res
}
```

No silent refresh needed in the PWA — the backend handles it transparently via the `RequireAuth` middleware.

---

## API Endpoints

```
GET  /api/auth/login          → redirect to IdP (stores state/verifier/nonce in session)
GET  /api/auth/callback       → code exchange, set session cookie, redirect to return_to
GET  /api/auth/logout         → destroy session, RP-initiated logout redirect
GET  /api/me                  → return {id, email, name} from session; 401 if not logged in
```

---

## Provider Differences

| | Zitadel | Keycloak |
|---|---|---|
| Roles claim | `urn:zitadel:iam:org:project:roles` (map of maps) | `realm_access.roles` + `resource_access.<client>.roles` |
| Refresh token rotation | ON by default | Must be enabled: Revoke Refresh Token = ON |
| Default access token lifetime | 12h (reduce to 5–15 min) | 5 min (good default) |
| Discovery URL | `https://<host>/.well-known/openid-configuration` | `https://<host>/realms/<realm>/.well-known/openid-configuration` |
| PKCE support | ✅ Required by default | ✅ Supported; enable per-client |

---

## Security Checklist

- [ ] `HttpOnly`, `Secure`, `SameSite=Lax` on session cookie
- [ ] PKCE with S256 on every authorization request
- [ ] Nonce validated in ID token claims
- [ ] State parameter validated in callback (CSRF)
- [ ] Access token lifetime ≤ 15 minutes
- [ ] Refresh token rotation enabled on IdP
- [ ] `id_token_hint` sent on logout
- [ ] `post_logout_redirect_uri` allowlisted on IdP
- [ ] Session regenerated after successful login (session fixation)
