---
date: 2026-07-21T18:17:10.547473+00:00
git_commit: 144d9d92cc5004b195151cca3329dfc2f0ea7883
branch: main
topic: "RFC 9457: Problem Details for HTTP APIs"
tags: [research, web, rfc9457, http, errors, api, go, typescript, openapi]
status: complete
---

# Research: RFC 9457 — Problem Details for HTTP APIs

## Research Question

What is RFC 9457 and how should it be applied to a Go + React/TypeScript stack (chi router, oapi-codegen, Vite/React frontend)?

## Summary

RFC 9457 (*Problem Details for HTTP APIs*, published July 2023, IETF Standards Track) is the successor to RFC 7807. It defines a **standardised JSON (and XML) format for HTTP API error responses**, identified by the media type `application/problem+json`. The goal is to give machine-readable, self-describing error bodies to API consumers without forcing each API to invent its own error schema.

The document introduces **no breaking changes** over RFC 7807. The only major additions are a formal **IANA Problem Types Registry** and clearer guidance on handling multiple concurrent problems. All existing RFC 7807 clients and servers are compatible.

```
┌─────────────────────────────────────────────────────────────────┐
│  RFC 9457 Problem Detail Object (application/problem+json)      │
├─────────────────────────────────────────────────────────────────┤
│  {                                                              │
│    "type":     "https://example.com/probs/out-of-credit",   ← URI │
│    "title":    "You do not have enough credit.",             ← static │
│    "status":   403,                                          ← HTTP code │
│    "detail":   "Your balance is 30, but that costs 50.",    ← per-occurrence │
│    "instance": "/account/12345/msgs/abc",                   ← occurrence URI │
│                                                                 │
│    "balance":  30,       ← extension member (problem-type-specific) │
│    "accounts": ["/account/12345", "/account/67890"]         ← extension │
│  }                                                              │
└─────────────────────────────────────────────────────────────────┘
```

Key files in this repo (none yet; this research informs future implementation):

```
splitkauf/
└── (no RFC 9457 implementation yet)
```

---

## Detailed Findings

### 1. Core Specification

**Publication**: RFC 9457, July 2023. Authors: M. Nottingham, E. Wilde, S. Dalal.
Obsoletes: RFC 7807 (March 2016).
Track: IETF Standards Track (Proposed Standard).
Reference: [rfc-editor.org/rfc/rfc9457](https://www.rfc-editor.org/rfc/rfc9457.html)

**Purpose**: HTTP status codes cannot always convey enough error context for API clients. RFC 9457 defines a JSON (and XML) document format — "problem details" — that carries structured, machine-readable error information beyond what a status code alone expresses.

---

### 2. The Five Standard Members

All members are OPTIONAL in the schema but carry normative semantics when present.

| Member | Type | Semantics |
|---|---|---|
| `type` | URI reference | Identifies the problem type. When absent, defaults to `"about:blank"`. If an HTTP/HTTPS URI, SHOULD resolve to human-readable HTML documentation. MAY be a non-resolvable URI (e.g. `tag:` scheme). |
| `title` | string | Short, human-readable summary of the problem **type** (not occurrence). Does NOT vary between occurrences. Advisory only — clients MUST NOT rely on it programmatically. |
| `status` | integer | The HTTP status code generated for this occurrence. SHOULD match the actual response status code. |
| `detail` | string | Human-readable explanation **specific to this occurrence**. SHOULD help the client correct the problem. Clients MUST NOT parse `detail` for programmatic information — use extension members instead. |
| `instance` | URI reference | Identifies the **specific occurrence**. Useful for logging and support reference. May not be actionable by the client. |

**`about:blank` semantics**: when `type` is absent or set to `"about:blank"`, the problem carries no additional semantics beyond the HTTP status code. `title` MUST be the standard HTTP reason phrase for that code (e.g. `"Not Found"` for 404, `"Too Many Requests"` for 429). This is the IANA-registered entry with the title "See HTTP Status Code".

---

### 3. Extension Members

Problem type definitions MAY extend the problem details object with additional members. Extensions are **scoped to the problem type** — there are no "global" extension fields.

Clients MUST ignore unrecognised extension members; this allows evolution without breaking clients.

Common extension patterns:
```json
// Validation errors — multiple field failures
{
  "type": "https://example.com/probs/validation",
  "title": "Validation Failed",
  "status": 400,
  "detail": "One or more fields are invalid.",
  "errors": [
    { "detail": "must not be blank",   "pointer": "/name" },
    { "detail": "must be positive",    "pointer": "/amount" }
  ]
}

// Rate limiting
{
  "type": "https://example.com/probs/rate-limit",
  "title": "Too Many Requests",
  "status": 429,
  "retryAfterSeconds": 60
}

// Observability
{
  "type": "https://example.com/probs/internal",
  "title": "Internal Server Error",
  "status": 500,
  "traceId": "a1b2c3d4-e5f6"
}
```

For validation errors, the RFC mentions a `"errors"` array extension with per-error `"detail"` and `"pointer"` (a JSON Pointer [RFC 6901]) as the canonical pattern. When multiple simultaneous problems occur, the RFC RECOMMENDS returning the most relevant one rather than inventing a generic "batch" problem type (which maps poorly to HTTP semantics).

---

### 4. Media Types

| Media Type | Format | Note |
|---|---|---|
| `application/problem+json` | JSON | Primary; use in `Content-Type` response header |
| `application/problem+xml` | XML | Appendix B of RFC 9457; XML namespace `urn:ietf:rfc:7807` preserved for backward compat |

Servers MUST set `Content-Type: application/problem+json` when returning problem details as JSON. Clients SHOULD check `Content-Type` before attempting to parse a problem detail body.

---

### 5. RFC 9457 vs RFC 7807 Differences

| Feature | RFC 7807 | RFC 9457 |
|---|---|---|
| Status | Obsolete | Current (July 2023) |
| JSON media type | `application/problem+json` | Same |
| XML media type | `application/problem+xml` | Same |
| XML namespace | `urn:ietf:rfc:7807` | Same (preserved for compatibility) |
| Extension members | Allowed; clients ignore unknown | Same |
| IANA Problem Types Registry | Not present | ✅ Introduced |
| Multiple problems guidance | Not addressed | Added (use most-relevant) |
| Non-resolvable URIs | Allowed | Clarified; resolvable absolute URIs preferred |
| Breaking changes | — | None |

---

### 6. Security Considerations (Section 5 of RFC 9457)

- Generators MUST carefully vet what information goes into each field; problem details can inadvertently leak implementation internals (stack traces, internal paths, database identifiers).
- `detail` and `instance` are the highest-risk fields for information disclosure.
- Links in `instance` to occurrence log endpoints SHOULD NOT expose stack traces or other sensitive server details via the HTTP interface.
- The `type` URI may be non-resolvable (e.g. `tag:` scheme, URN) — this is valid, but the spec encourages resolvable absolute URIs because switching later is a breaking change if tooling begins resolving types.
- Prefix `"https://iana.org/assignments/http-problem-types#"` is available for type URIs registered in the IANA registry, though those URIs may not themselves resolve.

---

### 7. IANA Problem Types Registry

RFC 9457 introduces a formal IANA registry for common, reusable problem type URIs. Registered entries include the `about:blank` default ("See HTTP Status Code"). Implementors can register custom types via IETF process. Prefix for registered types: `https://iana.org/assignments/http-problem-types#`.

---

### 8. OpenAPI Integration

RFC 9457 is NOT part of the OpenAPI Specification. It must be integrated manually:

```yaml
components:
  schemas:
    ProblemDetail:
      type: object
      properties:
        type:
          type: string
          format: uri
          default: "about:blank"
        title:
          type: string
        status:
          type: integer
        detail:
          type: string
        instance:
          type: string
          format: uri
      additionalProperties: true   # allows extension members
```

Then reference `$ref: '#/components/schemas/ProblemDetail'` in 4xx/5xx response content for `application/problem+json`.

---

### 9. Go Implementation Options

Two community libraries exist; both implement RFC 9457:

**`kodeart/go-problem`** — minimal, has native chi integration:
```go
problem.New().
    WithStatus(http.StatusServiceUnavailable).
    WithExtension("maintenance", true).
    JSON(w)
```

**`neocotic/go-problem`** — more flexible, builder + functional options API:
```go
problem.Build().
    Definition(myProbType).
    Detail("balance is insufficient").
    Wrap(err).
    Problem()
```

**oapi-codegen + chi**: The default `ErrorHandler` in the chi-middleware package returns plain text or a simple JSON error, not RFC 9457. A custom `ErrorHandler` (via `Options`) must be provided to return `application/problem+json` responses.

**No external library required** for simple cases. A minimal struct suffices:
```go
type Problem struct {
    Type     string `json:"type,omitempty"`
    Title    string `json:"title,omitempty"`
    Status   int    `json:"status,omitempty"`
    Detail   string `json:"detail,omitempty"`
    Instance string `json:"instance,omitempty"`
}

func WriteProblem(w http.ResponseWriter, p Problem) {
    w.Header().Set("Content-Type", "application/problem+json")
    w.WriteHeader(p.Status)
    json.NewEncoder(w).Encode(p)
}
```

---

### 10. React/TypeScript Client Handling

The standard enables a single, typed error-handling path in frontend code:

```typescript
interface ProblemDetail {
  type?: string;      // URI, default "about:blank"
  title?: string;
  status?: number;
  detail?: string;
  instance?: string;
  [key: string]: unknown;   // extension members
}

async function apiFetch(input: RequestInfo, init?: RequestInit) {
  const res = await fetch(input, init);
  if (!res.ok) {
    const ct = res.headers.get("content-type") ?? "";
    if (ct.includes("application/problem+json")) {
      const problem: ProblemDetail = await res.json();
      throw problem;
    }
    throw { status: res.status, title: res.statusText };
  }
  return res.json();
}
```

Centralising the check for `application/problem+json` in a fetch wrapper or Axios interceptor means every endpoint's error can be rendered uniformly without per-endpoint custom parsing.

---

## Architecture Documentation

RFC 9457 is a **wire format standard**, not a framework. Its adoption in splitkauf would involve:

1. **OpenAPI spec** (`splitkauf.openapi.yaml`): define `ProblemDetail` in `components/schemas`; reference it in all 4xx/5xx responses.
2. **Go server** (`ports/rest/v1/api.go`): return `application/problem+json` from error paths; wire a custom oapi-codegen `ErrorHandler` to emit RFC 9457 bodies for validation failures.
3. **React frontend** (`frontend/src/`): typed `ProblemDetail` interface; centralised fetch wrapper checking `Content-Type`.

This is consistent with the hexagonal layout already planned in `docs/agents/plans/2026-07-21-project-setup.md`.

---

## Open Questions

- Whether to adopt a library (`kodeart/go-problem`, `neocotic/go-problem`) or write a minimal in-house struct. The library approach adds a dependency; the in-house approach is ~20 lines and sufficient for early phases.
- Extension member set for splitkauf-specific errors (e.g. validation field pointers, idempotency keys) — to be defined when the first domain errors are implemented.
- Whether to register a splitkauf-specific `type` URI namespace (e.g. `https://splitkauf.example.com/probs/`) or rely on `about:blank` for early development.

---

## Sources

- [RFC 9457 — RFC Editor](https://www.rfc-editor.org/rfc/rfc9457.html)
- [RFC 9457 — IETF Datatracker](https://datatracker.ietf.org/doc/rfc9457/)
- [RFC 9457: Doing API Errors Well — Swagger/SmartBear](https://swagger.io/blog/problem-details-rfc9457-doing-api-errors-well/)
- [RFC 9457: Better information for bad situations — Redocly](https://redocly.com/blog/problem-details-9457)
- [RFC 7807 vs RFC 9457 Deep Dive — Codecentric](https://www.codecentric.de/en/knowledge-hub/blog/charge-your-apis-volume-19-understanding-problem-details-for-http-apis-a-deep-dive-into-rfc-7807-and-rfc-9457)
- [RFC 9457 JSON Examples — Dev Kraken](https://devkraken.com/blog/rfc-9457-problem-details-json-examples/)
- [Problem Details explained — http.dev](https://http.dev/problem-details)
- [Building API Problem Details — OneUptime (2026)](https://oneuptime.com/blog/post/2026-01-30-api-problem-details/view)
- [neocotic/go-problem — GitHub](https://github.com/neocotic/go-problem)
- [kodeart/go-problem — GitHub](https://github.com/kodeart/go-problem)
- [rfc-9457 topic — GitHub](https://github.com/topics/rfc-9457)
