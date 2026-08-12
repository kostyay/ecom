APP := ecom
BIN_DIR := bin

.DEFAULT_GOAL := build

.PHONY: build fmt lint test tidy

build:
	mkdir -p $(BIN_DIR)
	go build -trimpath -o $(BIN_DIR)/$(APP) .

fmt:
	go fmt ./...
	go tool -modfile=golangci-lint.mod golangci-lint fmt ./...

lint:
	go tool -modfile=golangci-lint.mod golangci-lint run ./...

test:
	go test ./...

tidy:
	go mod tidy
