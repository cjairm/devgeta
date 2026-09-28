package tmux

// RootPaneMoveKeys and its parser: ADR-0057
// (docs/decisions/ADR-0057-the-dashboard-takes-its-pane-move-keys-from-tmux.md).
// The dashboard reads its pane-move keys from tmux itself rather than
// hard-coding a copy, so a rebind in tmux carries over without touching
// devgeta. See that ADR for the full rationale.

import (
	"bufio"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

// PaneMoveKeys names the four keys that move focus between tmux panes, each
// as a bubbletea KeyPressMsg.String() value (e.g. "ctrl+h") - the same
// notation every other keybinding in the dashboard's handleKey switches on.
type PaneMoveKeys struct {
	Left, Down, Up, Right string
}

// defaultPaneMoveKeys is ADR-0057's fallback, used for any direction
// list-keys doesn't resolve (no tmux, no server, no such binding, or a
// parse failure): devgeta's own shipped vim-tmux-navigator default.
var defaultPaneMoveKeys = PaneMoveKeys{
	Left:  "ctrl+h",
	Down:  "ctrl+j",
	Up:    "ctrl+k",
	Right: "ctrl+l",
}

// RootPaneMoveKeys parses `tmux list-keys -T root` for whichever key runs
// select-pane -L/-D/-U/-R, directly or as the fallback branch of an
// if-shell (the shipped is_vim-style bindings) - see ADR-0057. When more
// than one key binds the same direction, the first one list-keys reports
// wins. A direction with no match at all keeps defaultPaneMoveKeys' value.
func (t *Tmux) RootPaneMoveKeys() PaneMoveKeys {
	keys := defaultPaneMoveKeys
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"list-keys", "-T", "root"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return keys
	}

	var foundL, foundD, foundU, foundR bool
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		key, dir, ok := parseRootKeyBindingLine(scanner.Text())
		if !ok {
			continue
		}
		bkey := tmuxKeyToBubbletea(key)
		switch dir {
		case "L":
			if !foundL {
				keys.Left = bkey
				foundL = true
			}
		case "D":
			if !foundD {
				keys.Down = bkey
				foundD = true
			}
		case "U":
			if !foundU {
				keys.Up = bkey
				foundU = true
			}
		case "R":
			if !foundR {
				keys.Right = bkey
				foundR = true
			}
		}
	}
	return keys
}

// parseRootKeyBindingLine parses one line of `list-keys -T root` output,
// reporting the key it binds and which select-pane direction ("L"/"D"/"U"/
// "R") it ultimately runs, either directly or as an if-shell's fallback
// branch. ok is false for any line that isn't a root-table select-pane
// binding at all (every other tmux binding, mouse events, etc.).
func parseRootKeyBindingLine(line string) (key, dir string, ok bool) {
	tokens := tokenizeShellLike(line)
	if len(tokens) < 5 || tokens[0] != "bind-key" || tokens[1] != "-T" || tokens[2] != "root" {
		return "", "", false
	}
	key = tokens[3]
	cmdTokens := tokens[4:]

	switch cmdTokens[0] {
	case "select-pane":
		if d, ok2 := selectPaneDirection(cmdTokens[1:]); ok2 {
			return key, d, true
		}
	case "if-shell":
		// The fallback branch (the case with no supporting is_vim-style
		// check) is the LAST quoted argument, whichever flags (-F, -b, ...)
		// precede the condition string.
		last := cmdTokens[len(cmdTokens)-1]
		lastTokens := tokenizeShellLike(last)
		if len(lastTokens) >= 1 && lastTokens[0] == "select-pane" {
			if d, ok2 := selectPaneDirection(lastTokens[1:]); ok2 {
				return key, d, true
			}
		}
	}
	return "", "", false
}

// selectPaneDirection scans a select-pane command's own arguments for one
// of the four direction flags.
func selectPaneDirection(args []string) (string, bool) {
	for _, a := range args {
		switch a {
		case "-L":
			return "L", true
		case "-D":
			return "D", true
		case "-U":
			return "U", true
		case "-R":
			return "R", true
		}
	}
	return "", false
}

// tokenizeShellLike splits line into tokens the way a shell would: outside
// quotes, whitespace separates tokens; a double-quoted region is one token
// (quotes stripped), with a backslash escaping the next character while
// inside quotes. This is what lets a single pass find the outermost quoted
// arguments of a `bind-key ... if-shell "cond" "true" "false"` line without
// needing to understand whatever shell syntax the condition string itself
// contains.
func tokenizeShellLike(line string) []string {
	var tokens []string
	var cur strings.Builder
	inQuotes := false
	hasCur := false
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '"':
			inQuotes = !inQuotes
			hasCur = true
		case c == '\\' && inQuotes && i+1 < len(line):
			cur.WriteByte(line[i+1])
			i++
			hasCur = true
		case !inQuotes && (c == ' ' || c == '\t'):
			if hasCur {
				tokens = append(tokens, cur.String())
				cur.Reset()
				hasCur = false
			}
		default:
			cur.WriteByte(c)
			hasCur = true
		}
	}
	if hasCur {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// tmuxKeyToBubbletea converts a tmux key notation (as list-keys prints it,
// e.g. "C-h", "M-Left") to the equivalent bubbletea KeyPressMsg.String()
// form (e.g. "ctrl+h", "alt+left") - bridging tmux's own config syntax to
// the key strings handleKey switches on for every other keybinding.
func tmuxKeyToBubbletea(tmuxKey string) string {
	var mods []string
	rest := tmuxKey
	for {
		switch {
		case strings.HasPrefix(rest, "C-"):
			mods = append(mods, "ctrl")
			rest = rest[2:]
		case strings.HasPrefix(rest, "M-"):
			mods = append(mods, "alt")
			rest = rest[2:]
		case strings.HasPrefix(rest, "S-"):
			mods = append(mods, "shift")
			rest = rest[2:]
		default:
			name := strings.ToLower(rest)
			return strings.Join(append(mods, name), "+")
		}
	}
}
