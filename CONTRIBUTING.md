# Contributing to Devgeta

Thank you for your interest in contributing! This guide covers development setup, testing, and release workflows.

---

## Getting Started

### Prerequisites

- **Go 1.21+** — [Install Go](https://golang.org/doc/install)
- **Git** — Version control
- **Make** — Build automation (included on macOS/Linux)
- **A supported OS** — macOS 13+ or Debian 12+/Ubuntu 24+

### Development Setup

1. **Clone the repository:**

   ```bash
   git clone https://github.com/cjairm/devgeta.git
   cd devgeta
   ```

2. **Install dependencies:**

   ```bash
   go mod download
   ```

3. **Build for your platform:**

   ```bash
   make build
   ```

4. **Verify the build:**
   ```bash
   ./devgeta-$(uname -m | sed 's/aarch64/darwin-arm64/;s/x86_64/darwin-amd64/') --version
   ```

---

## Build Commands

### Makefile Targets

```bash
# Build for current platform only
make build

# Build all platforms (macOS arm64 + amd64, Linux amd64)
make all

# Platform-specific builds
make build-darwin-arm64    # macOS Apple Silicon
make build-darwin-amd64    # macOS Intel
make build-linux-amd64     # Linux/Debian/Ubuntu

# Development
make test                  # Run all tests
make lint                  # Format & analyze code
make clean                 # Remove build artifacts

# Help
make help                  # Show all targets
```

### Manual Builds (Platform-Specific)

If you prefer direct Go commands:

**For macOS Apple Silicon (M1/M2/M3+):**

```bash
GOOS=darwin GOARCH=arm64 go build -o devgeta-darwin-arm64
```

**For macOS Intel:**

```bash
GOOS=darwin GOARCH=amd64 go build -o devgeta-darwin-amd64
```

**For Linux/Debian/Ubuntu (x86_64):**

```bash
GOOS=linux GOARCH=amd64 go build -o devgeta-linux-amd64
```

---

## Testing

### Run All Tests

```bash
# Run all tests in the project
go test ./...

# Run tests with coverage report
go test -cover ./...

# Run specific package tests
go test ./internal/apps/neovim/

# Verbose output (see each test)
go test -v ./...

# Run with race detector (catch concurrency bugs)
go test -race ./...
```

### Test Coverage

```bash
# Generate coverage report
go test -coverprofile=coverage.out ./...

# View coverage as HTML
go tool cover -html=coverage.out
```

### Testing Local Builds

Before submitting a PR, test your binary:

```bash
# Build your binary
make build

# Install it locally for testing
bash install.sh --local ./devgeta-$(uname -m | sed 's/aarch64/darwin-arm64/;s/x86_64/darwin-amd64/')

# Test the installation
devgeta install --only terminal
```

---

## Code Quality

### Lint & Format

```bash
# Check code quality
go vet ./...

# Format code to standard
go fmt ./...

# Or use make (runs both)
make lint
```

### Style Guide

Follow the [Effective Go](https://golang.org/doc/effective_go) conventions:

- **Naming:** camelCase for functions/variables, PascalCase for exports
- **Comments:** Explain WHY, not WHAT (code is self-documenting)
- **Errors:** Never ignore errors; always handle or return them
- **Formatting:** Run `go fmt` before committing
- **Testing:** Place `*_test.go` files alongside implementation

---

## Submitting Changes

### Before You Submit

**Workflow: implement → verify manually → add tests → commit.**
Never commit before the feature is confirmed working end-to-end — tests written against broken behavior encode bugs, not correctness.

- [ ] Feature verified manually (run the binary, exercise the golden path)
- [ ] Tests added or updated — ask _"does this change need tests?"_ and write them before committing
- [ ] `go test ./...` passes
- [ ] Code builds without errors: `make build`
- [ ] Lint passes: `make lint`
- [ ] Commit messages are clear and descriptive
- [ ] Your changes follow the style guide

### Pull Request

1. Push your branch to GitHub
2. Create a Pull Request against `main`
3. In the PR description, explain:
   - What problem does this solve?
   - How does it solve it?
   - Are there any breaking changes?
   - How should this be tested?

### Review Process

- At least one maintainer review required
- All CI checks must pass (lint, tests, build)
- Code must follow project conventions

---

## Release Process

### Version Numbers

See [CLAUDE.md section 9](CLAUDE.md#9-versioning--tagging) for the full versioning policy, bump rules, and tagging workflow.

### Making a Release

1. **Update version** (if not automated):
   - Version is injected from git tags during build
   - Create git tag: `git tag v1.2.3`

2. **Push tag to GitHub:**

   ```bash
   git push origin v1.2.3
   ```

3. **GitHub Actions** automatically:
   - Builds all platform binaries (darwin-arm64, darwin-amd64, linux-amd64)
   - Creates GitHub Release with auto-generated notes
   - Uploads binaries as release assets

4. **Verify** the release:
   - Check [GitHub Releases](https://github.com/cjairm/devgeta/releases)
   - Test the installer script
   - Verify binary checksums

---

## Project Structure

Understand the codebase organization:

| Directory            | Purpose                                                          |
| -------------------- | ---------------------------------------------------------------- |
| `cmd/`               | CLI command handlers (install, version, worktree, root)          |
| `internal/apps/`     | Individual app installers (19 apps, 2 files each)                |
| `internal/tooling/`  | Category coordinators (terminal, languages, databases, worktree) |
| `internal/commands/` | Platform-specific installers (Darwin, Debian)                    |
| `internal/config/`   | Configuration state management                                   |
| `internal/tui/`      | Terminal UI components                                           |
| `pkg/`               | Shared utilities (logging, paths, file ops, etc.)                |
| `configs/`           | Embedded configuration templates                                 |
| `docs/`              | User documentation and guides                                    |
| `specs/`             | Detailed implementation specs for features                       |

### Adding a New App Installer

1. Create directory: `internal/apps/{appname}/`
2. Implement installer interface in `{appname}.go`
3. Add tests in `{appname}_test.go`
4. Add config templates to `configs/{appname}/`
5. Register in appropriate category: `internal/tooling/{category}/`
6. Document in `docs/apps/{appname}.md`

### Adding a New Command

1. Create handler in `cmd/{command}/`
2. Implement command logic
3. Add tests alongside code
4. Register in CLI (cmd/root.go or similar)
5. Document in README or CLI help

---

## Getting Help

- **Documentation:** See `docs/` directory
- **Architecture:** Read `docs/guides/cross-platform-installation.md`
- **Decisions:** Check `docs/decisions/` for ADRs (Architecture Decision Records)
- **Roadmap:** See `ROADMAP.md` for planned features
- **Issues:** Check [GitHub Issues](https://github.com/cjairm/devgeta/issues)

---

## Code of Conduct

- Be respectful and inclusive
- Provide constructive feedback
- Help others learn and grow
- Report problems to maintainers

---

## Questions?

Open an issue or discussion on GitHub, or reach out to the maintainers directly.

Thank you for contributing to Devgeta! 🎉
