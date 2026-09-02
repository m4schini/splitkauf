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
COPY openapi.yaml ./
# Per-build cache buster for the frontend's persisted query cache
# (frontend/src/queryClient.ts). CI passes the git SHA; .git is dockerignored,
# so it cannot be derived here. An empty value falls back to 'dev' in the app.
ARG VITE_BUILD_ID=
RUN VITE_BUILD_ID="$VITE_BUILD_ID" npm run build --prefix frontend

# ── Go build ────────────────────────────────────────────────────────────
# The builder runs natively on the build host and cross-compiles for the
# requested target platform (CGO is off, so no cross toolchain is needed).
# BuildKit/buildah set TARGETOS/TARGETARCH from --platform, defaulting to the
# host platform, so a plain build on an amd64 host still yields linux/amd64.
FROM --platform=$BUILDPLATFORM golang:1.26 AS builder

ARG TARGETOS
ARG TARGETARCH

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
    CGO_ENABLED=0 GOOS="$TARGETOS" GOARCH="$TARGETARCH" \
    go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /app .

# ── Final image ─────────────────────────────────────────────────────────
FROM gcr.io/distroless/static:nonroot

COPY --from=builder /app /app

ENTRYPOINT ["/app"]
CMD ["serve"]
