package cmd

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// rootHelpOutput captures what `dg --help` prints.
//
// It goes through rootCmd.Help() rather than rendering a template itself, so
// whatever help function the root is wired to is the thing under test - a help
// func that writes straight to os.Stdout instead of the command's writer shows
// up here as empty output, which is itself a finding.
func rootHelpOutput(t *testing.T) string {
	t.Helper()

	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
	})

	if err := rootCmd.Help(); err != nil {
		t.Fatalf("rootCmd.Help() failed: %v", err)
	}
	return buf.String()
}

// helpCommandNames returns the command names listed under "Available
// Commands:" in help output - the first word of each indented line, until the
// section ends at a blank line.
func helpCommandNames(t *testing.T, help string) []string {
	t.Helper()

	lines := strings.Split(help, "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == "Available Commands:" {
			start = i + 1
			break
		}
	}
	if start == -1 {
		t.Fatalf("help output has no \"Available Commands:\" section:\n%s", help)
	}

	var names []string
	for _, line := range lines[start:] {
		if strings.TrimSpace(line) == "" {
			break
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			break
		}
		names = append(names, fields[0])
	}
	return names
}

// TestRootHelpListsExactlyTheRegisteredCommands is the regression test for a
// help screen that advertised six commands devgeta does not have - reinstall,
// re-configure, update, check-updates, backup, restore - while hiding five it
// does: completion, archive, task, workspace and worktree.
//
// The cause was structural, not a typo: the root's help function printed only
// Use and Long, so the command list had to be hand-typed into Long and drifted
// from the code every time a command was added or renamed. Comparing the
// rendered section against the registered set fails on a hand-typed list in
// both directions, so the only way to keep it passing is to let Cobra generate
// it from the commands that actually exist.
func TestRootHelpListsExactlyTheRegisteredCommands(t *testing.T) {
	help := rootHelpOutput(t)
	listed := helpCommandNames(t, help)

	// IsAvailableCommand is the same predicate Cobra's own template uses, so
	// this compares against what is registered and runnable - including the
	// auto-added `help` - and not against a second hand-kept list.
	registered := make(map[string]bool)
	for _, c := range rootCmd.Commands() {
		if c.IsAvailableCommand() {
			registered[c.Name()] = true
		}
	}

	listedSet := make(map[string]bool, len(listed))
	for _, name := range listed {
		listedSet[name] = true
		if !registered[name] {
			t.Errorf(
				"help lists %q, which is not a registered command - a planned command belongs in ROADMAP.md, not in the help",
				name,
			)
		}
	}
	for name := range registered {
		if !listedSet[name] {
			t.Errorf("help does not list the registered command %q", name)
		}
	}
}

// TestRootHelpShowsGlobalFlags: the branded help printed no flag section at
// all, so --verbose was undiscoverable from `dg --help`.
func TestRootHelpShowsGlobalFlags(t *testing.T) {
	help := rootHelpOutput(t)

	if !strings.Contains(help, "--verbose") {
		t.Errorf("`dg --help` does not mention --verbose:\n%s", help)
	}
}

// TestRootExamplesRunRealCommands pins the examples to commands that exist.
// Two of the five shipped examples could not run: `dg re-configure
// --app=neovim` and `dg backup --output=...` named commands that were never
// built. An example is the first thing a new user copies, so a stale one is a
// broken first impression.
func TestRootExamplesRunRealCommands(t *testing.T) {
	registered := make(map[string]bool)
	for _, c := range rootCmd.Commands() {
		registered[c.Name()] = true
		for _, alias := range c.Aliases {
			registered[alias] = true
		}
	}

	// Long as well as Example: the stale examples were written into Long, and
	// scanning only the Example field would pass on an empty one while the
	// broken examples still shipped.
	scanned := rootCmd.Long + "\n" + rootCmd.Example
	for _, line := range strings.Split(scanned, "\n") {
		fields := strings.Fields(line)
		// Every example is `dg <command> ...`; anything shorter is prose.
		if len(fields) < 2 || fields[0] != "dg" {
			continue
		}
		if !registered[fields[1]] {
			t.Errorf(
				"example %q names %q, which is not a registered command",
				strings.TrimSpace(line),
				fields[1],
			)
		}
	}
}

// TestSubcommandHelpListsItsOwnSubcommands guards the inheritance the root's
// help function now relies on.
//
// taskCmd and configCmd used to call SetHelpFunc themselves, purely to escape
// a root help func that printed only Use+Long. With the root on
// standardHelpFunc those calls were redundant and were removed - but only
// because Cobra resolves a command's help function by walking up to its
// parent. If that ever stops holding, `dg task --help` silently stops listing
// subcommands again, which is the exact failure the removal must not
// reintroduce.
func TestSubcommandHelpListsItsOwnSubcommands(t *testing.T) {
	for _, parent := range []string{"task", "config"} {
		t.Run(parent, func(t *testing.T) {
			var cmd *cobra.Command
			for _, c := range rootCmd.Commands() {
				if c.Name() == parent {
					cmd = c
					break
				}
			}
			if cmd == nil {
				t.Fatalf("no %q command is registered", parent)
			}

			var buf bytes.Buffer
			cmd.SetOut(&buf)
			cmd.SetErr(&buf)
			t.Cleanup(func() {
				cmd.SetOut(nil)
				cmd.SetErr(nil)
			})
			if err := cmd.Help(); err != nil {
				t.Fatalf("%s.Help() failed: %v", parent, err)
			}

			help := buf.String()
			for _, sub := range cmd.Commands() {
				if !sub.IsAvailableCommand() {
					continue
				}
				if !strings.Contains(help, sub.Name()) {
					t.Errorf("`dg %s --help` does not list its subcommand %q", parent, sub.Name())
				}
			}
		})
	}
}

// TestHelpGoesToStdoutNotStderr pins the stream help is written on.
//
// standardHelpFunc originally used cmd.Print/cmd.Println, which resolve to
// Cobra's OutOrStderr and therefore fall back to os.Stderr when nothing has
// called SetOut. Every command inherits the root's help function, so the whole
// tree printed its help on the error stream: `dg --help | grep export` matched
// nothing and `dg task --help > notes.txt` wrote an empty file.
//
// The fallback is the whole point, so this test must NOT call SetOut: doing so
// gives the command an output writer, which is exactly what stops OutOrStderr
// from reaching for stderr, and the bug then hides. That is why
// TestSubcommandHelpListsItsOwnSubcommands passes either way. Redirecting the
// real os.Stdout/os.Stderr is the only way to observe the choice the binary
// actually makes.
func TestHelpGoesToStdoutNotStderr(t *testing.T) {
	for _, name := range []string{"", "task", "config", "export"} {
		label := name
		if label == "" {
			label = "root"
		}
		t.Run(label, func(t *testing.T) {
			cmd := rootCmd
			if name != "" {
				cmd = nil
				for _, c := range rootCmd.Commands() {
					if c.Name() == name {
						cmd = c
						break
					}
				}
				if cmd == nil {
					t.Fatalf("no %q command is registered", name)
				}
			}
			// Undo any writer a sibling test left behind, so the fallback is
			// live for this call.
			cmd.SetOut(nil)
			cmd.SetErr(nil)

			outStr, errStr := captureStdStreams(t, func() {
				if err := cmd.Help(); err != nil {
					t.Fatalf("%s.Help() failed: %v", label, err)
				}
			})

			if errStr != "" {
				t.Errorf(
					"%s help wrote %d bytes to stderr; asking for help is not an error:\n%s",
					label, len(errStr), errStr,
				)
			}
			if outStr == "" {
				t.Fatalf("%s help wrote nothing to stdout", label)
			}
			if !strings.Contains(outStr, "Usage:") {
				t.Errorf("%s help on stdout has no usage block:\n%s", label, outStr)
			}
		})
	}
}

// captureStdStreams swaps os.Stdout and os.Stderr for pipes while fn runs and
// returns what was written to each. Cobra reads the os.Stdout/os.Stderr
// package variables at call time, so reassigning them here is what the
// fallback sees. Help output is a few KB, well inside the pipe buffer, so fn
// cannot block before the read.
func captureStdStreams(t *testing.T, fn func()) (string, string) {
	t.Helper()

	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}

	origOut, origErr := os.Stdout, os.Stderr
	os.Stdout, os.Stderr = outW, errW
	restore := func() { os.Stdout, os.Stderr = origOut, origErr }
	t.Cleanup(restore)

	fn()

	restore()
	if cerr := outW.Close(); cerr != nil {
		t.Fatalf("closing stdout pipe: %v", cerr)
	}
	if cerr := errW.Close(); cerr != nil {
		t.Fatalf("closing stderr pipe: %v", cerr)
	}

	outBytes, err := io.ReadAll(outR)
	if err != nil {
		t.Fatalf("reading stdout: %v", err)
	}
	errBytes, err := io.ReadAll(errR)
	if err != nil {
		t.Fatalf("reading stderr: %v", err)
	}
	return string(outBytes), string(errBytes)
}
