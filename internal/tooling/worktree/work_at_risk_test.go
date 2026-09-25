package worktree

// Tests for ADR-0053: a removal without force refuses when it would lose work,
// and an unanswered check refuses too.

import (
	"errors"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/git"
	"github.com/cjairm/devgeta/internal/commands"
)

// riskWM builds a manager whose git answers each command in turn, as set by
// script on its mock base.
func riskWM(script func(*commands.MockBaseCommand)) *WorktreeManager {
	base := commands.NewMockBaseCommand()
	script(base)
	return &WorktreeManager{Git: &git.Git{Cmd: commands.NewMockCommand(), Base: base}}
}

func TestWorkAtRisk(t *testing.T) {
	t.Run("reports uncommitted changes and unpushed commits", func(t *testing.T) {
		wm := riskWM(func(b *commands.MockBaseCommand) {
			b.SetExecCommandResults(
				commands.ExecCommandResult(" M f.go", "", nil),     // status --porcelain
				commands.ExecCommandResult("origin/main", "", nil), // default branch
				commands.ExecCommandResult("3", "", nil),           // rev-list --count
			)
		})
		risk, err := wm.workAtRisk("/wt")
		if err != nil {
			t.Fatal(err)
		}
		if !risk.Dirty || risk.Unpushed != 3 {
			t.Errorf("expected dirty with 3 unpushed, got %+v", risk)
		}
		if got := risk.String(); got != "uncommitted changes and 3 unpushed commits" {
			t.Errorf("unexpected description %q", got)
		}
	})

	t.Run("a clean, pushed worktree has nothing at risk", func(t *testing.T) {
		wm := riskWM(func(b *commands.MockBaseCommand) {
			b.SetExecCommandResults(
				commands.ExecCommandResult("", "", nil),
				commands.ExecCommandResult("origin/main", "", nil),
				commands.ExecCommandResult("0", "", nil),
			)
		})
		risk, err := wm.workAtRisk("/wt")
		if err != nil || risk.Any() {
			t.Errorf("expected nothing at risk, got %+v, %v", risk, err)
		}
	})

	t.Run("a failed check is an error, not a clean answer", func(t *testing.T) {
		for name, wm := range map[string]*WorktreeManager{
			"status fails": riskWM(func(b *commands.MockBaseCommand) {
				b.SetExecCommandResults(commands.ExecCommandResult("", "", errors.New("boom")))
			}),
			"rev-list fails": riskWM(func(b *commands.MockBaseCommand) {
				b.SetExecCommandResults(
					commands.ExecCommandResult("", "", nil),
					commands.ExecCommandResult("origin/main", "", nil),
					commands.ExecCommandResult("", "", errors.New("boom")),
				)
			}),
		} {
			if _, err := wm.workAtRisk("/wt"); err == nil {
				t.Errorf("%s: expected an error", name)
			}
		}
	})
}

func TestWouldLoseWorkErrorNamesTheLoss(t *testing.T) {
	err := error(&WouldLoseWorkError{Name: "feat", Risk: WorkAtRisk{Unpushed: 1}})
	var target *WouldLoseWorkError
	if !errors.As(err, &target) {
		t.Fatal("expected errors.As to find WouldLoseWorkError")
	}
	if msg := err.Error(); !strings.Contains(msg, "1 unpushed commit") ||
		!strings.Contains(msg, "--force") {
		t.Errorf("message should name the loss and the way out, got %q", msg)
	}
}
