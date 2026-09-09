package desktop

import (
	"errors"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
	"github.com/cjairm/devgeta/pkg/constants"
)

func init() { testutil.InitLogger() }

func TestAptPolicyHasCandidate(t *testing.T) {
	cases := []struct {
		name   string
		output string
		want   bool
	}{
		{
			name: "present package",
			output: `alacritty:
  Installed: (none)
  Candidate: 0.12.2-1
  Version table:
     0.12.2-1 500
        500 http://deb.debian.org/debian bookworm/main amd64 Packages
`,
			want: true,
		},
		{
			name: "candidate none",
			output: `shottr:
  Installed: (none)
  Candidate: (none)
  Version table:
`,
			want: false,
		},
		{
			name:   "empty output (unknown package)",
			output: "",
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := aptPolicyHasCandidate(tc.output)
			if got != tc.want {
				t.Errorf("aptPolicyHasCandidate(%q) = %v, want %v", tc.output, got, tc.want)
			}
		})
	}
}

func TestAptCandidateAvailable(t *testing.T) {
	t.Run("reports available when apt-cache finds a candidate", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("Candidate: 1.2.3-1", "", nil)

		if !aptCandidateAvailable(mockApp.Base, constants.Ghostty) {
			t.Error("expected aptCandidateAvailable to report true")
		}
		lastCall := mockApp.Base.GetLastExecCommandCall()
		if lastCall == nil || lastCall.Command != "apt-cache" {
			t.Fatalf("expected apt-cache to be invoked, got %+v", lastCall)
		}
	})

	t.Run("reports unavailable when the command errors", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("", "", errors.New("apt-cache: command not found"))

		if aptCandidateAvailable(mockApp.Base, constants.Shottr) {
			t.Error("expected aptCandidateAvailable to report false on error")
		}
	})
}

func TestPlatformAvailable(t *testing.T) {
	t.Run("mac uses the static map, never touches the command executor", func(t *testing.T) {
		mockApp := testutil.NewMockApp()

		if platformAvailable(mockApp.Base, true, constants.Alacritty) {
			t.Error("expected alacritty unavailable on macOS (Gatekeeper-disabled cask)")
		}
		if !platformAvailable(mockApp.Base, true, constants.Ghostty) {
			t.Error("expected ghostty available on macOS")
		}
		testutil.VerifyNoRealCommands(t, mockApp.Base)
	})

	t.Run("linux probes apt-cache", func(t *testing.T) {
		mockApp := testutil.NewMockApp()
		mockApp.Base.SetExecCommandResult("Candidate: 0.12.2-1", "", nil)

		if !platformAvailable(mockApp.Base, false, constants.Alacritty) {
			t.Error("expected alacritty available on Linux per the mocked apt-cache output")
		}
	})
}

func TestChooseOne(t *testing.T) {
	t.Run("no candidates available: warns, returns not-found", func(t *testing.T) {
		candidates := []candidate{
			{name: "alacritty", available: func() bool { return false }},
			{name: "ghostty", available: func() bool { return false }},
		}
		selectCalled := false
		selectFn := func(label string, options []string) (string, error) {
			selectCalled = true
			return "", nil
		}

		name, ok := chooseOne("terminal", candidates, selectFn)
		if ok {
			t.Errorf("expected ok=false, got name=%q", name)
		}
		if selectCalled {
			t.Error("expected selectFn not to be called when nothing is available")
		}
	})

	t.Run("exactly one candidate available: returns it silently", func(t *testing.T) {
		candidates := []candidate{
			{name: "alacritty", available: func() bool { return false }},
			{name: "ghostty", available: func() bool { return true }},
		}
		selectCalled := false
		selectFn := func(label string, options []string) (string, error) {
			selectCalled = true
			return "", nil
		}

		name, ok := chooseOne("terminal", candidates, selectFn)
		if !ok || name != "ghostty" {
			t.Errorf("expected (ghostty, true), got (%q, %v)", name, ok)
		}
		if selectCalled {
			t.Error("expected selectFn not to be called when exactly one candidate is available")
		}
	})

	t.Run("more than one candidate available: prompts and returns the choice", func(t *testing.T) {
		candidates := []candidate{
			{name: "alacritty", available: func() bool { return true }},
			{name: "ghostty", available: func() bool { return true }},
		}
		var gotLabel string
		var gotOptions []string
		selectFn := func(label string, options []string) (string, error) {
			gotLabel = label
			gotOptions = options
			return "alacritty", nil
		}

		name, ok := chooseOne("terminal", candidates, selectFn)
		if !ok || name != "alacritty" {
			t.Errorf("expected (alacritty, true), got (%q, %v)", name, ok)
		}
		if gotLabel != "terminal" {
			t.Errorf("expected label %q passed through, got %q", "terminal", gotLabel)
		}
		if len(gotOptions) != 2 {
			t.Errorf("expected 2 options passed to selectFn, got %v", gotOptions)
		}
	})

	t.Run("selection error: warns, returns not-found", func(t *testing.T) {
		candidates := []candidate{
			{name: "alacritty", available: func() bool { return true }},
			{name: "ghostty", available: func() bool { return true }},
		}
		selectFn := func(label string, options []string) (string, error) {
			return "", errors.New("interrupted")
		}

		name, ok := chooseOne("terminal", candidates, selectFn)
		if ok {
			t.Errorf("expected ok=false on selection error, got name=%q", name)
		}
	})
}
