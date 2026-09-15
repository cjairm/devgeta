package main

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/constants"
)

// TestShellConfigNeverHardcodesAHomebrewBinaryPath pins devgeta.zsh to resolving
// external binaries through PATH (or the HOMEBREW_PREFIX the template already
// computes once at the top) rather than naming a Homebrew bin directory
// outright.
//
// A hardcoded prefix is not a style problem, it is a machine-class outage: a
// line that says /usr/local/bin/tmux works on Intel and fails outright on every
// Apple Silicon box, where Homebrew lives at /opt/homebrew. tmn() shipped that
// way from the repo's first commit and nobody noticed until devgeta was
// installed on new hardware.
//
// The check is written against constants.HomebrewPrefixes so it covers both
// prefixes and picks up a third automatically if one is ever added, and it runs
// with every feature flag on so no {{if}} branch can hide a line from it.
func TestShellConfigNeverHardcodesAHomebrewBinaryPath(t *testing.T) {
	rendered := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())

	for _, prefix := range constants.HomebrewPrefixes {
		needle := prefix + "/bin/"
		for i, line := range strings.Split(rendered, "\n") {
			if strings.Contains(line, needle) {
				t.Errorf(
					"devgeta.zsh line %d hardcodes the Homebrew bin directory %q, which breaks on machines whose Homebrew prefix differs: %q\n"+
						"Call the binary by name so PATH resolves it, or use ${HOMEBREW_PREFIX}.",
					i+1,
					needle,
					strings.TrimSpace(line),
				)
			}
		}
	}
}

// TestShellConfigTmuxHelperCallsTmuxByName is the positive half of the check
// above, aimed at the one function that had the bug. Asserting only "no
// hardcoded prefix" would also pass if tmn() lost its tmux calls entirely, so
// this pins the calls themselves.
func TestShellConfigTmuxHelperCallsTmuxByName(t *testing.T) {
	rendered := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())

	for _, want := range []string{
		`tmux new-session -d -s "$session_name" -c "$path"`,
		`tmux switch-client -t "$session_name"`,
	} {
		found := false
		for _, line := range strings.Split(rendered, "\n") {
			// An exact match on the trimmed line, not Contains: a line that
			// merely ends with the wanted text would still match Contains even
			// with an absolute path prefixed onto the binary.
			if strings.TrimSpace(line) == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf(
				"Expected devgeta.zsh's tmn() to contain the line %q, calling tmux by name",
				want,
			)
		}
	}
}

// TestShellConfigDoesNotReferenceOhMyZsh keeps devgeta.zsh from shipping aliases
// for tools devgeta does not install.
//
// `alias edit-ohmyzsh="nvim ~/.oh-my-zsh"` rode along from the first commit.
// Devgeta configures powerlevel10k directly and installs no oh-my-zsh, so on
// every machine the alias opens nvim on a directory that does not exist - and
// it points nvim at a directory rather than a file besides.
func TestShellConfigDoesNotReferenceOhMyZsh(t *testing.T) {
	rendered := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())

	for i, line := range strings.Split(rendered, "\n") {
		lower := strings.ToLower(line)
		if strings.Contains(lower, "oh-my-zsh") || strings.Contains(lower, "ohmyzsh") {
			t.Errorf(
				"devgeta.zsh line %d references oh-my-zsh, which devgeta does not install: %q",
				i+1,
				strings.TrimSpace(line),
			)
		}
	}
}

// TestShellConfigAliasesSpellOptionValues pins every rendered alias against the
// swallowed-argument bug: an option whose value is OPTIONAL takes the next argv
// entry when it is written bare, so the user's first argument becomes the
// option's value. `alias ls='eza ... --icons'` meant `ls ~/foo` reached eza as
// `--icons ~/foo` and failed with "invalid value '/Users/you/foo' for '--icons
// [<WHEN>]'" - plain `ls` worked, so the alias looked fine until someone passed
// a path.
//
// The flags below are the value-taking options that appear in the shipped
// aliases. Spelling the value with `=` is what makes the bug impossible: an
// option that already has its value can never consume an argument. Adding a
// value-taking flag to an alias means adding it here.
func TestShellConfigAliasesSpellOptionValues(t *testing.T) {
	valueTakingFlags := []string{
		"--icons", // eza: --icons[=WHEN], the one that bit us
		"--color", // eza/bat: --color[=WHEN]
		"--level", // eza: --level=DEPTH
		"--style", // bat: --style=COMPONENTS
		"--type",  // fd: --type=FILETYPE
	}

	rendered := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())
	for _, line := range strings.Split(rendered, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "alias ") {
			continue
		}
		for _, flag := range valueTakingFlags {
			for _, field := range strings.Fields(line) {
				if strings.Trim(field, `'"`) == flag {
					t.Errorf(
						"alias line %q writes %s bare; spell it %s=<value> so it cannot "+
							"consume the user's first argument",
						line, flag, flag,
					)
				}
			}
		}
	}
}
