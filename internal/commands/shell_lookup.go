package commands

import (
	"context"
	"os"
	"os/exec"
	"time"
)

// ShellCommandExistsFn reports whether name resolves as a runnable command in
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
var ShellCommandExistsFn = defaultShellCommandExists

// shellLookupTimeout bounds the interactive-shell probe so a slow or hung shell
// startup (a heavy ~/.zshrc, a plugin waiting on the network) can't stall a
// worktree create indefinitely; on timeout the tool is reported absent.
const shellLookupTimeout = 5 * time.Second

// defaultShellCommandExists runs `command -v <name>` in the user's interactive
// shell and reports whether it resolved (exit status 0).
//
//   - $SHELL, falling back to zsh, so it matches the login shell a pane runs.
//   - -i makes the shell source ~/.zshrc (where devgeta.zsh defines cc/oc);
//     ~/.zshenv (PATH repair) is sourced regardless of -i. Together this mirrors
//     an interactive pane's view of both PATH and aliases.
//   - name is passed as a positional argument ($1), never interpolated into the
//     script string, so it can't be interpreted as shell syntax.
//   - stdin is /dev/null and stdout/stderr are discarded: an interactive shell
//     must never block on the tty here, and prompt/plugin banner noise on
//     startup is irrelevant — only the exit status matters.
func defaultShellCommandExists(name string) bool {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "zsh"
	}

	ctx, cancel := context.WithTimeout(context.Background(), shellLookupTimeout)
	defer cancel()

	cmd := exec.CommandContext(
		ctx,
		shell,
		"-i",
		"-c",
		`command -v -- "$1" >/dev/null 2>&1`,
		shell,
		name,
	)
	if devnull, err := os.Open(os.DevNull); err == nil {
		cmd.Stdin = devnull
		defer func() { _ = devnull.Close() }()
	}
	return cmd.Run() == nil
}
