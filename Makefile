GO ?= go

# Passed through to `go test` by test-unit only; CI sets it to `-json` so the
# dashboard can count pass/skip/fail. Empty by default: local `make
# test-unit` output stays byte-identical to before this existed.
GOTESTFLAGS ?=

# golangci-lint is expected on PATH (v2.12.2 — the pin lives in
# .pre-commit-config.yaml and .github/workflows/quality.yml, kept in sync by
# hack/lint/check-golangci-pin.sh).
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK_PACKAGE ?= golang.org/x/vuln/cmd/govulncheck@v1

.PHONY: all
all: build

.PHONY: help
help:
	@echo "Make Routines:"
	@echo " - build                build everything"
	@echo " - dist                 build the release binary (real frontend embedded)"
	@echo " - generate             run code generation"
	@echo " - test                 run full test suite (race, shuffle; DB tests need SPLITKAUF_TEST_DATABASE_DSN)"
	@echo " - test-short           run short tests (shuffle) -- pre-push budget"
	@echo " - test-unit            run unit tests (race, short, shuffle, coverage) -- CI test contract"
	@echo " - coverage             report function coverage from coverage.out"
	@echo " - fmt                  format Go code (golangci-lint fmt)"
	@echo " - fmt-check            check Go formatting (non-mutating)"
	@echo " - lint                 lint Go files"
	@echo " - lint-fix             lint Go files and fix issues"
	@echo " - lint-config          verify .golangci.yml"
	@echo " - tidy                 run go mod tidy"
	@echo " - tidy-check           check go.mod/go.sum are tidy"
	@echo " - security             run govulncheck + trivy (trivy optional; REQUIRE_TRIVY=1 to enforce)"
	@echo " - deps                 install tool dependencies"
	@echo " - frontend-deps        install frontend dependencies"
	@echo " - frontend-build       build the frontend into ports/web/dist"
	@echo " - frontend-check       run frontend lint/format/typecheck/test"
	@echo " - check                run every local gate"

# ── Embedded frontend stub ─────────────────────────────────────────────
# ports/web embeds ports/web/dist via go:embed. The real content is built by
# the frontend (see frontend-build / dist). This stub rule creates a minimal
# placeholder so the Go packages compile without a frontend build. The dist
# directory is gitignored; only oapi-codegen output is committed.
ports/web/dist/index.html:
	@mkdir -p ports/web/dist
	@printf '<!doctype html><title>splitkauf</title>\n' > $@

# ── Code generation ────────────────────────────────────────────────────
ports/rest/v1/api.go: splitkauf.openapi.yaml ports/rest/v1/config.yaml
	go generate ./ports/rest/v1/...
	@touch $@

client/gen.go: splitkauf.openapi.yaml client/config.yaml
	go generate ./client/...
	@touch $@

.PHONY: generate
generate: ports/web/dist/index.html ports/rest/v1/api.go client/gen.go

.PHONY: build
build: generate
	$(GO) build ./...

# dist builds the frontend and produces the single release binary embedding
# the real React app.
.PHONY: dist
dist: frontend-build generate
	$(GO) build -o splitkauf .

.PHONY: test
test: generate
	$(GO) test -race -shuffle=on ./...

.PHONY: test-short
test-short: generate
	$(GO) test -short -shuffle=on ./...

.PHONY: test-unit
test-unit: generate
	$(GO) test -race -short -shuffle=on -covermode=atomic -coverprofile=coverage.out $(GOTESTFLAGS) ./...

# Report-only: reads the profile test-unit produced; never gates.
.PHONY: coverage
coverage:
	@if [ ! -f coverage.out ]; then \
		echo "coverage.out not found; run 'make test-unit' first" >&2; \
		exit 1; \
	fi
	$(GO) tool cover -func=coverage.out

.PHONY: fmt
fmt: generate
	$(GOLANGCI_LINT) fmt

.PHONY: fmt-check
fmt-check: generate
	$(GOLANGCI_LINT) fmt --diff

.PHONY: lint
lint: generate
	$(GOLANGCI_LINT) run

.PHONY: lint-fix
lint-fix: generate
	$(GOLANGCI_LINT) run --fix

.PHONY: lint-config
lint-config:
	$(GOLANGCI_LINT) config verify

.PHONY: tidy
tidy:
	$(GO) mod tidy

.PHONY: tidy-check
tidy-check:
	$(GO) mod verify
	$(GO) mod tidy -diff

# Set REQUIRE_TRIVY=1 to hard-fail when trivy is missing (CI does this);
# locally trivy is optional and skipped with a warning.
REQUIRE_TRIVY ?=

.PHONY: security
security:
	$(GO) run $(GOVULNCHECK_PACKAGE) ./...
	@if command -v trivy >/dev/null 2>&1; then \
		trivy fs --scanners vuln,secret --exit-code 1 --severity HIGH,CRITICAL . && \
		trivy fs --scanners license --exit-code 0 .; \
	elif [ -n "$(REQUIRE_TRIVY)" ]; then \
		echo "error: trivy is required (REQUIRE_TRIVY is set) but not installed" >&2; \
		exit 1; \
	else \
		echo "warning: trivy not installed; skipping trivy scans (see trivy.yaml)" >&2; \
	fi

.PHONY: deps
deps:
	$(GO) install tool
	$(GO) install $(GOVULNCHECK_PACKAGE)
	@echo "note: golangci-lint v2.12.2 is expected on PATH:"
	@echo "  go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.12.2"

# ── Frontend (implemented in Phase 3) ──────────────────────────────────
.PHONY: frontend-deps
frontend-deps:
	npm ci --prefix frontend

# Per-build cache buster for the frontend's persisted query cache (see
# frontend/src/queryClient.ts); overridable for reproducible builds.
VITE_BUILD_ID ?= $(shell git rev-parse --short HEAD 2>/dev/null)

.PHONY: frontend-build
frontend-build:
	VITE_BUILD_ID=$(VITE_BUILD_ID) npm run build --prefix frontend

.PHONY: frontend-check
frontend-check:
	npm run lint --prefix frontend
	npm run format-check --prefix frontend
	npm run typecheck --prefix frontend
	npm run test --prefix frontend

# ── Aggregate local gate ───────────────────────────────────────────────
.PHONY: check
check: fmt-check lint-config lint tidy-check test-unit security frontend-check
