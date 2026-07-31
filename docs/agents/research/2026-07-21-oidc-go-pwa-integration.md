---
date: 2026-07-21T16:18:39Z
git_commit: HEAD
branch: main
topic: "OIDC (OpenID Connect) integration for Go web apps (splitkauf)"
tags: [research, oidc, oauth2, go, pwa, authentication, zitadel, keycloak, session-management]
status: complete
---

# Research: OIDC Integration for splitkauf (Go Backend + PWA)

## Research Question

Investigate the OIDC flow (authorization code flow with PKCE), how to integrate OIDC into a Go backend (relevant libraries: coreos/go-oidc, zitadel/oidc, etc.), session management after token exchange, token refresh, logout, and how to wire it into a PWA frontend (storing tokens safely, silent refresh, redirects). The report will be used to add authentication to "splitkauf" — a Go backend + PWA frontend app where users log in with an existing OIDC provider (e.g. a self-hosted Zitadel or Keycloak instance).

---

## Summary

This document covers the full OIDC Authorization Code Flow with PKCE as used in a Go backend (acting as confidential OIDC relying party / BFF — Backend for Frontend) paired with a PWA frontend. The core architecture principle is that the **Go backend owns all tokens**; the PWA frontend never sees an access token, refresh token, or raw ID token. The browser communicates with the backend via a session cookie (HttpOnly, Secure, SameSite=Lax).

Key files to create (does not yet exist in this repo):

```
splitkauf/
├── internal/
│   └── auth/
│       ├── handler.go          # Login, Callback, Logout, Me handlers
│       ├── middleware.go       # RequireAuth middleware (token refresh)
│       ├── session.go          # SessionData struct, session helpers
│       └── pkce.go             # PKCE code_verifier/challenge helpers
├── internal/
│   └── session/
│       └── store.go            # Redis or in-memory session store wrapper
└── web/
    └── src/
        └── auth/
            ├── auth.js         # initAuth(), apiFetch(), authStore
            └── guard.js        # Router guard (redirect to /login if unauthenticated)
```

### ASCII Architecture Diagram

```
Browser (splitkauf PWA)
        │
        │  ① window.location.href = '/api/auth/login'
        ▼
Go Backend ──────────────────────────────────────────────────────────────────
  /api/auth/login          Generate PKCE (verifier + S256 challenge)
                           Generate state (random, stored in temp session)
                           Redirect → OIDC Provider /authorize?...
        │
        │  ② 302 redirect to OIDC Provider
        ▼
OIDC Provider (Zitadel / Keycloak)
  /authorize               Show login page
  user enters credentials
        │
        │  ③ 302 redirect to /api/auth/callback?code=xxx&state=yyy
        ▼
Go Backend
  /api/auth/callback       Validate state (CSRF check)
                           Exchange code for tokens (with PKCE verifier)
                           Verify ID token signature + claims (nonce, aud, iss)
                           Create server-side session
                           Store access_token + refresh_token in Redis
                           Set-Cookie: session=<opaque-id>; HttpOnly; Secure; SameSite=Lax
                           302 redirect → PWA (returnTo path)
        │
        │  ④ GET /api/me (Cookie: session=xxx)
        ▼
Go Backend
  /api/me                  Load session from Redis
                           Return {sub, email, name, roles}
        │
        │  ⑤ All subsequent API calls (Cookie: session=xxx)
        ▼
Go Backend
  RequireAuth middleware   Load session
                           Check access_token expiry
                           If near expiry → call /token with refresh_token
                           Store updated tokens in Redis
                           Attach access_token to upstream calls
```

---

## Detailed Findings

### 1. The OIDC Authorization Code Flow with PKCE

**PKCE (Proof Key for Code Exchange, RFC 7636)** prevents authorization code interception attacks. It is now required for all authorization code flows by RFC 9700 (OAuth 2.0 Security BCP, February 2025), even for confidential clients.

#### PKCE Generation

```go
// pkce.go
import (
    "crypto/rand"
    "crypto/sha256"
    "encoding/base64"
)

func generateVerifier() (string, error) {
    b := make([]byte, 32)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return base64.RawURLEncoding.EncodeToString(b), nil
}

func generateChallenge(verifier string) string {
    h := sha256.Sum256([]byte(verifier))
    return base64.RawURLEncoding.EncodeToString(h[:])
}
```

#### Full HTTP Exchange

**Step 1 — Authorization Request:**
```
GET /authorize
  ?response_type=code
  &client_id=splitkauf-client
  &redirect_uri=https://splitkauf.example.com/api/auth/callback
  &scope=openid profile email offline_access
  &state=<32-byte-random-base64>
  &nonce=<32-byte-random-base64>
  &code_challenge=<sha256(verifier)-base64url>
  &code_challenge_method=S256
```

**Step 2 — Provider redirects back:**
```
302 Location: https://splitkauf.example.com/api/auth/callback
  ?code=<authorization-code>
  &state=<same-state-value>
```

**Step 3 — Token Exchange:**
```
POST /token
Content-Type: application/x-www-form-urlencoded

grant_type=authorization_code
&code=<authorization-code>
&redirect_uri=https://splitkauf.example.com/api/auth/callback
&client_id=splitkauf-client
&client_secret=<secret>          ← confidential client only
&code_verifier=<original-verifier>
```

**Step 4 — Token Response:**
```json
{
  "access_token":  "eyJ...",
  "token_type":    "Bearer",
  "expires_in":    300,
  "refresh_token": "eyJ...",
  "id_token":      "eyJ..."
}
```

**Step 5 — ID Token Verification (mandatory):**
1. Decode JWT header, get `kid`
2. Fetch IdP's JWK Set from `jwks_uri` (discovered via `.well-known/openid-configuration`)
3. Verify signature using the matching JWK
4. Validate claims: `iss` == provider issuer, `aud` == client_id, `exp` > now, `nonce` == stored nonce, `iat` within acceptable clock skew

---

### 2. Go Library Landscape

#### 2.1 `github.com/coreos/go-oidc/v3` (recommended for Keycloak or provider-agnostic)

The most widely adopted Go OIDC library. Handles:
- Provider discovery via `.well-known/openid-configuration`
- JWK Set fetching + caching + rotation
- ID token verification (signature, claims validation)
- UserInfo endpoint calls

**Does NOT handle:**
- The OAuth2 authorization code flow (delegates to `golang.org/x/oauth2`)
- Session management
- PKCE (must be added manually or via `golang.org/x/oauth2`)

**Key types:**
- `oidc.Provider` — represents a discovered OIDC provider
- `oidc.IDTokenVerifier` — verifies raw ID token JWTs
- `oidc.IDToken` — parsed, verified ID token with `.Claims()` method
- `oidc.Config` — verifier configuration (ClientID, optional: SkipNonce, etc.)

**Usage:**
```go
import (
    "github.com/coreos/go-oidc/v3/oidc"
    "golang.org/x/oauth2"
)

// Discovery (fetches /.well-known/openid-configuration)
provider, err := oidc.NewProvider(ctx, "https://keycloak.example.com/realms/splitkauf")

// Discover end_session_endpoint (not exposed in oidc.Provider directly)
var meta struct {
    EndSessionEndpoint string `json:"end_session_endpoint"`
}
_ = provider.Claims(&meta)

// OAuth2 config (coreos/go-oidc provides provider.Endpoint())
oauthCfg := &oauth2.Config{
    ClientID:     os.Getenv("CLIENT_ID"),
    ClientSecret: os.Getenv("CLIENT_SECRET"),
    RedirectURL:  "https://splitkauf.example.com/api/auth/callback",
    Endpoint:     provider.Endpoint(),
    Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "offline_access"},
}

// Build auth URL with PKCE
verifier, _ := generateVerifier()
authURL := oauthCfg.AuthCodeURL(state,
    oauth2.S256ChallengeOption(verifier),  // PKCE
    oauth2.SetAuthURLParam("nonce", nonce),
)

// Exchange code
token, err := oauthCfg.Exchange(ctx, code, oauth2.VerifierOption(verifier))

// Verify ID token
idTokenVerifier := provider.Verifier(&oidc.Config{ClientID: clientID})
idToken, err := idTokenVerifier.Verify(ctx, token.Extra("id_token").(string))

// Parse claims
var claims struct {
    Sub   string `json:"sub"`
    Email string `json:"email"`
    Name  string `json:"name"`
    Nonce string `json:"nonce"`
}
_ = idToken.Claims(&claims)
```

#### 2.2 `github.com/zitadel/oidc/v3` (recommended for Zitadel or full-featured RP)

Zitadel's own Go OIDC library. It provides a complete Relying Party (RP) implementation, handling both the OIDC and OAuth2 layers. It wraps `coreos/go-oidc` and `golang.org/x/oauth2` with a higher-level API that includes PKCE out of the box.

**Key package:** `github.com/zitadel/oidc/v3/pkg/client/rp`

**Key types:**
- `rp.RelyingParty` — the main entry point (interface)
- `rp.NewRelyingPartyOIDC()` — creates an RP from issuer URL, client credentials, redirect URI, scopes
- `rp.AuthURLHandler()` — HTTP handler that generates the auth URL + state + PKCE and redirects
- `rp.CodeExchangeHandler()` — HTTP handler for the callback
- `rp.WithPKCE(nil)` — option to enable PKCE

**Usage:**
```go
import "github.com/zitadel/oidc/v3/pkg/client/rp"

provider, err := rp.NewRelyingPartyOIDC(
    ctx,
    "https://myinstance.zitadel.cloud",
    os.Getenv("CLIENT_ID"),
    os.Getenv("CLIENT_SECRET"),
    "https://splitkauf.example.com/api/auth/callback",
    []string{"openid", "profile", "email", "offline_access"},
    rp.WithPKCE(nil),              // PKCE with auto-generated verifier
)

// The zitadel/oidc library can generate handler functions directly:
mux.Handle("/api/auth/login", rp.AuthURLHandler(
    stateFunc,      // func() string — returns a random state
    provider,
))

mux.Handle("/api/auth/callback", rp.CodeExchangeHandler(
    func(w http.ResponseWriter, r *http.Request,
        tokens *oidc.Tokens[*oidc.IDTokenClaims],
        state string,
        rp rp.RelyingParty,
    ) {
        // tokens.IDToken — verified IDToken
        // tokens.Token — oauth2 Token (AccessToken, RefreshToken, Expiry)
        // ... save session, set cookie, redirect
    },
    provider,
))
```

`github.com/zitadel/oidc/v3` also provides:
- Token introspection client (`rp.Introspect`)
- UserInfo endpoint client (`rp.Userinfo`)
- RP-initiated logout helper (`rp.EndSession`)
- Token refresh (`rp.RefreshTokens`)

#### 2.3 `golang.org/x/oauth2`

The standard Go OAuth2 library. Does not know about OIDC (no ID token handling), but is the foundation:
- `oauth2.Config` — OAuth2 client configuration
- `oauth2.Config.AuthCodeURL()` — builds the authorization URL
- `oauth2.Config.Exchange()` — exchanges authorization code for tokens
- `oauth2.Config.TokenSource()` — creates a `TokenSource` that auto-refreshes
- `oauth2.Token` — holds access_token, refresh_token, expiry
- `oauth2.ReuseTokenSource` — caches the token, only refreshes when needed
- `oauth2.RetrieveError` — typed error from token endpoint (`.ErrorCode == "invalid_grant"` means refresh token expired)

PKCE support (added in later versions):
- `oauth2.S256ChallengeOption(verifier)` — adds code_challenge params to auth URL
- `oauth2.VerifierOption(verifier)` — adds code_verifier to token exchange

---

### 3. Session Management After Token Exchange

#### 3.1 What to Store in the Session

```go
// session.go
type SessionData struct {
    // Tokens (server-side only — never sent to browser)
    AccessToken  string    `json:"access_token"`
    RefreshToken string    `json:"refresh_token"`
    IDToken      string    `json:"id_token"`       // raw JWT; needed for logout id_token_hint
    Expiry       time.Time `json:"expiry"`

    // Identity (from ID token claims)
    Subject      string    `json:"sub"`             // stable user ID; use as DB foreign key
    Email        string    `json:"email"`
    Name         string    `json:"name"`
    Roles        []string  `json:"roles,omitempty"` // from custom claims
}
```

**Rule:** The session cookie only contains an opaque, random session ID. All token data lives server-side.

#### 3.2 Session Library Comparison

| Library | Cookie handling | Type safety | Backends | Status |
|---|---|---|---|---|
| `github.com/alexedwards/scs/v2` | Automatic via middleware | Typed getters (GetString, GetBytes, etc.) | Redis, Postgres, MySQL, SQLite, DynamoDB, memstore | **Actively maintained — recommended** |
| `github.com/gorilla/sessions` | Per-handler (session.Save) | `map[interface{}]interface{}` (gob) | Redis (via rbcervilla/redisstore), Postgres, filesystem | Maintenance mode, stable |

**Recommendation:** Use `alexedwards/scs/v2` — modern API, middleware-based, avoids gob registration.

#### 3.3 SCS Setup

```go
import (
    "github.com/alexedwards/scs/v2"
    "github.com/alexedwards/scs/goredisstore"
    "github.com/redis/go-redis/v9"
)

rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})

sm := scs.New()
sm.Store = goredisstore.New(rdb)
sm.Lifetime = 24 * time.Hour
sm.Cookie.Name = "session"
sm.Cookie.HttpOnly = true
sm.Cookie.Secure = true             // require HTTPS
sm.Cookie.SameSite = http.SameSiteLaxMode  // Lax: allows OAuth redirects, blocks CSRF
sm.Cookie.Persist = true            // Set Max-Age (survives browser restart)

// Wrap the HTTP handler
handler := sm.LoadAndSave(mux)
```

**SameSite mode selection:**
- `SameSiteLaxMode` ← use this. Sent on top-level navigations (the OAuth callback redirect). Blocked on cross-origin subresource requests.
- `SameSiteStrictMode` — **do NOT use**. The callback redirect from the IdP is a cross-site redirect; the session cookie would not be sent, breaking the flow.
- `SameSiteNoneMode` — only if backend and frontend are on different domains (cross-origin cookies; requires `Secure: true`).

#### 3.4 Storing/Retrieving Session Data (SCS)

```go
// Save after login
func (a *App) saveSession(ctx context.Context, sd SessionData) error {
    b, err := json.Marshal(sd)
    if err != nil {
        return err
    }
    a.sm.Put(ctx, "oidc", b)
    return nil
}

// Load in middleware
func (a *App) loadSession(ctx context.Context) (*SessionData, error) {
    b := a.sm.GetBytes(ctx, "oidc")
    if b == nil {
        return nil, nil
    }
    var sd SessionData
    if err := json.Unmarshal(b, &sd); err != nil {
        return nil, err
    }
    return &sd, nil
}

// Destroy on logout
func (a *App) destroySession(ctx context.Context) error {
    return a.sm.Destroy(ctx)
}
```

---

### 4. Complete Go Handler Implementations

#### 4.1 App Struct

```go
type App struct {
    oauthConfig        *oauth2.Config
    oidcProvider       *oidc.Provider
    idTokenVerifier    *oidc.IDTokenVerifier
    sm                 *scs.SessionManager
    endSessionEndpoint string
}

func NewApp(ctx context.Context) (*App, error) {
    issuer := os.Getenv("OIDC_ISSUER")    // e.g. "https://myinstance.zitadel.cloud"
                                            // or "https://keycloak.example.com/realms/splitkauf"

    provider, err := oidc.NewProvider(ctx, issuer)
    if err != nil {
        return nil, fmt.Errorf("oidc discovery: %w", err)
    }

    var meta struct {
        EndSessionEndpoint string `json:"end_session_endpoint"`
    }
    _ = provider.Claims(&meta)

    oauthCfg := &oauth2.Config{
        ClientID:     os.Getenv("OIDC_CLIENT_ID"),
        ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
        RedirectURL:  os.Getenv("OIDC_REDIRECT_URL"), // https://app/api/auth/callback
        Endpoint:     provider.Endpoint(),
        Scopes:       []string{oidc.ScopeOpenID, "profile", "email", "offline_access"},
    }

    verifier := provider.Verifier(&oidc.Config{ClientID: oauthCfg.ClientID})

    // Session manager (Redis)
    rdb := redis.NewClient(&redis.Options{Addr: os.Getenv("REDIS_ADDR")})
    sm := scs.New()
    sm.Store = goredisstore.New(rdb)
    sm.Lifetime = 7 * 24 * time.Hour
    sm.Cookie.HttpOnly = true
    sm.Cookie.Secure = true
    sm.Cookie.SameSite = http.SameSiteLaxMode
    sm.Cookie.Persist = true

    return &App{
        oauthConfig:        oauthCfg,
        oidcProvider:       provider,
        idTokenVerifier:    verifier,
        sm:                 sm,
        endSessionEndpoint: meta.EndSessionEndpoint,
    }, nil
}
```

#### 4.2 Login Handler

```go
func (a *App) LoginHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Safe return path (validate against allowlist to prevent open redirect)
    returnTo := r.URL.Query().Get("return_to")
    if returnTo == "" || !isAllowedPath(returnTo) {
        returnTo = "/"
    }

    // PKCE
    verifier, err := generateVerifier()
    if err != nil {
        http.Error(w, "server error", http.StatusInternalServerError)
        return
    }

    // State (CSRF for OAuth)
    stateBytes := make([]byte, 32)
    rand.Read(stateBytes)
    state := base64.RawURLEncoding.EncodeToString(stateBytes)

    // Nonce (binds ID token to this auth request)
    nonceBytes := make([]byte, 32)
    rand.Read(nonceBytes)
    nonce := base64.RawURLEncoding.EncodeToString(nonceBytes)

    // Store PKCE verifier, nonce, returnTo in session (pre-login temp data)
    a.sm.Put(ctx, "oauth_state", state)
    a.sm.Put(ctx, "pkce_verifier", verifier)
    a.sm.Put(ctx, "oauth_nonce", nonce)
    a.sm.Put(ctx, "return_to", returnTo)

    authURL := a.oauthConfig.AuthCodeURL(state,
        oauth2.S256ChallengeOption(verifier),
        oauth2.SetAuthURLParam("nonce", nonce),
    )

    http.Redirect(w, r, authURL, http.StatusFound)
}

// Only allow paths within the same app (no external redirects)
func isAllowedPath(path string) bool {
    return strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")
}
```

#### 4.3 Callback Handler

```go
func (a *App) CallbackHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Retrieve and validate state
    state := r.URL.Query().Get("state")
    savedState := a.sm.GetString(ctx, "oauth_state")
    if state == "" || state != savedState {
        http.Error(w, "invalid state parameter", http.StatusBadRequest)
        return
    }

    // Retrieve pre-login values
    verifier := a.sm.GetString(ctx, "pkce_verifier")
    nonce := a.sm.GetString(ctx, "oauth_nonce")
    returnTo := a.sm.GetString(ctx, "return_to")
    if returnTo == "" {
        returnTo = "/"
    }

    // Clean up pre-login session data
    a.sm.Remove(ctx, "oauth_state")
    a.sm.Remove(ctx, "pkce_verifier")
    a.sm.Remove(ctx, "oauth_nonce")
    a.sm.Remove(ctx, "return_to")

    // Exchange authorization code for tokens
    code := r.URL.Query().Get("code")
    token, err := a.oauthConfig.Exchange(ctx, code, oauth2.VerifierOption(verifier))
    if err != nil {
        http.Error(w, "token exchange failed", http.StatusInternalServerError)
        return
    }

    // Extract and verify ID token
    rawIDToken, ok := token.Extra("id_token").(string)
    if !ok {
        http.Error(w, "missing id_token in response", http.StatusInternalServerError)
        return
    }

    idToken, err := a.idTokenVerifier.Verify(ctx, rawIDToken)
    if err != nil {
        http.Error(w, "invalid id_token", http.StatusUnauthorized)
        return
    }

    // Parse claims
    var claims struct {
        Sub   string `json:"sub"`
        Email string `json:"email"`
        Name  string `json:"name"`
        Nonce string `json:"nonce"`
    }
    if err := idToken.Claims(&claims); err != nil {
        http.Error(w, "failed to parse claims", http.StatusInternalServerError)
        return
    }

    // Validate nonce (prevents replay attacks)
    if claims.Nonce != nonce {
        http.Error(w, "nonce mismatch", http.StatusUnauthorized)
        return
    }

    // Rotate session token to prevent session fixation
    if err := a.sm.RenewToken(ctx); err != nil {
        http.Error(w, "session renewal failed", http.StatusInternalServerError)
        return
    }

    // Persist token data in server-side session
    sd := SessionData{
        AccessToken:  token.AccessToken,
        RefreshToken: token.RefreshToken,
        IDToken:      rawIDToken,
        Expiry:       token.Expiry,
        Subject:      claims.Sub,
        Email:        claims.Email,
        Name:         claims.Name,
    }
    if err := a.saveSession(ctx, sd); err != nil {
        http.Error(w, "session save failed", http.StatusInternalServerError)
        return
    }

    http.Redirect(w, r, returnTo, http.StatusFound)
}
```

#### 4.4 RequireAuth Middleware (with Token Refresh)

```go
type contextKey string

const ctxKeySession contextKey = "session"

func (a *App) RequireAuth(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        ctx := r.Context()

        sd, err := a.loadSession(ctx)
        if err != nil || sd == nil {
            // No session — for API routes return 401; for page routes redirect
            if isAPIRoute(r.URL.Path) {
                http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
            } else {
                http.Redirect(w, r,
                    "/api/auth/login?return_to="+url.QueryEscape(r.URL.RequestURI()),
                    http.StatusFound)
            }
            return
        }

        // Reconstruct oauth2 token
        oauthToken := &oauth2.Token{
            AccessToken:  sd.AccessToken,
            RefreshToken: sd.RefreshToken,
            Expiry:       sd.Expiry,
            TokenType:    "Bearer",
        }

        // Auto-refresh via TokenSource (uses oauth2's built-in 10s expiry buffer)
        ts := a.oauthConfig.TokenSource(ctx, oauthToken)
        freshToken, err := ts.Token()
        if err != nil {
            var rerr *oauth2.RetrieveError
            if errors.As(err, &rerr) && rerr.ErrorCode == "invalid_grant" {
                // Refresh token expired or revoked — force re-login
                _ = a.destroySession(ctx)
                if isAPIRoute(r.URL.Path) {
                    http.Error(w, `{"error":"session_expired"}`, http.StatusUnauthorized)
                } else {
                    http.Redirect(w, r, "/api/auth/login", http.StatusFound)
                }
                return
            }
            // Transient IdP error
            http.Error(w, "authentication service unavailable", http.StatusServiceUnavailable)
            return
        }

        // Persist refreshed token if it changed
        if freshToken.AccessToken != sd.AccessToken {
            sd.AccessToken = freshToken.AccessToken
            sd.Expiry = freshToken.Expiry
            if freshToken.RefreshToken != "" {
                // Refresh token rotation: always update
                sd.RefreshToken = freshToken.RefreshToken
            }
            _ = a.saveSession(ctx, *sd)
        }

        // Inject session into request context for downstream handlers
        ctx = context.WithValue(ctx, ctxKeySession, sd)
        next.ServeHTTP(w, r.WithContext(ctx))
    })
}

func isAPIRoute(path string) bool {
    return strings.HasPrefix(path, "/api/")
}

// Helper: get session from context (for use in handlers downstream of RequireAuth)
func SessionFromContext(ctx context.Context) *SessionData {
    v, _ := ctx.Value(ctxKeySession).(*SessionData)
    return v
}
```

#### 4.5 /api/me Handler

```go
func (a *App) MeHandler(w http.ResponseWriter, r *http.Request) {
    sd := SessionFromContext(r.Context())
    if sd == nil {
        http.Error(w, `{"error":"unauthenticated"}`, http.StatusUnauthorized)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    json.NewEncoder(w).Encode(map[string]interface{}{
        "sub":   sd.Subject,
        "email": sd.Email,
        "name":  sd.Name,
        "roles": sd.Roles,
        // Never include: access_token, refresh_token, id_token
    })
}
```

#### 4.6 Logout Handler

```go
func (a *App) LogoutHandler(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()

    // Retrieve ID token before destroying session (needed for id_token_hint)
    sd, _ := a.loadSession(ctx)
    var rawIDToken string
    if sd != nil {
        rawIDToken = sd.IDToken
    }

    // Destroy local session + clear cookie
    _ = a.destroySession(ctx)

    // RP-Initiated Logout: redirect to IdP's end_session_endpoint
    if a.endSessionEndpoint != "" && rawIDToken != "" {
        params := url.Values{}
        params.Set("id_token_hint", rawIDToken)
        params.Set("client_id", a.oauthConfig.ClientID)
        params.Set("post_logout_redirect_uri",
            os.Getenv("APP_BASE_URL")+"/post-logout")

        http.Redirect(w, r,
            a.endSessionEndpoint+"?"+params.Encode(),
            http.StatusFound)
        return
    }

    // Fallback if no end_session_endpoint configured
    http.Redirect(w, r, "/", http.StatusFound)
}

func (a *App) PostLogoutHandler(w http.ResponseWriter, r *http.Request) {
    // After IdP logout redirect — the session is already destroyed.
    // Render a logged-out page or redirect to home.
    http.Redirect(w, r, "/", http.StatusFound)
}
```

#### 4.7 Router Wiring

```go
func main() {
    ctx := context.Background()
    app, err := NewApp(ctx)
    if err != nil {
        log.Fatal(err)
    }

    mux := http.NewServeMux()

    // Auth endpoints (no RequireAuth)
    mux.HandleFunc("GET /api/auth/login", app.LoginHandler)
    mux.HandleFunc("GET /api/auth/callback", app.CallbackHandler)
    mux.HandleFunc("POST /api/auth/logout", app.LogoutHandler)
    mux.HandleFunc("GET /post-logout", app.PostLogoutHandler)

    // Protected endpoints
    mux.Handle("GET /api/me", app.RequireAuth(http.HandlerFunc(app.MeHandler)))
    mux.Handle("/api/", app.RequireAuth(app.APIRouter()))

    // Serve PWA static files
    mux.Handle("/", http.FileServer(http.Dir("./web/dist")))

    // Wrap with SCS session middleware
    handler := app.sm.LoadAndSave(mux)

    log.Println("Listening on :8080")
    log.Fatal(http.ListenAndServe(":8080", handler))
}
```

---

### 5. PWA Frontend Integration

#### 5.1 Token Storage: Why HttpOnly Cookies (BFF Pattern)

The PWA never receives or stores any OIDC tokens. The backend sets an HttpOnly session cookie after login. All API calls automatically include this cookie (same-origin).

| Storage option | JS readable | XSS vulnerable | CSRF vulnerable | Verdict |
|---|---|---|---|---|
| localStorage | Yes | **Yes** | No (not auto-sent) | **Forbidden** (RFC 9700) |
| sessionStorage | Yes | **Yes** | No | **Forbidden** |
| In-memory (JS var) | Yes (same tab) | Yes | No | Avoid (lost on reload) |
| HttpOnly cookie | **No** | **No** | Yes (mitigated by SameSite=Lax) | **Required** |

#### 5.2 Initiating Login from the PWA

```javascript
// auth.js
export function initiateLogin(returnPath = window.location.pathname + window.location.search) {
  // Save where user was (fallback if the backend's returnTo param fails)
  sessionStorage.setItem('post_login_path', returnPath);

  const loginUrl = '/api/auth/login?return_to=' + encodeURIComponent(returnPath);
  window.location.href = loginUrl;
}
```

#### 5.3 Auth State Management

```javascript
// auth.js — framework-agnostic reactive store (Svelte/Solid/Vue signals pattern)
let _user = null;
let _loading = true;
const _listeners = new Set();

export const authStore = {
  get user() { return _user; },
  get loading() { return _loading; },
  get authenticated() { return _user !== null; },

  subscribe(fn) {
    _listeners.add(fn);
    return () => _listeners.delete(fn);
  },

  _notify() { _listeners.forEach(fn => fn()); },

  setUser(user) {
    _user = user;
    _loading = false;
    this._notify();
  },

  clear() {
    _user = null;
    _loading = false;
    this._notify();
  }
};

// Called on app boot and after every redirect from IdP
export async function initAuth() {
  try {
    const res = await fetch('/api/me', { credentials: 'include' });
    if (res.ok) {
      const user = await res.json();
      authStore.setUser(user);  // { sub, email, name, roles }
    } else {
      authStore.clear();
    }
  } catch {
    authStore.clear();
  }
}
```

#### 5.4 Global API Fetch Wrapper (Handles 401)

```javascript
// api.js
export async function apiFetch(url, options = {}) {
  const res = await fetch(url, {
    ...options,
    credentials: 'include',       // Always send session cookie
    headers: {
      'Content-Type': 'application/json',
      ...options.headers,
    },
  });

  if (res.status === 401) {
    // Session expired — redirect to login
    authStore.clear();
    const returnPath = window.location.pathname + window.location.search;
    sessionStorage.setItem('post_login_path', returnPath);
    window.location.href = '/api/auth/login?return_to=' + encodeURIComponent(returnPath);
    // Return a never-resolving promise to prevent cascading errors in callers
    return new Promise(() => {});
  }

  return res;
}
```

#### 5.5 Router Guard

```javascript
// guard.js
import { authStore, initAuth } from './auth.js';

export async function authGuard(toPath) {
  // Ensure auth state is initialized
  if (authStore.loading) {
    await initAuth();
  }

  if (!authStore.authenticated) {
    // Save intended destination and redirect to login
    sessionStorage.setItem('post_login_path', toPath);
    window.location.href = '/api/auth/login?return_to=' + encodeURIComponent(toPath);
    return false; // Abort navigation
  }

  return true; // Allow navigation
}
```

#### 5.6 Silent Refresh — No Action Required (BFF Handles It)

In the BFF pattern, the PWA does **nothing** for token refresh. Every call to `/api/*` goes through `RequireAuth` middleware on the backend, which:
1. Checks access token expiry
2. Proactively refreshes if needed (using the stored refresh token)
3. Updates the session in Redis
4. Proceeds with the API response

The PWA only sees the final API response. Token refresh is invisible.

**What the PWA must handle:** A `401` response — which means the refresh token itself has expired and the user needs to re-login. The global `apiFetch` wrapper in section 5.4 handles this.

#### 5.7 Logout from the PWA

```javascript
// auth.js
export async function logout() {
  try {
    // Backend destroys session + redirects to IdP end_session_endpoint
    // (or redirects to /post-logout if no end_session_endpoint)
    window.location.href = '/api/auth/logout';
  } catch {
    // Fallback: clear local state and redirect to home
    authStore.clear();
    window.location.href = '/';
  }
}
```

Note: The logout is a full page navigation (not an `apiFetch` call) because the backend needs to redirect the browser to the IdP's `end_session_endpoint`.

#### 5.8 Multi-Tab Auth Synchronization

```javascript
// In app initialization
const authChannel = new BroadcastChannel('splitkauf_auth');

// Broadcast logout to other tabs
export function broadcastLogout() {
  authChannel.postMessage({ type: 'logout' });
}

// Listen for auth events from other tabs
authChannel.onmessage = async (event) => {
  if (event.data.type === 'logout') {
    authStore.clear();
    window.location.href = '/';
  }
  if (event.data.type === 'login') {
    await initAuth();
  }
};
```

---

### 6. Provider-Specific Configuration

#### 6.1 Zitadel

**Application setup in Zitadel console:**
- Application type: **Web**
- Authentication method: **Code** (authorization code with secret; your BFF is a confidential client)
- Redirect URIs: `https://splitkauf.example.com/api/auth/callback`
- Post-logout redirect URIs: `https://splitkauf.example.com/post-logout`
- Clock skew: default (Zitadel is strict about token timestamps)

**Scopes:**
```
openid profile email offline_access
urn:zitadel:iam:org:project:roles    ← for role-based access control
```

**Role claim structure in ID/access token:**
```json
{
  "urn:zitadel:iam:org:project:roles": {
    "admin": { "<orgID>": "<orgDomain>" },
    "viewer": { "<orgID>": "<orgDomain>" }
  }
}
```

Parse roles in Go:
```go
var zitadelClaims struct {
    Sub   string `json:"sub"`
    Email string `json:"email"`
    Name  string `json:"name"`
    Roles map[string]map[string]string `json:"urn:zitadel:iam:org:project:roles"`
}
idToken.Claims(&zitadelClaims)

roles := make([]string, 0, len(zitadelClaims.Roles))
for role := range zitadelClaims.Roles {
    roles = append(roles, role)
}
```

**Issuer URL (discovery base):**
```
https://<instance-id>.zitadel.cloud
or
https://<custom-domain>
```

**Zitadel logout URL:**
```
https://<instance-id>.zitadel.cloud/oidc/v1/end_session
```
(Discovered from `.well-known/openid-configuration`; `end_session_endpoint` field)

**Key Zitadel behavior:** Refresh token rotation is enabled by default. Always update the refresh token in the session after a refresh call.

#### 6.2 Keycloak

**Realm settings:**
- Access Token Lifespan: `5 minutes` (BFF refreshes transparently)
- Client Session Idle: `30 minutes`
- SSO Session Idle: `30 minutes`
- Revoke Refresh Token: **ON** (enables refresh token rotation)
- Refresh Token Max Reuse: `0`

**Client settings (in Keycloak Admin):**
- Client ID: `splitkauf-backend`
- Client type: `OpenID Connect`
- Client authentication: **ON** (confidential client)
- Authorization: OFF
- Valid redirect URIs: `https://splitkauf.example.com/api/auth/callback`
- Valid post-logout redirect URIs: `https://splitkauf.example.com/post-logout`
- Web Origins: `https://splitkauf.example.com`
- PKCE: Set in Advanced → Proof Key for Code Exchange Code Challenge Method: `S256`

**Scopes to request:**
```
openid profile email roles offline_access
```

**Role claim structure in Keycloak tokens:**
```json
{
  "realm_access": { "roles": ["viewer", "offline_access"] },
  "resource_access": {
    "splitkauf-backend": { "roles": ["admin"] }
  }
}
```

Parse in Go:
```go
var keycloakClaims struct {
    Sub           string `json:"sub"`
    Email         string `json:"email"`
    Name          string `json:"name"`
    RealmAccess   struct {
        Roles []string `json:"roles"`
    } `json:"realm_access"`
    ResourceAccess map[string]struct {
        Roles []string `json:"roles"`
    } `json:"resource_access"`
}
```

**Issuer URL (discovery base):**
```
https://<keycloak-host>/realms/<realm-name>
```

**Keycloak logout URL:**
```
https://<keycloak-host>/realms/<realm-name>/protocol/openid-connect/logout
```
(Discovered from `.well-known/openid-configuration`; `end_session_endpoint` field)

---

### 7. Token Refresh: Detailed Flow

#### 7.1 `oauth2.TokenSource` Auto-Refresh

`oauth2.Config.TokenSource(ctx, token)` returns a `TokenSource` that automatically calls the token endpoint when `token.Valid()` returns false. `token.Valid()` considers the token expired if expiry is within 10 seconds (`expiryDelta` constant in the oauth2 package).

```go
// This single call handles the refresh if needed:
ts := a.oauthConfig.TokenSource(ctx, savedOAuth2Token)
freshToken, err := ts.Token()
// freshToken is guaranteed to be valid (not expired) if err == nil
```

#### 7.2 Error Discrimination

```go
var rerr *oauth2.RetrieveError
if errors.As(err, &rerr) {
    switch rerr.ErrorCode {
    case "invalid_grant":
        // Refresh token is expired or has been revoked (rotation)
        // → Must re-authenticate
    case "invalid_client":
        // Client credentials wrong — config error
    default:
        // Other OAuth error
    }
} else {
    // Network error, timeout, etc. — potentially transient
}
```

#### 7.3 Refresh Token Rotation

When a provider returns a new `refresh_token` in the refresh response, the old one is immediately invalidated. The oauth2 package does NOT automatically save the new refresh token anywhere — your code must check and persist it:

```go
if freshToken.RefreshToken != "" && freshToken.RefreshToken != sd.RefreshToken {
    sd.RefreshToken = freshToken.RefreshToken
    // Persist sd to session store
}
```

---

### 8. Security Checklist

| Item | Details |
|---|---|
| PKCE (S256) | Use `oauth2.S256ChallengeOption()` on auth URL, `oauth2.VerifierOption()` on exchange |
| `state` parameter | Generated fresh per login; stored in session; validated on callback |
| `nonce` parameter | Generated fresh per login; stored in session; validated in ID token claims |
| Session fixation prevention | Call `sm.RenewToken(ctx)` after successful login |
| Session cookie: HttpOnly | `sm.Cookie.HttpOnly = true` — prevents JS access |
| Session cookie: Secure | `sm.Cookie.Secure = true` — HTTPS only |
| Session cookie: SameSite=Lax | Allows OAuth redirects; blocks cross-origin subresource requests |
| No tokens in browser storage | Backend owns all tokens; browser only has session ID cookie |
| No tokens in /api/me response | Only return sanitized user info (sub, email, name, roles) |
| Refresh token in Redis only | Never serialize refresh token to the cookie |
| Refresh token rotation | Always update `sd.RefreshToken` if `freshToken.RefreshToken != ""` |
| `invalid_grant` → re-login | Redirect to login; destroy session when refresh fails with invalid_grant |
| id_token_hint on logout | Pass raw ID token to end_session_endpoint |
| post_logout_redirect_uri registered | Must be pre-registered in IdP before use |
| Open redirect prevention | Validate `return_to` parameter: must be relative path only (`strings.HasPrefix(path, "/") && !strings.HasPrefix(path, "//")`) |
| Short access token lifetime | Configure at IdP: 5 minutes for Keycloak; Zitadel default is 12 hours (reduce it) |
| Content-Security-Policy header | Set CSP in Go middleware to reduce XSS blast radius |

---

## Code References

- `golang.org/x/oauth2` — `oauth2.Config`, `oauth2.Token`, `oauth2.TokenSource`, `oauth2.S256ChallengeOption`, `oauth2.VerifierOption`, `oauth2.RetrieveError`
- `github.com/coreos/go-oidc/v3/oidc` — `oidc.NewProvider`, `oidc.Provider.Verifier`, `oidc.IDTokenVerifier.Verify`, `oidc.IDToken.Claims`, `oidc.Provider.Claims` (for end_session_endpoint)
- `github.com/zitadel/oidc/v3/pkg/client/rp` — `rp.NewRelyingPartyOIDC`, `rp.AuthURLHandler`, `rp.CodeExchangeHandler`, `rp.RefreshTokens`
- `github.com/alexedwards/scs/v2` — `scs.New`, `scs.SessionManager.LoadAndSave`, `sm.Put`, `sm.GetBytes`, `sm.Destroy`, `sm.RenewToken`
- `github.com/alexedwards/scs/goredisstore` — `goredisstore.New` (Redis backend for scs)
- `github.com/rbcervilla/redisstore/v9` — Redis backend for gorilla/sessions (alternative)
- IETF RFC 9700 — OAuth 2.0 Security Best Current Practice (February 2025)
- IETF RFC 7636 — PKCE (Proof Key for Code Exchange)
- OpenID Connect RP-Initiated Logout 1.0 — `end_session_endpoint` usage

---

## Architecture Documentation

### Recommended Library Stack for splitkauf

```
Authentication: coreos/go-oidc/v3 + golang.org/x/oauth2
  → Works with both Zitadel and Keycloak
  → Mature, widely tested, stable API

  Alternative: zitadel/oidc/v3
  → Higher-level RP helpers (fewer lines for handlers)
  → First-class Zitadel support; also works with Keycloak

Session Management: alexedwards/scs/v2 + goredisstore
  → Clean middleware pattern
  → Typed accessors (no gob registration)
  → Redis backend for production (survives restarts, supports horizontal scale)
  → In-memory store for development/testing (memstore)
```

### Environment Variables

```
OIDC_ISSUER            = https://myinstance.zitadel.cloud
                       | https://keycloak.example.com/realms/splitkauf
OIDC_CLIENT_ID         = splitkauf-backend
OIDC_CLIENT_SECRET     = <secret>
OIDC_REDIRECT_URL      = https://splitkauf.example.com/api/auth/callback
APP_BASE_URL           = https://splitkauf.example.com
REDIS_ADDR             = localhost:6379
SESSION_KEY            = <32-byte random key for cookie signing, if needed>
```

### Zitadel vs Keycloak: Practical Differences

| Aspect | Zitadel | Keycloak |
|---|---|---|
| Self-hosted setup | Docker image, single binary | Docker + Postgres; more complex |
| Go SDK | `zitadel/oidc` (official) | Use `coreos/go-oidc` |
| Roles in token | Custom claim: `urn:zitadel:iam:org:project:roles` | `realm_access.roles` + `resource_access.<client>.roles` |
| Refresh token rotation | ON by default | Must enable: Revoke Refresh Token = ON |
| Access token lifetime | Default 12h (reduce to 5-15 min) | Default 5 min (good) |
| End session endpoint | `/oidc/v1/end_session` | `/realms/<realm>/protocol/openid-connect/logout` |
| `id_token_hint` required for logout | Strongly recommended | Required or shows confirmation page |

---

## Open Questions

1. **Does splitkauf need multi-tenancy?** Zitadel's organization model maps well to multi-tenant apps. If splitkauf is single-tenant (one organization, multiple users), this is not needed.

2. **What upstream services does the access token need to call?** If the Go backend itself is the only resource server (no separate microservices), the access token may only be needed internally. In that case, the ID token claims are sufficient for the session.

3. **Is Redis available in the deployment environment?** If not, `scs/postgresstore` is the next best option if a Postgres database is available. SQLite works for single-instance deployments.

4. **Role model for splitkauf:** Where do roles (admin, member, viewer) come from? Direct IdP claims (Keycloak realm roles / Zitadel project roles) or a separate database table?

5. **PKCE-only (public) vs. confidential client:** Since the Go backend is a server-side process with a client secret, it should be a **confidential client** (client_id + client_secret + PKCE). This is more secure than PKCE-only.

6. **Backchannel logout:** If users may be logged out server-side (e.g., admin-initiated logout from IdP console), implement Keycloak's backchannel logout endpoint or Zitadel's equivalent to proactively invalidate server-side sessions.
