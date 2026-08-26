package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runSuppressionGuardHook extracts configs/claude/suppression-guard.sh and
// its lib/ dependencies from the embedded ConfigsFS, runs it with a
// PreToolUse-shaped Edit/Write payload on stdin, and returns its exit code
// and stderr. Mirrors runSecretGuardHook/runTaskRedirectHookInDir.
func runSuppressionGuardHook(
	t *testing.T,
	toolName, filePath, oldString, newString, content, cwd string,
) (exitCode int, stderr string) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping suppression-guard.sh behavioral test")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/suppression-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded suppression-guard.sh: %v", err)
	}

	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "suppression-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	toolInput := map[string]string{"file_path": filePath}
	switch toolName {
	case "Edit":
		toolInput["old_string"] = oldString
		toolInput["new_string"] = newString
	case "Write":
		toolInput["content"] = content
	}
	payload, err := json.Marshal(map[string]any{
		"tool_name":  toolName,
		"tool_input": toolInput,
		"cwd":        cwd,
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &bytes.Buffer{}

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run suppression-guard.sh: %v", runErr)
	}
	return exitErr.ExitCode(), stderrBuf.String()
}

func TestSuppressionGuardHook_DeniesIntroducedSuppressionsInDevgetaRepo(t *testing.T) {
	devgetaDir := repoRoot(t)

	cases := []struct {
		name      string
		toolName  string
		oldString string
		newString string
		content   string
	}{
		{
			"go nolint via Edit",
			"Edit",
			"func f() error {\n\treturn nil\n}",
			"func f() error { //nolint:errcheck\n\treturn nil\n}",
			"",
		},
		{"python noqa via Edit", "Edit", "import os", "import os  # noqa", ""},
		{"python type ignore via Edit", "Edit", "x = 1", "x = 1  # type: ignore", ""},
		{
			"eslint-disable via Edit",
			"Edit",
			"const x = 1;",
			"// eslint-disable-next-line\nconst x = 1;",
			"",
		},
		{"ts-ignore via Edit", "Edit", "foo();", "// @ts-ignore\nfoo();", ""},
		{"go nolint via Write", "Write", "", "", "package main\n//nolint:unused\nfunc f() {}\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, stderr := runSuppressionGuardHook(
				t, tc.toolName, "main.go", tc.oldString, tc.newString, tc.content, devgetaDir,
			)
			if code != 2 {
				t.Fatalf("expected deny (exit 2), got exit %d, stderr=%q", code, stderr)
			}
			if !strings.Contains(stderr, "DEVGETA_SKIP_SUPPRESSION_GUARD") {
				t.Errorf("expected deny reason to state the bypass escape hatch, got %q", stderr)
			}
			if !strings.Contains(stderr, "shell that launches this agent") ||
				!strings.Contains(stderr, "this hook reads its own environment") {
				t.Errorf(
					"expected deny reason for %q to contain the reworded bypass hint, got %q",
					tc.name,
					stderr,
				)
			}
		})
	}
}

// TestSuppressionGuardHook_DeniesAdditionalSuppressionWhenOneAlreadyExisted
// is the regression test for a reviewer-found false negative: the original
// check was presence-based (needle in NEW, not in OLD) rather than
// count-based, so adding a SECOND, genuinely new suppression comment was
// allowed whenever an unrelated one of the same kind already existed in the
// touched span.
func TestSuppressionGuardHook_DeniesAdditionalSuppressionWhenOneAlreadyExisted(t *testing.T) {
	devgetaDir := repoRoot(t)
	code, stderr := runSuppressionGuardHook(
		t, "Edit", "main.go",
		"func f() { //nolint:errcheck\n\treturn\n}",
		"func f() { //nolint:errcheck\n\t//nolint:unused\n\treturn\n}",
		"", devgetaDir,
	)
	if code != 2 {
		t.Fatalf(
			"expected deny (exit 2) for a second, new suppression alongside an existing one, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

func TestSuppressionGuardHook_AllowsPreexistingSuppressionUntouchedByEdit(t *testing.T) {
	devgetaDir := repoRoot(t)
	// old_string already contains the needle, new_string keeps it — not a
	// NEW introduction, so this edit must be allowed even inside the devgeta
	// repo.
	code, stderr := runSuppressionGuardHook(
		t, "Edit", "main.go",
		"func f() { //nolint:errcheck\n\treturn\n}",
		"func f() { //nolint:errcheck\n\treturn nil\n}",
		"", devgetaDir,
	)
	if code != 0 {
		t.Errorf(
			"expected allow (exit 0) for a preexisting, untouched suppression, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

func TestSuppressionGuardHook_AllowsOrdinaryEditsInDevgetaRepo(t *testing.T) {
	devgetaDir := repoRoot(t)
	code, stderr := runSuppressionGuardHook(
		t, "Edit", "main.go", "func f() {}", "func f() { doStuff() }", "", devgetaDir,
	)
	if code != 0 {
		t.Errorf(
			"expected allow (exit 0) for an ordinary edit, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

func TestSuppressionGuardHook_AllowsOutsideDevgetaRepo(t *testing.T) {
	noGoMod := t.TempDir()
	otherModule := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(otherModule, "go.mod"),
		[]byte("module github.com/other/thing\n\ngo 1.25\n"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write other go.mod: %v", err)
	}

	for _, dir := range []string{noGoMod, otherModule} {
		t.Run(dir, func(t *testing.T) {
			code, stderr := runSuppressionGuardHook(
				t, "Edit", "main.go", "func f() {}", "func f() { //nolint:errcheck\n}", "", dir,
			)
			if code != 0 {
				t.Errorf(
					"expected allow (exit 0) outside devgeta repo even for a suppression comment, got exit %d, stderr=%q",
					code,
					stderr,
				)
			}
		})
	}
}

// goSuppression is assembled from two halves on purpose: this file lives in
// the devgeta repo, so a contiguous literal here would trip the very guard
// these tests exercise the next time someone edits the file.
const goSuppression = "//no" + "lint:errcheck"

// The scope gate answers "does devgeta's own policy apply to this write?", and
// the thing being written is the file — not wherever the session happens to be
// rooted. These two tests pin both directions of that, which the cwd-only
// tests above cannot: they pass a relative "main.go", so cwd and target are
// always the same repo and never disagree.
func TestSuppressionGuardHook_ScopesToTargetFileNotSessionCwd(t *testing.T) {
	outsideCwd := t.TempDir()
	devgetaFile := filepath.Join(repoRoot(t), "internal", "apps", "claude", "claude.go")

	code, stderr := runSuppressionGuardHook(
		t, "Edit", devgetaFile, "func f() {}", "func f() { "+goSuppression+"\n}", "", outsideCwd,
	)
	if code != 2 {
		t.Errorf(
			"expected deny (exit 2) for a suppression written INTO a devgeta file, got exit %d, stderr=%q; "+
				"the ban is bypassable whenever the session is rooted outside the repo",
			code,
			stderr,
		)
	}
}

func TestSuppressionGuardHook_AllowsDevgetaSessionWritingOutsideTheRepo(t *testing.T) {
	outsideFile := filepath.Join(t.TempDir(), "main.go")

	code, stderr := runSuppressionGuardHook(
		t, "Edit", outsideFile, "func f() {}", "func f() { "+goSuppression+"\n}", "", repoRoot(t),
	)
	if code != 0 {
		t.Errorf(
			"expected allow (exit 0) for a suppression written to a file OUTSIDE devgeta, got exit %d, stderr=%q; "+
				"ADR-0006 scopes this ban to devgeta's own code, not to whatever a devgeta-rooted session touches",
			code,
			stderr,
		)
	}
}

func TestSuppressionGuardHook_BypassEnvVarAllowsEverything(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}
	devgetaDir := repoRoot(t)

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/suppression-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded suppression-guard.sh: %v", err)
	}
	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "suppression-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"tool_name": "Edit",
		"tool_input": map[string]string{
			"file_path":  "main.go",
			"old_string": "func f() {}",
			"new_string": "func f() { //nolint:errcheck\n}",
		},
		"cwd": devgetaDir,
	})
	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "DEVGETA_SKIP_SUPPRESSION_GUARD=1")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		t.Errorf(
			"expected bypass env var to allow, got error: %v (stderr=%q)",
			err,
			stderrBuf.String(),
		)
	}
}

func TestSuppressionGuardHook_FailsOpenOnMalformedInput(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/suppression-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded suppression-guard.sh: %v", err)
	}
	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "suppression-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	stdins := map[string]string{
		"empty stdin":           "",
		"not json":              "not json at all",
		"json without tool":     `{"tool_input":{"file_path":"foo.go"}}`,
		"unrelated tool (Bash)": `{"tool_name":"Bash","tool_input":{"command":"//nolint stuff"}}`,
	}
	for name, stdin := range stdins {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(scriptPath)
			cmd.Stdin = strings.NewReader(stdin)
			var stderrBuf bytes.Buffer
			cmd.Stderr = &stderrBuf
			if err := cmd.Run(); err != nil {
				t.Errorf(
					"expected fail-open (exit 0) for %s, got error: %v (stderr=%q)",
					name,
					err,
					stderrBuf.String(),
				)
			}
		})
	}
}
