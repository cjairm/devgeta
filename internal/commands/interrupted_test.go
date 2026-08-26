package commands_test

import (
	"context"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// TestInterrupted covers the predicate task 14 adds so install-loop
// coordinators can notice a Ctrl-C/SIGTERM that already cancelled the
// process-level root context (see SetRootContext) instead of grinding
// through every remaining ExecCommand call only to have each one fail.
func TestInterrupted(t *testing.T) {
	t.Cleanup(func() { commands.SetRootContext(context.Background()) })

	if commands.Interrupted() {
		t.Fatal("expected Interrupted() to be false against the default, uncancelled root context")
	}

	ctx, cancel := context.WithCancel(context.Background())
	commands.SetRootContext(ctx)
	if commands.Interrupted() {
		t.Fatal("expected Interrupted() to be false before the context is cancelled")
	}

	cancel()
	if !commands.Interrupted() {
		t.Fatal("expected Interrupted() to be true once the root context is cancelled")
	}

	// Restoring the default root must clear it again — Interrupted() reads
	// whatever is currently installed, not a sticky flag.
	commands.SetRootContext(context.Background())
	if commands.Interrupted() {
		t.Fatal("expected Interrupted() to be false again after restoring the default root context")
	}
}
