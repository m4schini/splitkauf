// SPDX-License-Identifier: CC0-1.0

package auth

import (
	"net"
	"net/http"
	"sync"
	"time"
)

// Login throttling. Every credential POST spends a token from a per-client-IP
// bucket and, once the username is known, from a per-username bucket, both
// checked BEFORE the bcrypt comparison so a throttled request costs no CPU. A
// successful login refills the username bucket (the legitimate user is not
// punished for earlier typos); the IP bucket is never refilled early, so one
// valid account cannot be used to reset guessing against others.
const (
	// loginIPBurst / loginIPInterval: 10 attempts up front, then one every
	// 6s (10/min sustained) per client IP.
	loginIPBurst    = 10
	loginIPInterval = 6 * time.Second
	// loginUserBurst / loginUserInterval: 5 attempts up front, then one every
	// 12s (5/min sustained) per username, across all IPs.
	loginUserBurst    = 5
	loginUserInterval = 12 * time.Second
	// loginLimiterMaxKeys bounds each limiter's memory. Idle buckets are pruned
	// once they have fully refilled; if the map is still full, new keys are
	// refused (fail closed) rather than growing without bound.
	loginLimiterMaxKeys = 10_000
	// loginLimiterPruneEvery is how often stale buckets are swept.
	loginLimiterPruneEvery = time.Minute
)

// bucket is one key's token-bucket state.
type bucket struct {
	tokens float64
	last   time.Time
}

// keyedLimiter is a small stdlib-only token-bucket rate limiter keyed by an
// arbitrary string. It is safe for concurrent use and bounded in memory.
type keyedLimiter struct {
	mu        sync.Mutex
	buckets   map[string]*bucket
	burst     float64
	interval  time.Duration // time to regain one token
	maxKeys   int
	lastPrune time.Time
	now       func() time.Time
}

func newKeyedLimiter(burst int, interval time.Duration, maxKeys int) *keyedLimiter {
	return &keyedLimiter{
		mu:        sync.Mutex{},
		buckets:   make(map[string]*bucket),
		burst:     float64(burst),
		interval:  interval,
		maxKeys:   maxKeys,
		lastPrune: time.Time{},
		now:       time.Now,
	}
}

// allow spends one token for key. When no token is available it returns false
// and how long until the next token accrues (for Retry-After).
func (l *keyedLimiter) allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := l.now()
	l.pruneLocked(now)

	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) >= l.maxKeys {
			// Full even after pruning: fail closed until buckets go stale.
			return false, l.interval
		}

		b = &bucket{tokens: l.burst, last: now}
		l.buckets[key] = b
	}

	l.refillLocked(b, now)

	if b.tokens < 1 {
		wait := time.Duration((1 - b.tokens) * float64(l.interval))

		return false, wait
	}

	b.tokens--

	return true, 0
}

// reset forgets key, restoring its full burst.
func (l *keyedLimiter) reset(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	delete(l.buckets, key)
}

func (l *keyedLimiter) refillLocked(b *bucket, now time.Time) {
	elapsed := now.Sub(b.last)
	if elapsed > 0 {
		b.tokens += float64(elapsed) / float64(l.interval)
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}

	b.last = now
}

// pruneLocked drops buckets that have fully refilled (equivalent to a fresh
// bucket), at most once per loginLimiterPruneEvery, or immediately when the
// map is at capacity.
func (l *keyedLimiter) pruneLocked(now time.Time) {
	if len(l.buckets) < l.maxKeys && now.Sub(l.lastPrune) < loginLimiterPruneEvery {
		return
	}

	l.lastPrune = now

	for key, b := range l.buckets {
		l.refillLocked(b, now)

		if b.tokens >= l.burst {
			delete(l.buckets, key)
		}
	}
}

// clientIP returns the host part of the request's RemoteAddr. Forwarding
// headers (X-Forwarded-For etc.) are deliberately ignored: the server has no
// trusted-proxy configuration, so they are client-controlled and would let an
// attacker pick a fresh key per request.
func clientIP(req *http.Request) string {
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}

	return host
}
