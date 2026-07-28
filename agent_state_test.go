package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// writeFakeTmux writes a fake `tmux` executable to dir that records every
// invocation it receives as ONE appended line in recordPath (args joined by
// a space via "$*"), then exits with exitCode. This stands in for a real
// tmux server the same way this codebase's Go tests inject a mock Base/Cmd
// instead of calling a real command (CLAUDE.md §6: "never execute real
// commands in tests") — here the dependency being stubbed is a real
// subprocess's own dependency (agent-state.sh's `tmux` call), not a Go call,
// so the mechanism is a fake binary on PATH rather than an injected
// interface.
//
// Step 8 makes agent-state.sh invoke tmux TWICE per run (the pane write,
// then the window-mirror write/clear) instead of once. The append (">>",
// rather than the ">" this script used before Step 8) is required so the
// second invocation's args don't clobber the first's in the record file;
// joining each invocation's args onto a single line (via "$*" rather than
// "$@", which would print one line per ARG) is what lets a test read "the
// first call" and "the second call" back out as literally the first and
// second lines of the file, in call order.
func writeFakeTmux(t *testing.T, dir, recordPath string, exitCode int) {
	t.Helper()
	// recordPath is always a t.TempDir()-derived path in this test file, so
	// it never contains a single quote; the naive quoting below is safe for
	// that reason, not because it is general-purpose shell quoting.
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + recordPath + "'\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	fakeTmuxPath := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake tmux: %v", err)
	}
}

// paneEnv describes how TMUX_PANE should be set for one invocation of
// agent-state.sh: unset (the variable is not present in the environment at
// all) or set to an explicit value (which may itself be the empty string,
// covering "set but empty" as a separate case from "not set").
type paneEnv struct {
	set   bool
	value string
}

// runAgentStateHook extracts configs/claude/agent-state.sh from the embedded
// ConfigsFS (the same bytes that ship in the built binary), runs it as a real
// subprocess with the given positional argument and TMUX_PANE environment
// state, with a fake `tmux` (see writeFakeTmux) prepended onto PATH so the
// script's own `tmux set-option ...` call never reaches a real tmux server.
// Returns the script's exit code and the fake tmux's recorded args file
// contents (empty string if the file was never written, i.e. tmux was never
// invoked).
func runAgentStateHook(
	t *testing.T,
	arg string,
	pane paneEnv,
	fakeTmuxExitCode int,
) (exitCode int, recordedArgs string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("agent-state.sh is a bash script; not exercised on windows")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/agent-state.sh")
	if err != nil {
		t.Fatalf("failed to read embedded agent-state.sh: %v", err)
	}

	scriptDir := t.TempDir()
	scriptPath := filepath.Join(scriptDir, "agent-state.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	fakeBinDir := t.TempDir()
	recordPath := filepath.Join(t.TempDir(), "tmux-args.txt")
	writeFakeTmux(t, fakeBinDir, recordPath, fakeTmuxExitCode)

	cmd := exec.Command(scriptPath, arg)
	// Deliberately not inheriting the parent environment beyond PATH/HOME/
	// TMUX_PANE: this test controls TMUX_PANE's presence exactly (including
	// "unset"), and a leaked real TMUX_PANE from the environment this test
	// binary happens to run in must never let the script talk to a real
	// tmux server.
	cmd.Env = []string{
		"PATH=" + fakeBinDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"HOME=" + os.Getenv("HOME"),
		// Defense-in-depth: the fake tmux shadowing the real one on PATH is
		// already sufficient isolation, but if that shadow were ever defeated,
		// a real tmux binary would still look for its server socket in a
		// throwaway directory instead of the user's real one.
		"TMUX_TMPDIR=" + t.TempDir(),
	}
	if pane.set {
		cmd.Env = append(cmd.Env, "TMUX_PANE="+pane.value)
	}

	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &bytes.Buffer{}

	runErr := cmd.Run()
	code := 0
	if runErr != nil {
		var exitErr *exec.ExitError
		if !isExitError(runErr, &exitErr) {
			t.Fatalf(
				"failed to run agent-state.sh (arg=%q): %v (stderr=%q)",
				arg,
				runErr,
				stderrBuf.String(),
			)
		}
		code = exitErr.ExitCode()
	}

	recorded, readErr := os.ReadFile(recordPath)
	if readErr != nil {
		if os.IsNotExist(readErr) {
			return code, ""
		}
		t.Fatalf("failed to read recorded tmux args: %v", readErr)
	}
	return code, string(recorded)
}

// callLines splits the fake tmux's recorded output into one string per
// invocation (see writeFakeTmux: each invocation is exactly one line, args
// space-joined, in call order), dropping the trailing empty element left by
// the final newline.
func callLines(recorded string) []string {
	trimmed := strings.TrimRight(recorded, "\n")
	if trimmed == "" {
		return nil
	}
	return strings.Split(trimmed, "\n")
}

// TestAgentStateHook_WritesGivenValue confirms the script is genuinely
// argument-driven (ADR-0005 / task-4-brief.md's "Design" section): whatever
// value it's invoked with is exactly what reaches `tmux set-option`, not a
// hardcoded one. Since Step 8, the script also calls tmux a second time to
// write/clear the window-level mirror (@dg_window_agent_state); this test
// checks both calls, in order.
func TestAgentStateHook_WritesGivenValue(t *testing.T) {
	for _, value := range []string{"idle", "busy", "blocked", "error"} {
		t.Run(value, func(t *testing.T) {
			code, recorded := runAgentStateHook(t, value, paneEnv{set: true, value: "%3"}, 0)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}

			calls := callLines(recorded)
			if len(calls) != 2 {
				t.Fatalf(
					"expected exactly 2 tmux invocations (pane write + window mirror), got %d: %q",
					len(calls), recorded,
				)
			}

			wantPaneWrite := "set-option -p -t %3 @dg_agent_state " + value
			if calls[0] != wantPaneWrite {
				t.Errorf("first (pane-level) call = %q, want %q", calls[0], wantPaneWrite)
			}

			// busy CLEARS the window mirror (-u, no trailing value);
			// idle/blocked/error SET it to the value - see ADR-0005's
			// Step 8 note and agent-state.sh's header comment for why.
			var wantMirror string
			if value == "busy" {
				wantMirror = "set-option -w -u -t %3 @dg_window_agent_state"
			} else {
				wantMirror = "set-option -w -t %3 @dg_window_agent_state " + value
			}
			if calls[1] != wantMirror {
				t.Errorf("second (window-mirror) call = %q, want %q", calls[1], wantMirror)
			}
		})
	}
}

// TestAgentStateHook_NoOpsWithoutTmuxPane confirms the same silent no-op
// contract as the OpenCode plugin (notify.js's writeState): Claude Code run
// outside tmux must not invoke tmux at all, and must still exit 0. Covers
// both TMUX_PANE entirely absent from the environment and TMUX_PANE present
// but set to the empty string.
func TestAgentStateHook_NoOpsWithoutTmuxPane(t *testing.T) {
	cases := map[string]paneEnv{
		"unset": {set: false},
		"empty": {set: true, value: ""},
	}
	for name, pane := range cases {
		t.Run(name, func(t *testing.T) {
			code, recorded := runAgentStateHook(t, "idle", pane, 0)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}
			if recorded != "" {
				t.Errorf("expected tmux to never be invoked, but it recorded: %q", recorded)
			}
		})
	}
}

// TestAgentStateHook_SwallowsTmuxFailure confirms the script never fails the
// hook chain (task-4-brief.md: "a hook that exits non-zero and blocks/
// confuses Claude Code is not acceptable") even when the tmux call itself
// fails (dead server, missing binary, ...), simulated here by a fake tmux
// that exits non-zero on EVERY invocation. Since Step 8 the script makes two
// tmux calls per run; the same fake tmux binary backs both, so this also
// confirms the script still exits 0 even though BOTH calls fail, not just
// the first (the mirror write's `|| true` is independent of the pane
// write's, matching the pre-existing pattern format.sh already uses).
func TestAgentStateHook_SwallowsTmuxFailure(t *testing.T) {
	code, recorded := runAgentStateHook(t, "idle", paneEnv{set: true, value: "%3"}, 1)
	if code != 0 {
		t.Fatalf("expected the hook to swallow the tmux failure and exit 0, got %d", code)
	}

	calls := callLines(recorded)
	if len(calls) != 2 {
		t.Fatalf(
			"expected both the pane write and the window-mirror write to have been attempted "+
				"(and recorded) despite each failing, got %d recorded call(s): %q",
			len(calls), recorded,
		)
	}
	wantPaneWrite := "set-option -p -t %3 @dg_agent_state idle"
	wantMirror := "set-option -w -t %3 @dg_window_agent_state idle"
	if calls[0] != wantPaneWrite {
		t.Errorf("first (pane-level) call = %q, want %q", calls[0], wantPaneWrite)
	}
	if calls[1] != wantMirror {
		t.Errorf("second (window-mirror) call = %q, want %q", calls[1], wantMirror)
	}
}
