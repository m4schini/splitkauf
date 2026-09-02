// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"
	"go.uber.org/zap"

	"github.com/m4schini/splitkauf/ports/rest/problem"
)

// fakeClock is a manually advanced time source for the limiter.
type fakeClock struct{ t time.Time }

func (c *fakeClock) now() time.Time { return c.t }

func newTestLimiter(burst int, interval time.Duration, maxKeys int) (*keyedLimiter, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)}
	lim := newKeyedLimiter(burst, interval, maxKeys)
	lim.now = clock.now

	return lim, clock
}

func TestKeyedLimiterBurstThenRefill(t *testing.T) {
	t.Parallel()

	lim, clock := newTestLimiter(3, 10*time.Second, 100)

	for i := range 3 {
		if ok, _ := lim.allow("k"); !ok {
			t.Fatalf("attempt %d denied, want allowed within burst", i+1)
		}
	}

	ok, wait := lim.allow("k")
	if ok {
		t.Fatal("attempt past burst allowed, want denied")
	}

	if wait <= 0 || wait > 10*time.Second {
		t.Errorf("retry wait = %v, want in (0, 10s]", wait)
	}

	// Other keys are independent.
	if ok, _ := lim.allow("other"); !ok {
		t.Error("independent key denied")
	}

	clock.t = clock.t.Add(10 * time.Second)

	if ok, _ := lim.allow("k"); !ok {
		t.Error("attempt after one interval denied, want one refilled token")
	}

	if ok, _ := lim.allow("k"); ok {
		t.Error("second attempt after one interval allowed, want denied")
	}
}

func TestKeyedLimiterReset(t *testing.T) {
	t.Parallel()

	lim, _ := newTestLimiter(1, time.Minute, 100)

	lim.allow("k")

	if ok, _ := lim.allow("k"); ok {
		t.Fatal("want denied after burst")
	}

	lim.reset("k")

	if ok, _ := lim.allow("k"); !ok {
		t.Error("want allowed after reset")
	}
}

func TestKeyedLimiterBoundedMemory(t *testing.T) {
	t.Parallel()

	lim, clock := newTestLimiter(1, time.Second, 2)

	lim.allow("a")
	lim.allow("b")

	// At capacity with no stale buckets: new keys fail closed.
	if ok, _ := lim.allow("c"); ok {
		t.Fatal("new key allowed at capacity, want denied")
	}

	if got := len(lim.buckets); got != 2 {
		t.Fatalf("buckets = %d, want 2", got)
	}

	// Once the existing buckets have fully refilled they are pruned, freeing room.
	clock.t = clock.t.Add(2 * time.Second)

	if ok, _ := lim.allow("c"); !ok {
		t.Fatal("new key denied after stale buckets expired, want allowed")
	}

	if got := len(lim.buckets); got != 1 {
		t.Errorf("buckets = %d after prune, want 1", got)
	}
}

func TestClientIP(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"192.0.2.1:1234":     "192.0.2.1",
		"[2001:db8::1]:8080": "2001:db8::1",
		"not-a-hostport":     "not-a-hostport",
	}
	for remote, want := range cases {
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/", nil)
		req.RemoteAddr = remote
		req.Header.Set("X-Forwarded-For", "203.0.113.9")

		if got := clientIP(req); got != want {
			t.Errorf("clientIP(%q) = %q, want %q", remote, got, want)
		}
	}
}

// newThrottleTestPassword builds a password authenticator whose limiters run
// on a frozen clock (so bcrypt latency never refills a bucket mid-test). The
// users repository knows no accounts, so every credential attempt is a 401.
func newThrottleTestPassword() *passwordAuthenticator {
	a := newPassword(scs.New(), stubUsers{}, noopMembers{})
	a.logger = zap.NewNop()

	frozen := time.Date(2026, time.September, 29, 12, 0, 0, 0, time.UTC)
	a.ipLimiter.now = func() time.Time { return frozen }
	a.userLimiter.now = func() time.Time { return frozen }

	return a
}

func doLogin(t *testing.T, a *passwordAuthenticator, remote, body string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/auth/login", strings.NewReader(body))
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	a.Login(rec, req)

	return rec
}

// assertTooManyRequests checks a throttled login: 429, a problem body of the
// too-many-requests type, and a positive Retry-After.
func assertTooManyRequests(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}

	if ct := rec.Header().Get("Content-Type"); ct != problem.ContentType {
		t.Errorf("Content-Type = %q, want %q", ct, problem.ContentType)
	}

	if secs, err := strconv.Atoi(rec.Header().Get("Retry-After")); err != nil || secs < 1 {
		t.Errorf("Retry-After = %q, want a positive integer", rec.Header().Get("Retry-After"))
	}

	var prob problem.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &prob); err != nil {
		t.Fatalf("decoding problem: %v", err)
	}

	if prob.Type != problem.TooManyRequests.URI() {
		t.Errorf("problem type = %q, want %q", prob.Type, problem.TooManyRequests.URI())
	}
}

func TestPasswordLoginThrottlesPerUsername(t *testing.T) {
	t.Parallel()

	a := newThrottleTestPassword()

	// Each attempt comes from a different IP, so only the username limit applies.
	for i := range loginUserBurst {
		rec := doLogin(t, a, "192.0.2."+strconv.Itoa(i+1)+":1000", `{"username":"alex","password":"wrong"}`)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d status = %d, want 401", i+1, rec.Code)
		}
	}

	assertTooManyRequests(t, doLogin(t, a, "192.0.2.200:1000", `{"username":"alex","password":"wrong"}`))

	// Another username is unaffected.
	if rec := doLogin(t, a, "192.0.2.201:1000", `{"username":"bob","password":"wrong"}`); rec.Code != http.StatusUnauthorized {
		t.Errorf("other username status = %d, want 401", rec.Code)
	}
}

func TestPasswordLoginThrottlesPerClientIP(t *testing.T) {
	t.Parallel()

	a := newThrottleTestPassword()

	// The IP limit is checked before the body is even decoded, so malformed
	// requests spend it too (and cost no bcrypt).
	for i := range loginIPBurst {
		if rec := doLogin(t, a, "198.51.100.7:4000", `not json`); rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d status = %d, want 400", i+1, rec.Code)
		}
	}

	assertTooManyRequests(t, doLogin(t, a, "198.51.100.7:5000", `{"username":"alex","password":"x"}`))

	// A different client IP is unaffected.
	if rec := doLogin(t, a, "198.51.100.8:4000", `not json`); rec.Code != http.StatusBadRequest {
		t.Errorf("other IP status = %d, want 400", rec.Code)
	}
}

func TestPasswordLoginBcryptSlotRespectsCancellation(t *testing.T) {
	t.Parallel()

	a := newThrottleTestPassword()
	// Occupy every bcrypt slot so the login has to wait.
	for range cap(a.bcryptSem) {
		a.bcryptSem <- struct{}{}
	}

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/auth/login",
		strings.NewReader(`{"username":"alex","password":"x"}`))
	req.RemoteAddr = "203.0.113.5:1234"
	rec := httptest.NewRecorder()
	a.Login(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 when cancelled while waiting for a bcrypt slot", rec.Code)
	}
}
