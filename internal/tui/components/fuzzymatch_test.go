package tuicomponents

import "testing"

func TestFuzzyMatch(t *testing.T) {
	t.Run("exact prefix ranks highest", func(t *testing.T) {
		if got := FuzzyMatch("dev", "devgeta"); got != FuzzyExactPrefix {
			t.Errorf("expected FuzzyExactPrefix, got %v", got)
		}
	})

	t.Run("substring ranks below prefix", func(t *testing.T) {
		if got := FuzzyMatch("get", "devgeta"); got != FuzzySubstring {
			t.Errorf("expected FuzzySubstring, got %v", got)
		}
	})

	t.Run("bare subsequence ranks below substring", func(t *testing.T) {
		if got := FuzzyMatch("dvg", "devgeta"); got != FuzzySubsequence {
			t.Errorf("expected FuzzySubsequence, got %v", got)
		}
	})

	t.Run("ranking order is exact prefix > substring > subsequence", func(t *testing.T) {
		if !(FuzzyExactPrefix > FuzzySubstring && FuzzySubstring > FuzzySubsequence && FuzzySubsequence > FuzzyNoMatch) {
			t.Error("expected FuzzyExactPrefix > FuzzySubstring > FuzzySubsequence > FuzzyNoMatch")
		}
	})

	t.Run("non-subsequence does not match", func(t *testing.T) {
		if got := FuzzyMatch("xyz", "devgeta"); got != FuzzyNoMatch {
			t.Errorf("expected FuzzyNoMatch, got %v", got)
		}
	})

	t.Run("out-of-order characters do not match", func(t *testing.T) {
		if got := FuzzyMatch("tgd", "devgeta"); got != FuzzyNoMatch {
			t.Errorf("expected FuzzyNoMatch, got %v", got)
		}
	})

	t.Run("case-insensitive prefix", func(t *testing.T) {
		if got := FuzzyMatch("DEV", "devgeta"); got != FuzzyExactPrefix {
			t.Errorf("expected FuzzyExactPrefix, got %v", got)
		}
	})

	t.Run("case-insensitive substring", func(t *testing.T) {
		if got := FuzzyMatch("GET", "devgeta"); got != FuzzySubstring {
			t.Errorf("expected FuzzySubstring, got %v", got)
		}
	})

	t.Run("case-insensitive subsequence", func(t *testing.T) {
		if got := FuzzyMatch("DVG", "devgeta"); got != FuzzySubsequence {
			t.Errorf("expected FuzzySubsequence, got %v", got)
		}
	})

	t.Run("empty query matches everything at the weakest rank", func(t *testing.T) {
		if got := FuzzyMatch("", "anything"); got != FuzzySubsequence {
			t.Errorf("expected FuzzySubsequence, got %v", got)
		}
	})

	t.Run("query longer than candidate does not match", func(t *testing.T) {
		if got := FuzzyMatch("devgeta-worktree", "dev"); got != FuzzyNoMatch {
			t.Errorf("expected FuzzyNoMatch, got %v", got)
		}
	})
}
