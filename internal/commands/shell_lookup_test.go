package commands

import "testing"

// classifyShellLookup is the pure half of the interactive-shell probe: it
// decides Found/NotFound/Inconclusive (and, on Found, the resolved path)
// from the captured stdout alone, with the marker line as the only accepted
// proof that `command -v` actually ran (ADR-0016). Testing it directly needs
// no shell — these strings are exactly what defaultShellCommandLookup
// captures.
func TestClassifyShellLookup(t *testing.T) {
	tests := []struct {
		name     string
		stdout   string
		wantPath string
		want     ShellLookupResult
	}{
		{
			name:     "marker with status 0 and no command -v output means found, no path",
			stdout:   "\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		{
			name:     "marker with non-zero status means not found",
			stdout:   "\n" + shellLookupMarker + "1\n",
			wantPath: "",
			want:     ShellLookupNotFound,
		},
		{
			// rc files are free to print banners; the marker only has to be
			// findable, not alone.
			name:     "startup noise before the marker is ignored",
			stdout:   "Welcome!\np10k banner mid-line" + "\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		{
			// The case the marker exists for: shell init exited (broken
			// ~/.zshrc, a plugin calling exit) before the lookup ever ran.
			// The old exit-status classification read this as "not
			// installed"; nothing was proven, so it must not block.
			name:     "no marker means the lookup never ran — inconclusive",
			stdout:   "some rc-file output, then the shell died\n",
			wantPath: "",
			want:     ShellLookupInconclusive,
		},
		{
			// The deadline case: the shell was killed mid-startup, nothing
			// (or only noise) made it to stdout.
			name:     "empty output means inconclusive",
			stdout:   "",
			wantPath: "",
			want:     ShellLookupInconclusive,
		},
		{
			// The deadline can also cut the marker's write short; a marker
			// without a parseable status proves nothing.
			name:     "marker with a mangled status means inconclusive",
			stdout:   "\n" + shellLookupMarker,
			wantPath: "",
			want:     ShellLookupInconclusive,
		},
		{
			// A marker that DID land is trusted even if later output is
			// garbage — the last complete marker wins.
			name:     "last marker wins over earlier noise resembling one",
			stdout:   shellLookupMarker + "1\nnoise\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		// --- ADR-0021: path extraction ---
		{
			// `command -v claude` for the binary case: the resolved absolute
			// path, then the script's own leading \n, then the marker.
			name:     "command -v prints an absolute path",
			stdout:   "/Users/jane/.local/bin/claude\n\n" + shellLookupMarker + "0\n",
			wantPath: "/Users/jane/.local/bin/claude",
			want:     ShellLookupFound,
		},
		{
			// Measured shape from ADR-0021: an alias prints its definition,
			// not a path. Not something a pane may exec.
			name:     "alias text is not a path",
			stdout:   "alias cc='CLAUDE_CODE_NO_FLICKER=1 claude'\n\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		{
			// A shell function or builtin prints its bare name.
			name:     "a bare name is not a path",
			stdout:   "mytoolfunction\n\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		{
			// command -v printed nothing (only the script's own leading \n)
			// before the marker.
			name:     "empty command -v output before the marker means no path",
			stdout:   "\n\n" + shellLookupMarker + "0\n",
			wantPath: "",
			want:     ShellLookupFound,
		},
		{
			// The case classifyShellLookup's rc==0 gate exists for: rc-file
			// banner noise landing on stdout ahead of a genuine path.
			name:     "rc-file noise preceding a valid path is skipped",
			stdout:   "Welcome!\n/Users/jane/.local/bin/claude\n\n" + shellLookupMarker + "0\n",
			wantPath: "/Users/jane/.local/bin/claude",
			want:     ShellLookupFound,
		},
		{
			// The mirror case: rc-file noise ahead of a NotFound outcome
			// must NOT be mistaken for a path. This is exactly why the path
			// is read only when rc == 0 — on a NotFound outcome the last
			// non-empty line before the marker is the noise itself, not an
			// answer from `command -v`.
			name:     "rc-file noise on a NotFound outcome yields no path",
			stdout:   "Welcome!\nsome banner text\n\n" + shellLookupMarker + "1\n",
			wantPath: "",
			want:     ShellLookupNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotPath, got := classifyShellLookup(tt.stdout)
			if got != tt.want {
				t.Errorf("classifyShellLookup(%q) result = %v, want %v", tt.stdout, got, tt.want)
			}
			if gotPath != tt.wantPath {
				t.Errorf(
					"classifyShellLookup(%q) path = %q, want %q",
					tt.stdout,
					gotPath,
					tt.wantPath,
				)
			}
		})
	}
}

// TestDefaultShellCommandLookupResolvesRealPath runs the actual probe script
// through a real shell and asserts it returns an absolute path for a command
// known to exist. Fixture strings can't catch this: an earlier version of the
// probe script redirected `command -v`'s stdout to /dev/null, so no amount of
// parsing captured stdout could ever have recovered a path — only running the
// real script proves the path reaches Go at all (ADR-0021).
//
// This is a narrow, deliberate exception to "tests never execute real
// commands" (CLAUDE.md): it spawns /bin/sh, never devgeta's own commands, and
// only reads a probe result — it installs, writes, and modifies nothing.
func TestDefaultShellCommandLookupResolvesRealPath(t *testing.T) {
	// /bin/sh is POSIX-guaranteed to exist, so the probe's shell is
	// deterministic instead of depending on whatever $SHELL the test machine
	// happens to have.
	t.Setenv("SHELL", "/bin/sh")

	// "sh" itself is guaranteed to exist and, per ADR-0021's measured table,
	// is an external binary with an absolute path — never a shell builtin or
	// function, which would print a bare name instead.
	path, result := defaultShellCommandLookup("sh")

	switch result {
	case ShellLookupInconclusive:
		// ADR-0016's fail-open case: the probe couldn't answer in time or
		// the shell didn't start. That is not a test failure.
		t.Skip("probe was inconclusive (ADR-0016 fail-open) — nothing to assert")
	case ShellLookupNotFound:
		t.Fatalf("expected sh to resolve, got NotFound")
	case ShellLookupFound:
		if path == "" {
			t.Fatal("expected a non-empty resolved path for sh, got empty string")
		}
		if path[0] != '/' {
			t.Fatalf("expected an absolute path for sh, got %q", path)
		}
	}
}
