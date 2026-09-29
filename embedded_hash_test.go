package main

import (
	"regexp"
	"testing"
)

// The fingerprint names the extracted config tree, so it must be stable for
// the same embedded configs and fit the directory-name character set.
func TestConfigsContentHash_StableAndShort(t *testing.T) {
	first := configsContentHash()
	if !regexp.MustCompile(`^[0-9a-f]{12}$`).MatchString(first) {
		t.Fatalf("configsContentHash() = %q, want 12 hex characters", first)
	}
	if again := configsContentHash(); again != first {
		t.Errorf("configsContentHash() changed between calls: %q then %q", first, again)
	}
}
