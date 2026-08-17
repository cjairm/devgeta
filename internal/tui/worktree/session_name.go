// Auto-generation of a standalone-session name when the user leaves the name
// prompt blank. Names are "<folder>-<character>": the folder the session opens
// in, followed by a Dragon Ball character — a nod to devgeta itself, whose name
// comes from Vegeta. They are always checked against the live tmux sessions so
// a blank-name create can never collide with an existing session (tmux
// new-session -s fails on a duplicate).

package tuiworktree

import (
	"fmt"
	"math/rand"
	"path/filepath"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/paths"
)

// defaultSessionLabel is the fallback prefix when a chosen folder yields no
// usable label (its name flattens to empty, or is the filesystem root). It can
// never be tmux-hostile, so a generated name is always valid.
const defaultSessionLabel = "root"

// sessionLabelForDir derives the auto-session-name prefix from the folder the
// session opens in: the folder's own name, with the characters tmux treats
// specially in a target name (".", ":", whitespace) flattened via
// worktree.TmuxSessionName, so "<label>-<character>" is always a valid session
// name. The home directory maps to "home" — its basename is the opaque account
// name, useless as a label — and a folder whose name flattens to nothing (empty
// or the filesystem root) falls back to defaultSessionLabel.
func sessionLabelForDir(workdir string) string {
	// Canonicalize BOTH sides. CanonicalRepoPath resolves symlinks, and workdir
	// arrives unresolved (it is whatever validateSessionDirFn returned), so
	// canonicalizing only the home side meant a home reached through a symlink
	// never matched - the label silently fell through to the basename, which for
	// a home directory is the opaque account name this branch exists to avoid.
	// The empty string is excluded rather than canonicalized: CanonicalRepoPath("")
	// resolves to the current directory, which would make a blank workdir report
	// "home" whenever devgeta happened to be run from home, instead of taking the
	// defaultSessionLabel fallback below.
	if workdir != "" &&
		config.CanonicalRepoPath(workdir) == config.CanonicalRepoPath(paths.Paths.Home.Root) {
		return "home"
	}
	// Guard the raw basename before flattening: filepath.Base("") is "." and
	// Base("/") is "/", neither of which is a usable label — flattening would
	// turn "." into "_" and hide the fallback.
	base := filepath.Base(workdir)
	if base == "" || base == "/" || base == "." {
		return defaultSessionLabel
	}
	return worktree.TmuxSessionName(base)
}

// dragonBallNames is the pool the auto-namer draws from. Every entry is
// already lowercase and free of spaces, dots, and colons, so the character half
// of a "<label>-<character>" name needs no sanitizing (tmux treats "." and ":"
// specially in target names); the label half comes from sessionLabelForDir,
// which sanitizes it via worktree.TmuxSessionName. Kept short and recognizable
// on purpose — a generated name should read as a friendly label, not a random
// string.
var dragonBallNames = []string{
	"goku", "vegeta", "gohan", "piccolo", "trunks", "goten",
	"krillin", "bulma", "roshi", "beerus", "whis", "frieza",
	"cell", "buu", "raditz", "nappa", "tien", "yamcha",
	"videl", "bardock", "broly", "zamasu", "gotenks", "shenron",
}

// nextFreeSessionName returns a session name that is not present in taken,
// preferring "<label>-<character>" for a Dragon Ball character (tried in the
// order given, so callers control randomness/determinism), and falling back to
// "<label>-<character>-<n>" with an incrementing n only if every bare
// character name is already taken. It is guaranteed to return a name absent
// from taken, so the caller's blank-name create can always proceed without
// clashing with a live session.
//
// label is the folder-derived prefix (see sessionLabelForDir). order indexes
// into dragonBallNames; callers pass a shuffled order for a random pick (see
// randomSessionNameOrder) or a fixed one in tests. It must be non-empty —
// dragonBallNames is a non-empty package var, so its randomSessionNameOrder
// output always is too.
func nextFreeSessionName(label string, taken map[string]bool, order []int) string {
	for _, i := range order {
		if name := label + "-" + dragonBallNames[i]; !taken[name] {
			return name
		}
	}
	// Every bare character is already taken (the user has >= len(pool) live
	// sessions named after the pool): disambiguate the first-ordered character
	// with an incrementing numeric suffix. Bounded by taken's size, so it
	// always terminates on a free name rather than looping forever.
	base := label + "-" + dragonBallNames[order[0]]
	for n := 2; ; n++ {
		if name := fmt.Sprintf("%s-%d", base, n); !taken[name] {
			return name
		}
	}
}

// randomSessionNameOrder returns the indices of dragonBallNames in a random
// order, so successive blank-name creates tend to pick different characters
// rather than always starting from "goku". Go auto-seeds the global rand
// source (1.20+), so this needs no manual seeding. Extracted from
// nextFreeSessionName so tests can supply a deterministic order instead.
func randomSessionNameOrder() []int {
	return rand.Perm(len(dragonBallNames))
}
