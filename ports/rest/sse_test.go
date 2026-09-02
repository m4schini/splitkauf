// SPDX-License-Identifier: CC0-1.0

package rest_test

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alexedwards/scs/v2"

	"github.com/m4schini/splitkauf/auth"
	"github.com/m4schini/splitkauf/config"
	"github.com/m4schini/splitkauf/events"
	"github.com/m4schini/splitkauf/ports/rest"
	v1 "github.com/m4schini/splitkauf/ports/rest/v1"
)

// sseServer builds a full dev-mode REST handler wired to the given broker and
// starts it. Dev-auth injects a user, so the guarded /api/v1/events stream
// opens without a real session.
func sseServer(t *testing.T, broker *events.Broker) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(sseHandlerFor(t, broker))
	t.Cleanup(srv.Close)

	return srv
}

// sseHandlerFor builds the full dev-mode REST handler wired to broker.
func sseHandlerFor(t *testing.T, broker *events.Broker) http.Handler {
	t.Helper()

	sessions := scs.New()

	var cfg config.Config

	authr, err := auth.New(context.Background(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (dev): %v", err)
	}

	return rest.New(&v1.V1{DB: nil, Service: nil, Events: nil}, sessions, authr, broker)
}

// TestSSEStreamsPublishedEvent opens the SSE stream, publishes an event on the
// broker, and asserts it arrives as a `data:` frame carrying the JSON payload.
// Cancelling the request context ends the stream (and, via the handler's
// context-done path, unsubscribes) — exercising the no-leak shutdown path.
func TestSSEStreamsPublishedEvent(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	srv := sseServer(t, broker)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	if cc := resp.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("Cache-Control = %q, want no-cache", cc)
	}

	if conn := resp.Header.Get("Connection"); conn != "keep-alive" {
		t.Errorf("Connection = %q, want keep-alive", conn)
	}

	// The subscription is registered by the time the response headers flush;
	// wait briefly for the broker to report the subscriber, then publish.
	waitForSubscribers(t, broker, 1)
	broker.Publish(events.Event{Type: events.TypeItems, ListID: "list-123"})

	line := readDataLine(t, resp.Body)
	if !strings.Contains(line, `"type":"items"`) || !strings.Contains(line, `"listId":"list-123"`) {
		t.Errorf("data frame = %q, want items/list-123 JSON", line)
	}

	// Cancelling ends the stream; the handler returns and unsubscribes.
	cancel()
	waitForSubscribers(t, broker, 0)
}

// TestSSEHandlerReturnsOnContextCancel proves the handler exits (and drops its
// subscription) when the request context is cancelled — no goroutine/subscriber
// leak on client disconnect.
func TestSSEHandlerReturnsOnContextCancel(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()
	srv := sseServer(t, broker)

	ctx, cancel := context.WithCancel(context.Background())

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}

	waitForSubscribers(t, broker, 1)

	cancel()

	_ = resp.Body.Close()

	// The subscriber must be gone once the handler observes the cancellation.
	waitForSubscribers(t, broker, 0)
}

// TestSSEStreamEndsOnServerShutdown proves an open SSE stream does not stall a
// graceful shutdown: with broker.Close registered as a shutdown hook (as
// cmd/serve does), Shutdown closes the subscription, the handler returns, and
// Shutdown completes well before its deadline instead of timing out.
func TestSSEStreamEndsOnServerShutdown(t *testing.T) {
	t.Parallel()

	broker := events.NewBroker()

	srv := httptest.NewUnstartedServer(sseHandlerFor(t, broker))
	srv.Config.RegisterOnShutdown(broker.Close)
	srv.Start()
	t.Cleanup(srv.Close)

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, srv.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET /api/v1/events: %v", err)
	}

	defer func() { _ = resp.Body.Close() }()

	waitForSubscribers(t, broker, 1)

	// A generous deadline: a hanging stream would consume all of it and fail
	// with context.DeadlineExceeded.
	shutdownCtx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	if err := srv.Config.Shutdown(shutdownCtx); err != nil {
		t.Fatalf("Shutdown with an open SSE stream: %v", err)
	}

	if broker.Count() != 0 {
		t.Errorf("broker subscriber count after shutdown = %d, want 0", broker.Count())
	}

	// The stream ended cleanly from the client's point of view too.
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		t.Errorf("reading stream to end after shutdown: %v", err)
	}
}

// TestSSERequiresAuth confirms the stream is behind RequireAuth: in OIDC mode
// with no session, the request is rejected with 401 (never opening a stream).
func TestSSERequiresAuth(t *testing.T) {
	t.Parallel()

	sessions := scs.New()

	var cfg config.Config

	cfg.Auth.OIDC.Issuer = newDiscoveryServer(t)
	cfg.Auth.OIDC.ClientID = "client-id"
	cfg.Auth.OIDC.ClientSecret = "client-secret"
	cfg.Auth.OIDC.RedirectURL = "https://app.example.com/api/auth/callback"

	authr, err := auth.New(context.Background(), &cfg, sessions, noopMembers{}, nil)
	if err != nil {
		t.Fatalf("auth.New (oidc): %v", err)
	}

	srv := httptest.NewServer(rest.New(&v1.V1{DB: nil, Service: nil, Events: nil}, sessions, authr, events.NewBroker()))
	t.Cleanup(srv.Close)

	resp := testGet(t, srv.URL+"/api/v1/events")

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", resp.StatusCode)
	}
}

// newDiscoveryServer starts an httptest server serving a minimal OIDC discovery
// document so auth.New's provider discovery succeeds without a live IdP.
func newDiscoveryServer(t *testing.T) string {
	t.Helper()

	mux := http.NewServeMux()

	var issuer string

	mux.HandleFunc("/.well-known/openid-configuration", func(writer http.ResponseWriter, _ *http.Request) {
		doc := map[string]any{
			"issuer":                                issuer,
			"authorization_endpoint":                issuer + "/authorize",
			"token_endpoint":                        issuer + "/token",
			"jwks_uri":                              issuer + "/jwks",
			"end_session_endpoint":                  issuer + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		}

		writer.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(writer).Encode(doc)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	issuer = srv.URL

	return issuer
}

// readDataLine reads from an SSE stream until it sees a `data:` line, returning
// its content. Heartbeat comment lines (`: ping`) and blank separators are
// skipped. It fails on timeout via a background deadline on the underlying
// request context (the caller's context cancellation).
func readDataLine(t *testing.T, reader io.Reader) string {
	t.Helper()

	type result struct {
		line string
		err  error
	}

	done := make(chan result, 1)

	go func() {
		scanner := bufio.NewScanner(reader)
		for scanner.Scan() {
			line := scanner.Text()
			if after, ok := strings.CutPrefix(line, "data:"); ok {
				done <- result{line: strings.TrimSpace(after), err: nil}

				return
			}
		}

		done <- result{line: "", err: scanner.Err()}
	}()

	select {
	case res := <-done:
		if res.line == "" {
			t.Fatalf("stream ended before a data frame: %v", res.err)
		}

		return res.line
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for a data frame")

		return ""
	}
}

// waitForSubscribers polls the broker until it reports want subscribers.
func waitForSubscribers(t *testing.T, broker *events.Broker, want int) {
	t.Helper()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if broker.Count() == want {
			return
		}

		time.Sleep(5 * time.Millisecond)
	}

	t.Fatalf("broker subscriber count = %d, want %d", broker.Count(), want)
}
