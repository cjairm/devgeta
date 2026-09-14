package archive

import "testing"

func TestWindowsIncompatibleReasonReservedChars(t *testing.T) {
	cases := []string{
		"notes:draft.txt", "a<b.txt", "a>b.txt", `a"b.txt`,
		"a?b.txt", "a*b.txt", "a|b.txt",
	}
	for _, name := range cases {
		if reason := WindowsIncompatibleReason(name); reason == "" {
			t.Errorf("WindowsIncompatibleReason(%q) = \"\", want a reason", name)
		}
	}
}

func TestWindowsIncompatibleReasonTrailingDotOrSpace(t *testing.T) {
	cases := []string{"trailing dot.", "trailing space "}
	for _, name := range cases {
		if reason := WindowsIncompatibleReason(name); reason == "" {
			t.Errorf("WindowsIncompatibleReason(%q) = \"\", want a reason", name)
		}
	}
}

func TestWindowsIncompatibleReasonReservedNames(t *testing.T) {
	cases := []string{"CON", "con", "NUL", "COM1", "LPT9", "CON.txt", "aux.log"}
	for _, name := range cases {
		if reason := WindowsIncompatibleReason(name); reason == "" {
			t.Errorf("WindowsIncompatibleReason(%q) = \"\", want a reason", name)
		}
	}
}

func TestWindowsIncompatibleReasonReservedNameRequiresExactMatch(t *testing.T) {
	// "console" is not the reserved name "CON" — only an exact base name (with
	// or without extension) counts.
	cases := []string{"console", "console.txt", "CONTACT.txt"}
	for _, name := range cases {
		if reason := WindowsIncompatibleReason(name); reason != "" {
			t.Errorf("WindowsIncompatibleReason(%q) = %q, want no reason", name, reason)
		}
	}
}

func TestWindowsIncompatibleReasonOrdinaryNamesAreFine(t *testing.T) {
	cases := []string{"readme.txt", "my-notes_v2.md", "a.b.c.txt", "unicode-café.txt"}
	for _, name := range cases {
		if reason := WindowsIncompatibleReason(name); reason != "" {
			t.Errorf("WindowsIncompatibleReason(%q) = %q, want no reason", name, reason)
		}
	}
}

func TestCaseInsensitiveCollisionsFindsGroups(t *testing.T) {
	names := []string{"Readme.txt", "readme.txt", "NOTES.md", "other.txt"}
	collisions := CaseInsensitiveCollisions(names)
	if len(collisions) != 1 {
		t.Fatalf("got %d collision groups, want 1: %v", len(collisions), collisions)
	}
	group := collisions[0]
	if len(group) != 2 {
		t.Fatalf("collision group = %v, want 2 entries", group)
	}
	found := map[string]bool{}
	for _, n := range group {
		found[n] = true
	}
	if !found["Readme.txt"] || !found["readme.txt"] {
		t.Errorf("collision group = %v, want Readme.txt and readme.txt", group)
	}
}

func TestCaseInsensitiveCollisionsNoneWhenAllDistinct(t *testing.T) {
	names := []string{"a.txt", "b.txt", "c.txt"}
	if collisions := CaseInsensitiveCollisions(names); len(collisions) != 0 {
		t.Fatalf("got %d collision groups, want 0: %v", len(collisions), collisions)
	}
}
