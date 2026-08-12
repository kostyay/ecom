APP := ecom
BIN_DIR := bin
GO_FILES = $(shell find . -type f -name '*.go' -not -path './.git/*' -not -path './vendor/*' | sort)
TEMP_BASE := $(if $(TMPDIR),$(TMPDIR),/tmp/)
GOCACHE ?= $(TEMP_BASE)ecom-go-build
GOLANGCI_LINT_CACHE ?= $(TEMP_BASE)ecom-golangci-lint

export GOCACHE GOLANGCI_LINT_CACHE

.DEFAULT_GOAL := build

.PHONY: build doc-test fixtures fmt fmt-check lint quality race test test-unit tidy vet

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BIN_DIR)/$(APP) .

fmt:
	go fmt ./...
	go tool -modfile=golangci-lint.mod golangci-lint fmt ./...

fmt-check:
	@unformatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$unformatted" ]; then \
		echo "These Go files need formatting:"; \
		printf '%s\n' "$$unformatted"; \
		exit 1; \
	fi
	go tool -modfile=golangci-lint.mod golangci-lint fmt --diff ./...

lint:
	go tool -modfile=golangci-lint.mod golangci-lint run ./...

# All tests are offline. They use saved fixtures and local test servers.
test:
	go test ./...

test-unit:
	go test ./internal/... ./provider/... ./providers/...

fixtures:
	go test ./provider/conformance ./providers/bikediscount

doc-test:
	go test ./provider -run '^TestProviderAuthorGuideExampleCompiles$$'
	go test ./internal/cli -run '^TestUserGuideDocumentsCurrentCommandsAndConfiguration$$'

race:
	go test -race ./...

vet:
	go vet ./...

# Run each check in a fixed order, also when the caller enables parallel make.
quality:
	$(MAKE) fmt-check
	$(MAKE) lint
	$(MAKE) vet
	$(MAKE) test
	$(MAKE) fixtures
	$(MAKE) doc-test
	$(MAKE) race
	$(MAKE) build

tidy:
	go mod tidy
