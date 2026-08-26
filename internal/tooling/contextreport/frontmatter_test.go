package contextreport

import "testing"

func TestFrontmatterBytesExtractsTheDelimitedBlock(t *testing.T) {
	content := "---\nname: foo\ndescription: bar\n---\n\n# Body\n\nlots of body text here\n"
	got := frontmatterBytes(content)
	want := len("---\nname: foo\ndescription: bar\n---\n")
	if got != want {
		t.Errorf("frontmatterBytes = %d, want %d", got, want)
	}
}

func TestFrontmatterBytesIsZeroWithoutFrontmatter(t *testing.T) {
	content := "# Just a body\n\nNo frontmatter here at all.\n"
	if got := frontmatterBytes(content); got != 0 {
		t.Errorf("frontmatterBytes = %d, want 0", got)
	}
}

func TestFrontmatterBytesHandlesUnclosedDelimiter(t *testing.T) {
	// A "---" with no closing delimiter is not a frontmatter block —
	// nothing should be counted, and this must not hang or panic.
	content := "---\nname: foo\nno closing delimiter\n"
	if got := frontmatterBytes(content); got != 0 {
		t.Errorf("frontmatterBytes = %d, want 0 (no closing delimiter)", got)
	}
}

func TestFrontmatterHasPathsKeyDetectsAPathsKey(t *testing.T) {
	content := "---\npaths:\n  - \"src/**/*.ts\"\n---\n\nRule body.\n"
	if !frontmatterHasPathsKey(content) {
		t.Error("expected paths: key to be detected")
	}
}

func TestFrontmatterHasPathsKeyFalseWithoutIt(t *testing.T) {
	content := "---\nname: foo\n---\n\nRule body.\n"
	if frontmatterHasPathsKey(content) {
		t.Error("expected no paths: key to be detected")
	}
}

func TestExtractImportsFindsABareReference(t *testing.T) {
	content := "See @docs/git-instructions.md for the workflow.\n"
	got := extractImports(content)
	want := []string{"docs/git-instructions.md"}
	assertStringSlicesEqual(t, got, want)
}

func TestExtractImportsIgnoresBacktickEscapedReferences(t *testing.T) {
	content := "Mentioning `@README` here should not import it.\n"
	got := extractImports(content)
	if len(got) != 0 {
		t.Errorf("extractImports = %v, want none (backtick-escaped)", got)
	}
}

func TestExtractImportsIgnoresFencedCodeBlocks(t *testing.T) {
	content := "Text before.\n```\n@should/not/import.md\n```\nText after @really/import.md here.\n"
	got := extractImports(content)
	want := []string{"really/import.md"}
	assertStringSlicesEqual(t, got, want)
}

func TestExtractImportsFindsMultipleOnOneLine(t *testing.T) {
	content := "See @README and @package.json for details.\n"
	got := extractImports(content)
	want := []string{"README", "package.json"}
	assertStringSlicesEqual(t, got, want)
}

func TestExtractImportsFindsAHomeRelativeReference(t *testing.T) {
	content := "- @~/.claude/my-project-instructions.md\n"
	got := extractImports(content)
	want := []string{"~/.claude/my-project-instructions.md"}
	assertStringSlicesEqual(t, got, want)
}

func TestResolveImportPathHandlesAbsoluteRelativeAndHome(t *testing.T) {
	home := "/home/user"
	fromDir := "/repo/sub"
	cases := []struct {
		ref  string
		want string
	}{
		{"README.md", "/repo/sub/README.md"},
		{"../TOP.md", "/repo/TOP.md"},
		{"/abs/path.md", "/abs/path.md"},
		{"~/.claude/x.md", "/home/user/.claude/x.md"},
	}
	for _, tc := range cases {
		got := resolveImportPath(tc.ref, fromDir, home)
		if got != tc.want {
			t.Errorf(
				"resolveImportPath(%q, %q, %q) = %q, want %q",
				tc.ref,
				fromDir,
				home,
				got,
				tc.want,
			)
		}
	}
}

func assertStringSlicesEqual(t *testing.T, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}
