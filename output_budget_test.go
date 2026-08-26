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

// outputBudgetSidecar is the minimal shape output-budget.sh reads from
// agent-runtime.json.
type outputBudgetSidecar struct {
	OutputBudget        any    `json:"outputBudget"`
	Runner              string `json:"runner"`
	LineContentLimit    any    `json:"lineContentLimit"`
	MaxTotalBytes       any    `json:"maxTotalBytes"`
	CaptureContentLimit any    `json:"captureContentLimit"`
	Rules               any    `json:"rules"`
}

// outputBudgetTestHarness wires a temp HOME/XDG_CONFIG_HOME with a sidecar
// file, and the hook script extracted alongside its lib/ dependencies.
type outputBudgetTestHarness struct {
	t          *testing.T
	homeDir    string
	scriptPath string
}

func newOutputBudgetTestHarness(t *testing.T) *outputBudgetTestHarness {
	t.Helper()
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping output-budget.sh behavioral test")
	}

	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/output-budget.sh")
	if err != nil {
		t.Fatalf("failed to read embedded output-budget.sh: %v", err)
	}
	scriptPath := filepath.Join(dir, "output-budget.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	homeDir := t.TempDir()
	return &outputBudgetTestHarness{t: t, homeDir: homeDir, scriptPath: scriptPath}
}

// writeSidecar writes sc as agent-runtime.json under the harness's fake
// XDG_CONFIG_HOME. A blank Runner is filled in with a real, existing file
// (a copy of the real runner script) unless the caller wants to test a
// missing-runner case with an explicit nonexistent path.
func (h *outputBudgetTestHarness) writeSidecar(sc outputBudgetSidecar) {
	h.t.Helper()
	if sc.Runner == "" {
		runnerBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/output-budget-run.sh")
		if err != nil {
			h.t.Fatalf("failed to read embedded output-budget-run.sh: %v", err)
		}
		runnerPath := filepath.Join(h.homeDir, "output-budget-run.sh")
		if err := os.WriteFile(runnerPath, runnerBytes, 0o755); err != nil {
			h.t.Fatalf("failed to write runner: %v", err)
		}
		sc.Runner = runnerPath
	}
	data, err := json.Marshal(sc)
	if err != nil {
		h.t.Fatalf("failed to marshal sidecar: %v", err)
	}
	dir := filepath.Join(h.homeDir, ".config", "devgeta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatalf("failed to create sidecar dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agent-runtime.json"), data, 0o644); err != nil {
		h.t.Fatalf("failed to write sidecar: %v", err)
	}
}

// writeMalformedSidecar writes raw, possibly-invalid JSON directly, for the
// degenerate-sidecar cases a typed struct can't express (not an object,
// rules not an array, etc.).
func (h *outputBudgetTestHarness) writeMalformedSidecar(raw string) {
	h.t.Helper()
	dir := filepath.Join(h.homeDir, ".config", "devgeta")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		h.t.Fatalf("failed to create sidecar dir: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(dir, "agent-runtime.json"),
		[]byte(raw),
		0o644,
	); err != nil {
		h.t.Fatalf("failed to write sidecar: %v", err)
	}
}

// defaultRules is a small representative slice of the real built-in table —
// enough to exercise precedence and tokenization without hand-copying all
// nine entries into every test.
func defaultTestRules() []map[string]any {
	return []map[string]any{
		{"name": "go-test", "match": []string{"go", "test"}, "head": 30, "tail": 120},
		{"name": "npm-test", "match": []string{"npm", "test"}, "head": 30, "tail": 120},
		{"name": "npm-run", "match": []string{"npm", "run"}, "head": 30, "tail": 100},
		{"name": "make", "match": []string{"make"}, "head": 20, "tail": 100},
	}
}

func defaultTestSidecar() outputBudgetSidecar {
	return outputBudgetSidecar{
		OutputBudget:        true,
		LineContentLimit:    1984,
		MaxTotalBytes:       65536,
		CaptureContentLimit: 16777088,
		Rules:               defaultTestRules(),
	}
}

// run invokes the hook with a PreToolUse-shaped payload on stdin and
// returns its exit code, stdout, and stderr. extraToolInput lets a test add
// fields (description, timeout) beyond command, to check they survive the
// rewrite untouched.
func (h *outputBudgetTestHarness) run(
	t *testing.T, command string, extraToolInput map[string]any, env ...string,
) (exitCode int, stdout, stderr string) {
	t.Helper()
	toolInput := map[string]any{"command": command}
	for k, v := range extraToolInput {
		toolInput[k] = v
	}
	payload, err := json.Marshal(map[string]any{"tool_input": toolInput, "cwd": h.homeDir})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	cmd := exec.Command(h.scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(
		os.Environ(),
		"HOME="+h.homeDir,
		"XDG_CONFIG_HOME="+filepath.Join(h.homeDir, ".config"),
	)
	cmd.Env = append(cmd.Env, env...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stdoutBuf.String(), stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run output-budget.sh: %v\nstderr: %s", runErr, stderrBuf.String())
	}
	return exitErr.ExitCode(), stdoutBuf.String(), stderrBuf.String()
}

func rewrittenCommand(t *testing.T, stdout string) string {
	t.Helper()
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput struct {
				Command string `json:"command"`
			} `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse hook stdout as JSON: %v\nstdout: %s", err, stdout)
	}
	return parsed.HookSpecificOutput.UpdatedInput.Command
}

func TestOutputBudgetHook_NoSidecarAllowsUnmodified(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	code, stdout, _ := h.run(t, "go test ./...", nil)
	if code != 0 || stdout != "" {
		t.Errorf("code=%d stdout=%q, want 0 and empty (no sidecar -> no rewrite)", code, stdout)
	}
}

func TestOutputBudgetHook_GateFalseAllowsUnmodified(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	sc := defaultTestSidecar()
	sc.OutputBudget = false
	h.writeSidecar(sc)

	code, stdout, _ := h.run(t, "go test ./...", nil)
	if code != 0 || stdout != "" {
		t.Errorf("code=%d stdout=%q, want 0 and empty (gate off)", code, stdout)
	}
}

func TestOutputBudgetHook_EscapeHatchAllowsUnmodified(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	code, stdout, _ := h.run(t, "go test ./...", nil, "DEVGETA_OUTPUT_BUDGET=off")
	if code != 0 || stdout != "" {
		t.Errorf("code=%d stdout=%q, want 0 and empty (escape hatch)", code, stdout)
	}
}

func TestOutputBudgetHook_MatchedCommandRewrites(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	sc := defaultTestSidecar()
	h.writeSidecar(sc)

	code, stdout, stderr := h.run(t, "go test ./...", nil)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stderr=%s", code, stderr)
	}
	if stdout == "" {
		t.Fatal("expected a rewrite, got no output")
	}
	got := rewrittenCommand(t, stdout)
	if !containsAll(got, sc.Runner, "30", "120", "1984", "65536", "16777088", "go test ./...") {
		t.Errorf("rewritten command missing expected pieces: %q", got)
	}
}

func TestOutputBudgetHook_UnmatchedCommandPassesThrough(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	code, stdout, _ := h.run(t, "ls -la", nil)
	if code != 0 || stdout != "" {
		t.Errorf("code=%d stdout=%q, want 0 and empty (no matching rule)", code, stdout)
	}
}

func TestOutputBudgetHook_PrecedenceNpmTestBeatsNpmRun(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	_, stdout, _ := h.run(t, "npm test", nil)
	got := rewrittenCommand(t, stdout)
	// npm-test is head=30 tail=120; npm-run is head=30 tail=100. Assert the
	// tail value that identifies which rule actually fired.
	if !containsAll(got, "120") {
		t.Errorf("expected npm-test's tail (120) in the rewrite, got: %q", got)
	}
}

func TestOutputBudgetHook_EnvPrefixedCommandStillMatches(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	for _, cmd := range []string{"CI=1 go test ./...", "env CI=1 go test ./..."} {
		t.Run(cmd, func(t *testing.T) {
			_, stdout, _ := h.run(t, cmd, nil)
			if stdout == "" {
				t.Errorf("expected %q to match go-test", cmd)
			}
		})
	}
}

func TestOutputBudgetHook_QuotedExecutableRefusesToMatch(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	code, stdout, _ := h.run(t, `"/opt/my go/bin/go" test`, nil)
	if code != 0 || stdout != "" {
		t.Errorf(
			"code=%d stdout=%q, want a pass-through (quote in the compared prefix)",
			code, stdout,
		)
	}
}

func TestOutputBudgetHook_QuotedAssignmentRefusesToMatch(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	code, stdout, _ := h.run(t, `FOO="a b" go test`, nil)
	if code != 0 || stdout != "" {
		t.Errorf(
			"code=%d stdout=%q, want a pass-through (quote in a stripped assignment token)",
			code, stdout,
		)
	}
}

func TestOutputBudgetHook_ArgumentMetacharactersDoNotBlockAMatch(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	for _, cmd := range []string{
		`make -j$(nproc)`,
		`npm test -- --grep "foo bar"`,
	} {
		t.Run(cmd, func(t *testing.T) {
			_, stdout, _ := h.run(t, cmd, nil)
			if stdout == "" {
				t.Errorf(
					"expected %q to still match (metachars are in arguments, not the compared prefix)",
					cmd,
				)
			}
		})
	}
}

func TestOutputBudgetHook_NotStrippedPrefixesDoNotMatch(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	for _, cmd := range []string{"command go test", "timeout 60 make"} {
		t.Run(cmd, func(t *testing.T) {
			code, stdout, _ := h.run(t, cmd, nil)
			if code != 0 || stdout != "" {
				t.Errorf(
					"code=%d stdout=%q, want a pass-through (%q is not a recognized strip)",
					code,
					stdout,
					cmd,
				)
			}
		})
	}
}

func TestOutputBudgetHook_UpdatedInputPreservesOtherFields(t *testing.T) {
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	_, stdout, _ := h.run(
		t,
		"go test ./...",
		map[string]any{"description": "run the suite", "timeout": 60000},
	)
	var parsed struct {
		HookSpecificOutput struct {
			UpdatedInput map[string]any `json:"updatedInput"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(stdout), &parsed); err != nil {
		t.Fatalf("failed to parse hook stdout: %v\nstdout: %s", err, stdout)
	}
	ui := parsed.HookSpecificOutput.UpdatedInput
	if ui["description"] != "run the suite" {
		t.Errorf("description dropped from updatedInput: %+v", ui)
	}
	if v, ok := ui["timeout"].(float64); !ok || v != 60000 {
		t.Errorf("timeout dropped or changed in updatedInput: %+v", ui)
	}
}

func TestOutputBudgetHook_DegenerateSidecarCasesAllPassThrough(t *testing.T) {
	cases := []struct {
		name  string
		setup func(h *outputBudgetTestHarness)
	}{
		{"absent sidecar", func(h *outputBudgetTestHarness) {}},
		{"unreadable/malformed JSON", func(h *outputBudgetTestHarness) {
			h.writeMalformedSidecar("{not valid json")
		}},
		{"outputBudget key missing", func(h *outputBudgetTestHarness) {
			h.writeMalformedSidecar(
				`{"runner": "/bin/true", "lineContentLimit": 100, "maxTotalBytes": 100, "captureContentLimit": 100, "rules": []}`,
			)
		}},
		{"outputBudget wrong type", func(h *outputBudgetTestHarness) {
			sc := defaultTestSidecar()
			sc.OutputBudget = "yes"
			h.writeSidecar(sc)
		}},
		{"runner names a nonexistent path", func(h *outputBudgetTestHarness) {
			sc := defaultTestSidecar()
			sc.Runner = "/no/such/file/exists/anywhere"
			h.writeSidecar(sc)
		}},
		{"limit violates the width contract (16 digits)", func(h *outputBudgetTestHarness) {
			sc := defaultTestSidecar()
			sc.CaptureContentLimit = "1000000000000000"
			h.writeSidecar(sc)
		}},
		{"rules is not an array", func(h *outputBudgetTestHarness) {
			sc := defaultTestSidecar()
			sc.Rules = "not-an-array"
			h.writeSidecar(sc)
		}},
		{"one rule entry is malformed", func(h *outputBudgetTestHarness) {
			sc := defaultTestSidecar()
			rules := defaultTestRules()
			rules = append(
				rules,
				map[string]any{"name": "bad", "match": []string{}, "head": 1, "tail": 2},
			)
			sc.Rules = rules
			h.writeSidecar(sc)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := newOutputBudgetTestHarness(t)
			tc.setup(h)
			code, stdout, stderr := h.run(t, "go test ./...", nil)
			if code != 0 || stdout != "" {
				t.Errorf(
					"code=%d stdout=%q stderr=%q, want a clean pass-through",
					code,
					stdout,
					stderr,
				)
			}
		})
	}
}

func containsAll(s string, substrs ...string) bool {
	for _, sub := range substrs {
		if !strings.Contains(s, sub) {
			return false
		}
	}
	return true
}
