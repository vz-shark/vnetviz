# vnetviz Makefile

BINARY      := vnetviz
PKG         := ./cmd/vnetviz
BIN_DIR     := bin
BIN         := $(BIN_DIR)/$(BINARY)
GO          ?= go

# Build a static binary with no libc dependency, so it runs on older Linux
# regardless of the build host's glibc. vnetviz needs no cgo (netlink/netns are
# pure Go). This matches the release workflow; override with CGO_ENABLED=1.
export CGO_ENABLED ?= 0

# Embed version from git if available, else "dev".
VERSION     ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS     := -s -w -X main.version=$(VERSION)

.DEFAULT_GOAL := build

.PHONY: build
build: ## Build the vnetviz binary into bin/
	@mkdir -p $(BIN_DIR)
	$(GO) build -ldflags '$(LDFLAGS)' -o $(BIN) $(PKG)

.PHONY: install
install: ## go install vnetviz into GOBIN/GOPATH
	$(GO) install -ldflags '$(LDFLAGS)' $(PKG)

.PHONY: run
run: ## Run vnetviz (pass flags via ARGS=, e.g. make run ARGS="--format dot --all")
	$(GO) run $(PKG) $(ARGS)

.PHONY: test
test: ## Run the test suite
	$(GO) test ./...

.PHONY: cover
cover: ## Run tests with a coverage summary
	$(GO) test -coverprofile=coverage.out ./...
	$(GO) tool cover -func=coverage.out

.PHONY: vet
vet: ## Run go vet
	$(GO) vet ./...

.PHONY: fmt
fmt: ## Format all Go sources
	$(GO) fmt ./...

.PHONY: fmtcheck
fmtcheck: ## Fail if any file is not gofmt-clean
	@out="$$(gofmt -l .)"; \
	if [ -n "$$out" ]; then \
		echo "gofmt needed on:"; echo "$$out"; exit 1; \
	fi

.PHONY: tidy
tidy: ## Tidy go.mod / go.sum
	$(GO) mod tidy

.PHONY: check
check: fmtcheck vet test ## Run fmt check, vet, and tests

.PHONY: svg
svg: build ## Render a topology SVG via graphviz (needs root + dot)
	sudo $(BIN) --format svg --all --output vnetviz.svg
	@echo "wrote vnetviz.svg"

.PHONY: clean
clean: ## Remove build artifacts
	rm -rf $(BIN_DIR) coverage.out vnetviz.svg

.PHONY: help
help: ## Show this help
	@grep -hE '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) \
		| awk 'BEGIN{FS=":.*?## "}{printf "  \033[36m%-12s\033[0m %s\n", $$1, $$2}'
