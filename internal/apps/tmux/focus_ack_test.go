package tmux_test

import (
	"strings"
	"testing"
)

// hookBlock returns the body of the brace-delimited `set-hook -g <name> {`
// block in conf, or "" when the hook isn't set. It relies on the block's
// closing brace sitting alone at the start of a line, which is how the
// shipped config writes every multi-line hook.
func hookBlock(conf, name string) string {
	start := strings.Index(conf, "set-hook -g "+name+" {\n")
	if start < 0 {
		return ""
	}
	body := conf[start:]
	end := strings.Index(body, "\n}\n")
	if end < 0 {
		return ""
	}
	return body[:end]
}

// A pane counts as seen whenever it gains or loses focus, however the user
// got there - tmux keys, the mouse, switch-client, or the dashboard. Before
// this, only the dashboard's own attach cleared agent state, so reaching an
// agent's pane any other way left its "wants you" dot behind. Losing focus
// counts too: an agent that finished while you were watching it has been
// seen once you move on.
func TestShippedConfAcknowledgesAgentStateOnPaneFocus(t *testing.T) {
	conf := string(renderShippedTmuxConf(t))

	for _, hook := range []string{"pane-focus-in", "pane-focus-out"} {
		t.Run(hook, func(t *testing.T) {
			block := hookBlock(conf, hook)
			if block == "" {
				t.Fatalf("shipped tmux.conf has no `set-hook -g %s { ... }` block", hook)
			}
			// Only the "wants you" states are acknowledged; a working agent
			// must keep reporting busy while you look at it.
			for _, state := range []string{"idle", "blocked", "error"} {
				if !strings.Contains(block, "#{==:#{@dg_agent_state},"+state+"}") {
					t.Errorf("%s does not acknowledge the %q state", hook, state)
				}
			}
			if strings.Contains(block, "busy") {
				t.Errorf("%s must never touch a busy agent", hook)
			}
			if !strings.Contains(block, "set -pu @dg_agent_state") {
				t.Errorf("%s does not unset the pane's @dg_agent_state", hook)
			}
			// The status-bar mirror is shared by every pane in the window, so
			// it may only be cleared once no pane in that window still wants
			// you (ADR-0008's per-pane granularity).
			if !strings.Contains(block, "#{P:") {
				t.Errorf(
					"%s clears the window mirror without checking the window's other panes",
					hook,
				)
			}
			if !strings.Contains(block, "set -wu @dg_window_agent_state") {
				t.Errorf("%s never clears the window-level @dg_window_agent_state mirror", hook)
			}
		})
	}

	if in, out := hookBlock(conf, "pane-focus-in"), hookBlock(conf, "pane-focus-out"); in != "" &&
		strings.TrimPrefix(in, "set-hook -g pane-focus-in") !=
			strings.TrimPrefix(out, "set-hook -g pane-focus-out") {
		t.Errorf("pane-focus-in and pane-focus-out must run the same acknowledgement")
	}
}
