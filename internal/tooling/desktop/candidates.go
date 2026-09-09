package desktop

import (
	"fmt"
	"strings"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/utils"
)

// candidate pairs a name with the predicate that decides whether it belongs
// in a chooser at all. It deliberately does not carry the app instance
// itself: chooseOne's only job is picking a name out of the ones actually
// installable on this platform, and each call site already knows which
// concrete app a chosen name maps to (see desktop.go's terminal and
// screenshot groups).
type candidate struct {
	name      string
	available func() bool
}

// chooseOne filters candidates down to the ones available on this platform,
// then:
//   - 0 available: warns and returns ("", false) — nothing to install
//   - 1 available: returns it without prompting. This is the common case
//     today: macOS and Debian 12 each resolve every pair to exactly one
//     candidate, so most installs never see a prompt.
//   - >1 available: prompts via selectFn and returns the user's pick
//
// selectFn is injected (production wires promptui.Select) so tests never
// drive a real interactive prompt.
func chooseOne(
	label string,
	candidates []candidate,
	selectFn func(label string, options []string) (string, error),
) (string, bool) {
	var available []candidate
	for _, c := range candidates {
		if c.available() {
			available = append(available, c)
		}
	}

	switch len(available) {
	case 0:
		names := make([]string, len(candidates))
		for i, c := range candidates {
			names[i] = c.name
		}
		utils.PrintWarning(
			fmt.Sprintf(
				"None of %s is available on this platform; skipping.",
				strings.Join(names, ", "),
			),
		)
		return "", false
	case 1:
		return available[0].name, true
	default:
		options := make([]string, len(available))
		for i, c := range available {
			options[i] = c.name
		}
		choice, err := selectFn(label, options)
		if err != nil {
			utils.PrintWarning(fmt.Sprintf("%s selection cancelled; skipping.", label))
			return "", false
		}
		return choice, true
	}
}

// macStaticAvailability records, for macOS, which candidates are actually
// installable via Homebrew today. Declared rather than probed — the "Probe
// apt, declare macOS" trade-off (cycle 2026-09-09): probing brew here would
// cost a network round-trip on every macOS install to answer a question
// that is a documented, permanent fact, not a transient one.
var macStaticAvailability = map[string]bool{
	// Homebrew disabled these two casks on 2026-09-01: neither notarizes,
	// so both fail Gatekeeper and Homebrew stopped carrying them in
	// homebrew/cask. Permanent per ADR-0037, not an outage.
	constants.Alacritty: false,
	constants.Flameshot: false,
	constants.Ghostty:   true,
	constants.Shottr:    true,
}

// platformAvailable reports whether pkg can be installed on this platform.
// macOS answers from the static map above; Linux probes apt-cache, since
// which apt packages exist is exactly the kind of distro-version detail
// that rots if hardcoded (see the trade-off doc on macStaticAvailability).
func platformAvailable(base cmd.BaseCommandExecutor, isMac bool, pkg string) bool {
	if isMac {
		return macStaticAvailability[pkg]
	}
	return aptCandidateAvailable(base, pkg)
}

// aptCandidateAvailable reports whether apt-cache knows an installable
// candidate version for pkg. Routed through base.ExecCommand rather than
// shelling out directly so tests can mock it.
func aptCandidateAvailable(base cmd.BaseCommandExecutor, pkg string) bool {
	stdout, _, err := base.ExecCommand(cmd.CommandParams{
		Command: "apt-cache",
		Args:    []string{"policy", pkg},
	})
	if err != nil {
		return false
	}
	return aptPolicyHasCandidate(stdout)
}

// aptPolicyHasCandidate parses `apt-cache policy <pkg>` output for a real
// Candidate line. `apt-cache policy` exits 0 whether or not the package
// exists, so checking the exit code alone would mark every absent package
// available; an absent or unknown package instead prints no Candidate line
// at all, or "Candidate: (none)".
func aptPolicyHasCandidate(output string) bool {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if after, ok := strings.CutPrefix(line, "Candidate:"); ok {
			version := strings.TrimSpace(after)
			return version != "" && version != "(none)"
		}
	}
	return false
}
