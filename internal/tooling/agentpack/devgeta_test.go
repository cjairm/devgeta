package agentpack

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// devgetaTree is a built devgeta pack with the given DEVGETA.md.
func (r *claudeRig) devgetaTree(t *testing.T, text string) Tree {
	t.Helper()
	dir := filepath.Join(r.root, "devgeta")
	writeFile(t, filepath.Join(dir, "DEVGETA.md"), text, 0o644)
	writeFile(t, filepath.Join(dir, "package.json"), `{"name":"devgeta","pi":{}}`, 0o644)
	return Tree{Pack: embeddedManifest(), Dir: dir}
}

func (r *claudeRig) claudeMD() string { return filepath.Join(r.configDir, "CLAUDE.md") }

func (r *claudeRig) instructions() string { return filepath.Join(r.configDir, "DEVGETA.md") }

const devgetaText = "# Response style\n\n- Lead with the answer.\n"

func TestDevgetaImportsLine(t *testing.T) {
	yes := []string{
		"@DEVGETA.md",
		"  @DEVGETA.md  ",
		"@./DEVGETA.md",
		"@~/.claude/DEVGETA.md",
		"See @DEVGETA.md for the style rules.",
		"Follow (@DEVGETA.md).",
	}
	for _, line := range yes {
		if !importsDevgeta(line) {
			t.Errorf("%q imports DEVGETA.md", line)
		}
	}
	no := []string{
		"",
		"DEVGETA.md",
		"@DEVGETA.md.bak",
		"@OTHER/DEVGETA.md",
		"@other-DEVGETA.md",
		"email@DEVGETA.md",
		"# Notes",
	}
	for _, line := range no {
		if importsDevgeta(line) {
			t.Errorf("%q does not import DEVGETA.md", line)
		}
	}
}

func TestClaudeInstaller_InstallingTheDevgetaPackCopiesTheFileAndAddsTheLine(t *testing.T) {
	r := newClaudeRig(t)
	tree := r.devgetaTree(t, devgetaText)

	rec, err := r.in.Install(tree)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := readFile(t, r.instructions()); got != devgetaText {
		t.Errorf("DEVGETA.md must be a byte copy of the pack's, got %q", got)
	}
	if got := readFile(t, r.claudeMD()); got != "@DEVGETA.md\n" {
		t.Errorf("a new CLAUDE.md should hold just the import, got %q", got)
	}
	if rec.InstructionsFile != r.instructions() || !rec.ClaudeMDLineAdded || rec.Plugin != "" {
		t.Errorf("unexpected record %+v", rec)
	}
	if r.f.called("plugin") != 0 {
		t.Errorf("the devgeta pack is not a Claude plugin, so no claude call: %v", r.f.calls)
	}
	if exists(filepath.Join(r.root, ".claude-plugin")) {
		t.Error("no marketplace file for a pack that is not a plugin")
	}
}

func TestClaudeInstaller_TheLineIsAppendedAfterWhatTheUserHas(t *testing.T) {
	cases := map[string]string{
		"a file ending in a newline":    "# My rules\n\n- be nice\n",
		"a file with no final newline":  "# My rules\n\n- be nice",
		"a file that is only a comment": "<!-- empty for now -->\n",
	}
	for name, existing := range cases {
		t.Run(name, func(t *testing.T) {
			r := newClaudeRig(t)
			writeFile(t, r.claudeMD(), existing, 0o640)

			rec, err := r.in.Install(r.devgetaTree(t, devgetaText))
			if err != nil {
				t.Fatal(err)
			}
			want := strings.TrimSuffix(existing, "\n") + "\n@DEVGETA.md\n"
			if got := readFile(t, r.claudeMD()); got != want {
				t.Errorf("CLAUDE.md:\n got %q\nwant %q", got, want)
			}
			if !rec.ClaudeMDLineAdded {
				t.Error("devgeta added the line")
			}
			info, err := os.Stat(r.claudeMD())
			if err != nil || info.Mode().Perm() != 0o640 {
				t.Errorf("the user's file mode must be kept, got %v (%v)", info.Mode().Perm(), err)
			}
		})
	}
}

func TestClaudeInstaller_AnExistingImportIsLeftAlone(t *testing.T) {
	for _, existing := range []string{
		"# Mine\n@DEVGETA.md\n",
		"# Mine\n@./DEVGETA.md\nmore\n",
		"# Mine\nSee @~/.claude/DEVGETA.md for style.\n",
	} {
		r := newClaudeRig(t)
		writeFile(t, r.claudeMD(), existing, 0o644)

		rec, err := r.in.Install(r.devgetaTree(t, devgetaText))
		if err != nil {
			t.Fatal(err)
		}
		if got := readFile(t, r.claudeMD()); got != existing {
			t.Errorf("CLAUDE.md must be untouched when it already imports the file:\n got %q\nwant %q", got, existing)
		}
		if rec.ClaudeMDLineAdded {
			t.Errorf("devgeta did not add the line in %q", existing)
		}
		if got := readFile(t, r.instructions()); got != devgetaText {
			t.Error("the file itself is still copied")
		}
	}
}

func TestClaudeInstaller_LookalikesAreNotImports(t *testing.T) {
	r := newClaudeRig(t)
	writeFile(t, r.claudeMD(), "@DEVGETA.md.bak\nemail@DEVGETA.md\n", 0o644)

	rec, err := r.in.Install(r.devgetaTree(t, devgetaText))
	if err != nil {
		t.Fatal(err)
	}
	if !rec.ClaudeMDLineAdded || !strings.HasSuffix(readFile(t, r.claudeMD()), "\n@DEVGETA.md\n") {
		t.Errorf("neither line imports the file, so ours must be added:\n%s", readFile(t, r.claudeMD()))
	}
}

func TestClaudeInstaller_ReinstallingKeepsOneLineAndTheFactThatDevgetaAddedIt(t *testing.T) {
	r := newClaudeRig(t)
	tree := r.devgetaTree(t, devgetaText)
	first, err := r.in.Install(tree)
	if err != nil {
		t.Fatal(err)
	}
	second, err := r.in.Install(tree)
	if err != nil {
		t.Fatal(err)
	}

	if got := readFile(t, r.claudeMD()); got != "@DEVGETA.md\n" {
		t.Errorf("a second install must not add a second line, got %q", got)
	}
	if second.ClaudeMDLineAdded {
		t.Error("the second run did not add the line, and says so")
	}
	// The engine carries the fact across a reinstall through MergeRecord.
	merged := r.in.MergeRecord(first, second)
	if !merged.ClaudeMDLineAdded || merged.InstructionsFile != r.instructions() {
		t.Errorf("devgeta added the line on the first run; that must survive: %+v", merged)
	}
}

func TestClaudeInstaller_MergeRecordLeavesAPluginRecordAlone(t *testing.T) {
	r := newClaudeRig(t)
	next := Record{Plugin: "superpowers@devgeta", Marketplace: "devgeta"}

	if got := r.in.MergeRecord(Record{Plugin: "old"}, next); !reflect.DeepEqual(got, next) {
		t.Errorf("a plugin record has nothing to carry, got %+v", got)
	}
}

func TestClaudeInstaller_UninstallingTheDevgetaPackRemovesTheFileAndOnlyOurLine(t *testing.T) {
	r := newClaudeRig(t)
	writeFile(t, r.claudeMD(), "# Mine\n\n- be nice\n", 0o644)
	rec, err := r.in.Install(r.devgetaTree(t, devgetaText))
	if err != nil {
		t.Fatal(err)
	}
	r.in.TakeNotes()

	if err := r.in.Uninstall(rec); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if exists(r.instructions()) {
		t.Error("DEVGETA.md belongs to devgeta and should be deleted")
	}
	if got := readFile(t, r.claudeMD()); got != "# Mine\n\n- be nice\n" {
		t.Errorf("only devgeta's line may go; the rest must be exactly as it was, got %q", got)
	}
	notes := strings.Join(r.in.TakeNotes(), "\n")
	if !strings.Contains(notes, "@DEVGETA.md") || !strings.Contains(strings.ToLower(notes), "removed") {
		t.Errorf("the output must say the line was removed, got %q", notes)
	}
	if r.f.called("plugin") != 0 {
		t.Errorf("no claude call for the devgeta pack: %v", r.f.calls)
	}
}

func TestClaudeInstaller_UninstallLeavesALineDevgetaDidNotAddOrThatWasChanged(t *testing.T) {
	cases := []struct {
		name     string
		content  string
		added    bool
		wantNote string
	}{
		{"the user had it first", "@DEVGETA.md\n", false, "did not add"},
		{"the user changed the line", "@DEVGETA.md # my style file\n", true, "no longer exactly"},
		{"the user pointed it somewhere else", "@./DEVGETA.md\n", true, "no longer exactly"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := newClaudeRig(t)
			writeFile(t, r.claudeMD(), tc.content, 0o644)
			writeFile(t, r.instructions(), devgetaText, 0o644)

			err := r.in.Uninstall(Record{InstructionsFile: r.instructions(), ClaudeMDLineAdded: tc.added})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got := readFile(t, r.claudeMD()); got != tc.content {
				t.Errorf("CLAUDE.md must be left alone, got %q", got)
			}
			if exists(r.instructions()) {
				t.Error("devgeta's own DEVGETA.md is still deleted")
			}
			if notes := strings.Join(r.in.TakeNotes(), "\n"); !strings.Contains(notes, tc.wantNote) {
				t.Errorf("expected a note containing %q, got %q", tc.wantNote, notes)
			}
		})
	}
}

func TestClaudeInstaller_UninstallToleratesMissingFiles(t *testing.T) {
	r := newClaudeRig(t) // neither DEVGETA.md nor CLAUDE.md exists

	if err := r.in.Uninstall(Record{InstructionsFile: r.instructions(), ClaudeMDLineAdded: true}); err != nil {
		t.Fatalf("files the user already deleted are not an error: %v", err)
	}
}

func TestClaudeInstaller_UninstallWillNotDeleteAFileOutsideItsOwnFolder(t *testing.T) {
	r := newClaudeRig(t)
	victim := filepath.Join(t.TempDir(), "important.txt")
	writeFile(t, victim, "keep me", 0o644)

	err := r.in.Uninstall(Record{InstructionsFile: victim})
	if err == nil || !strings.Contains(err.Error(), victim) {
		t.Fatalf("expected a refusal naming the path, got %v", err)
	}
	if !exists(victim) {
		t.Error("state is data a person can edit; a path in it must never make devgeta delete something outside ~/.claude/DEVGETA.md")
	}
}

func TestClaudeInstaller_TakeNotesDrains(t *testing.T) {
	r := newClaudeRig(t)
	if _, err := r.in.Install(r.devgetaTree(t, devgetaText)); err != nil {
		t.Fatal(err)
	}
	if first := r.in.TakeNotes(); len(first) == 0 {
		t.Error("installing should say what it did to CLAUDE.md")
	}
	if second := r.in.TakeNotes(); len(second) != 0 {
		t.Errorf("notes must not repeat, got %v", second)
	}
}

// TestDevgetaPack_AllThreeCodersGetTheSameBytes carries ADR-0058's
// single-source guarantee over to the three coders: the real shipped pack is
// built once and each real installer is pointed at it, with only the coder
// CLIs faked. Whatever each coder is given must be the shipped DEVGETA.md,
// byte for byte.
func TestDevgetaPack_AllThreeCodersGetTheSameBytes(t *testing.T) {
	shipped := filepath.Join("..", "..", "..", "configs", "agent-packs")
	root := t.TempDir()
	removeAllWritable(t, root)
	pb := &PackBuilder{Builder: &Builder{Root: root}, ShippedLayerDir: shipped, UserLayerDir: t.TempDir()}

	tree, _, err := pb.Prepare(embeddedManifest(), nil)
	if err != nil {
		t.Fatalf("the shipped devgeta pack does not build: %v", err)
	}
	want := readFile(t, filepath.Join(shipped, "devgeta", instructionsFile))
	if strings.TrimSpace(want) == "" {
		t.Fatal("the shipped DEVGETA.md is empty")
	}

	_, cli := newFakeClaude(t)
	claudeDir := t.TempDir()
	claudeIn := &ClaudeInstaller{CLI: cli, PacksRoot: root, ConfigDir: claudeDir}
	claudeRec, err := claudeIn.Install(tree)
	if err != nil {
		t.Fatalf("claude: %v", err)
	}

	ocIn, _ := newOpenCodeInstaller()
	ocRec, err := ocIn.Install(tree)
	if err != nil {
		t.Fatalf("opencode: %v", err)
	}

	piIn := &PiInstaller{App: &fakePi{installed: true}, PacksRoot: root}
	piRec, err := piIn.Install(tree)
	if err != nil {
		t.Fatalf("pi: %v", err)
	}

	if len(ocRec.OpenCodeInstructions) != 1 {
		t.Fatalf("opencode should list one instructions file, got %+v", ocRec)
	}
	got := map[string]string{
		"claude":   readFile(t, claudeRec.InstructionsFile),
		"opencode": readFile(t, ocRec.OpenCodeInstructions[0]),
		// The Pi extension reads DEVGETA.md from its package root.
		"pi": readFile(t, filepath.Join(piRec.PiPackage, instructionsFile)),
	}
	for coder, text := range got {
		if text != want {
			t.Errorf("%s was given different bytes than the shipped DEVGETA.md", coder)
		}
	}
}
