GO ?= go

GOFUMPT_PACKAGE ?= mvdan.cc/gofumpt@v0.7.0
GOLANGCI_LINT_PACKAGE ?= github.com/golangci/golangci-lint/cmd/golangci-lint@v1.62.2
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
	@echo " - fmt                  format Go code"
	@echo " - fmt-check            check Go formatting"
	@echo " - lint                 lint Go files"
	@echo " - lint-fix             lint Go files and fix issues"
	@echo " - lint-vet             run go vet"
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
fmt:
	$(GO) run $(GOFUMPT_PACKAGE) -w .

.PHONY: fmt-check
fmt-check: fmt
	@diff=$$(git diff --color=always); \
	if [ -n "$$diff" ]; then \
		echo "Please run 'make fmt' and commit the result:"; \
		echo "$${diff}"; \
		exit 1; \
	fi

.PHONY: lint
lint:
	$(GO) run $(GOLANGCI_LINT_PACKAGE) run

.PHONY: lint-fix
lint-fix:
	$(GO) run $(GOLANGCI_LINT_PACKAGE) run --fix

.PHONY: lint-vet
lint-vet:
	@echo "Running go vet..."
	@$(GO) vet ./...

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
	$(GO) install $(GOFUMPT_PACKAGE)
	$(GO) install $(GOLANGCI_LINT_PACKAGE)
	$(GO) install $(GOVULNCHECK_PACKAGE)

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
check: fmt-check lint lint-vet tidy-check test-unit security frontend-check
