package commands

import "testing"

// classifyShellLookup is the pure half of the interactive-shell probe: it
// decides Found/NotFound/Inconclusive from the captured stdout alone, with
// the marker line as the only accepted proof that `command -v` actually ran
// (ADR-0016). Testing it directly needs no shell — these strings are exactly
// what defaultShellCommandLookup captures.
func TestClassifyShellLookup(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   ShellLookupResult
	}{
		{
			name:   "marker with status 0 means found",
			stdout: "\n" + shellLookupMarker + "0\n",
			want:   ShellLookupFound,
		},
		{
			name:   "marker with non-zero status means not found",
			stdout: "\n" + shellLookupMarker + "1\n",
			want:   ShellLookupNotFound,
		},
		{
			// rc files are free to print banners; the marker only has to be
			// findable, not alone.
			name:   "startup noise before the marker is ignored",
			stdout: "Welcome!\np10k banner mid-line" + "\n" + shellLookupMarker + "0\n",
			want:   ShellLookupFound,
		},
		{
			// The case the marker exists for: shell init exited (broken
			// ~/.zshrc, a plugin calling exit) before the lookup ever ran.
			// The old exit-status classification read this as "not
			// installed"; nothing was proven, so it must not block.
			name:   "no marker means the lookup never ran — inconclusive",
			stdout: "some rc-file output, then the shell died\n",
			want:   ShellLookupInconclusive,
		},
		{
			// The deadline case: the shell was killed mid-startup, nothing
			// (or only noise) made it to stdout.
			name:   "empty output means inconclusive",
			stdout: "",
			want:   ShellLookupInconclusive,
		},
		{
			// The deadline can also cut the marker's write short; a marker
			// without a parseable status proves nothing.
			name:   "marker with a mangled status means inconclusive",
			stdout: "\n" + shellLookupMarker,
			want:   ShellLookupInconclusive,
		},
		{
			// A marker that DID land is trusted even if later output is
			// garbage — the last complete marker wins.
			name:   "last marker wins over earlier noise resembling one",
			stdout: shellLookupMarker + "1\nnoise\n" + shellLookupMarker + "0\n",
			want:   ShellLookupFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyShellLookup(tt.stdout); got != tt.want {
				t.Errorf("classifyShellLookup(%q) = %v, want %v", tt.stdout, got, tt.want)
			}
		})
	}
}
