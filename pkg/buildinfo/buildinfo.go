// Package buildinfo holds build-time metadata injected via linker -X flags
// (see Makefile and .github/workflows/release.yml). It has no imports of its
// own beyond the values below, so anything in this codebase can depend on it
// without risking an import cycle.
//
// Consumed today by cmd (dg version, --version), and, in a later task, by
// internal/apps/devgeta to name a stamped config-extract directory after the
// binary's version and commit.
package buildinfo

var (
	// Version is set during build via ldflags
	Version = "dev"
	// Commit is set during build via ldflags
	Commit = "unknown"
	// BuildDate is set during build via ldflags
	BuildDate = "unknown"
)
