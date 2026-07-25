package jq

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/logger"
)

func init() { logger.Init(false) }

func TestNew(t *testing.T) {
	app := New()
	if app == nil {
		t.Fatal("New() returned nil")
	}
}

func TestInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Jq{Cmd: mc}

	if err := app.Install(); err != nil {
		t.Fatalf("Install error: %v", err)
	}
	if mc.InstalledPkg != "jq" {
		t.Fatalf("expected InstallPackage(jq), got %q", mc.InstalledPkg)
	}
}

func TestSoftInstall(t *testing.T) {
	mc := commands.NewMockCommand()
	app := &Jq{Cmd: mc}

	if err := app.SoftInstall(); err != nil {
		t.Fatalf("SoftInstall error: %v", err)
	}
	if mc.MaybeInstalled != "jq" {
		t.Fatalf("expected MaybeInstallPackage(jq), got %q", mc.MaybeInstalled)
	}
}

func TestForceConfigure(t *testing.T) {
	app := &Jq{Cmd: commands.NewMockCommand()}
	if err := app.ForceConfigure(); err != nil {
		t.Fatalf("ForceConfigure error: %v", err)
	}
}

func TestSoftConfigure(t *testing.T) {
	app := &Jq{Cmd: commands.NewMockCommand()}
	if err := app.SoftConfigure(); err != nil {
		t.Fatalf("SoftConfigure error: %v", err)
	}
}

func TestExecuteCommand(t *testing.T) {
	mc := commands.NewMockCommand()
	mockBase := commands.NewMockBaseCommand()
	app := &Jq{Cmd: mc, Base: mockBase}

	t.Run("successful execution", func(t *testing.T) {
		mockBase.SetExecCommandResult(`{"key":"value"}`, "", nil)

		if err := app.ExecuteCommand(".", "file.json"); err != nil {
			t.Fatalf("ExecuteCommand failed: %v", err)
		}
		if mockBase.GetExecCommandCallCount() != 1 {
			t.Fatalf("expected 1 ExecCommand call, got %d", mockBase.GetExecCommandCallCount())
		}
		lastCall := mockBase.GetLastExecCommandCall()
		if lastCall.Command != "jq" {
			t.Fatalf("expected command 'jq', got %q", lastCall.Command)
		}
		if len(lastCall.Args) != 2 || lastCall.Args[0] != "." || lastCall.Args[1] != "file.json" {
			t.Fatalf("unexpected args: %v", lastCall.Args)
		}
		if lastCall.IsSudo {
			t.Fatal("expected IsSudo to be false")
		}
	})

	t.Run("command execution error", func(t *testing.T) {
		mockBase.ResetExecCommand()
		mockBase.SetExecCommandResult("", "parse error", fmt.Errorf("exit 2"))

		err := app.ExecuteCommand(".invalid")
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed to run jq command") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}

func boolPtr(b bool) *bool { return &b }

func TestFormatReviewThreads(t *testing.T) {
	const sampleJSON = `{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[]}}}}}`

	// assertArgSeq verifies that wantSeq appears as a contiguous subsequence of args.
	assertArgSeq := func(t *testing.T, args []string, wantSeq ...string) {
		t.Helper()
		for i := 0; i+len(wantSeq) <= len(args); i++ {
			match := true
			for j, w := range wantSeq {
				if args[i+j] != w {
					match = false
					break
				}
			}
			if match {
				return
			}
		}
		t.Fatalf("expected args to contain sequence %v, got %v", wantSeq, args)
	}

	t.Run("unresolved passes resolved=false", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("## Location\nFile: `a.go`", "", nil)

		out, err := app.FormatReviewThreads(sampleJSON, boolPtr(false))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "## Location\nFile: `a.go`" {
			t.Fatalf("unexpected output: %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call == nil {
			t.Fatal("no ExecCommand call recorded")
		}
		if call.Command != "jq" {
			t.Fatalf("expected command 'jq', got %q", call.Command)
		}
		if call.Args[0] != "-r" {
			t.Fatalf("expected first arg '-r', got %q", call.Args[0])
		}
		assertArgSeq(t, call.Args, "--argjson", "resolved", "false")

		// The filter must be present and the JSON must be passed as a temp file.
		if !strings.Contains(call.Args[4], "reviewThreads") {
			t.Fatalf("expected jq filter in args, got %q", call.Args[4])
		}
		last := call.Args[len(call.Args)-1]
		if !strings.HasSuffix(last, ".json") {
			t.Fatalf("expected last arg to be a .json temp file, got %q", last)
		}
	})

	t.Run("resolved passes resolved=true", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if _, err := app.FormatReviewThreads(sampleJSON, boolPtr(true)); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertArgSeq(t, mockBase.GetLastExecCommandCall().Args, "--argjson", "resolved", "true")
	})

	t.Run("nil resolved passes null", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if _, err := app.FormatReviewThreads(sampleJSON, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		assertArgSeq(t, mockBase.GetLastExecCommandCall().Args, "--argjson", "resolved", "null")
	})

	t.Run("surfaces jq stderr on failure", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "jq: error: syntax", fmt.Errorf("exit 3"))

		_, err := app.FormatReviewThreads(sampleJSON, boolPtr(false))
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "jq: error: syntax") {
			t.Fatalf("expected stderr in error, got: %v", err)
		}
	})

	t.Run("wraps error when stderr empty", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", fmt.Errorf("jq not found"))

		_, err := app.FormatReviewThreads(sampleJSON, nil)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "failed to run filter") {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("filter includes resolvedBy suffix and comment createdAt", func(t *testing.T) {
		// Structural check (matching the "null path degrades..." test below):
		// the jq subprocess is mocked, so this verifies the filter constant
		// itself carries the resolved-by header suffix and the per-comment
		// timestamp rather than the old unsuffixed/untimestamped format.
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if _, err := app.FormatReviewThreads(sampleJSON, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(mockBase.GetLastExecCommandCall().Args, " ")
		for _, want := range []string{
			`if .resolvedBy.login then " — resolved by`,
			`.createdAt // "?"`,
		} {
			if !strings.Contains(joined, want) {
				t.Fatalf("expected filter to contain %q, got: %v", want, joined)
			}
		}
	})

	t.Run("filter marks outdated threads via if/then/else, not // empty", func(t *testing.T) {
		// Structural regression guard: `.isOutdated // empty` would collapse
		// the whole "+" concatenation chain to no output (empty is a
		// zero-value generator), silently dropping the thread line. The
		// filter must use an if/then/else that always yields exactly one
		// value instead.
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if _, err := app.FormatReviewThreads(sampleJSON, nil); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(mockBase.GetLastExecCommandCall().Args, " ")
		if !strings.Contains(joined, `if .isOutdated then " (outdated)"`) {
			t.Fatalf("expected filter to contain outdated marker construct, got: %v", joined)
		}
		if strings.Contains(joined, `.isOutdated // empty`) {
			t.Fatalf(
				"filter must not use '.isOutdated // empty' (collapses the whole + chain), got: %v",
				joined,
			)
		}
	})

	t.Run("null path degrades to empty instead of erroring", func(t *testing.T) {
		// This exercises the filter's null-guard: the jq subprocess call is
		// mocked (per repo policy), so this verifies the filter string itself
		// contains the guard rather than the raw unguarded selector, and that
		// a missing/null nodes path is handled by the caller as empty output
		// (never an error) once fed through the mocked jq call.
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		// Simulate what jq itself does for a null path once null-guarded: it
		// exits 0 with empty stdout, never an error.
		mockBase.SetExecCommandResult("", "", nil)

		out, err := app.FormatReviewThreads(`{"data":{"repository":{}}}`, nil)
		if err != nil {
			t.Fatalf("expected no error for missing/null path, got: %v", err)
		}
		if out != "" {
			t.Fatalf("expected empty output, got %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, `reviewThreads.nodes // []`) {
			t.Fatalf("expected null-guarded node selector in filter, got: %v", call.Args)
		}
	})
}

func TestFormatPRDiscussion(t *testing.T) {
	const sampleJSON = `{"data":{"repository":{"pullRequest":{"reviews":{"nodes":[]},"comments":{"nodes":[]}}}}}`

	t.Run("both sections rendered", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		want := "## Review summaries\n\n**alice** [APPROVED] (2026-01-02T10:00:00Z): Looks good\n\n" +
			"## Conversation\n\n**dave** (2026-01-03T09:00:00Z): Thanks for the PR"
		mockBase.SetExecCommandResult(want, "", nil)

		out, err := app.FormatPRDiscussion(sampleJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
		if !strings.Contains(out, "2026-01-02T10:00:00Z") {
			t.Fatalf("expected review submittedAt in output, got %q", out)
		}
		if !strings.Contains(out, "2026-01-03T09:00:00Z") {
			t.Fatalf("expected comment createdAt in output, got %q", out)
		}

		call := mockBase.GetLastExecCommandCall()
		if call.Command != "jq" {
			t.Fatalf("expected command 'jq', got %q", call.Command)
		}
		if call.Args[0] != "-r" {
			t.Fatalf("expected first arg '-r', got %q", call.Args[0])
		}
		joined := strings.Join(call.Args, " ")
		if !strings.Contains(joined, "Review summaries") {
			t.Fatalf("expected review summaries header in filter, got: %v", call.Args)
		}
		if !strings.Contains(joined, "Conversation") {
			t.Fatalf("expected conversation header in filter, got: %v", call.Args)
		}
		last := call.Args[len(call.Args)-1]
		if !strings.HasSuffix(last, ".json") {
			t.Fatalf("expected last arg to be a .json temp file, got %q", last)
		}
	})

	t.Run("filter guards empty-body reviews and missing fields", func(t *testing.T) {
		// The filter itself is exercised directly with the real jq binary in a
		// separate manual verification step (jq calls in this test file are
		// always mocked, matching the rest of this package). Here we assert the
		// filter constant carries the guards the contract requires so a
		// regression that removes one is caught structurally.
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		if _, err := app.FormatPRDiscussion(sampleJSON); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		joined := strings.Join(mockBase.GetLastExecCommandCall().Args, " ")
		for _, guard := range []string{
			`reviews.nodes // []`,
			`comments.nodes // []`,
			`test("\\S")`, // skips empty/blank review bodies
			`.author.login // "unknown"`,
			`.state // "?"`,
			`.body // ""`,
			`.submittedAt // "?"`,
			`.createdAt // "?"`,
		} {
			if !strings.Contains(joined, guard) {
				t.Fatalf("expected filter to contain guard %q, got: %v", guard, joined)
			}
		}
	})

	t.Run("only reviews (no conversation comments)", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		want := "## Review summaries\n\n**alice** [APPROVED]: LGTM"
		mockBase.SetExecCommandResult(want, "", nil)

		out, err := app.FormatPRDiscussion(sampleJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("only conversation (no reviews with a body)", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		want := "## Conversation\n\n**dave**: hi"
		mockBase.SetExecCommandResult(want, "", nil)

		out, err := app.FormatPRDiscussion(sampleJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != want {
			t.Fatalf("unexpected output: %q", out)
		}
	})

	t.Run("nothing to render yields empty output", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "", nil)

		out, err := app.FormatPRDiscussion(sampleJSON)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if out != "" {
			t.Fatalf("expected empty output, got %q", out)
		}
	})

	t.Run("surfaces jq stderr on failure", func(t *testing.T) {
		mockBase := commands.NewMockBaseCommand()
		app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
		mockBase.SetExecCommandResult("", "jq: error: syntax", fmt.Errorf("exit 3"))

		_, err := app.FormatPRDiscussion(sampleJSON)
		if err == nil {
			t.Fatal("expected error")
		}
		if !strings.Contains(err.Error(), "jq: error: syntax") {
			t.Fatalf("expected stderr in error, got: %v", err)
		}
	})
}

func TestFormatPRView(t *testing.T) {
	mockBase := commands.NewMockBaseCommand()
	app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
	mockBase.SetExecCommandResult("PR #42: Title\nstate: OPEN", "", nil)

	out, err := app.FormatPRView(`{"number":42}`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "PR #42: Title\nstate: OPEN" {
		t.Fatalf("unexpected output: %q", out)
	}
	call := mockBase.GetLastExecCommandCall()
	if call.Command != "jq" || call.Args[0] != "-r" {
		t.Fatalf("expected jq -r, got %v / %v", call.Command, call.Args)
	}
	if !strings.Contains(call.Args[1], "PR #") {
		t.Fatalf("expected prViewFilter, got %q", call.Args[1])
	}
	if !strings.HasSuffix(call.Args[len(call.Args)-1], ".json") {
		t.Fatalf("expected temp .json file last, got %q", call.Args[len(call.Args)-1])
	}
}

func TestFormatPRChecks(t *testing.T) {
	mockBase := commands.NewMockBaseCommand()
	app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
	mockBase.SetExecCommandResult("SUCCESS\tbuild", "", nil)

	out, err := app.FormatPRChecks(`[{"name":"build","state":"SUCCESS"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if out != "SUCCESS\tbuild" {
		t.Fatalf("unexpected output: %q", out)
	}
	call := mockBase.GetLastExecCommandCall()
	if !strings.Contains(call.Args[1], "No checks") {
		t.Fatalf("expected prChecksFilter, got %q", call.Args[1])
	}
}

func TestFormatErrorSurfacing(t *testing.T) {
	mockBase := commands.NewMockBaseCommand()
	app := &Jq{Cmd: commands.NewMockCommand(), Base: mockBase}
	mockBase.SetExecCommandResult("", "jq: syntax error", fmt.Errorf("exit 3"))

	if _, err := app.FormatPRView("{}"); err == nil {
		t.Fatal("expected error")
	} else if !strings.Contains(err.Error(), "jq: syntax error") {
		t.Fatalf("expected stderr surfaced, got: %v", err)
	}
}
