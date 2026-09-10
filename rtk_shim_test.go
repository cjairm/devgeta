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

// rtkShimTestHarness extracts the embedded configs/claude/rtk-shim.sh into a
// temp dir and runs it against a fake `rtk` binary placed first on PATH, so
// the test never depends on rtk actually being installed (ADR-0038's shim
// must run in CI where it is not) and never invokes the real one.
type rtkShimTestHarness struct {
	t          *testing.T
	scriptPath string
}

func newRtkShimTestHarness(t *testing.T) *rtkShimTestHarness {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping rtk-shim.sh behavioral test")
	}

	dir := t.TempDir()
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/rtk-shim.sh")
	if err != nil {
		t.Fatalf("failed to read embedded rtk-shim.sh: %v", err)
	}
	scriptPath := filepath.Join(dir, "rtk-shim.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}
	return &rtkShimTestHarness{t: t, scriptPath: scriptPath}
}

// writeFakeRtk drops an executable named `rtk` in its own temp dir and
// returns that dir, so a test can prepend it to PATH. `script` is the body
// of the fake binary (shebang added).
func (h *rtkShimTestHarness) writeFakeRtk(script string) string {
	h.t.Helper()
	dir := h.t.TempDir()
	path := filepath.Join(dir, "rtk")
	body := "#!/usr/bin/env bash\n" + script
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		h.t.Fatalf("failed to write fake rtk: %v", err)
	}
	return dir
}

// pathWithout returns a PATH value with every real rtk directory removed,
// so "rtk missing" tests can't accidentally see the real binary installed on
// this dev machine.
func pathWithoutRealRtk(t *testing.T) string {
	t.Helper()
	realRtk, err := exec.LookPath("rtk")
	if err != nil {
		return os.Getenv("PATH")
	}
	realDir := filepath.Dir(realRtk)
	var kept []string
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p != realDir {
			kept = append(kept, p)
		}
	}
	return strings.Join(kept, string(os.PathListSeparator))
}

// run invokes rtk-shim.sh with the given command on a PreToolUse-shaped
// stdin payload, with fakeRtkDir prepended to PATH (empty means "no rtk
// anywhere on PATH"), plus any extra env assignments.
func (h *rtkShimTestHarness) run(
	command, fakeRtkDir string, extraEnv ...string,
) (exitCode int, stdout, stderr string) {
	h.t.Helper()
	payload, err := json.Marshal(map[string]any{
		"tool_name":  "Bash",
		"tool_input": map[string]string{"command": command},
		"cwd":        "/tmp",
	})
	if err != nil {
		h.t.Fatalf("failed to marshal payload: %v", err)
	}

	cmd := exec.Command(h.scriptPath)
	cmd.Stdin = bytes.NewReader(payload)

	basePath := pathWithoutRealRtk(h.t)
	path := basePath
	if fakeRtkDir != "" {
		path = fakeRtkDir + string(os.PathListSeparator) + basePath
	}
	env := append(os.Environ(), "PATH="+path)
	env = append(env, extraEnv...)
	cmd.Env = env

	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stdoutBuf.String(), stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		h.t.Fatalf("failed to run rtk-shim.sh: %v\nstderr: %s", runErr, stderrBuf.String())
	}
	return exitErr.ExitCode(), stdoutBuf.String(), stderrBuf.String()
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", "rtk-hook-payloads", name))
	if err != nil {
		t.Fatalf("failed to read fixture %s: %v", name, err)
	}
	return string(b)
}

// TestRtkHookFixturesCarryTheExpectedKey pins the recorded payloads' shape:
// the with-decision fixture literally names "permissionDecision" and the
// without-decision one does not. If a future re-recording against a newer
// rtk renames that key, THIS test goes red first — before the shim's own
// del() silently stops matching anything (ADR-0038's stated risk).
func TestRtkHookFixturesCarryTheExpectedKey(t *testing.T) {
	withDecision := readFixture(t, "with-decision.json")
	if !strings.Contains(withDecision, `"permissionDecision"`) {
		t.Fatalf(
			"with-decision.json no longer contains \"permissionDecision\" — re-check rtk's payload shape before updating the shim:\n%s",
			withDecision,
		)
	}
	withoutDecision := readFixture(t, "without-decision.json")
	if strings.Contains(withoutDecision, `"permissionDecision"`) {
		t.Fatalf(
			"without-decision.json unexpectedly contains \"permissionDecision\":\n%s",
			withoutDecision,
		)
	}
}

func updatedInputCommand(t *testing.T, stdout string) string {
	t.Helper()
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse shim stdout as JSON: %v\nstdout: %s", err, stdout)
	}
	return parsed.HookSpecificOutput.UpdatedInput.Command
}

func TestRtkShimStripsDecisionAndForwardsRewrite(t *testing.T) {
	h := newRtkShimTestHarness(t)
	fixture := readFixture(t, "with-decision.json")
	fakeDir := h.writeFakeRtk(`cat <<'FIXTURE'
` + fixture + `
FIXTURE
`)

	exitCode, stdout, stderr := h.run("gh pr merge 12 --squash", fakeDir)

	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr)
	}
	if strings.Contains(stdout, "permissionDecision") {
		t.Fatalf("shim forwarded permissionDecision instead of stripping it:\n%s", stdout)
	}
	if got, want := updatedInputCommand(t, stdout), "rtk gh pr merge 12 --squash"; got != want {
		t.Fatalf("updatedInput.command = %q, want %q (byte-for-byte forward)", got, want)
	}
}

func TestRtkShimForwardsRewriteWhenRtkHadNoDecision(t *testing.T) {
	h := newRtkShimTestHarness(t)
	fixture := readFixture(t, "without-decision.json")
	fakeDir := h.writeFakeRtk(`cat <<'FIXTURE'
` + fixture + `
FIXTURE
`)

	exitCode, stdout, stderr := h.run("gh api rate_limit", fakeDir)

	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr)
	}
	if strings.Contains(stdout, "permissionDecision") {
		t.Fatalf("shim introduced a permissionDecision that wasn't there:\n%s", stdout)
	}
	if got, want := updatedInputCommand(t, stdout), "rtk gh api rate_limit"; got != want {
		t.Fatalf("updatedInput.command = %q, want %q (byte-for-byte forward)", got, want)
	}
}

func TestRtkShimFailsOpenToSilence(t *testing.T) {
	h := newRtkShimTestHarness(t)

	cases := []struct {
		name       string
		fakeRtkDir func() string
	}{
		{
			name:       "rtk missing from PATH",
			fakeRtkDir: func() string { return "" },
		},
		{
			name: "rtk exits non-zero",
			fakeRtkDir: func() string {
				return h.writeFakeRtk("exit 1\n")
			},
		},
		{
			name: "rtk emits empty output",
			fakeRtkDir: func() string {
				return h.writeFakeRtk("exit 0\n")
			},
		},
		{
			name: "rtk emits unparseable JSON",
			fakeRtkDir: func() string {
				return h.writeFakeRtk("echo 'not json'\n")
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exitCode, stdout, stderr := h.run("git status", tc.fakeRtkDir())
			if exitCode != 0 {
				t.Fatalf("expected exit 0 (fail open), got %d (stderr: %s)", exitCode, stderr)
			}
			if stdout != "" {
				t.Fatalf("expected empty stdout (fail open to silence), got:\n%s", stdout)
			}
		})
	}
}

func TestRtkShimSkipEnvVarBypassesEvenAWorkingRtk(t *testing.T) {
	h := newRtkShimTestHarness(t)
	fixture := readFixture(t, "with-decision.json")
	fakeDir := h.writeFakeRtk(`cat <<'FIXTURE'
` + fixture + `
FIXTURE
`)

	exitCode, stdout, stderr := h.run(
		"gh pr merge 12 --squash", fakeDir, "DEVGETA_SKIP_RTK_SHIM=1",
	)

	if exitCode != 0 {
		t.Fatalf("expected exit 0, got %d (stderr: %s)", exitCode, stderr)
	}
	if stdout != "" {
		t.Fatalf("DEVGETA_SKIP_RTK_SHIM=1 should silence the shim entirely, got:\n%s", stdout)
	}
}
