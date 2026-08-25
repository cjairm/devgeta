.PHONY: all build-darwin-arm64 build-darwin-amd64 build-linux-amd64 clean test

# Version information
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE ?= $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")

# Linker flags to inject version info
LDFLAGS := -X 'github.com/cjairm/devgeta/pkg/buildinfo.Version=$(VERSION)' \
           -X 'github.com/cjairm/devgeta/pkg/buildinfo.Commit=$(COMMIT)' \
           -X 'github.com/cjairm/devgeta/pkg/buildinfo.BuildDate=$(BUILD_DATE)'

# Build all platform binaries
all: build-darwin-arm64 build-darwin-amd64 build-linux-amd64

# macOS ARM64 (Apple Silicon - M1, M2, M3 chips)
build-darwin-arm64:
	@echo "Building for macOS ARM64 (Apple Silicon)..."
	@echo "Version: $(VERSION), Commit: $(COMMIT), Build Date: $(BUILD_DATE)"
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o devgeta-darwin-arm64 .
	@echo "✓ devgeta-darwin-arm64 built successfully"

# macOS AMD64 (Intel chips)
build-darwin-amd64:
	@echo "Building for macOS AMD64 (Intel)..."
	@echo "Version: $(VERSION), Commit: $(COMMIT), Build Date: $(BUILD_DATE)"
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o devgeta-darwin-amd64 .
	@echo "✓ devgeta-darwin-amd64 built successfully"

# Linux AMD64 (Debian/Ubuntu)
build-linux-amd64:
	@echo "Building for Linux AMD64..."
	@echo "Version: $(VERSION), Commit: $(COMMIT), Build Date: $(BUILD_DATE)"
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o devgeta-linux-amd64 .
	@echo "✓ devgeta-linux-amd64 built successfully"

# Clean build artifacts
clean:
	@echo "Cleaning build artifacts..."
	rm -f devgeta-darwin-arm64 devgeta-darwin-amd64 devgeta-linux-amd64
	@echo "✓ Clean complete"

# Run the full test suite. This is the release gate (CLAUDE.md §9) — while
# implementing, run the changed package plus its direct importers instead.
# CLAUDE.md §6 "Which tests to run" has the go list query that produces that list.
test:
	@echo "Running full test suite..."
	go test -p 4 ./...
	@echo "✓ Tests passed"

# Run code quality checks
lint:
	@echo "Running code quality checks..."
	go vet ./...
	go fmt ./...
	@echo "✓ Code quality checks passed"

# Build for current platform only
build:
	@echo "Building for current platform..."
	@echo "Version: $(VERSION), Commit: $(COMMIT), Build Date: $(BUILD_DATE)"
	go build -ldflags "$(LDFLAGS)" -o devgeta .
	@echo "✓ devgeta built successfully"

# Help
help:
	@echo "Available targets:"
	@echo "  all                - Build all platform binaries"
	@echo "  build-darwin-arm64 - Build for macOS ARM64 (Apple Silicon)"
	@echo "  build-darwin-amd64 - Build for macOS AMD64 (Intel)"
	@echo "  build-linux-amd64  - Build for Linux AMD64"
	@echo "  build              - Build for current platform"
	@echo "  clean              - Remove build artifacts"
	@echo "  test               - Run full test suite (release gate; use targeted go test while working)"
	@echo "  lint               - Run code quality checks"
	@echo "  help               - Show this help message"
