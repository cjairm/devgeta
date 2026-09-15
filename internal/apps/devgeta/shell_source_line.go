package devgeta

// devgeta.zsh is wired into the user's shell config by two independent writers,
// and they spell the path differently on purpose:
//
//   - install.sh appends a guarded block whose loading line is
//     `. "$HOME/.local/share/devgeta/devgeta.zsh"`. $HOME is left UNEXPANDED so
//     the config survives being copied to another machine or user.
//   - this package appends `source "<absolute path>"`, because by the time it
//     runs it has the resolved path in hand.
//
// Both load the same file, so only one of them may survive. Deciding that by
// substring-searching for one of the two spellings cannot work, and that is the
// bug this file exists to fix: the search for the absolute path never matched
// the installer's $HOME-relative line, so every freshly installed machine ended
// up sourcing devgeta.zsh twice. Nothing broke - mise, zoxide, p10k,
// zsh-autosuggestions and zsh-syntax-highlighting all happen to guard
// themselves against being loaded twice - but the second pass still cost about
// 230ms on every new shell, every tmux pane and every subshell.
//
// So "is devgeta.zsh already sourced here?" is answered structurally instead:
// find the lines that are source STATEMENTS for a path ending in
// devgeta/devgeta.zsh, whatever the leading path expression happens to look
// like.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
)

// sourceStatementSuffix is the trailing path fragment that every spelling of
// devgeta.zsh's location has in common - `devgeta/devgeta.zsh`.
//
// It is derived from getZshConfigPath() rather than written out, so it keeps
// matching if the app's data directory is ever renamed. Two segments and not
// one: a bare `devgeta.zsh` would also match a same-named file somewhere else
// in the user's config, and the full path would be back to matching one
// spelling only.
func sourceStatementSuffix() string {
	configPath := getZshConfigPath()
	return filepath.Join(filepath.Base(filepath.Dir(configPath)), filepath.Base(configPath))
}

// ownSourceLine is the line this package writes. It is also the only line the
// repair below is ever allowed to delete.
func ownSourceLine() string {
	return fmt.Sprintf(`source "%s"`, getZshConfigPath())
}

// isDevgetaSourceStatement reports whether a shell config line actually loads
// devgeta.zsh.
//
// "Actually loads" is the whole point, because a shell config names the path in
// places that load nothing:
//
//   - `# source ".../devgeta.zsh"` - commented out, and a user who did that
//     meant it.
//   - `if [ -f ".../devgeta.zsh" ]; then` - the installer's guard. It names the
//     path, but only the `.` on the next line loads it. Counting mentions here
//     would report a correctly wired config as a duplicate and start deleting
//     from it.
//
// The `source`/`.` check looks for the command as a whitespace-delimited field
// rather than as a line prefix, so a guarded one-liner
// (`[ -f "X" ] && source "X"`) is recognised too.
func isDevgetaSourceStatement(line, suffix string) bool {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return false
	}
	if !strings.Contains(trimmed, suffix) {
		return false
	}
	for _, field := range strings.Fields(trimmed) {
		if field == "source" || field == "." {
			return true
		}
	}
	return false
}

// ensureShellConfigSourcesDevgeta makes the user's shell config load
// devgeta.zsh exactly once, and is safe to run repeatedly.
//
// Not sourced at all -> this package's source line is appended.
// Sourced once       -> nothing is touched, whichever spelling is in use.
// Sourced twice+     -> this package's own line is dropped until one remains.
//
// The three rules that keep it safe on machines that already exist:
//
//   - Only ownSourceLine() is ever removed, matched as a whole line. A line the
//     user wrote, or the installer wrote, is never deleted.
//   - The last remaining statement is never removed. A config carrying only
//     devgeta's own absolute line - someone who installed with `go install`, or
//     whose installer block has since been edited away - keeps working exactly
//     as before.
//   - When there are several statements but none of them is ours, nothing
//     happens. Two hand-written lines are the user's business, not ours to
//     guess at.
//
// Keeping the installer's line rather than ours when both are present is
// deliberate: `$HOME` is the portable spelling, and the absolute one breaks if
// the home directory ever moves.
func ensureShellConfigSourcesDevgeta() error {
	shellConfig := paths.Files.ShellConfig
	suffix := sourceStatementSuffix()
	ownLine := ownSourceLine()

	raw, err := os.ReadFile(shellConfig)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("failed to read %s: %w", shellConfig, err)
		}
		// No file means it was never wired and there is nothing to repair.
		return files.AddLineToFile(ownLine, shellConfig)
	}

	// Splitting on "\n" leaves a trailing newline as a final empty element, so
	// rejoining the kept lines reproduces the file's original ending instead of
	// adding or swallowing one.
	lines := strings.Split(string(raw), "\n")
	statements := 0
	for _, line := range lines {
		if isDevgetaSourceStatement(line, suffix) {
			statements++
		}
	}

	switch statements {
	case 0:
		return files.AddLineToFile(ownLine, shellConfig)
	case 1:
		return nil
	}

	kept := make([]string, 0, len(lines))
	remaining := statements
	for _, line := range lines {
		if remaining > 1 && strings.TrimSpace(line) == ownLine {
			remaining--
			logger.L().Debugw(
				"Removing a redundant devgeta.zsh source line",
				"file", shellConfig,
				"line", strings.TrimSpace(line),
			)
			continue
		}
		kept = append(kept, line)
	}
	if remaining == statements {
		logger.L().Debugw(
			"Shell config sources devgeta.zsh more than once, but none of the lines is devgeta's own - leaving it alone",
			"file", shellConfig,
			"statements", statements,
		)
		return nil
	}

	return overwriteInPlace(shellConfig, strings.Join(kept, "\n"))
}

// overwriteInPlace rewrites filePath with content, keeping the file's mode and
// its inode.
//
// In place, rather than write-a-temp-file-and-rename: ~/.zshrc is very often a
// symlink into a dotfiles repository, and a rename would replace that symlink
// with a regular file, silently detaching the user's config from the repo they
// manage it in. Forcing a mode would be just as rude on a config someone keeps
// at 0600. install.sh's own repair path writes back with `cat` over the
// original file for exactly these reasons, and this matches it - including the
// small window between the truncate and the write, which `cat >` has too.
//
// O_TRUNC without O_CREATE is what keeps the existing mode: the permission
// argument only applies to a file this call would have had to create.
func overwriteInPlace(filePath, content string) error {
	file, err := os.OpenFile(filePath, os.O_WRONLY|os.O_TRUNC, files.FilePermission)
	if err != nil {
		return fmt.Errorf("failed to open %s for rewriting: %w", filePath, err)
	}
	if _, err := file.WriteString(content); err != nil {
		// The write already failed; a close error on top of it adds nothing
		// actionable, and the write error is the one worth reporting.
		_ = file.Close()
		return fmt.Errorf("failed to rewrite %s: %w", filePath, err)
	}
	// Checked, not deferred: on a write-back this close is where a full disk or
	// a failing volume surfaces, and losing it would mean reporting success
	// over a truncated shell config.
	if err := file.Close(); err != nil {
		return fmt.Errorf("failed to flush %s: %w", filePath, err)
	}
	return nil
}
