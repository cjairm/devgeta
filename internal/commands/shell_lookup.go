package commands

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// ShellLookupResult is the outcome of probing for a command in the user's
// interactive shell. It is deliberately three-valued, not a bool: a probe
// that timed out or failed to run has NOT established that the command is
// missing, and collapsing that into "not found" is how a slow machine once
// blocked every worktree create with a false "opencode is not installed"
// error. See ADR-0016.
type ShellLookupResult int

const (
	// ShellLookupFound: the shell resolved the command (exit 0).
	ShellLookupFound ShellLookupResult = iota
	// ShellLookupNotFound: the shell ran the lookup and it exited non-zero.
	// The only outcome that proves the command is absent.
	ShellLookupNotFound
	// ShellLookupInconclusive: the probe could not determine an answer — the
	// deadline fired before the shell finished, or the probe shell itself
	// failed to start. Callers gating an action on this probe must treat it
	// as "unknown", never as "absent" (ADR-0016).
	ShellLookupInconclusive
)

// ShellCommandLookupFn reports whether name resolves as a runnable command in
// the user's interactive shell — the same environment a tmux pane runs in.
//
// This exists because exec.LookPath (and LookPathFn) only sees the current
// process's PATH. When `dg ws` is launched from a non-login tmux pane whose
// PATH was never repaired, that PATH can be truncated and miss tools that are
// actually installed (e.g. ~/.local/bin/claude), producing a false "not
// installed" error even though the coder would launch fine. Worktree windows
// run their coder by sending shell commands to an interactive pane, which
// sources ~/.zshenv (PATH self-repair) and ~/.zshrc (devgeta.zsh: the cc/oc
// aliases). Resolving a tool the same way that pane will is the only check that
// matches reality; a bare exec.LookPath in dg's own process does not.
//
// It is a package var so tests can swap it without spawning a real shell (the
// same pattern as LookPathFn).
var ShellCommandLookupFn = defaultShellCommandLookup

// shellLookupTimeout bounds the interactive-shell probe so a slow or hung shell
// startup (a heavy ~/.zshrc, a plugin waiting on the network) can't stall a
// worktree create indefinitely. A timeout is reported as Inconclusive, not
// NotFound — the deadline bounds how long a create can be delayed, it does not
// get to decide that a tool is missing (ADR-0016).
const shellLookupTimeout = 5 * time.Second

// shellLookupMarker prefixes the one line of probe output that carries the
// lookup's exit status. The probe script prints it AFTER `command -v` runs, so
// its presence is the proof that the lookup actually happened — a shell whose
// exit status merely came back non-zero proves nothing, because an rc file or
// plugin exiting during init produces the same status without ever reaching
// the lookup (ADR-0016). The leading marker text is deliberately odd enough
// that rc-file banner noise on stdout can't collide with it.
const shellLookupMarker = "__DEVGETA_SHELL_LOOKUP_RC="

// defaultShellCommandLookup runs `command -v <name>` in the user's interactive
// shell and classifies the outcome (see classifyShellLookup).
//
//   - $SHELL, falling back to zsh, so it matches the login shell a pane runs.
//   - -i makes the shell source ~/.zshrc (where devgeta.zsh defines cc/oc);
//     ~/.zshenv (PATH repair) is sourced regardless of -i. Together this mirrors
//     an interactive pane's view of both PATH and aliases.
//   - name is passed as a positional argument ($1), never interpolated into the
//     script string, so it can't be interpreted as shell syntax.
//   - stdin is /dev/null so an interactive shell can never block on the tty;
//     stderr is discarded (prompt/plugin startup noise); stdout is captured,
//     because the marker line on it — not the process exit status — is what
//     the classification trusts.
func defaultShellCommandLookup(name string) ShellLookupResult {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellLookupTimeout)
	defer cancel()

	var stdout bytes.Buffer
	cmd := exec.CommandContext(
		ctx,
		shell,
		"-i",
		"-c",
		// The leading \n guards against rc-file noise that ends mid-line;
		// the marker only has to start a line to be found again.
		`command -v -- "$1" >/dev/null 2>&1; printf '\n`+shellLookupMarker+`%d\n' "$?"`,
		shell,
		name,
	)
	cmd.Stdout = &stdout
	if devnull, err := os.Open(os.DevNull); err == nil {
		cmd.Stdin = devnull
		defer func() { _ = devnull.Close() }()
	}
	// The run error is deliberately unused: with the marker as the source of
	// truth, the exit status carries no extra information. Marker present →
	// the lookup ran and its status is right there; marker absent → the shell
	// died, hung, or was killed before the lookup, and no exit status can
	// prove the tool missing.
	_ = cmd.Run()
	return classifyShellLookup(stdout.String())
}

// classifyShellLookup maps the probe's captured stdout onto the three-valued
// result. Pure so the decision ADR-0016 exists for — only a lookup that
// PROVABLY RAN may report NotFound — is unit-testable without spawning a
// shell.
//
// The marker line is that proof. Its absence covers every way the shell can
// fail without answering the question: init exited before the lookup (a
// broken ~/.zshrc, a plugin calling exit), the deadline killed the shell, a
// $SHELL that can't parse the POSIX probe script, or a shell that never
// started. All of those are Inconclusive — none of them may block a create
// with "not installed".
func classifyShellLookup(stdout string) ShellLookupResult {
	i := strings.LastIndex(stdout, shellLookupMarker)
	if i < 0 {
		return ShellLookupInconclusive
	}
	status := stdout[i+len(shellLookupMarker):]
	if j := strings.IndexByte(status, '\n'); j >= 0 {
		status = status[:j]
	}
	rc, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		// A marker with a mangled status (e.g. the deadline cut the write
		// short) is not proof of anything.
		return ShellLookupInconclusive
	}
	if rc == 0 {
		return ShellLookupFound
	}
	return ShellLookupNotFound
}
