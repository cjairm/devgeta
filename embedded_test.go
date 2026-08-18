package main

import (
	"io/fs"
	"regexp"
	"strings"
	"testing"
)

// tmux-resurrect restores panes with `cat <saved-contents>; exec <default-command>`.
// If default-command is a compound command (contains `;` or `&&`), the splice
// truncates at the first separator, the pane execs the wrong word and dies at
// birth — restored sessions collapse and the tmux server exits (terminal
// window auto-closes). This guard keeps default-command a single simple
// command so that class of breakage cannot be reintroduced.
func TestTmuxDefaultCommandStaysResurrectSafe(t *testing.T) {
	data, err := fs.ReadFile(ConfigsFS, "configs/tmux/tmux.conf.tmpl")
	if err != nil {
		t.Fatalf("failed to read embedded tmux.conf.tmpl: %v", err)
	}

	var defaultCommandLine string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "set -g default-command") {
			defaultCommandLine = trimmed
			break
		}
	}
	if defaultCommandLine == "" {
		// Not setting default-command at all is safe (tmux spawns login shells).
		return
	}

	value := strings.TrimSpace(strings.TrimPrefix(defaultCommandLine, "set -g default-command"))
	value = strings.Trim(value, `'"`)
	for _, sep := range []string{";", "&&", "||"} {
		if strings.Contains(value, sep) {
			t.Errorf(
				"default-command %q contains %q: it must stay a single simple command — tmux-resurrect splices it after `exec` when restoring panes, and a compound value kills every restored pane at birth",
				value,
				sep,
			)
		}
	}
}

// TestTmuxWindowStatusFormatFlagsUnattendedAgents confirms Step 8's status-bar
// signal (docs/plans/cycles/2026-07-28-agent-activity-notifications.md, Step 8;
// ADR-0005) is wired to the correct, SEPARATE option name for the window-level
// mirror. This is a string-only check against the embedded config text — it
// never starts a real tmux server, per this cycle's safety requirement.
//
// The one mistake this task's brief warns against: reusing @dg_agent_state
// (the pane-level, authoritative option) as the window-level mirror's name.
// Because tmux options cascade window -> pane, a window-level write under the
// SAME name would be inherited by every pane in that window without its own
// override (e.g. the nvim pane in a claude-nvim layout), corrupting
// Tmux.PaneStates()'s existing per-pane read. The mirror must be
// @dg_window_agent_state instead — this test asserts both the presence of the
// correct name and the absence of the bare pane-level name on the same line.
func TestTmuxWindowStatusFormatFlagsUnattendedAgents(t *testing.T) {
	data, err := fs.ReadFile(ConfigsFS, "configs/tmux/tmux.conf.tmpl")
	if err != nil {
		t.Fatalf("failed to read embedded tmux.conf.tmpl: %v", err)
	}
	content := string(data)

	var formatLine string
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.Contains(trimmed, "window-status-format") {
			formatLine = trimmed
			break
		}
	}
	if formatLine == "" {
		t.Fatal("expected a window-status-format override in tmux.conf, found none")
	}

	if !strings.Contains(formatLine, "@dg_window_agent_state") {
		t.Errorf(
			"window-status-format line must reference @dg_window_agent_state, got: %s",
			formatLine,
		)
	}
	// Strip the correct name before checking for the bare pane-level name, so
	// a substring match on "@dg_window_agent_state" (which itself does NOT
	// contain "@dg_agent_state" — "window_agent_state" != "agent_state")
	// can't hide a distinct, mistaken reference to the pane-level option
	// elsewhere on the same line.
	withoutMirrorName := strings.ReplaceAll(formatLine, "@dg_window_agent_state", "")
	if strings.Contains(withoutMirrorName, "@dg_agent_state") {
		t.Errorf(
			"window-status-format line must not reference the pane-level @dg_agent_state directly "+
				"(only the window-level mirror @dg_window_agent_state) - reusing the pane-level name "+
				"would let it be inherited by every pane in the window without its own override: %s",
			formatLine,
		)
	}

	if !strings.Contains(formatLine, "window_active_clients") {
		t.Errorf(
			"window-status-format line must gate visibility on window_active_clients, got: %s",
			formatLine,
		)
	}
	// Same stripping trick as above: window_active_clients contains
	// window_active as a literal prefix, so a naive "must not contain
	// window_active" check would always fail. Strip the correct token first.
	withoutActiveClients := strings.ReplaceAll(formatLine, "window_active_clients", "")
	if strings.Contains(withoutActiveClients, "window_active") {
		t.Errorf(
			"window-status-format line must not use the bare window_active (session-active, not "+
				"visible-to-a-client) - use window_active_clients instead: %s",
			formatLine,
		)
	}

	if strings.Contains(content, "window-status-current-format") {
		t.Error(
			"window-status-current-format must remain unset: the active window's own " +
				"window_active_clients is always >= 1 for the client viewing it, so the visibility " +
				"gate already excludes it without separate handling",
		)
	}
}

// configs/zsh/zshenv.zsh is sourced from ~/.zshenv on every zsh startup
// (login or not), before /etc/zshrc runs. zsh startup semantics — not a
// convention we control — impose the guard shape this test checks: without
// the PATH probe the script would eval path_helper unconditionally on every
// shell (including already-healthy ones), and without the executable check
// it would blow up on Linux, where path_helper doesn't exist.
func TestZshenvStaysPathRepairSafe(t *testing.T) {
	data, err := fs.ReadFile(ConfigsFS, "configs/zsh/zshenv.zsh")
	if err != nil {
		t.Fatalf("failed to read embedded zshenv.zsh: %v", err)
	}
	content := string(data)

	if !strings.Contains(content, `":$PATH:" != *":/usr/bin:"*`) {
		t.Error(
			"zshenv.zsh must guard on PATH missing /usr/bin — without it, the script would " +
				"re-run path_helper on every shell startup instead of only broken ones",
		)
	}
	if !strings.Contains(content, "/usr/libexec/path_helper") {
		t.Error(
			"zshenv.zsh must check for /usr/libexec/path_helper — without it, the script " +
				"would fail on Linux, where path_helper doesn't exist",
		)
	}
}

// The project was renamed devgita -> devgeta, and the installed binary is now
// devgeta. Embedded configs are the one place where the old name fails silently
// rather than loudly: the agent and command prompts under configs/shared invoke
// `devgeta task ...` as shell commands and allowlist those same strings in their
// frontmatter, so a stale name there produces "command not found" (or a
// permission prompt that never matches) inside an agent run, far from anything
// a compiler or a normal test would catch. Nothing embedded has a legitimate
// reason to name the old binary, so this guard covers the whole FS rather than
// enumerating files, and it will catch the next rename's leftovers too.
func TestEmbeddedConfigsDoNotReferenceOldProjectName(t *testing.T) {
	err := fs.WalkDir(ConfigsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(ConfigsFS, path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), "devgita") {
				t.Errorf(
					"%s:%d references the old project name \"devgita\" — use \"devgeta\": %s",
					path,
					lineNo+1,
					strings.TrimSpace(line),
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk embedded configs: %v", err)
	}
}

// Embedded configs are installed into other people's home directories, so a
// path they name has to exist for every one of them. `$HOME/.local/share/...`
// and friends do; `$HOME/<some-checkout>` does not — that is one machine's own
// layout riding along in what everybody installs. So every `$HOME/` path in an
// embedded config must continue into a dot-directory.
//
// One exception is grandfathered below, deliberately: the formatter hook's HTML
// case skips a private repository until it goes public. The maintainer decided
// to keep it, so this guard's job here is to stop the class from spreading
// rather than to relitigate that one — the TODO beside the code tracks its
// removal, and this entry goes when it does.
func TestEmbeddedConfigsNameNoPersonalCheckoutPaths(t *testing.T) {
	// file -> the one $HOME path it may name. See the note above.
	grandfathered := map[string]string{
		"configs/claude/format.sh": "$HOME/lever",
	}

	// `\{HOME\}` and not `\{HOME[^}]*\}`: the loose form also matches
	// ${HOMEBREW_PREFIX}, a legitimate path in the shell templates.
	homePath := regexp.MustCompile(`\$(?:HOME|\{HOME\})/([A-Za-z0-9._-]+)`)

	err := fs.WalkDir(ConfigsFS, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(ConfigsFS, path)
		if err != nil {
			return err
		}
		for lineNo, line := range strings.Split(string(data), "\n") {
			for _, match := range homePath.FindAllStringSubmatch(line, -1) {
				if strings.HasPrefix(match[1], ".") {
					continue
				}
				if allowed, ok := grandfathered[path]; ok && allowed == match[0] {
					continue
				}
				t.Errorf(
					"%s:%d names %q under $HOME — an embedded config may only reach into dot-directories, "+
						"since anything else is one machine's own checkout and does not exist for the users "+
						"who install this: %s",
					path,
					lineNo+1,
					match[0],
					strings.TrimSpace(line),
				)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("failed to walk embedded configs: %v", err)
	}
}
