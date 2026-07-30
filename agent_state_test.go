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
	"time"
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
//
// Step 10 (ADR-0009) adds two READ-only tmux calls to the mix
// (`show-option -gqv @dg_notify_sound` and `display-message ...
// window_active_clients`), and unlike the plain writes, agent-state.sh
// actually consumes their stdout via `$(...)`. notifySound and
// activeClients are the canned answers this fake returns for those two
// calls — printed unconditionally based on argv[0] ("show-option" /
// "display-message"), never parsed further, because each test configures
// only one meaning per run and the recorded-args file is what a test reads
// back to confirm which call was actually made and with what arguments.
func writeFakeTmux(
	t *testing.T,
	dir, recordPath string,
	exitCode int,
	notifySound, activeClients string,
) {
	t.Helper()
	// recordPath, notifySound, and activeClients are always test-controlled
	// literals in this file, never containing a single quote, so the naive
	// quoting below is safe for that reason, not because it is general-
	// purpose shell quoting.
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + recordPath + "'\n" +
		"case \"$1\" in\n" +
		"show-option) printf '%s\\n' '" + notifySound + "' ;;\n" +
		"display-message) printf '%s\\n' '" + activeClients + "' ;;\n" +
		"esac\n" +
		"exit " + strconv.Itoa(exitCode) + "\n"
	fakeTmuxPath := filepath.Join(dir, "tmux")
	if err := os.WriteFile(fakeTmuxPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake tmux: %v", err)
	}
}

// writeFakePlayer writes a fake sound-player executable (name is "afplay" or
// "paplay") to dir that records its invocation (the full argv — i.e. which
// sound file it was told to play) as one line in recordPath, then exits 0.
// This stands in for the platform's real player the same way writeFakeTmux
// stands in for tmux: play_notify_sound backgrounds and detaches whatever
// this "plays," so its own exit code is never observed by the script either
// way, and no real afplay/paplay may run in a test (CLAUDE.md §6).
func writeFakePlayer(t *testing.T, dir, name, recordPath string) {
	t.Helper()
	script := "#!/bin/sh\n" +
		"printf '%s\\n' \"$*\" >> '" + recordPath + "'\n" +
		"exit 0\n"
	fakePlayerPath := filepath.Join(dir, name)
	if err := os.WriteFile(fakePlayerPath, []byte(script), 0o755); err != nil {
		t.Fatalf("failed to write fake %s: %v", name, err)
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

// hookEnv configures the fake tmux and fake sound players that
// runAgentStateHook builds on PATH for one run of agent-state.sh.
//
// The zero value (tmuxExitCode 0, notifySound "", activeClients "", no
// players) reproduces the realistic "sound feature is off, no player is
// stubbed" baseline: @dg_notify_sound reads back as unset (ADR-0009: unset
// means off, matching real tmux's own -gqv behavior for an option nobody
// set), so the gate closes before any player would ever be probed.
type hookEnv struct {
	tmuxExitCode int
	// notifySound is what the fake tmux answers `show-option -gqv
	// @dg_notify_sound` with.
	notifySound string
	// activeClients is what the fake tmux answers `display-message ...
	// window_active_clients` with.
	activeClients string
	// players lists which sound-player binary names ("afplay", "paplay")
	// get a fake, recording implementation on PATH. Nil/empty means NEITHER
	// exists anywhere in the child's PATH — used to exercise the "player
	// missing entirely" fallback to the terminal bell.
	players []string
}

// runAgentStateHook extracts configs/claude/agent-state.sh from the embedded
// ConfigsFS (the same bytes that ship in the built binary), runs it as a real
// subprocess with the given positional argument and TMUX_PANE environment
// state, with a fake `tmux` (see writeFakeTmux) and any fake sound players
// (see writeFakePlayer) requested by env prepended onto PATH.
//
// The child is launched via bash's own RESOLVED ABSOLUTE PATH (found through
// exec.LookPath against this test process's real environment) rather than by
// executing scriptPath directly via its "#!/usr/bin/env bash" shebang. That
// distinction matters here: the shebang path would need the CHILD's PATH to
// still contain a directory holding a real bash so `env` can find it — and
// on Debian/Ubuntu 12+, /bin is usr-merged into the very same directory that
// holds the real afplay/paplay, so "keep enough PATH for bash" and "keep
// real sound players off PATH" would be the same requirement pointing two
// different ways. Resolving bash up front and exec'ing it directly needs no
// PATH lookup for bash at all, so the child's PATH can be set to EXACTLY the
// fake bin dir and nothing else — the fakes on PATH are the only mechanism,
// full stop, matching the task's "a test that lets a real binary through is
// a defect."
//
// Returns the script's exit code, the fake tmux's recorded args file
// contents (empty string if tmux was never invoked), and the fake player's
// recorded args file contents (empty string if no player was ever invoked —
// note this is NOT racy for that "never invoked" case: if the sound gate
// closed, nothing was ever forked to write it in the first place, so there
// is nothing to wait for. Callers that expect a player WAS invoked must use
// waitForPlayerInvocation instead, since play_notify_sound fires detached
// and the write can land after this function already returned).
func runAgentStateHook(
	t *testing.T,
	arg string,
	pane paneEnv,
	env hookEnv,
) (exitCode int, tmuxRecorded string, playerRecordPath string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("agent-state.sh is a bash script; not exercised on windows")
	}

	bashPath, err := exec.LookPath("bash")
	if err != nil {
		t.Fatalf("bash not found: %v", err)
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
	tmuxRecordPath := filepath.Join(t.TempDir(), "tmux-args.txt")
	writeFakeTmux(
		t,
		fakeBinDir,
		tmuxRecordPath,
		env.tmuxExitCode,
		env.notifySound,
		env.activeClients,
	)

	playerRecPath := filepath.Join(t.TempDir(), "player-args.txt")
	for _, name := range env.players {
		writeFakePlayer(t, fakeBinDir, name, playerRecPath)
	}

	cmd := exec.Command(bashPath, scriptPath, arg)
	// Deliberately NOT inheriting the parent's real PATH: the fake tmux (and
	// any fake players) on fakeBinDir are the ONLY binaries this child can
	// find, by construction — see the doc comment above for why the child is
	// launched via bash's resolved path rather than the script's own
	// shebang, which is what makes this total exclusion possible.
	cmd.Env = []string{
		"PATH=" + fakeBinDir,
		"HOME=" + os.Getenv("HOME"),
		// Defense-in-depth: PATH already excludes any real tmux, but if that
		// exclusion were ever defeated, a real tmux binary would still look
		// for its server socket in a throwaway directory instead of the
		// user's real one.
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

	recorded, readErr := os.ReadFile(tmuxRecordPath)
	if readErr != nil {
		if !os.IsNotExist(readErr) {
			t.Fatalf("failed to read recorded tmux args: %v", readErr)
		}
		recorded = nil
	}
	return code, string(recorded), playerRecPath
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

// waitForPlayerInvocation polls recordPath until it has content or a 2s
// timeout elapses, then returns it. This is needed ONLY when a test expects
// the player WAS invoked: play_notify_sound fires it detached and
// backgrounded (ADR-0009's whole point — the hook returns before the sound
// finishes), so by the time runAgentStateHook's cmd.Run() returns, the fake
// player's own write to recordPath may not have landed yet. Tests asserting
// SILENCE need no such wait: if the gate closed, nothing was ever forked to
// race with, so absence is immediately and permanently true.
func waitForPlayerInvocation(t *testing.T, recordPath string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(recordPath); err == nil && len(data) > 0 {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for fake player invocation at %s", recordPath)
	return ""
}

// assertPlayerNeverInvoked fails the test if the fake player recorded
// anything at all. Safe to call immediately (no wait needed) — see
// waitForPlayerInvocation's doc comment for why absence is never racy.
func assertPlayerNeverInvoked(t *testing.T, recordPath string) {
	t.Helper()
	data, err := os.ReadFile(recordPath)
	if err == nil && len(data) > 0 {
		t.Errorf("expected the player to never be invoked, but it recorded: %q", string(data))
	} else if err != nil && !os.IsNotExist(err) {
		t.Fatalf("failed to read player record file: %v", err)
	}
}

// TestAgentStateHook_WritesGivenValue confirms the script is genuinely
// argument-driven (ADR-0005 / task-4-brief.md's "Design" section): whatever
// value it's invoked with is exactly what reaches `tmux set-option`, not a
// hardcoded one. Since Step 8, the script also calls tmux a second time to
// write/clear the window-level mirror (@dg_window_agent_state); this test
// checks both calls, in order.
//
// Since Step 10 (ADR-0009), idle/blocked/error ALSO make a third tmux call —
// the sound gate's `show-option -gqv @dg_notify_sound` — before exiting,
// because the gate is checked even when (as here, the zero-value hookEnv)
// the feature is off; that is the "checked first so the common case costs
// one call" behavior the ADR calls for, and this test asserts that third
// call's exact args alongside the pre-existing two. busy never reaches the
// sound gate at all, so it stays at exactly 2 calls, unchanged.
func TestAgentStateHook_WritesGivenValue(t *testing.T) {
	for _, value := range []string{"idle", "busy", "blocked", "error"} {
		t.Run(value, func(t *testing.T) {
			code, recorded, playerRecPath := runAgentStateHook(
				t, value, paneEnv{set: true, value: "%3"}, hookEnv{},
			)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}

			calls := callLines(recorded)
			wantCalls := 2
			if value != "busy" {
				wantCalls = 3 // + the sound gate's show-option probe
			}
			if len(calls) != wantCalls {
				t.Fatalf(
					"expected exactly %d tmux invocation(s), got %d: %q",
					wantCalls, len(calls), recorded,
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

			if value != "busy" {
				wantGate := "show-option -gqv @dg_notify_sound"
				if calls[2] != wantGate {
					t.Errorf("third (sound-gate) call = %q, want %q", calls[2], wantGate)
				}
			}

			// The gate answered "off" (zero-value hookEnv), so no player
			// was ever probed.
			assertPlayerNeverInvoked(t, playerRecPath)
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
			code, recorded, playerRecPath := runAgentStateHook(t, "idle", pane, hookEnv{})
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}
			if recorded != "" {
				t.Errorf("expected tmux to never be invoked, but it recorded: %q", recorded)
			}
			assertPlayerNeverInvoked(t, playerRecPath)
		})
	}
}

// TestAgentStateHook_SwallowsTmuxFailure confirms the script never fails the
// hook chain (task-4-brief.md: "a hook that exits non-zero and blocks/
// confuses Claude Code is not acceptable") even when the tmux call itself
// fails (dead server, missing binary, ...), simulated here by a fake tmux
// that exits non-zero on EVERY invocation. Since Step 8 the script makes two
// tmux calls per run for busy; since Step 10, idle additionally attempts the
// sound gate's show-option call (which also fails, and also can't stop the
// script from exiting 0) before giving up on sound. The same fake tmux
// binary backs all of them, confirming the script tolerates every one
// failing, not just the first.
func TestAgentStateHook_SwallowsTmuxFailure(t *testing.T) {
	code, recorded, playerRecPath := runAgentStateHook(
		t, "idle", paneEnv{set: true, value: "%3"}, hookEnv{tmuxExitCode: 1},
	)
	if code != 0 {
		t.Fatalf("expected the hook to swallow the tmux failure and exit 0, got %d", code)
	}

	calls := callLines(recorded)
	if len(calls) != 3 {
		t.Fatalf(
			"expected the pane write, the window-mirror write, and the sound-gate probe to "+
				"have all been attempted (and recorded) despite each failing, got %d recorded "+
				"call(s): %q",
			len(calls), recorded,
		)
	}
	wantPaneWrite := "set-option -p -t %3 @dg_agent_state idle"
	wantMirror := "set-option -w -t %3 @dg_window_agent_state idle"
	wantGate := "show-option -gqv @dg_notify_sound"
	if calls[0] != wantPaneWrite {
		t.Errorf("first (pane-level) call = %q, want %q", calls[0], wantPaneWrite)
	}
	if calls[1] != wantMirror {
		t.Errorf("second (window-mirror) call = %q, want %q", calls[1], wantMirror)
	}
	if calls[2] != wantGate {
		t.Errorf("third (sound-gate) call = %q, want %q", calls[2], wantGate)
	}
	assertPlayerNeverInvoked(t, playerRecPath)
}

// TestAgentStateHook_Sound_SilentWhenNotifySoundOff confirms the switch half
// of ADR-0009's gate: with the window unattended (activeClients "0", i.e.
// the SECOND gate would pass if reached), no sound plays unless
// @dg_notify_sound reads back exactly "on" — covering both "never set" and
// "explicitly set to off", per the ADR's "anything else, including unset,
// means off." Also confirms the gate is cheap: the sound-gate probe
// (show-option) happens, but display-message never does, because the first
// check already failed.
func TestAgentStateHook_Sound_SilentWhenNotifySoundOff(t *testing.T) {
	for _, notifySound := range []string{"", "off"} {
		t.Run("notifySound="+notifySound, func(t *testing.T) {
			for _, state := range []string{"idle", "blocked", "error"} {
				t.Run(state, func(t *testing.T) {
					code, recorded, playerRecPath := runAgentStateHook(
						t,
						state,
						paneEnv{set: true, value: "%3"},
						hookEnv{
							notifySound:   notifySound,
							activeClients: "0",
							players:       []string{"afplay"},
						},
					)
					if code != 0 {
						t.Fatalf("expected exit 0, got %d", code)
					}

					calls := callLines(recorded)
					if len(calls) != 3 {
						t.Fatalf(
							"expected pane write + window mirror + sound-gate probe only (no "+
								"display-message call), got %d: %q",
							len(calls), recorded,
						)
					}
					wantGate := "show-option -gqv @dg_notify_sound"
					if calls[2] != wantGate {
						t.Errorf("third call = %q, want %q", calls[2], wantGate)
					}

					assertPlayerNeverInvoked(t, playerRecPath)
				})
			}
		})
	}
}

// TestAgentStateHook_Sound_SilentWhenWindowAttended confirms the gate half
// of ADR-0009: even with @dg_notify_sound "on", a window with an active
// client (window_active_clients != "0") stays silent. Also confirms BOTH
// gate probes fire in this case (show-option then display-message), since
// the first one passed.
func TestAgentStateHook_Sound_SilentWhenWindowAttended(t *testing.T) {
	for _, state := range []string{"idle", "blocked", "error"} {
		t.Run(state, func(t *testing.T) {
			code, recorded, playerRecPath := runAgentStateHook(
				t, state, paneEnv{set: true, value: "%3"},
				hookEnv{notifySound: "on", activeClients: "1", players: []string{"afplay"}},
			)
			if code != 0 {
				t.Fatalf("expected exit 0, got %d", code)
			}

			calls := callLines(recorded)
			if len(calls) != 4 {
				t.Fatalf(
					"expected pane write + window mirror + BOTH sound-gate probes, got %d: %q",
					len(calls), recorded,
				)
			}
			wantGate := "show-option -gqv @dg_notify_sound"
			wantAttended := "display-message -p -t %3 #{window_active_clients}"
			if calls[2] != wantGate {
				t.Errorf("third call = %q, want %q", calls[2], wantGate)
			}
			if calls[3] != wantAttended {
				t.Errorf("fourth call = %q, want %q", calls[3], wantAttended)
			}

			assertPlayerNeverInvoked(t, playerRecPath)
		})
	}
}

// TestAgentStateHook_Sound_SilentOnBusy confirms busy NEVER makes a sound,
// even with the option on and the window unattended (the ADR-0009 table:
// busy is "the agent starting work is not an event you asked to hear").
// Also confirms this is cheap: busy doesn't reach the sound gate's code path
// at all, so neither show-option nor display-message is ever called —
// tmux invocations stay at exactly 2 (pane write + mirror clear), same as
// before Step 10.
func TestAgentStateHook_Sound_SilentOnBusy(t *testing.T) {
	code, recorded, playerRecPath := runAgentStateHook(
		t, "busy", paneEnv{set: true, value: "%3"},
		hookEnv{notifySound: "on", activeClients: "0", players: []string{"afplay"}},
	)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}

	calls := callLines(recorded)
	if len(calls) != 2 {
		t.Fatalf(
			"expected exactly 2 tmux invocations (pane write + mirror clear only, no sound-gate "+
				"probes for busy), got %d: %q",
			len(calls), recorded,
		)
	}

	assertPlayerNeverInvoked(t, playerRecPath)
}

// soundFileForState is the ADR-0009 player table (idle / blocked / error
// column order), duplicated here (rather than parsed out of the script) so
// this test asserts against the SPEC's literal file names, not whatever the
// script happens to contain.
var soundFileForState = map[string]struct{ afplay, paplay string }{
	"idle": {
		"/System/Library/Sounds/Glass.aiff",
		"/usr/share/sounds/freedesktop/stereo/complete.oga",
	},
	"blocked": {
		"/System/Library/Sounds/Ping.aiff",
		"/usr/share/sounds/freedesktop/stereo/message.oga",
	},
	"error": {
		"/System/Library/Sounds/Basso.aiff",
		"/usr/share/sounds/freedesktop/stereo/dialog-error.oga",
	},
}

// TestAgentStateHook_Sound_PlaysDistinctFileViaAfplay confirms that with the
// gate open and only afplay stubbed on PATH, each state plays its own
// distinct file (ADR-0009: "distinguishable without looking, which is the
// entire point"), and that afplay is genuinely probed first (per "Probe
// order is afplay first, then paplay").
func TestAgentStateHook_Sound_PlaysDistinctFileViaAfplay(t *testing.T) {
	for _, state := range []string{"idle", "blocked", "error"} {
		t.Run(state, func(t *testing.T) {
			_, _, playerRecPath := runAgentStateHook(
				t, state, paneEnv{set: true, value: "%3"},
				hookEnv{notifySound: "on", activeClients: "0", players: []string{"afplay"}},
			)

			recorded := waitForPlayerInvocation(t, playerRecPath)
			// The fake player records "$*" - its OWN positional args, not
			// argv[0] - so the recorded line is just the sound file, not
			// "afplay <file>". Which binary answered is what env.players
			// controls and this test already fixes to afplay alone.
			want := soundFileForState[state].afplay
			got := strings.TrimRight(recorded, "\n")
			if got != want {
				t.Errorf("player invocation = %q, want %q", got, want)
			}
		})
	}
}

// TestAgentStateHook_Sound_PaplayFallbackWhenAfplayAbsent confirms the
// second half of the probe order: with afplay absent from PATH entirely and
// only paplay stubbed, each state falls back to playing ITS OWN distinct
// paplay file — not just that paplay runs at all.
func TestAgentStateHook_Sound_PaplayFallbackWhenAfplayAbsent(t *testing.T) {
	for _, state := range []string{"idle", "blocked", "error"} {
		t.Run(state, func(t *testing.T) {
			_, _, playerRecPath := runAgentStateHook(
				t, state, paneEnv{set: true, value: "%3"},
				hookEnv{notifySound: "on", activeClients: "0", players: []string{"paplay"}},
			)

			recorded := waitForPlayerInvocation(t, playerRecPath)
			want := soundFileForState[state].paplay
			got := strings.TrimRight(recorded, "\n")
			if got != want {
				t.Errorf("player invocation = %q, want %q", got, want)
			}
		})
	}
}

// TestAgentStateHook_Sound_ExitsZeroWhenPlayerMissingEntirely confirms the
// last-resort fallback: with the gate open but NEITHER afplay NOR paplay
// anywhere on PATH (env.players is nil, and runAgentStateHook's PATH
// contains only the fake bin dir — see its doc comment for why no real
// player can leak in), the script still exits 0. It falls back to the
// terminal bell (a bash builtin, nothing external to record), so there is
// no player invocation to assert on — only that nothing failed.
func TestAgentStateHook_Sound_ExitsZeroWhenPlayerMissingEntirely(t *testing.T) {
	for _, state := range []string{"idle", "blocked", "error"} {
		t.Run(state, func(t *testing.T) {
			code, _, playerRecPath := runAgentStateHook(
				t, state, paneEnv{set: true, value: "%3"},
				hookEnv{notifySound: "on", activeClients: "0", players: nil},
			)
			if code != 0 {
				t.Fatalf("expected exit 0 even with no player on PATH, got %d", code)
			}
			assertPlayerNeverInvoked(t, playerRecPath)
		})
	}
}
