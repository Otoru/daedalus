GO ?= go
GO_BIN := $(if $(shell $(GO) env GOBIN),$(shell $(GO) env GOBIN),$(shell $(GO) env GOPATH)/bin)

# Tool resolution. An explicit override (make BUF=/path/buf) always wins.
# Otherwise the tool is looked up first in GOBIN/GOPATH-bin (where
# `go install` places version-pinned tools, as on the local machine) and
# then on PATH (where the CI setup actions install them). A path fixed in
# GOBIN took down Geppetto's CI: there the action installs onto PATH and
# the binary did not exist in GOBIN. A target must never pass because the
# tool was absent: resolution failures fail loudly with an install
# instruction.
find-tool = $(firstword $(wildcard $(GO_BIN)/$(1)) $(shell command -v $(1) 2>/dev/null))

BUF ?= $(call find-tool,buf)
GOLANGCI_LINT ?= $(call find-tool,golangci-lint)

# The CI runs golangci-lint v2.14.0. Older binaries can panic on this
# module's Go version or report nothing in silence, so lint requires at
# least the CI version and fails loudly instead of producing a false green.
GOLANGCI_LINT_MIN := 2.14.0

# Version stamp for builds outside a git checkout. The release passes
# VERSION=vX.Y.Z on the command line, which overrides this value.
DEV_VERSION := dev
VERSION := $(shell git describe --always --dirty 2>/dev/null || echo $(DEV_VERSION))

# Cross-compile matrix as GOOS/GOARCH pairs; the windows entry gets .exe.
# Mirrors the supported systems in section 13 of the specification.
PLATFORMS := linux/amd64 darwin/arm64 windows/amd64

# The subprocess command (specification section 2) is implemented by the
# core front. While cmd/daedalus does not exist, build/build-all degrade to
# a compile check of every package (including cross-compilation), with a
# loud warning, instead of failing — and they go back to packaging the
# binary as soon as the command exists. This Makefile creates no .go file.
CMD_DIR := ./cmd/daedalus
HAS_CMD := $(wildcard cmd/daedalus)

.PHONY: generate lint test bench build build-all

generate:
	@test -n "$(BUF)" || { echo "buf not found; install with: $(GO) install github.com/bufbuild/buf/cmd/buf@latest (or pass BUF=/path/to/buf)" >&2; exit 1; }
	$(BUF) generate

lint:
	@test -n "$(BUF)" || { echo "buf not found; install with: $(GO) install github.com/bufbuild/buf/cmd/buf@latest (or pass BUF=/path/to/buf)" >&2; exit 1; }
	@test -n "$(GOLANGCI_LINT)" || { echo "golangci-lint not found; install with: $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_MIN) (or pass GOLANGCI_LINT=/path/to/golangci-lint)" >&2; exit 1; }
	@version=$$($(GOLANGCI_LINT) version 2>/dev/null | sed -n 's/.*has version \([0-9][0-9.]*\).*/\1/p'); \
	if [ -z "$$version" ]; then \
		echo "could not read the version of $(GOLANGCI_LINT); golangci-lint >= $(GOLANGCI_LINT_MIN) is required" >&2; exit 1; \
	fi; \
	if ! printf '%s %s\n' "$(GOLANGCI_LINT_MIN)" "$$version" | awk '{ split($$1, m, "."); split($$2, v, "."); for (i = 1; i <= 3; i++) { if (v[i]+0 > m[i]+0) exit 0; if (v[i]+0 < m[i]+0) exit 1 } }'; then \
		echo "golangci-lint $$version at $(GOLANGCI_LINT) is older than $(GOLANGCI_LINT_MIN), the CI version; an old linter can miss findings that fail this module. Install with: $(GO) install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v$(GOLANGCI_LINT_MIN)" >&2; exit 1; \
	fi
	$(BUF) lint
	$(GOLANGCI_LINT) run ./...

test:
	$(GO) test ./...

# The fixtures in testdata/golden are the v1 compatibility contract.
# Regenerating them is a deliberate act reserved for a major version
# change; there is no golden-update shortcut. Whoever genuinely needs
# to rewrite them runs:
#   go test ./ -run '^TestFrozenGoldenLayouts$' -update -count=1

# Generation benchmarks live in the root package. Navigation benchmarks
# live in utils/pathfinding. -benchmem records the allocations. The name
# is anchored so a substring cannot pull in another Benchmark.
bench:
	$(GO) test -run '^$$' -bench '^BenchmarkGenerate$$' -benchmem .
	$(GO) test -run '^$$' -bench '^BenchmarkComputeSteps$$' -benchmem ./utils/pathfinding
	$(GO) test -run '^$$' -bench '^BenchmarkComputeVisibility$$' -benchmem ./utils/vision
	$(GO) test -run '^$$' -bench '^BenchmarkComputeVisibilityIntoWarm$$' -benchmem ./utils/vision

build:
ifeq ($(HAS_CMD),)
	@echo "WARNING: cmd/daedalus does not exist yet (core front); checking compilation of every package." >&2
	$(GO) build ./...
else
	mkdir -p bin
	$(GO) build -trimpath -ldflags "-X main.Version=$(VERSION)" -o bin/daedalus $(CMD_DIR)
endif

build-all:
ifeq ($(HAS_CMD),)
	@echo "WARNING: cmd/daedalus does not exist yet (core front); checking cross-compilation of every package." >&2
	$(foreach platform,$(PLATFORMS),GOOS=$(word 1,$(subst /, ,$(platform))) GOARCH=$(word 2,$(subst /, ,$(platform))) $(GO) build ./...;)
else
	mkdir -p bin
	$(foreach platform,$(PLATFORMS),GOOS=$(word 1,$(subst /, ,$(platform))) GOARCH=$(word 2,$(subst /, ,$(platform))) $(GO) build -trimpath -ldflags "-X main.Version=$(VERSION)" -o bin/daedalus-$(subst /,-,$(platform))$(if $(filter windows/%,$(platform)),.exe) $(CMD_DIR);)
endif
