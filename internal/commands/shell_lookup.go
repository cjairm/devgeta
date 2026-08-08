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
// the user's interactive shell — the same environment a tmux pane runs in —
// and, on a Found outcome, the resolved absolute path `command -v` printed for
// it.
//
// This exists because exec.LookPath (and LookPathFn) only sees the current
// process's PATH. When `dg ws` is launched from a non-login tmux pane whose
// PATH was never repaired, that PATH can be truncated and miss tools that are
// actually installed (e.g. ~/.local/bin/claude), producing a false "not
// installed" error even though the coder would launch fine. The user's own
// interactive shell is where that tool provably resolves - its rc files add the
// PATH entries dg's process is missing - so resolving the tool there is the
// check that matches reality; a bare exec.LookPath in dg's own process does not.
//
// The path exists so a pane can later exec the resolved binary directly
// instead of relying on its own PATH (ADR-0021) — tmux runs a pane's
// shell-command through a non-interactive shell, which has no equivalent of
// zsh's ~/.zshenv PATH repair, and the probe's shell need not even be the
// shell tmux launches. The path is empty on every outcome except Found: a
// NotFound or Inconclusive result has no path by definition.
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
//   - $SHELL, falling back to zsh, so it matches the shell the user's own
//     commands run in.
//   - -i makes the shell read its interactive rc file (~/.zshrc, ~/.bashrc),
//     which is where a user's PATH additions and their own aliases and functions
//     live. Both matter: the PATH repair is what lets this probe see a tool dg's
//     own truncated PATH cannot, and a user-defined name for the tool exists
//     nowhere else - `command -v` reports one, and a non-path answer is what
//     selects the pane's interactive fallback recipe (ADR-0021 part 3). It is
//     NOT about devgeta's own cc/oc alias: nothing devgeta launches has named
//     that since ADR-0021's 2026-08-07 amendment, and an alias answer is not
//     path-shaped anyway (see lastPathLine).
//   - name is passed as a positional argument ($1), never interpolated into the
//     script string, so it can't be interpreted as shell syntax.
//   - stdin is /dev/null so an interactive shell can never block on the tty;
//     stderr is discarded (prompt/plugin startup noise); stdout is captured,
//     because the marker line on it — not the process exit status — is what
//     the classification trusts. stdout also carries `command -v`'s own
//     output (the resolved path, on a Found outcome), which is why it is no
//     longer redirected to /dev/null.
func defaultShellCommandLookup(name string) (string, ShellLookupResult) {
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
		// the marker only has to start a line to be found again. stdout is
		// no longer redirected to /dev/null: `command -v`'s own output (the
		// resolved path, on a Found outcome) has to reach stdout so
		// classifyShellLookup can read it. Only stderr is discarded here;
		// "$?" in the printf still refers to `command -v`'s exit status,
		// since printf is a separate statement that runs after it.
		`command -v -- "$1" 2>/dev/null; printf '\n`+shellLookupMarker+`%d\n' "$?"`,
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
// result, plus the resolved path on a Found outcome. Pure so the decision
// ADR-0016 exists for — only a lookup that PROVABLY RAN may report NotFound —
// and the path extraction ADR-0021 depends on are both unit-testable without
// spawning a shell.
//
// The marker line is the proof the lookup ran. Its absence covers every way
// the shell can fail without answering the question: init exited before the
// lookup (a broken ~/.zshrc, a plugin calling exit), the deadline killed the
// shell, a $SHELL that can't parse the POSIX probe script, or a shell that
// never started. All of those are Inconclusive — none of them may block a
// create with "not installed", and none of them carries a path.
//
// The path is read only on rc == 0, and only as the last non-empty line
// before the marker — not simply "the output before it". rc-file banner
// noise lands on stdout too (see defaultShellCommandLookup's doc comment), so
// on a Found outcome that noise necessarily precedes `command -v`'s own
// output (it prints during shell init, before the lookup runs), making the
// last non-empty line genuinely `command -v`'s answer. On a NotFound outcome
// that same noise would BE the last non-empty line, which is exactly why the
// path is never read there.
func classifyShellLookup(stdout string) (string, ShellLookupResult) {
	i := strings.LastIndex(stdout, shellLookupMarker)
	if i < 0 {
		return "", ShellLookupInconclusive
	}
	status := stdout[i+len(shellLookupMarker):]
	if j := strings.IndexByte(status, '\n'); j >= 0 {
		status = status[:j]
	}
	rc, err := strconv.Atoi(strings.TrimSpace(status))
	if err != nil {
		// A marker with a mangled status (e.g. the deadline cut the write
		// short) is not proof of anything.
		return "", ShellLookupInconclusive
	}
	if rc != 0 {
		return "", ShellLookupNotFound
	}
	return lastPathLine(stdout[:i]), ShellLookupFound
}

// lastPathLine returns the last non-empty line in before, if — and only if —
// it begins with "/". before is everything the probe captured ahead of the
// marker on a Found outcome (see classifyShellLookup): `command -v`'s own
// resolved answer, possibly preceded by rc-file banner noise.
//
// `command -v` does not always print a path (ADR-0021): an alias prints
// `alias cc='…'`, a shell function or builtin prints its bare name, and none
// of those are something a pane may exec. Requiring the leading "/" is a
// shape check, not a safety check — the caller still has to shell-quote
// whatever this returns before using it in a command line.
func lastPathLine(before string) string {
	lines := strings.Split(before, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "/") {
			return line
		}
		return ""
	}
	return ""
}
