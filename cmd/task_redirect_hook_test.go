package cmd

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// taskRedirectHookSources are read directly off disk (relative to this
// package's directory, which sits at <repo-root>/cmd) rather than through
// ConfigsFS (embedded in package main, which cmd cannot import without an
// import cycle). This is safe: main.go's `//go:embed all:configs` embeds
// these files byte-for-byte with no transformation, so reading them from disk
// here checks exactly what ships in the binary — see task_redirect_test.go
// (package main) for the companion test that runs the embedded bytes
// end-to-end.
var taskRedirectHookSources = []string{
	filepath.Join("..", "configs", "claude", "task-redirect.sh"),
	filepath.Join("..", "configs", "opencode", "plugin", "task-redirect.js"),
}

// devgetaTaskRefPattern extracts "devgeta task <subcommand>" references from
// the hook scripts' deny messages, e.g. "devgeta task review-package <base>
// <head>" -> "review-package".
var devgetaTaskRefPattern = regexp.MustCompile(`devgeta task ([a-z][a-z-]*)`)

// registeredTaskSubcommands returns the first word of each task subcommand's
// Use string (e.g. "review-package <base> <head>" -> "review-package"), i.e.
// the actual currently-registered `dg task` subcommand names.
func registeredTaskSubcommands(t *testing.T) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	for _, sub := range taskCmd.Commands() {
		use := sub.Use
		if i := strings.IndexAny(use, " \t"); i >= 0 {
			use = use[:i]
		}
		names[use] = true
	}
	if len(names) == 0 {
		t.Fatal("taskCmd has no registered subcommands — task registration may be broken")
	}
	return names
}

// TestRedirectHookDenyMessagesReferenceRegisteredTaskCommands is this slice's
// rule-5 embedded-config constraint test (CLAUDE.md's "if a config must
// satisfy a constraint imposed by an external tool... enforce that constraint
// with a test"): every `devgeta task <name>` the hook scripts recommend as a
// replacement must be a real, currently-registered `dg task` subcommand — so
// a future rename of review-package/worktree-start/worktree-finish/release
// breaks this test loudly instead of silently shipping a hook that
// recommends a command that no longer exists.
func TestRedirectHookDenyMessagesReferenceRegisteredTaskCommands(t *testing.T) {
	registered := registeredTaskSubcommands(t)

	for _, path := range taskRedirectHookSources {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}

		matches := devgetaTaskRefPattern.FindAllStringSubmatch(string(content), -1)
		if len(matches) == 0 {
			t.Fatalf("%s: expected at least one 'devgeta task <name>' reference, found none", path)
		}
		for _, m := range matches {
			name := m[1]
			if !registered[name] {
				t.Errorf(
					"%s references %q as a devgeta task replacement, but no such subcommand is registered in cmd/task.go",
					path,
					name,
				)
			}
		}
	}
}

// TestRedirectHookNeverUsesBareDgInvocation enforces the binary-invocation
// contract (only the installed `devgeta` binary is guaranteed on PATH where
// these hooks run — same reasoning as review-pr.md/code-reviewer.md's
// `devgeta task ...` rule) by failing if either hook script's replacement
// text falls back to the colloquial "dg task" form instead of "devgeta task".
func TestRedirectHookNeverUsesBareDgInvocation(t *testing.T) {
	for _, path := range taskRedirectHookSources {
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", path, err)
		}
		if strings.Contains(string(content), "dg task") {
			t.Errorf(
				"%s references 'dg task' — must use 'devgeta task' (dg is not guaranteed on PATH)",
				path,
			)
		}
	}
}
