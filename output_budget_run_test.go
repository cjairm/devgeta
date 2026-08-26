package main

import (
	"bytes"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// writeOutputBudgetRunScript extracts the embedded runner to a temp file and
// returns its path — same extraction pattern runSecretGuardHook uses for
// secret-guard.sh, but this script takes its arguments as argv, not a
// PreToolUse JSON payload on stdin, since it is called by the rewritten
// command line, not registered as a hook itself.
func writeOutputBudgetRunScript(t *testing.T) string {
	t.Helper()
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/output-budget-run.sh")
	if err != nil {
		t.Fatalf("failed to read embedded output-budget-run.sh: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "output-budget-run.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}
	return scriptPath
}

// runOutputBudgetRun invokes the runner with the standard argv shape:
// <head> <tail> <line-content-limit> <max-total-bytes> <capture-content-limit> <command>.
// Returns the exit code, and the replayed stdout (stderr is not part of the
// contract this wrapper cares about, so it's left to flow through for
// debugging test failures).
func runOutputBudgetRun(
	t *testing.T,
	scriptPath string,
	head, tail, lineContentLimit, maxTotalBytes, captureContentLimit int,
	command string,
	env ...string,
) (exitCode int, stdout string) {
	t.Helper()
	cmd := exec.Command(
		scriptPath,
		strconv.Itoa(head),
		strconv.Itoa(tail),
		strconv.Itoa(lineContentLimit),
		strconv.Itoa(maxTotalBytes),
		strconv.Itoa(captureContentLimit),
		command,
	)
	cmd.Env = append(os.Environ(), env...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stdoutBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf(
			"failed to run output-budget-run.sh for command %q: %v\nstderr: %s",
			command, runErr, stderrBuf.String(),
		)
	}
	return exitErr.ExitCode(), stdoutBuf.String()
}

// TestOutputBudgetRun_PreservesExitStatusOfAFailingCommand is written and
// watched to fail before any runner script exists — this is the regression
// that makes the whole feature dangerous rather than merely disappointing
// (docs/guides/output-budget-runner.md §3, §10): a wrapper that swallows a
// failure's exit status would make a failing test suite look green.
func TestOutputBudgetRun_PreservesExitStatusOfAFailingCommand(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	exitCode, _ := runOutputBudgetRun(t, scriptPath, 10, 10, 500, 4000, 4000, "exit 7")
	if exitCode != 7 {
		t.Fatalf("exit code = %d, want 7 (the wrapped command's own status)", exitCode)
	}
}

func TestOutputBudgetRun_SuccessfulCommandStillExitsZero(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	exitCode, _ := runOutputBudgetRun(t, scriptPath, 10, 10, 500, 4000, 4000, "true")
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0", exitCode)
	}
}

func TestOutputBudgetRun_UnderCapIsByteIdenticalToUnwrapped(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	_, stdout := runOutputBudgetRun(
		t, scriptPath, 10, 10, 500, 4000, 4000,
		`printf 'line one\nline two\nline three\n'`,
	)
	want := "line one\nline two\nline three\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q (byte-identical to unwrapped)", stdout, want)
	}
}

func TestOutputBudgetRun_CompoundCommandKeepsItsSemantics(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	exitCode, stdout := runOutputBudgetRun(
		t, scriptPath, 10, 10, 500, 4000, 4000,
		`echo first && exit 5`,
	)
	if exitCode != 5 {
		t.Errorf("exit code = %d, want 5", exitCode)
	}
	if stdout != "first\n" {
		t.Errorf("stdout = %q, want %q", stdout, "first\n")
	}
}

func TestOutputBudgetRun_CommandWithQuotesAndMetacharsSurvives(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	// Single quotes, $, and backticks in the command itself (not injected by
	// this test harness's own quoting — exec.Command passes it as one argv
	// element, exactly as the hook's rewrite would).
	command := `printf 'it'"'"'s $HOME and ` + "`date`" + ` literally\n'`
	_, stdout := runOutputBudgetRun(t, scriptPath, 10, 10, 500, 4000, 4000, command)
	want := "it's $HOME and `date` literally\n"
	if stdout != want {
		t.Fatalf("stdout = %q, want %q", stdout, want)
	}
}

func TestOutputBudgetRun_EscapeHatchIsATruePassThrough(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	exitCode, stdout := runOutputBudgetRun(
		t, scriptPath, 1, 1, 10, 20, 20,
		`printf 'this line is definitely longer than the tiny caps above\n'; exit 3`,
		"DEVGETA_OUTPUT_BUDGET=off",
	)
	want := "this line is definitely longer than the tiny caps above\n"
	if stdout != want {
		t.Errorf("stdout = %q, want the unwrapped output %q", stdout, want)
	}
	if exitCode != 3 {
		t.Errorf("exit code = %d, want 3", exitCode)
	}
}

func TestOutputBudgetRun_NonpositiveOrOutOfRangeArgsRunUnmodified(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	cases := []struct {
		name         string
		head, tail   int
		lineLimit    string
		totalLimit   string
		captureLimit string
	}{
		{"zero line limit", 10, 10, "0", "4000", "4000"},
		{"negative total limit", 10, 10, "500", "-1", "4000"},
		{"16-digit capture limit", 10, 10, "500", "4000", "1000000000000000"},
		{"leading zero", 10, 10, "0500", "4000", "4000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(
				scriptPath,
				strconv.Itoa(tc.head), strconv.Itoa(tc.tail),
				tc.lineLimit, tc.totalLimit, tc.captureLimit,
				`printf 'unmodified output\n'; exit 4`,
			)
			var stdoutBuf, stderrBuf bytes.Buffer
			cmd.Stdout = &stdoutBuf
			cmd.Stderr = &stderrBuf
			runErr := cmd.Run()
			var exitErr *exec.ExitError
			exitCode := 0
			if runErr != nil {
				if !isExitError(runErr, &exitErr) {
					t.Fatalf("failed to run: %v\nstderr: %s", runErr, stderrBuf.String())
				}
				exitCode = exitErr.ExitCode()
			}
			if exitCode != 4 {
				t.Errorf("exit code = %d, want 4 (command ran unmodified)", exitCode)
			}
			if stdoutBuf.String() != "unmodified output\n" {
				t.Errorf(
					"stdout = %q, want the command's real output, unmodified",
					stdoutBuf.String(),
				)
			}
		})
	}
}

func TestOutputBudgetRun_OverCapProducesHeadMarkerTail(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	// 20 numbered lines, head=3 tail=3 -> lines 4..17 must be omitted.
	command := `for i in $(seq 1 20); do echo "line$i"; done`
	_, stdout := runOutputBudgetRun(t, scriptPath, 3, 3, 500, 100000, 100000, command)

	if !strings.Contains(stdout, "line1\n") || !strings.Contains(stdout, "line3\n") {
		t.Errorf("expected the head lines present, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "line18\n") || !strings.Contains(stdout, "line20") {
		t.Errorf("expected the tail lines present, got:\n%s", stdout)
	}
	if strings.Contains(stdout, "line10\n") {
		t.Errorf("expected a middle line to be omitted, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "omitted") {
		t.Errorf("expected an omission marker mentioning what was omitted, got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "/") {
		t.Errorf("expected the marker to name a path to the full output, got:\n%s", stdout)
	}
}

// TestOutputBudgetRun_CaptureBoundary pins the exact-byte-wide signal from
// guide §4.3: content_limit, +1, and +2 all read as capped or not exactly as
// the table there specifies, and the notice never claims certain loss for
// the +1 case (nothing was actually discarded there).
func TestOutputBudgetRun_CaptureBoundary(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	const contentLimit = 50

	cases := []struct {
		name       string
		produce    int
		wantCapped bool
	}{
		{"exactly at the limit", contentLimit, false},
		{"one byte over", contentLimit + 1, true},
		{"two bytes over", contentLimit + 2, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			command := "printf '%" + strconv.Itoa(tc.produce) + "s' | tr ' ' x"
			_, stdout := runOutputBudgetRun(
				t,
				scriptPath,
				10,
				10,
				5000,
				5000,
				contentLimit,
				command,
			)

			gotCapped := strings.Contains(stdout, "may be incomplete")
			if gotCapped != tc.wantCapped {
				t.Errorf("capped = %v, want %v; stdout:\n%s", gotCapped, tc.wantCapped, stdout)
			}
			if gotCapped && strings.Contains(stdout, "truncated,") {
				t.Errorf(
					"capture-cap notice must not use per-line truncation wording; stdout:\n%s",
					stdout,
				)
			}
		})
	}
}

// TestOutputBudgetRun_ByteRefillFiresOnManyLargeLines is the 25-lines-of-1MB
// case from guide §5.1: line count alone (25) is well under head+tail, so
// step 2 never drops anything, but the total size still exceeds
// maxTotalBytes and the byte refill must still reduce it.
func TestOutputBudgetRun_ByteRefillFiresOnManyLargeLines(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	// 25 lines, each ~300 bytes: well under head+tail (30+120) by line
	// COUNT, well under maxTotalBytes's head budget by line SIZE (so at
	// least a few whole lines survive on each side), but 25*300 = 7500
	// bytes total still exceeds maxTotalBytes (4096), forcing the refill.
	command := `for i in $(seq 1 25); do printf 'L%d:' "$i"; printf 'y%.0s' $(seq 1 290); printf '\n'; done`
	_, stdout := runOutputBudgetRun(t, scriptPath, 30, 120, 2048, 4096, 5000000, command)

	if len(stdout) > 4096 {
		t.Errorf("stdout is %d bytes, want <= maxTotalBytes (4096)", len(stdout))
	}
	if !strings.Contains(stdout, "L1:") {
		t.Errorf(
			"expected the first line's content present in the head, got:\n%s",
			stdout[:min(200, len(stdout))],
		)
	}
	if !strings.Contains(stdout, "L25:") {
		t.Errorf(
			"expected the last line's content present in the tail, got tail:\n%s",
			stdout[max(0, len(stdout)-200):],
		)
	}
	if !strings.Contains(stdout, "omitted") {
		t.Errorf("expected an omission marker, got:\n%s", stdout)
	}
}

// TestOutputBudgetRun_NoReducedResultExceedsMaxTotalBytes is the property
// test from guide §10: across a range of over-cap fixtures, the reduced
// replay — marker included — must never exceed maxTotalBytes.
func TestOutputBudgetRun_NoReducedResultExceedsMaxTotalBytes(t *testing.T) {
	scriptPath := writeOutputBudgetRunScript(t)
	fixtures := []struct {
		name    string
		command string
	}{
		{"many short lines", `for i in $(seq 1 500); do echo "line $i"; done`},
		{
			"few huge lines",
			`for i in $(seq 1 5); do printf 'y%.0s' $(seq 1 20000); printf '\n'; done`,
		},
		{"one giant line", `printf 'z%.0s' $(seq 1 100000); printf '\n'`},
	}
	const maxTotalBytes = 2048
	for _, f := range fixtures {
		t.Run(f.name, func(t *testing.T) {
			_, stdout := runOutputBudgetRun(
				t,
				scriptPath,
				5,
				10,
				300,
				maxTotalBytes,
				10000000,
				f.command,
			)
			if len(stdout) > maxTotalBytes {
				t.Errorf("reduced output is %d bytes, want <= %d", len(stdout), maxTotalBytes)
			}
		})
	}
}
