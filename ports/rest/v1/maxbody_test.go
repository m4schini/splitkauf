// SPDX-License-Identifier: TODO

package v1_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/m4schini/splitkauf/lists"
)

// nopCloserReader wraps an io.Reader without exposing *bytes.Reader (or any
// other type net/http special-cases to compute Content-Length), so
// http.NewRequest leaves ContentLength at 0 and the client sends the request
// chunked — simulating a body whose size is not declared up front.
type nopCloserReader struct {
	io.Reader
}

func (nopCloserReader) Close() error { return nil }

// TestOversizedContentLengthReturns413Problem proves the fast path (US-Q.5):
// a declared Content-Length above the 1 MiB cap is rejected with a 413
// problem+json response before the handler (and thus the service) ever runs.
func TestOversizedContentLengthReturns413Problem(t *testing.T) {
	svc := &fakeService{createList: func(_ context.Context, _ string, _ uuid.UUID) (lists.List, error) {
		t.Fatal("service should not be called for an oversized body")
		return lists.List{}, nil
	}}
	srv := newServer(t, svc)

	oversized := strings.Repeat("a", (1<<20)+1)
	body := fmt.Sprintf(`{"name":%q}`, oversized)

	resp, err := http.Post(srv.URL+"/api/v1/lists", "application/json", bytes.NewBufferString(body)) //nolint:gosec // url is test-controlled
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusRequestEntityTooLarge)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
	var prob struct {
		Type   string `json:"type"`
		Status int    `json:"status"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&prob); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if !strings.HasSuffix(prob.Type, "/payload-too-large") {
		t.Errorf("type = %q, want suffix /payload-too-large", prob.Type)
	}
	if prob.Status != http.StatusRequestEntityTooLarge {
		t.Errorf("status member = %d, want %d", prob.Status, http.StatusRequestEntityTooLarge)
	}
}

// TestOversizedBodyWithoutDeclaredLengthIsRejected proves the backstop path
// (US-Q.5 / M5 plan Key Decision 2): a body over the cap sent WITHOUT a
// declared Content-Length (chunked transfer) is still rejected once
// http.MaxBytesReader trips, whether that surfaces as a 413 (caught directly
// by the middleware's reader) or a 400 validation problem (the OpenAPI
// validator failing to decode the truncated read) — both are
// application/problem+json, and the service must never be reached.
func TestOversizedBodyWithoutDeclaredLengthIsRejected(t *testing.T) {
	svc := &fakeService{createList: func(_ context.Context, _ string, _ uuid.UUID) (lists.List, error) {
		t.Fatal("service should not be called for an oversized body")
		return lists.List{}, nil
	}}
	srv := newServer(t, svc)

	oversized := strings.Repeat("a", (1<<20)+1)
	body := fmt.Sprintf(`{"name":%q}`, oversized)

	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/v1/lists", nopCloserReader{strings.NewReader(body)})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if req.ContentLength != 0 {
		t.Fatalf("test setup: ContentLength = %d, want 0 (undeclared)", req.ContentLength)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusRequestEntityTooLarge && resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d or %d", resp.StatusCode, http.StatusRequestEntityTooLarge, http.StatusBadRequest)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/problem+json" {
		t.Errorf("content-type = %q, want application/problem+json", ct)
	}
}

// TestNormalSizedPostStillSucceeds proves MaxBody does not interfere with
// ordinary requests well under the cap.
func TestNormalSizedPostStillSucceeds(t *testing.T) {
	want := lists.List{ID: uuid.New(), Name: "Groceries", CreatedAt: time.Now(), UpdatedAt: time.Now()}
	svc := &fakeService{createList: func(_ context.Context, name string, _ uuid.UUID) (lists.List, error) {
		if name != "Groceries" {
			t.Errorf("service got name %q", name)
		}
		return want, nil
	}}
	srv := newServer(t, svc)

	resp := postJSON(t, srv.URL+"/api/v1/lists", `{"name":"Groceries"}`)
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("status = %d, want 201", resp.StatusCode)
	}
}
