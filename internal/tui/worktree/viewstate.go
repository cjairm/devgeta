package tuiworktree

// Encode/decode/prune for the dashboard's saved view state (ADR-0050): folds
// and the left-pane width, stored as versioned JSON in the tmux server-global
// option @dg_ws_state. Model-level wiring (load on start, write triggers,
// best-effort failure handling) lives in model.go; this file holds only the
// pure functions, so they're testable without a tmux dependency at all.

import (
	"encoding/json"
	"sort"

	"github.com/cjairm/devgeta/internal/tooling/worktree"
)

// viewStateOptionName is the tmux server-global user option the saved state
// lives in.
const viewStateOptionName = "@dg_ws_state"

// viewStateVersion is bumped whenever the JSON shape changes incompatibly;
// decodeViewState rejects anything else rather than guess at migrating it.
const viewStateVersion = 1

// viewStateV1 is @dg_ws_state's JSON shape. Collapsed keys come from the
// dashboard's own rowKey (B5), so the saved state and the in-memory fold map
// can never disagree about identity.
type viewStateV1 struct {
	V         int      `json:"v"`
	Collapsed []string `json:"collapsed"`
	Left      int      `json:"left"`
}

// encodeViewState builds @dg_ws_state's value from the currently-collapsed
// keys and the left-pane width. Keys are sorted so the output is
// deterministic - useful for tests and for not rewriting the option to a
// byte-different value that means the same thing.
func encodeViewState(collapsed map[string]bool, left int) (string, error) {
	keys := make([]string, 0, len(collapsed))
	for k, v := range collapsed {
		if v {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	data, err := json.Marshal(viewStateV1{V: viewStateVersion, Collapsed: keys, Left: left})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// decodeViewState parses raw as a viewStateV1, reporting ok=false for an
// empty (unset option), corrupt, or unknown-version value (ADR-0050) - all
// three are treated identically as "nothing to restore, start from
// defaults," never as an error the caller has to handle specially.
func decodeViewState(raw string) (viewStateV1, bool) {
	if raw == "" {
		return viewStateV1{}, false
	}
	var vs viewStateV1
	if err := json.Unmarshal([]byte(raw), &vs); err != nil {
		return viewStateV1{}, false
	}
	if vs.V != viewStateVersion {
		return viewStateV1{}, false
	}
	return vs, true
}

// validCollapseKeys returns the rowKey identities that currently exist among
// statuses (repo headers and worktrees), standalone sessions, and repo
// sessions - the set saved state is pruned against before every write
// (ADR-0050: "keys for rows that no longer exist are dropped on write").
// Repo sessions share the exact "sess:<name>" namespace standalone sessions
// use (a session name is unique on the server, ADR-0052), and that key
// persists real state for either kind: a session's own pane-fold, the only
// thing paneParentKey ever returns for a rowSession/rowRepoSession (sessions
// have no collapse of their own beyond that - see ADR-0050's own example
// value, which persists "sess:misc").
//
// Deliberately excludes rowPane: pane IDs are ephemeral tmux identifiers
// that get reused across restarts, so persisting a fold for one buys nothing
// and only accumulates garbage - nothing ever writes a "pane:" key into
// m.collapsed in the first place.
func validCollapseKeys(
	statuses []worktree.WorktreeStatus,
	sessions []worktree.SessionStatus,
	repoSessions []worktree.RepoSessionStatus,
) map[string]bool {
	valid := make(map[string]bool, len(statuses)+len(sessions)+len(repoSessions))
	for _, s := range statuses {
		valid[repoKey(s.Repo)] = true
		valid["wt:"+s.Path] = true
	}
	for _, sess := range sessions {
		valid["sess:"+sess.Name] = true
	}
	for _, rs := range repoSessions {
		valid["sess:"+rs.Name] = true
	}
	return valid
}
