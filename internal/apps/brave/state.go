// Brave's StatePorter adapter: the first one, and the shape every later
// adapter copies. It is a data table plus three small lookups — the design
// lives in ADR-0045, and this file is only the transcription.
package brave

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.StatePorter = (*Brave)(nil)

// profilePattern is what a Chromium profile directory is called. A user
// data directory holds plenty that is not one — Crashpad, GrShaderCache,
// and the System and Guest profiles, which hold no state a user would miss.
var profilePattern = regexp.MustCompile(`^(Default|Profile \d+)$`)

// pgrepNoMatch is pgrep's exit code for "nothing matched". It is an answer,
// not a failure, and telling the two apart is the whole reason this goes
// through *exec.ExitError rather than treating any error as "not running" —
// which would silently turn the refusal off.
const pgrepNoMatch = 1

// StateGroups is ADR-0045's table for Brave, and nothing else. Adding,
// renaming or re-defaulting a row is an amendment to that ADR; state_test.go
// asserts this list against it so a silent edit fails rather than ships.
func (b *Brave) StateGroups() []apps.StateGroup {
	return []apps.StateGroup{
		{
			Name:    "bookmarks",
			Paths:   []string{"Bookmarks"},
			Default: true,
			Why:     "Plain JSON, stable across versions. What a user most expects to survive a move.",
		},
		{
			Name:    "preferences",
			Paths:   []string{"Preferences"},
			Default: true,
			Why:     "Settings, including the extension list. A newer Brave ignores keys it does not know.",
		},
		{
			Name:    "tabs",
			Paths:   []string{"Sessions"},
			Default: true,
			Why:     "The open tabs — the work in progress, which is the point of a machine move.",
		},
		{
			Name:    "extension-settings",
			Paths:   []string{"Extension State", "Local Extension Settings"},
			Default: true,
			Why:     "Per-extension data. Without it the extensions arrive reset to defaults.",
		},
		{
			Name:    "extensions",
			Paths:   []string{"Extensions"},
			Default: false,
			Why:     "The extension payloads, which the store re-downloads. Turn on for a machine that will be offline.",
		},
		{
			Name:    "history",
			Paths:   []string{"History"},
			Default: false,
			Why:     "The browsing record. Moving it is a decision you make, not a default you discover.",
		},
	}
}

// StateRoots finds every profile under Brave's user data directory. A
// machine where Brave has never run has no data directory at all, which is
// not an error — it is a destination with nothing to import into yet, and
// the caller says so in its own words.
func (b *Brave) StateRoots() (map[string]string, error) {
	base := b.dataDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]string{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", base, err)
	}

	roots := map[string]string{}
	for _, entry := range entries {
		if entry.IsDir() && profilePattern.MatchString(entry.Name()) {
			roots[entry.Name()] = filepath.Join(base, entry.Name())
		}
	}
	return roots, nil
}

// dataDir is Brave's user data directory, which is where the profiles live
// and so the base a bundle is written relative to.
//
// Both branches go through pkg/paths and never a literal "~": that is the
// layer the `go test` sandbox redirects, so a test gets a throwaway root
// instead of the real profile (CLAUDE.md §4).
func (b *Brave) dataDir() string {
	if b.Base.IsMac() {
		return paths.GetHomeDir(
			"Library",
			"Application Support",
			"BraveSoftware",
			"Brave-Browser",
		)
	}
	// Chromium's own rule on Linux is $XDG_CONFIG_HOME, so GetConfigDir
	// gets both the variable and the ~/.config fallback right for free.
	return paths.GetConfigDir("BraveSoftware", "Brave-Browser")
}

// processNames are the names Brave's main process runs under. macOS names
// the binary after the app; the Linux packages ship one or the other.
func (b *Brave) processNames() []string {
	if b.Base.IsMac() {
		return []string{"Brave Browser"}
	}
	return []string{"brave", "brave-browser"}
}

// IsRunning reports whether Brave is running, which both `dg export` and
// `dg import` refuse on: Sessions/, History and Local Extension Settings/
// are SQLite and LevelDB, and copied out from under a live process they are
// corrupt on arrival (ADR-0045).
//
// The match is on the exact process name (-x) and never the command line
// (-f). `pgrep -f brave` matches the devgeta process running `dg import
// brave`, so a full-command-line match would refuse every import on every
// machine.
func (b *Brave) IsRunning() (bool, error) {
	for _, name := range b.processNames() {
		stdout, _, err := b.Base.ExecCommand(cmd.CommandParams{
			Command: "pgrep",
			Args:    []string{"-x", name},
		})
		if err == nil {
			if strings.TrimSpace(stdout) != "" {
				return true, nil
			}
			continue
		}
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == pgrepNoMatch {
			continue
		}
		return false, fmt.Errorf("looking for a running %q process: %w", name, err)
	}
	return false, nil
}
