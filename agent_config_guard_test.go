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

// agentConfigGuardCase is one row of ADR-0014 §1's table, run against BOTH
// configs/claude/agent-config-guard.sh and configs/opencode/plugin/agent-config-guard.js
// with the same fixture and the same expected outcome. A behavioral run
// against both is the point (CLAUDE.md §12, ADR-0014's consequences): a
// string-level comparison of the two files cannot tell that one mirror
// resolves symlinks and the other forgot to.
type agentConfigGuardCase struct {
	name string
	tool string // "Edit" | "Write" (mapped to "edit"/"write" for the JS side)
	// setup creates whatever fixture this case needs under repo/home and
	// returns the absolute file path to target and the cwd to use.
	setup    func(t *testing.T, repo, home string) (filePath, cwd string)
	env      map[string]string
	wantDeny bool
}

func agentConfigGuardCases() []agentConfigGuardCase {
	j := filepath.Join
	return []agentConfigGuardCase{
		{
			name: "ordinary source file",
			tool: "Edit",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, "src"))
				p := j(repo, "src", "main.go")
				mustWriteFile(t, p, "")
				return p, repo
			},
			wantDeny: false,
		},
		{
			name: "row1: in-repo worktree source file is allowed",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "worktrees", "foo", "src"))
				p := j(repo, ".claude", "worktrees", "foo", "src", "main.go")
				return p, repo
			},
			wantDeny: false,
		},
		{
			name: "row2: .claude/agents/x.md",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "agents"))
				return j(repo, ".claude", "agents", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "row3: nested .claude/settings.json inside a worktree",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "worktrees", "foo", ".claude"))
				return j(repo, ".claude", "worktrees", "foo", ".claude", "settings.json"), repo
			},
			wantDeny: true,
		},
		{
			name: "row4: nested worktree-of-a-worktree source is allowed",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(
					t,
					j(repo, ".claude", "worktrees", "foo", ".claude", "worktrees", "bar"),
				)
				return j(
					repo,
					".claude",
					"worktrees",
					"foo",
					".claude",
					"worktrees",
					"bar",
					"a.go",
				), repo
			},
			wantDeny: false,
		},
		{
			name: "row5: ~/.claude/settings.json absolute",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				return j(home, ".claude", "settings.json"), repo
			},
			wantDeny: true,
		},
		{
			name: "row6: lexical .. escape out of worktrees",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "worktrees"))
				return j(repo, ".claude", "worktrees", "..", "agents", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "row7: symlink-in-worktree resolves into .claude/agents",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "agents"))
				mustMkdirAll(t, j(repo, ".claude", "worktrees", "foo"))
				mustSymlink(
					t,
					j(repo, ".claude", "agents"),
					j(repo, ".claude", "worktrees", "foo", "link"),
				)
				return j(repo, ".claude", "worktrees", "foo", "link", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "row8: an innocent-looking path reaches a denied target via a symlink",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "agents"))
				mustMkdirAll(t, j(repo, "src"))
				mustSymlink(t, j(repo, ".claude", "agents"), j(repo, "src", "link"))
				return j(repo, "src", "link", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: ~/.claude/projects/<slug>/memory/MEMORY.md is allowed",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj", "memory"))
				return j(home, ".claude", "projects", "-Users-me-proj", "memory", "MEMORY.md"), repo
			},
			wantDeny: false,
		},
		{
			name: "memory: a nested file under memory/ is allowed",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj", "memory", "sub"))
				return j(
					home, ".claude", "projects", "-Users-me-proj", "memory", "sub", "note.md",
				), repo
			},
			wantDeny: false,
		},
		{
			name: "memory: the exception is memory/ only, not the rest of projects/<slug>/",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj"))
				return j(home, ".claude", "projects", "-Users-me-proj", "settings.json"), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: memory/ must sit exactly one segment under projects/",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "memory"))
				return j(home, ".claude", "memory", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: memory itself as a file, with nothing below it, stays denied",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj"))
				return j(home, ".claude", "projects", "-Users-me-proj", "memory"), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: lexical .. escape out of memory/ is denied",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj", "memory"))
				return j(
					home, ".claude", "projects", "-Users-me-proj", "memory",
					"..", "..", "..", "settings.json",
				), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: a symlink under memory/ into ~/.claude/agents is denied",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".claude", "agents"))
				mustMkdirAll(t, j(home, ".claude", "projects", "-Users-me-proj", "memory"))
				mustSymlink(
					t,
					j(home, ".claude", "agents"),
					j(home, ".claude", "projects", "-Users-me-proj", "memory", "link"),
				)
				return j(
					home, ".claude", "projects", "-Users-me-proj", "memory", "link", "x.md",
				), repo
			},
			wantDeny: true,
		},
		{
			name: "memory: a nested .claude under memory/ is still evaluated on its own",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(
					t,
					j(home, ".claude", "projects", "-Users-me-proj", "memory", ".claude"),
				)
				return j(
					home, ".claude", "projects", "-Users-me-proj", "memory",
					".claude", "settings.json",
				), repo
			},
			wantDeny: true,
		},
		{
			name: "row9: .opencode/agent/x.md",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".opencode", "agent"))
				return j(repo, ".opencode", "agent", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "row10: .opencode/worktrees carries no exception",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".opencode", "worktrees", "foo", "src"))
				return j(repo, ".opencode", "worktrees", "foo", "src", "main.go"), repo
			},
			wantDeny: true,
		},
		{
			name: "row11: OPENCODE_CONFIG_DIR is an additional denied root",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				dir := j(filepath.Dir(repo), "oc-alt")
				mustMkdirAll(t, dir)
				return j(dir, "opencode.json"), repo
			},
			wantDeny: true,
			env:      map[string]string{"OPENCODE_CONFIG_DIR": "__OC_ALT__"},
		},
		{
			name: "row12a: OPENCODE_CONFIG relative to cwd",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				dir := j(filepath.Dir(repo), "ocfg")
				mustMkdirAll(t, dir)
				p := j(dir, "relcfg.json")
				mustWriteFile(t, p, "")
				return p, dir
			},
			wantDeny: true,
			env:      map[string]string{"OPENCODE_CONFIG": "relcfg.json"},
		},
		{
			name: "row12b: OPENCODE_CONFIG resolves through a symlink",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				dir := j(filepath.Dir(repo), "ocfg2")
				mustMkdirAll(t, dir)
				real := j(dir, "relcfg.json")
				mustWriteFile(t, real, "")
				mustSymlink(t, real, j(dir, "symcfg.json"))
				return real, dir
			},
			wantDeny: true,
			env:      map[string]string{"OPENCODE_CONFIG": "symcfg.json"},
		},
		{
			name: "row14: default OpenCode global config root",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				return j(home, ".config", "opencode", "opencode.json"), repo
			},
			wantDeny: true,
		},
		{
			name: "row15: OpenCode's plugin directory (the guards' own home)",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				return j(home, ".config", "opencode", "plugin", "secret-guard.js"), repo
			},
			wantDeny: true,
		},
		{
			name: "row16: custom XDG_CONFIG_HOME resolves the root, not a literal ~/.config",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				dir := j(filepath.Dir(repo), "xdgcfg")
				mustMkdirAll(t, j(dir, "opencode"))
				return j(dir, "opencode", "opencode.json"), repo
			},
			wantDeny: true,
			env:      map[string]string{"XDG_CONFIG_HOME": "__XDG_ALT__"},
		},
		{
			name: "sanity: .mcp.json is the settings-floor's job, not this guard's",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				return j(repo, ".mcp.json"), repo
			},
			wantDeny: false,
		},
		{
			name: "Edit tool denies identically to Write",
			tool: "Edit",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "agents"))
				return j(repo, ".claude", "agents", "x.md"), repo
			},
			wantDeny: true,
		},
		{
			name: "substring: .claude-backup is not .claude",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude-backup"))
				return j(repo, ".claude-backup", "x.md"), repo
			},
			wantDeny: false,
		},
		{
			name: "prefix: opencode-other must not match the opencode root",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(home, ".config", "opencode-other"))
				return j(home, ".config", "opencode-other", "x.json"), repo
			},
			wantDeny: false,
		},
		{
			name: "symlink leaf is itself a file, not a directory",
			tool: "Edit",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustMkdirAll(t, j(repo, ".claude", "agents"))
				mustMkdirAll(t, j(repo, "src"))
				real := j(repo, ".claude", "agents", "real.json")
				mustWriteFile(t, real, "")
				mustSymlink(t, real, j(repo, "src", "cfg-link.json"))
				return j(repo, "src", "cfg-link.json"), repo
			},
			wantDeny: true,
		},
		{
			name: "symlink loop under the repo root falls back safely, never hangs",
			tool: "Write",
			setup: func(t *testing.T, repo, home string) (string, string) {
				mustSymlink(t, j(repo, "loop-b"), j(repo, "loop-a"))
				mustSymlink(t, j(repo, "loop-a"), j(repo, "loop-b"))
				return j(repo, "loop-a", "x.md"), repo
			},
			wantDeny: false,
		},
	}
}

func mustMkdirAll(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("failed to create %s: %v", dir, err)
	}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("failed to create parent of %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write %s: %v", path, err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
		t.Fatalf("failed to create parent of %s: %v", link, err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("failed to symlink %s -> %s: %v", link, target, err)
	}
}

// resolveCaseEnv substitutes the fixture placeholders a case's env map may
// reference — OPENCODE_CONFIG_DIR and XDG_CONFIG_HOME point at directories
// only known once the fixture is built, and the setup func above puts them
// as siblings of repo (filepath.Dir(repo)/oc-alt, .../xdgcfg).
func resolveCaseEnv(env map[string]string, repo string) map[string]string {
	if env == nil {
		return nil
	}
	out := make(map[string]string, len(env))
	for k, v := range env {
		switch v {
		case "__OC_ALT__":
			v = filepath.Join(filepath.Dir(repo), "oc-alt")
		case "__XDG_ALT__":
			v = filepath.Join(filepath.Dir(repo), "xdgcfg")
		}
		out[k] = v
	}
	return out
}

// runAgentConfigGuardShHook extracts configs/claude/agent-config-guard.sh
// from the embedded ConfigsFS, runs it with a PreToolUse-shaped payload on
// stdin, and returns its exit code and stderr. Mirrors
// runSuppressionGuardHook. This guard has no devgeta-repo gating (it is
// GLOBAL — ADR-0014 §2) and sources no shared lib, so it needs no
// writeClaudeHookLib call.
func runAgentConfigGuardShHook(
	t *testing.T, toolName, filePath, cwd string, env map[string]string,
) (exitCode int, stderr string) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping agent-config-guard.sh behavioral test")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/agent-config-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded agent-config-guard.sh: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agent-config-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"tool_name":  toolName,
		"tool_input": map[string]string{"file_path": filePath},
		"cwd":        cwd,
	})
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &bytes.Buffer{}

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run agent-config-guard.sh: %v", runErr)
	}
	return exitErr.ExitCode(), stderrBuf.String()
}

// agentConfigGuardJSDriver imports the real plugin file by absolute path
// and drives its exported factory exactly as OpenCode's plugin loader would
// call "tool.execute.before" — exit 0 for allow, exit 2 (matching the bash
// guard's deny code, for a uniform Go-side assertion) for deny, with the
// thrown message on stderr.
const agentConfigGuardJSDriver = `
const { AgentConfigGuard } = await import(process.env.DG_GUARD_JS_PATH);
const guard = await AgentConfigGuard({ directory: process.env.DG_GUARD_CWD });
try {
  await guard["tool.execute.before"](
    { tool: process.env.DG_GUARD_TOOL },
    { args: { filePath: process.env.DG_GUARD_FILE_PATH } },
  );
  process.exit(0);
} catch (e) {
  process.stderr.write(String(e && e.message ? e.message : e));
  process.exit(2);
}
`

// runAgentConfigGuardJSHook extracts configs/opencode/plugin/agent-config-guard.js
// from the embedded ConfigsFS and drives it via `node --input-type=module`
// fed the driver script above on stdin — the same mechanism the cycle
// doc's manual verification uses, made repeatable. Executing the JS mirror
// (rather than only diffing it against the .sh textually, as
// task_redirect_test.go does for the OTHER three guards) is deliberate: a
// string comparison cannot tell that one mirror resolves symlinks and the
// other forgot to.
func runAgentConfigGuardJSHook(
	t *testing.T, toolName, filePath, cwd string, env map[string]string,
) (exitCode int, stderr string) {
	t.Helper()

	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node not found on PATH; skipping agent-config-guard.js behavioral test")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/opencode/plugin/agent-config-guard.js")
	if err != nil {
		t.Fatalf("failed to read embedded agent-config-guard.js: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agent-config-guard.js")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o644); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	tool := strings.ToLower(toolName)

	cmd := exec.Command("node", "--input-type=module")
	cmd.Stdin = strings.NewReader(agentConfigGuardJSDriver)
	cmd.Env = append(
		os.Environ(),
		"DG_GUARD_JS_PATH="+scriptPath,
		"DG_GUARD_CWD="+cwd,
		"DG_GUARD_TOOL="+tool,
		"DG_GUARD_FILE_PATH="+filePath,
	)
	for k, v := range env {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &bytes.Buffer{}

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run agent-config-guard.js: %v", runErr)
	}
	return exitErr.ExitCode(), stderrBuf.String()
}

// TestAgentConfigGuard_Matrix runs every ADR-0014 §1 table row against BOTH
// guard implementations. A dead pattern or a canonicalization bug in either
// mirror fails here, not just a textual diff between the two files.
func TestAgentConfigGuard_Matrix(t *testing.T) {
	for _, tc := range agentConfigGuardCases() {
		t.Run(tc.name, func(t *testing.T) {
			repo := filepath.Join(t.TempDir(), "repo")
			home := filepath.Join(t.TempDir(), "home")
			mustMkdirAll(t, repo)
			mustMkdirAll(t, filepath.Join(home, ".claude"))
			mustMkdirAll(t, filepath.Join(home, ".config"))

			filePath, cwd := tc.setup(t, repo, home)

			// pkg/paths sandboxes the TEST BINARY's own process environment
			// (os.Setenv on XDG_CONFIG_HOME et al. — see paths.go's
			// testSandbox) so `go test` can never touch a real user
			// directory. That sandbox env is inherited by os.Environ() and
			// therefore by every guard subprocess below unless explicitly
			// overridden here — a case testing the DEFAULT root (no
			// override) would otherwise silently resolve against the
			// sandbox's XDG_CONFIG_HOME instead of this fixture's HOME.
			envWithHome := map[string]string{
				"HOME":                home,
				"XDG_CONFIG_HOME":     filepath.Join(home, ".config"),
				"OPENCODE_CONFIG_DIR": "",
				"OPENCODE_CONFIG":     "",
			}
			for k, v := range resolveCaseEnv(tc.env, repo) {
				envWithHome[k] = v
			}

			t.Run("sh", func(t *testing.T) {
				code, stderr := runAgentConfigGuardShHook(t, tc.tool, filePath, cwd, envWithHome)
				assertGuardOutcome(t, tc.wantDeny, code, stderr)
			})
			t.Run("js", func(t *testing.T) {
				code, stderr := runAgentConfigGuardJSHook(t, tc.tool, filePath, cwd, envWithHome)
				assertGuardOutcome(t, tc.wantDeny, code, stderr)
			})
		})
	}
}

func assertGuardOutcome(t *testing.T, wantDeny bool, code int, stderr string) {
	t.Helper()
	gotDeny := code == 2
	if gotDeny != wantDeny {
		want := "allow (exit 0)"
		if wantDeny {
			want = "deny (exit 2)"
		}
		t.Errorf("expected %s, got exit %d, stderr=%q", want, code, stderr)
	}
}

func TestAgentConfigGuard_BypassEnvVarAllowsEverything(t *testing.T) {
	repo := filepath.Join(t.TempDir(), "repo")
	home := filepath.Join(t.TempDir(), "home")
	mustMkdirAll(t, filepath.Join(repo, ".claude", "agents"))
	mustMkdirAll(t, filepath.Join(home, ".claude"))
	filePath := filepath.Join(repo, ".claude", "agents", "x.md")

	env := map[string]string{"HOME": home, "DEVGETA_SKIP_AGENT_CONFIG_GUARD": "1"}

	code, stderr := runAgentConfigGuardShHook(t, "Write", filePath, repo, env)
	assertGuardOutcome(t, false, code, stderr)

	code, stderr = runAgentConfigGuardJSHook(t, "Write", filePath, repo, env)
	assertGuardOutcome(t, false, code, stderr)
}

func TestAgentConfigGuardShHook_FailsOpenOnMalformedInput(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/agent-config-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded agent-config-guard.sh: %v", err)
	}
	dir := t.TempDir()
	scriptPath := filepath.Join(dir, "agent-config-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	stdins := map[string]string{
		"empty stdin":       "",
		"not json":          "not json at all",
		"json without tool": `{"tool_input":{"file_path":"foo.go"}}`,
		"unrelated tool":    `{"tool_name":"Bash","tool_input":{"command":"echo hi"}}`,
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
					name, err, stderrBuf.String(),
				)
			}
		})
	}
}
