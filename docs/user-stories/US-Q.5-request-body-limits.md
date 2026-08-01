# US-Q.5 — Request body limits

**Milestone:** M5
**Depends on:** US-Q.2

**As an** operator, **I want** every API request body capped at a sane size,
**so that** a single oversized or malicious request cannot exhaust the
instance's memory.

## Acceptance criteria

- Every `/api/v1` request body is limited to 1 MiB via `http.MaxBytesReader`
  (installed by middleware before any body is read).
- A request whose body exceeds the limit receives an RFC 9457 problem with
  status 413 and a registered `payload-too-large` problem type (explanation
  page resolves; covered by the registry drift test).
- Requests declaring an oversized `Content-Length` are rejected with 413
  before the body is read.

## Origin

Deferred finding from the M1 go-review: `io.ReadAll(r.Body)` and
`json.NewDecoder(r.Body)` in `ports/rest/v1/handlers_lists.go` read request
bodies unbounded.
