package commands

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestMockCommand_IsPackageInstalled_PerNameMap(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true, "tmux": false}

	ok, err := m.IsPackageInstalled("git")
	if err != nil || !ok {
		t.Errorf("git: got (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = m.IsPackageInstalled("tmux")
	if err != nil || ok {
		t.Errorf("tmux: got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_FallsBackToGlobalBool(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalled = true // legacy global flag, no map entry for "unmapped"

	ok, err := m.IsPackageInstalled("unmapped")
	if err != nil || !ok {
		t.Errorf("got (%v, %v), want (true, nil) via fallback", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_PerNameError(t *testing.T) {
	m := NewMockCommand()
	wantErr := errors.New("brew: command not found")
	m.PackageInstalledErrors = map[string]error{"broken": wantErr}

	ok, err := m.IsPackageInstalled("broken")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_IsDesktopAppInstalled_PerNameMapAndError(t *testing.T) {
	m := NewMockCommand()
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	wantErr := errors.New("dpkg: not found")
	m.DesktopAppInstalledErrors = map[string]error{"broken-app": wantErr}

	if ok, err := m.IsDesktopAppInstalled("docker"); err != nil || !ok {
		t.Errorf("docker: got (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := m.IsDesktopAppInstalled("broken-app"); ok || err != wantErr {
		t.Errorf("broken-app: got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_Reset_ClearsPerNameMaps(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true}
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	m.PackageInstalledErrors = map[string]error{"x": errors.New("boom")}
	m.DesktopAppInstalledErrors = map[string]error{"y": errors.New("boom")}

	m.Reset()

	if len(m.PackageInstalledMap) != 0 || len(m.DesktopAppInstalledMap) != 0 ||
		len(m.PackageInstalledErrors) != 0 || len(m.DesktopAppInstalledErrors) != 0 {
		t.Error("Reset should clear all per-name maps")
	}
}

func TestMockBaseCommand_IsFontPresent_Error(t *testing.T) {
	m := NewMockBaseCommand()
	wantErr := errors.New("fc-list: not found")
	m.IsFontPresentResult = false
	m.IsFontPresentError = wantErr

	ok, err := m.IsFontPresent("JetBrainsMono")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

// TestResetExecCommandClearsExecCommandFn pins ResetExecCommand's contract:
// after a reset the mock must answer from whatever the NEXT test configures,
// never from the previous one.
//
// ExecCommandFn takes precedence over execResults and the fixed
// stdout/stderr/error fields, so a reset that clears those but leaves the
// callback in place is worse than no reset at all — the next subtest's setup
// is accepted silently and then ignored, and its assertions fail for reasons
// that have nothing to do with the code under test.
func TestResetExecCommandClearsExecCommandFn(t *testing.T) {
	mock := NewMockBaseCommand()
	mock.ExecCommandFn = func(CommandParams) (string, string, error) {
		return "from the stale callback", "", nil
	}

	mock.ResetExecCommand()

	if mock.ExecCommandFn != nil {
		t.Fatal("ResetExecCommand left ExecCommandFn set; a reused mock would keep " +
			"answering from the previous subtest's callback")
	}

	// The reset mock must now honor a freshly configured result.
	mock.SetExecCommandResult("fresh", "", nil)
	stdout, _, err := mock.ExecCommand(CommandParams{Command: "git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "fresh" {
		t.Errorf("ExecCommand returned %q; the stale callback is still winning", stdout)
	}
}

// TestConcurrentExecCommand pins the mock's concurrency contract: code under
// test may run several mocked executions at once, so every call has to be
// recorded and every canned answer has to reach exactly one caller.
//
// It also pins WHERE the lock is taken, which is the part that cannot be read
// off the code by a future editor. Both callbacks below reach back into the
// mock through GetExecCommandCallCount, an accessor that locks: that only works
// because ExecCommand holds the lock for bookkeeping alone and releases it
// before calling out. Take the lock in execCommandResultLocked as well, or hold
// it across the callbacks, and these cases deadlock rather than fail — which is
// what the package's short test timeout is for.
func TestConcurrentExecCommand(t *testing.T) {
	const callers = 8

	// runConcurrently fires `callers` ExecCommand calls at one mock and returns
	// each caller's stdout, indexed by caller.
	runConcurrently := func(m *MockBaseCommand, mkParams func(i int) CommandParams) []string {
		got := make([]string, callers)
		var wg sync.WaitGroup
		wg.Add(callers)
		for i := range callers {
			go func() {
				defer wg.Done()
				stdout, _, err := m.ExecCommand(mkParams(i))
				if err != nil {
					t.Errorf("caller %d: unexpected error: %v", i, err)
				}
				got[i] = stdout
			}()
		}
		wg.Wait()
		return got
	}

	t.Run("the positional queue gives each answer to exactly one caller", func(t *testing.T) {
		m := NewMockBaseCommand()
		queued := make([]execResult, callers)
		want := make([]string, callers)
		for i := range callers {
			want[i] = fmt.Sprintf("answer-%d", i)
			queued[i] = ExecCommandResult(want[i], "", nil)
		}
		m.SetExecCommandResults(queued...)

		got := runConcurrently(m, func(int) CommandParams {
			return CommandParams{Command: "git", Args: []string{"status"}}
		})

		// Order is undefined — which caller got which answer is the race. What
		// must hold is that no answer was handed out twice or skipped, which is
		// exactly what an unguarded execCallIdx++ would break.
		slices.Sort(got)
		if !slices.Equal(got, want) {
			t.Errorf("answers were duplicated or dropped: got %v, want %v", got, want)
		}
		if n := m.GetExecCommandCallCount(); n != callers {
			t.Errorf("recorded %d calls, want %d", n, callers)
		}
	})

	t.Run("ExecCommandFn answers each caller and may call back in", func(t *testing.T) {
		m := NewMockBaseCommand()
		m.ExecCommandFn = func(cmd CommandParams) (string, string, error) {
			// Reaching back into the mock from the hook must not deadlock.
			if m.GetExecCommandCallCount() == 0 {
				t.Error("the call should already be recorded when the hook runs")
			}
			return cmd.Args[0], "", nil
		}

		got := runConcurrently(m, func(i int) CommandParams {
			return CommandParams{Command: "git", Args: []string{fmt.Sprintf("caller-%d", i)}}
		})

		for i, stdout := range got {
			if want := fmt.Sprintf("caller-%d", i); stdout != want {
				t.Errorf("caller %d got %q, want %q", i, stdout, want)
			}
		}
		if n := m.GetExecCommandCallCount(); n != callers {
			t.Errorf("recorded %d calls, want %d", n, callers)
		}
	})

	t.Run("OnStdoutLine replays to each caller and may call back in", func(t *testing.T) {
		m := NewMockBaseCommand()
		m.SetExecCommandResult("one\ntwo", "", nil)

		lines := make([][]string, callers)
		got := runConcurrently(m, func(i int) CommandParams {
			return CommandParams{
				Command: "git",
				Args:    []string{"status"},
				OnStdoutLine: func(line string) {
					// Production code passes this callback (see
					// reviewrun.go), so it too runs outside the lock.
					if m.GetExecCommandCallCount() == 0 {
						t.Error("the call should already be recorded when a line is replayed")
					}
					lines[i] = append(lines[i], line)
				},
			}
		})

		for i := range callers {
			if got[i] != "one\ntwo" {
				t.Errorf("caller %d got stdout %q", i, got[i])
			}
			if !slices.Equal(lines[i], []string{"one", "two"}) {
				t.Errorf("caller %d saw lines %v, want [one two]", i, lines[i])
			}
		}
	})
}
