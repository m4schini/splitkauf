GO ?= go

# golangci-lint is expected on PATH (v2.12.2 — the pin lives in
# .pre-commit-config.yaml and .github/workflows/quality.yml, kept in sync by
# hack/lint/check-golangci-pin.sh).
GOLANGCI_LINT ?= golangci-lint
GOVULNCHECK_PACKAGE ?= golang.org/x/vuln/cmd/govulncheck@v1

RACE_ENABLED ?=
GOTESTFLAGS ?=
ifeq ($(RACE_ENABLED),true)
    GOTESTFLAGS += -race
endif

.PHONY: all
all: build

.PHONY: help
help:
	@echo "Make Routines:"
	@echo " - build                build everything"
	@echo " - dist                 build the release binary (real frontend embedded)"
	@echo " - generate             run code generation"
	@echo " - test                 run tests"
	@echo " - test-unit            run unit tests only (race, short, coverage) -- CI test contract"
	@echo " - coverage             run tests with coverage"
	@echo " - fmt                  format Go code (golangci-lint fmt)"
	@echo " - fmt-check            check Go formatting (non-mutating)"
	@echo " - lint                 lint Go files"
	@echo " - lint-fix             lint Go files and fix issues"
	@echo " - lint-config          verify .golangci.yml"
	@echo " - tidy                 run go mod tidy"
	@echo " - tidy-check           check go.mod/go.sum are tidy"
	@echo " - security             run vulnerability check"
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
	$(GO) test $(GOTESTFLAGS) ./...

.PHONY: coverage
coverage: generate
	$(GO) test $(GOTESTFLAGS) -cover -coverprofile=coverage.out ./...

.PHONY: test-unit
test-unit: generate
	$(GO) test -race -short -coverprofile=coverage.out ./...

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
	$(eval MIN_GO_VERSION := $(shell grep -Eo '^go\s+[0-9]+\.[0-9.]+' go.mod | cut -d' ' -f2))
	$(GO) mod tidy -compat=$(MIN_GO_VERSION)

.PHONY: tidy-check
tidy-check: tidy
	@diff=$$(git diff --color=always go.mod go.sum); \
	if [ -n "$$diff" ]; then \
		echo "Please run 'make tidy' and commit the result:"; \
		echo "$${diff}"; \
		exit 1; \
	fi

.PHONY: security
security:
	$(GO) run $(GOVULNCHECK_PACKAGE) ./...

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
