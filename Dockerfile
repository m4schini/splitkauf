# syntax=docker/dockerfile:1

# ── Frontend build ──────────────────────────────────────────────────────
# vite.config.ts redirects build.outDir to ../ports/web/dist (relative to
# frontend/), which lands at /src/ports/web/dist in this stage — exactly
# where the Go builder stage below expects the embedded dist directory.
FROM node:22 AS frontend

WORKDIR /src

COPY frontend/package.json frontend/package-lock.json frontend/
RUN --mount=type=cache,target=/root/.npm \
    npm ci --prefix frontend

COPY frontend frontend/
COPY splitkauf.openapi.yaml ./
# Per-build cache buster for the frontend's persisted query cache
# (frontend/src/queryClient.ts). CI passes the git SHA; .git is dockerignored,
# so it cannot be derived here. An empty value falls back to 'dev' in the app.
ARG VITE_BUILD_ID=
RUN VITE_BUILD_ID="$VITE_BUILD_ID" npm run build --prefix frontend

# ── Go build ────────────────────────────────────────────────────────────
FROM golang:1.26 AS builder

WORKDIR /src

# Download dependencies as a separate cached layer
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/root/go/pkg/mod \
    go mod download

# Build — generated files (openapi client/server stubs) are committed to the
# repo, so no `go generate` is needed here.
COPY . .
COPY --from=frontend /src/ports/web/dist ports/web/dist
RUN --mount=type=cache,target=/root/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux GOARCH=amd64 \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /app .

# ── Final image ─────────────────────────────────────────────────────────
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
CMD ["serve"]
