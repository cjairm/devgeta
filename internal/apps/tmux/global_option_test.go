package tmux_test

// Step 5 of the ws dashboard cycle: three small tmux wrapper methods the
// saved-state feature (ADR-0050) and the session rename feature (ADR-0052)
// build on. Each test asserts the exact argv the wrapper sends tmux, per the
// cycle's own instruction: "Mocked tests assert the exact argv, including -q
// on the read."

import (
	"errors"
	"slices"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/tmux"
	"github.com/cjairm/devgeta/internal/testutil"
)

func TestGlobalOption(t *testing.T) {
	t.Run("returns the value on a successful query", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("some-value\n", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		got, err := app.GlobalOption("@dg_ws_state")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "some-value" {
			t.Errorf("expected %q, got %q", "some-value", got)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"show-options", "-gqv", "@dg_ws_state"}
		if !slices.Equal(last.Args, expectedArgs) {
			t.Errorf("expected args %v, got %v", expectedArgs, last.Args)
		}
	})

	// -q is what makes an UNSET option answer with empty output instead of
	// exiting 1 with "invalid option" - the case on every first launch after
	// a tmux restart, before anything has ever set the option (verified on
	// tmux 3.7c; the mock can't reproduce the -q-less failure itself, only
	// confirm the flag is sent).
	t.Run("returns empty string, not an error, when the option is unset", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		got, err := app.GlobalOption("@dg_ws_state")
		if err != nil {
			t.Fatalf("expected no error for an unset option, got %v", err)
		}
		if got != "" {
			t.Errorf("expected empty string for an unset option, got %q", got)
		}
	})

	t.Run("returns the exec error on failure", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "no server", errors.New("no server"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if _, err := app.GlobalOption("@dg_ws_state"); err == nil {
			t.Error("expected an error when the exec fails")
		}
	})
}

func TestSetGlobalOption(t *testing.T) {
	t.Run("sends the exact set-option argv", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SetGlobalOption("@dg_ws_state", `{"v":1}`); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"set-option", "-g", "@dg_ws_state", `{"v":1}`}
		if !slices.Equal(last.Args, expectedArgs) {
			t.Errorf("expected args %v, got %v", expectedArgs, last.Args)
		}
	})

	t.Run("returns the exec error on failure", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "command too long", errors.New("command too long"))
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.SetGlobalOption("@dg_ws_state", "x"); err == nil {
			t.Error("expected an error when the exec fails")
		}
	})
}

func TestRenameSession(t *testing.T) {
	t.Run("sends the exact rename-session argv", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", nil)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.RenameSession("hire2-tien", "hire2-tien-v2"); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		last := mockApp.Base.GetLastExecCommandCall()
		if last == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		expectedArgs := []string{"rename-session", "-t", "hire2-tien", "hire2-tien-v2"}
		if !slices.Equal(last.Args, expectedArgs) {
			t.Errorf("expected args %v, got %v", expectedArgs, last.Args)
		}
	})

	t.Run("returns the exec error on a duplicate name", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult(
			"", "duplicate session: hire2-tien-v2", errors.New("duplicate session: hire2-tien-v2"),
		)
		app := &tmux.Tmux{Cmd: mockApp.Cmd, Base: mockApp.Base}

		if err := app.RenameSession("hire2-tien", "hire2-tien-v2"); err == nil {
			t.Error("expected an error on a duplicate session name")
		}
	})
}
