package commands_test

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
)

// These tests exercise the real ExecCommand rather than a mock. A mock only
// proves a caller populated CommandParams.Env — it can never prove that
// exec.Cmd.Env actually received the overlay or that inheritance survived,
// which are the two behaviors that matter here. internal/commands is the
// boundary that shells out (CLAUDE.md §6), so — like
// TestExecCommandCapturesLongSingleLine in exec_longline_test.go — these run
// a hermetic `bash -c` command that touches nothing outside the process: no
// packages, no user files, no network.

// TestExecCommandEnvOverlayReachesChild verifies that a variable set via
// CommandParams.Env is visible inside the spawned child process.
func TestExecCommandEnvOverlayReachesChild(t *testing.T) {
	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	out, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "echo $DEVGETA_TEST_OVERLAY"},
		Env:     []string{"DEVGETA_TEST_OVERLAY=child-only-value"},
	})
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}
	if strings.TrimSpace(out) != "child-only-value" {
		t.Fatalf("expected overlay variable to reach child, got %q", out)
	}
}

// TestExecCommandEnvOverlayPreservesInheritance verifies the overlay is
// additive: a variable already present in devgeta's own environment (set
// here with t.Setenv, so it is restored automatically) must still be visible
// to the child alongside the overlay variable. This is the behavior that
// distinguishes append(os.Environ(), cmd.Env...) from a bare assignment,
// which would replace the environment and silently drop PATH/HOME.
func TestExecCommandEnvOverlayPreservesInheritance(t *testing.T) {
	t.Setenv("DEVGETA_TEST_PREEXISTING", "inherited-value")

	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	out, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "echo $DEVGETA_TEST_PREEXISTING:$DEVGETA_TEST_OVERLAY"},
		Env:     []string{"DEVGETA_TEST_OVERLAY=child-only-value"},
	})
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}
	if strings.TrimSpace(out) != "inherited-value:child-only-value" {
		t.Fatalf("expected both inherited and overlay variables, got %q", out)
	}
}

// TestExecCommandEmptyEnvPreservesTodaysInheritance verifies that leaving
// CommandParams.Env empty (the zero value, used by every existing caller)
// leaves exec.Cmd.Env nil, so the child still inherits devgeta's full
// environment exactly as it does today. This is observed behaviorally
// (exec.Cmd is internal to ExecCommand): a pre-existing variable, set with
// t.Setenv so it is restored automatically, must still reach the child when
// no overlay is requested.
func TestExecCommandEmptyEnvPreservesTodaysInheritance(t *testing.T) {
	t.Setenv("DEVGETA_TEST_PREEXISTING", "inherited-value")

	b := commands.NewBaseCommandCustom(FakePlatform{Linux: true})

	out, _, err := b.ExecCommand(commands.CommandParams{
		Command: "bash",
		Args:    []string{"-c", "echo $DEVGETA_TEST_PREEXISTING"},
		// Env intentionally left nil.
	})
	if err != nil {
		t.Fatalf("ExecCommand returned error: %v", err)
	}
	if strings.TrimSpace(out) != "inherited-value" {
		t.Fatalf("expected inherited variable to survive nil Env, got %q", out)
	}
}
