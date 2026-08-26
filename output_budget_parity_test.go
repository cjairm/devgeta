package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/cjairm/devgeta/internal/apps/baseapp"
)

// outputBudgetCaseFile mirrors testdata/output-budget-cases.json — the one
// shared case table both the Go and Node parity suites read (guide §10:
// "drive the parity groups from one shared case table, not two hand-kept
// lists"). See output-budget.test.mjs for the JS side of the same file.
type outputBudgetCaseFile struct {
	Rules []struct {
		Name            string   `json:"name"`
		Match           []string `json:"match"`
		Head            int      `json:"head"`
		Tail            int      `json:"tail"`
		MatchingCommand string   `json:"matchingCommand"`
		NearMissCommand string   `json:"nearMissCommand"`
	} `json:"rules"`
	TokenizationCases []struct {
		Description string `json:"description"`
		Command     string `json:"command"`
		ShouldMatch bool   `json:"shouldMatch"`
	} `json:"tokenizationCases"`
	NumericCases []struct {
		Description string `json:"description"`
		Value       string `json:"value"`
		Valid       bool   `json:"valid"`
	} `json:"numericCases"`
}

func loadOutputBudgetCases(t *testing.T) outputBudgetCaseFile {
	t.Helper()
	data, err := os.ReadFile("testdata/output-budget-cases.json")
	if err != nil {
		t.Fatalf("failed to read testdata/output-budget-cases.json: %v", err)
	}
	var cases outputBudgetCaseFile
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatalf("failed to parse testdata/output-budget-cases.json: %v", err)
	}
	return cases
}

// TestOutputBudgetCasesRulesMatchTheGoSource is what makes the shared case
// table actually shared rather than a second hand-kept list: a rule added to
// baseapp.DefaultOutputBudgetRules without updating this fixture fails here,
// on the Go side, before either behavioral suite runs at all.
func TestOutputBudgetCasesRulesMatchTheGoSource(t *testing.T) {
	cases := loadOutputBudgetCases(t)
	if len(cases.Rules) != len(baseapp.DefaultOutputBudgetRules) {
		t.Fatalf(
			"testdata/output-budget-cases.json has %d rules, baseapp.DefaultOutputBudgetRules has %d — update the fixture",
			len(cases.Rules),
			len(baseapp.DefaultOutputBudgetRules),
		)
	}
	for i, want := range baseapp.DefaultOutputBudgetRules {
		got := cases.Rules[i]
		if got.Name != want.Name || got.Head != want.Head || got.Tail != want.Tail ||
			len(got.Match) != len(want.Match) {
			t.Errorf("rule %d: fixture %+v does not match source %+v", i, got, want)
			continue
		}
		for j := range want.Match {
			if got.Match[j] != want.Match[j] {
				t.Errorf(
					"rule %d (%s): match[%d] fixture %q != source %q",
					i,
					want.Name,
					j,
					got.Match[j],
					want.Match[j],
				)
			}
		}
	}
}

// fullRulesSidecarJSON returns the JSON encoding of the complete built-in
// rule table, exactly as EnsureAgentRuntime would generate it, for hooks
// under test that need every rule present (so a near-miss for rule N is
// verified against the SAME sidecar a matching command for rule N is).
func fullRulesSidecarJSON(t *testing.T) []byte {
	t.Helper()
	data, err := json.Marshal(baseapp.DefaultOutputBudgetRules)
	if err != nil {
		t.Fatalf("failed to marshal DefaultOutputBudgetRules: %v", err)
	}
	return data
}

func TestOutputBudgetHook_RuleDecisionParityOverEveryBuiltInRule(t *testing.T) {
	cases := loadOutputBudgetCases(t)
	rulesJSON := fullRulesSidecarJSON(t)

	h := newOutputBudgetTestHarness(t)
	sc := defaultTestSidecar()
	var rules any
	if err := json.Unmarshal(rulesJSON, &rules); err != nil {
		t.Fatalf("failed to unmarshal rules: %v", err)
	}
	sc.Rules = rules
	h.writeSidecar(sc)

	for _, rc := range cases.Rules {
		t.Run(rc.Name, func(t *testing.T) {
			_, stdout, stderr := h.run(t, rc.MatchingCommand, nil)
			if stdout == "" {
				t.Fatalf(
					"expected %q to match rule %s; stderr=%s",
					rc.MatchingCommand,
					rc.Name,
					stderr,
				)
			}
			got := rewrittenCommand(t, stdout)
			wantHead, wantTail := fmt.Sprint(rc.Head), fmt.Sprint(rc.Tail)
			if !containsAll(got, wantHead, wantTail) {
				t.Errorf(
					"rule %s: rewrite %q missing head/tail %s/%s",
					rc.Name,
					got,
					wantHead,
					wantTail,
				)
			}

			_, nearStdout, _ := h.run(t, rc.NearMissCommand, nil)
			if nearStdout != "" {
				t.Errorf(
					"near-miss %q for rule %s unexpectedly matched: %s",
					rc.NearMissCommand,
					rc.Name,
					nearStdout,
				)
			}
		})
	}
}

func TestOutputBudgetHook_TokenizationContractParity(t *testing.T) {
	cases := loadOutputBudgetCases(t)
	h := newOutputBudgetTestHarness(t)
	h.writeSidecar(defaultTestSidecar())

	for _, tc := range cases.TokenizationCases {
		t.Run(tc.Description, func(t *testing.T) {
			_, stdout, _ := h.run(t, tc.Command, nil)
			matched := stdout != ""
			if matched != tc.ShouldMatch {
				t.Errorf(
					"%q: matched=%v, want %v (stdout=%q)",
					tc.Command, matched, tc.ShouldMatch, stdout,
				)
			}
		})
	}
}

// TestOutputBudgetHook_NumericCaseParity is guide §10's out-of-range table,
// under the Claude hook: every invalid value must leave the command
// unmodified, and the one valid boundary value must still rewrite. The
// sidecar is built by direct string formatting, not json.Marshal — Go's
// json package would round-trip these huge literals through float64 and
// silently mangle them exactly the way the guide warns jq/JS can, which
// would test the wrong thing.
func TestOutputBudgetHook_NumericCaseParity(t *testing.T) {
	cases := loadOutputBudgetCases(t)
	for _, nc := range cases.NumericCases {
		t.Run(nc.Description, func(t *testing.T) {
			h := newOutputBudgetTestHarness(t)
			runnerPath := filepath.Join(h.homeDir, "output-budget-run.sh")
			writeExecutable(t, runnerPath, "#!/usr/bin/env bash\nexit 0\n")

			sidecarRaw := fmt.Sprintf(
				`{"outputBudget": true, "runner": %q, "lineContentLimit": %s, "maxTotalBytes": 65536, "captureContentLimit": 16777088, "rules": [{"name":"go-test","match":["go","test"],"head":30,"tail":120}]}`,
				runnerPath,
				nc.Value,
			)
			h.writeMalformedSidecar(sidecarRaw)

			code, stdout, stderr := h.run(t, "go test ./...", nil)
			matched := code == 0 && stdout != ""
			if matched != nc.Valid {
				t.Errorf(
					"value %q: matched=%v, want %v (stdout=%q stderr=%q)",
					nc.Value, matched, nc.Valid, stdout, stderr,
				)
			}
		})
	}
}

// TestOutputBudgetRun_NumericCaseParity is the same table against the
// runner's own defence-in-depth validation (guide §5.4): an invalid value in
// the lineContentLimit argv position must run the command unmodified, exit
// status preserved.
func TestOutputBudgetRun_NumericCaseParity(t *testing.T) {
	cases := loadOutputBudgetCases(t)
	scriptPath := writeOutputBudgetRunScript(t)

	for _, nc := range cases.NumericCases {
		t.Run(nc.Description, func(t *testing.T) {
			exitCode, stdout := runOutputBudgetRunRaw(
				t,
				scriptPath,
				"30",
				"120",
				nc.Value,
				"65536",
				"16777088",
				"printf 'x'; exit 6",
			)
			if nc.Valid {
				if exitCode != 6 || stdout != "x" {
					t.Errorf(
						"value %q (valid): exit=%d stdout=%q, want exit 6 stdout 'x'",
						nc.Value,
						exitCode,
						stdout,
					)
				}
			} else {
				if exitCode != 6 {
					t.Errorf(
						"value %q (invalid): exit=%d, want 6 (command still runs unmodified)",
						nc.Value,
						exitCode,
					)
				}
			}
		})
	}
}

func writeExecutable(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

// runOutputBudgetRunRaw invokes the runner with string argv values taken
// verbatim — unlike runOutputBudgetRun, which formats ints — because the
// out-of-range numeric cases include values wider than a 64-bit int
// (e.g. a 20-digit literal), which int can't even hold.
func runOutputBudgetRunRaw(
	t *testing.T, scriptPath string, args ...string,
) (exitCode int, stdout string) {
	t.Helper()
	cmd := exec.Command(scriptPath, args...)
	var stdoutBuf, stderrBuf bytes.Buffer
	cmd.Stdout = &stdoutBuf
	cmd.Stderr = &stderrBuf

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stdoutBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run output-budget-run.sh: %v\nstderr: %s", runErr, stderrBuf.String())
	}
	return exitErr.ExitCode(), stdoutBuf.String()
}
