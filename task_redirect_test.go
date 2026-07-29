package main

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// runTaskRedirectHook extracts configs/claude/task-redirect.sh from the
// embedded ConfigsFS (the same bytes that ship in the built binary — see
// TestTmuxDefaultCommandStaysResurrectSafe in embedded_test.go for the same
// against-the-embedded-FS pattern), runs it with a PreToolUse-shaped JSON
// payload on stdin, and returns its exit code and stderr.
//
// The script shells out to jq to parse its stdin payload, exactly as it does
// when Claude Code invokes it — jq is a required runtime dependency of the
// deployed hook (see docs/apps/claude.md), not a mocked external command, so
// running it here tests the shipped script's actual behavior rather than a
// stand-in. If jq isn't on PATH, the test is skipped rather than failing —
// the same posture the hook itself takes if jq is unavailable at runtime
// (documented fail-open behavior), and CI environments that lack it can't
// meaningfully validate this script anyway.
func runTaskRedirectHook(t *testing.T, command string) (exitCode int, stderr string) {
	t.Helper()
	// Default working dir for the process: this repo's own root (a devgeta
	// go.mod), reached by running with the test's own cwd. Global rules don't
	// care about it; the release- and worktree-gating tests below use the
	// cwd-aware helper.
	return runTaskRedirectHookInDir(t, command, "", "")
}

// runTaskRedirectHookInDir runs the hook with an explicit payload `cwd` field
// (payloadCwd, omitted when empty) and an explicit process working directory
// (procDir, inherited when empty). Both feed the devgeta-repo gate shared by
// the release rules AND the worktree-add/worktree-remove rules: the script
// reads `.cwd` from the payload and falls back to its own $PWD, so these two
// knobs exercise the gate's every input.
func runTaskRedirectHookInDir(
	t *testing.T,
	command, payloadCwd, procDir string,
) (exitCode int, stderr string) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping task-redirect.sh behavioral test")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/task-redirect.sh")
	if err != nil {
		t.Fatalf("failed to read embedded task-redirect.sh: %v", err)
	}

	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "task-redirect.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payloadMap := map[string]any{
		"tool_input": map[string]string{"command": command},
	}
	if payloadCwd != "" {
		payloadMap["cwd"] = payloadCwd
	}
	payload, err := json.Marshal(payloadMap)
	if err != nil {
		t.Fatalf("failed to marshal payload: %v", err)
	}

	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	if procDir != "" {
		cmd.Dir = procDir
	}
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdout = &bytes.Buffer{} // discard; must stay empty per the script's own contract

	runErr := cmd.Run()
	if runErr == nil {
		return 0, stderrBuf.String()
	}
	var exitErr *exec.ExitError
	if !isExitError(runErr, &exitErr) {
		t.Fatalf("failed to run task-redirect.sh for command %q: %v", command, runErr)
	}
	return exitErr.ExitCode(), stderrBuf.String()
}

// writeClaudeHookLib extracts configs/claude/lib/*.sh from the embedded
// ConfigsFS into <dir>/lib/, so a hook script written to a temp dir can
// `source "$SCRIPT_DIR/lib/..."` exactly as it does when deployed to
// ~/.claude/ (see internal/apps/claude.ForceConfigure's lib CopyDir). Every
// script under test here sources these unconditionally once past its own
// bypass-env-var check, so any test that doesn't set that bypass needs this.
func writeClaudeHookLib(t *testing.T, dir string) {
	t.Helper()
	libDir := filepath.Join(dir, "lib")
	if err := os.MkdirAll(libDir, 0o755); err != nil {
		t.Fatalf("failed to create lib dir: %v", err)
	}
	entries, err := fs.ReadDir(ConfigsFS, "configs/claude/lib")
	if err != nil {
		t.Fatalf("failed to read embedded configs/claude/lib: %v", err)
	}
	for _, entry := range entries {
		data, err := fs.ReadFile(ConfigsFS, filepath.Join("configs/claude/lib", entry.Name()))
		if err != nil {
			t.Fatalf("failed to read embedded configs/claude/lib/%s: %v", entry.Name(), err)
		}
		if err := os.WriteFile(filepath.Join(libDir, entry.Name()), data, 0o644); err != nil {
			t.Fatalf("failed to write lib/%s: %v", entry.Name(), err)
		}
	}
}

// repoRoot returns this test binary's repo root (the directory holding the
// devgeta go.mod), used as a real devgeta-cwd for the release-gating tests.
func repoRoot(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working directory: %v", err)
	}
	return wd
}

func isExitError(err error, target **exec.ExitError) bool {
	ee, ok := err.(*exec.ExitError)
	if ok {
		*target = ee
	}
	return ok
}

func TestTaskRedirectHook_AllowsLegitimateSingleCommands(t *testing.T) {
	allowed := []string{
		"git diff",
		"git diff HEAD~1",
		"git diff --stat",
		"git log",
		"git log -5",
		"git log --oneline",
		"git tag",
		"git tag v1.0.0",
		"git tag -l",
		"git reset --soft HEAD",
		"git worktree list",
		"git worktree prune",
		"git status",
		"git push origin main",
		"git commit -m \"fix: something\"",
		// Compound commands where no segment matches any rule.
		"cd some/dir && git status",
		"git fetch && git log -5",
		// A commit message mentioning a trigger word must not itself trigger
		// a rule — the "worktree" here is just message text, not a
		// `git worktree` invocation.
		"git commit -m \"fix: worktree stuff\"",
		// A commit message containing separator-like characters (';', '&&')
		// must not be split apart by the segmenter: the quoted span is one
		// segment, and neither half looks like a git invocation on its own.
		"git commit -m \"fix: a; b\"",
		// Pathological case for quote-aware splitting: this commit message
		// literally contains "&& git worktree" as message text. A naive
		// (non-quote-aware) splitter would slice this into a second segment
		// that starts with "git worktree" and falsely deny it. It's still a
		// single `git commit` command and must be allowed.
		"git commit -m \"notes && git worktree stuff\"",
		// gh commands that must NOT match the new gh rules: `pr view` is a
		// different subcommand from `pr review`/`pr checks`; `pr status`/`pr
		// list` are neither; a graphql query without reviewThreads and a bare
		// `gh api` are not the review-threads fetch.
		"gh pr view",
		"gh pr view --json title",
		"gh pr status",
		"gh pr list",
		"gh api graphql -f query='{ viewer { login } }'",
		"gh api repos/cjairm/devgeta",
		// Bare long-flag sanity check for the space-separated-long-flag
		// alternative added to DEVGETA_GH_GLOBAL_OPT: a bare boolean flag
		// followed by an unrelated command must not have that command
		// mistaken for the flag's "value".
		"git --no-pager status",
		"gh --paginate pr view",
	}
	for _, command := range allowed {
		t.Run(command, func(t *testing.T) {
			code, stderr := runTaskRedirectHook(t, command)
			if code != 0 {
				t.Errorf(
					"expected allow (exit 0) for %q, got exit %d, stderr=%q",
					command,
					code,
					stderr,
				)
			}
		})
	}
}

func TestTaskRedirectHook_DeniesNarrowPatterns(t *testing.T) {
	cases := []struct {
		command         string
		wantReplacement string
	}{
		{"git diff main..feature", "devgeta task review-package"},
		{"git diff v1.2.0..v1.3.0", "devgeta task review-package"},
		{"git diff --stat A..B", "devgeta task review-package"},
		{"git log --oneline base..head", "devgeta task review-package"},
		// The two worktree cases below also rely on this helper's default:
		// no payload cwd, process cwd the repo root (a devgeta go.mod) — so
		// the devgeta-repo gate they share with the release rules resolves
		// to "yes" here, same as TestTaskRedirectHook_WorktreeRulesGatedToDevgetaRepo
		// asserts explicitly.
		{"git worktree add ../wt -b feature-x", "devgeta task worktree-start"},
		{"git worktree remove ../wt", "devgeta task worktree-finish"},
		// New gh rules — all GLOBAL, so they deny regardless of cwd (this
		// helper runs with no payload cwd; the process cwd is the repo root,
		// but these rules never consult it).
		{"gh pr checks", "devgeta task pr-checks"},
		{"gh pr checks --watch", "devgeta task pr-checks"},
		{"gh pr review --approve", "devgeta task submit-review"},
		{"gh pr review --request-changes -b bad", "devgeta task submit-review"},
		{
			"gh api graphql --paginate -f query='{ repository { pullRequest { reviewThreads { nodes { id } } } } }'",
			"devgeta task review-threads",
		},
		// Compound commands: a matching segment anywhere in the chain must
		// deny, not just a bare command at position 0.
		{"cd some/dir && git worktree add ../wt -b x", "devgeta task worktree-start"},
		{"git status; git worktree remove ../wt", "devgeta task worktree-finish"},
		{"git fetch && git diff main..feature", "devgeta task review-package"},
		{"gh pr view && gh pr checks", "devgeta task pr-checks"},
		// git diff a..b | less: the LHS of the pipe is still a git
		// invocation itself, so this must deny too.
		{"git diff main..feature | less", "devgeta task review-package"},
		// Env-var-prefix case (no separator character before `git` at all):
		// deliberately handled now that the anchor is being reworked anyway
		// (see GIT_ANCHOR in task-redirect.sh / GIT_PREFIX in
		// task-redirect.js) — a simple `NAME=value` prefix, single or
		// repeated, in front of `git` still denies.
		{"GIT_PAGER=cat git diff main..feature", "devgeta task review-package"},
		{"FOO=bar BAZ=qux git worktree add ../wt -b x", "devgeta task worktree-start"},
		// Global-option-prefix case (same class of bypass a reviewer found in
		// secret-guard.sh, fixed here too): a git/gh global option between the
		// binary and its subcommand must not defeat the anchor.
		{"git -C ../wt worktree add ../other -b x", "devgeta task worktree-start"},
		{"gh -R owner/repo pr checks", "devgeta task pr-checks"},
		// Space-separated long-flag case (reviewer-found: the alternation only
		// recognized `--flag=value`/bare `--flag`, never `--flag value`).
		{"gh --repo owner/repo pr checks", "devgeta task pr-checks"},
		{"gh --repo=owner/repo pr checks", "devgeta task pr-checks"},
		// Regression-verify the three exact -C/-R anchor shapes named by this
		// cycle's hook-rescope plan (assert, do not re-implement — the fix
		// itself is the GIT_ANCHOR/GH_ANCHOR global-option handling above).
		// `git -C .` uses "." as the dir arg rather than a relative path, and
		// the gh case carries a trailing PR-number argument after `checks` —
		// neither shape was covered by an existing case above.
		{"git -C . worktree add wt", "devgeta task worktree-start"},
		{"git -C . diff main..HEAD", "devgeta task review-package"},
		{"gh -R o/r pr checks 1", "devgeta task pr-checks"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			code, stderr := runTaskRedirectHook(t, tc.command)
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) for %q, got exit %d, stderr=%q",
					tc.command,
					code,
					stderr,
				)
			}
			if !strings.Contains(stderr, tc.wantReplacement) {
				t.Errorf(
					"expected deny reason for %q to mention %q, got %q",
					tc.command,
					tc.wantReplacement,
					stderr,
				)
			}
			if !strings.Contains(stderr, "DEVGETA_SKIP_TASK_REDIRECT") {
				t.Errorf(
					"expected deny reason for %q to state the bypass escape hatch, got %q",
					tc.command,
					stderr,
				)
			}
			if !strings.Contains(stderr, "shell that launches this agent") ||
				!strings.Contains(stderr, "this hook reads its own environment") {
				t.Errorf(
					"expected deny reason for %q to contain the reworded bypass hint, got %q",
					tc.command,
					stderr,
				)
			}
		})
	}
}

// TestTaskRedirectHook_ReleaseRulesGatedToDevgetaRepo is the regression-proof
// assertion for this fix: the two release rules (git reset --soft HEAD~N, git
// tag -a v<semver>) deny ONLY when the command runs inside the devgeta repo,
// and allow the identical command everywhere else. "Inside devgeta" means a
// go.mod with module github.com/cjairm/devgeta found by walking up from the
// payload's cwd (falling back to the process $PWD). The gate fails toward NOT
// firing, so an indeterminate cwd allows the raw git through.
func TestTaskRedirectHook_ReleaseRulesGatedToDevgetaRepo(t *testing.T) {
	releaseCommands := []string{
		"git reset --soft HEAD~1",
		"git reset --soft HEAD~3",
		"git tag -a v0.12.0 -m release",
		"git tag -a -m release v0.12.0",
		"cd wt && git reset --soft HEAD~2",
		"git status && git tag -a v1.0.0 -m release",
	}

	devgetaDir := repoRoot(t) // this repo's own root has a devgeta go.mod

	// A non-devgeta dir with no go.mod, and one with a different module path —
	// both must ALLOW the release commands.
	noGoMod := t.TempDir()
	otherModule := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(otherModule, "go.mod"),
		[]byte("module github.com/other/thing\n\ngo 1.25\n"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write other go.mod: %v", err)
	}

	t.Run("devgeta cwd denies", func(t *testing.T) {
		for _, command := range releaseCommands {
			t.Run(command, func(t *testing.T) {
				code, stderr := runTaskRedirectHookInDir(t, command, devgetaDir, "")
				if code != 2 {
					t.Fatalf(
						"expected deny (exit 2) inside devgeta for %q, got exit %d, stderr=%q",
						command,
						code,
						stderr,
					)
				}
				if !strings.Contains(stderr, "devgeta task release") {
					t.Errorf(
						"expected deny reason to mention 'devgeta task release', got %q",
						stderr,
					)
				}
			})
		}
	})

	t.Run("non-devgeta cwd allows", func(t *testing.T) {
		for _, dir := range []string{noGoMod, otherModule} {
			for _, command := range releaseCommands {
				t.Run(dir+"/"+command, func(t *testing.T) {
					code, stderr := runTaskRedirectHookInDir(t, command, dir, "")
					if code != 0 {
						t.Fatalf(
							"expected allow (exit 0) outside devgeta for %q in %q, got exit %d, stderr=%q",
							command,
							dir,
							code,
							stderr,
						)
					}
				})
			}
		}
	})

	t.Run("no cwd field falls back to process PWD and allows outside devgeta", func(t *testing.T) {
		// No payload cwd; process cwd is a non-devgeta temp dir — the gate's
		// $PWD fallback must resolve to non-devgeta and allow.
		for _, command := range releaseCommands {
			t.Run(command, func(t *testing.T) {
				code, stderr := runTaskRedirectHookInDir(t, command, "", noGoMod)
				if code != 0 {
					t.Fatalf(
						"expected allow (exit 0) with no cwd and non-devgeta PWD for %q, got exit %d, stderr=%q",
						command,
						code,
						stderr,
					)
				}
			})
		}
	})
}

// TestTaskRedirectHook_WorktreeRulesGatedToDevgetaRepo is the regression-proof
// assertion for Task 1 of this cycle: the two worktree rules (git worktree
// add, git worktree remove) deny ONLY when the command runs inside the
// devgeta repo, and allow the identical command everywhere else — mirroring
// TestTaskRedirectHook_ReleaseRulesGatedToDevgetaRepo's structure exactly,
// since both rule pairs share the same is_devgeta_repo gate.
func TestTaskRedirectHook_WorktreeRulesGatedToDevgetaRepo(t *testing.T) {
	worktreeCommands := []struct {
		command         string
		wantReplacement string
	}{
		{"git worktree add ../wt -b x", "devgeta task worktree-start"},
		{"git worktree remove ../wt", "devgeta task worktree-finish"},
		{"cd some/dir && git worktree add ../wt -b x", "devgeta task worktree-start"},
	}

	devgetaDir := repoRoot(t) // this repo's own root has a devgeta go.mod

	// A non-devgeta dir with no go.mod, and one with a different module path —
	// both must ALLOW the worktree commands.
	noGoMod := t.TempDir()
	otherModule := t.TempDir()
	if err := os.WriteFile(
		filepath.Join(otherModule, "go.mod"),
		[]byte("module github.com/other/thing\n\ngo 1.25\n"),
		0o644,
	); err != nil {
		t.Fatalf("failed to write other go.mod: %v", err)
	}

	t.Run("devgeta cwd denies", func(t *testing.T) {
		for _, tc := range worktreeCommands {
			t.Run(tc.command, func(t *testing.T) {
				code, stderr := runTaskRedirectHookInDir(t, tc.command, devgetaDir, "")
				if code != 2 {
					t.Fatalf(
						"expected deny (exit 2) inside devgeta for %q, got exit %d, stderr=%q",
						tc.command,
						code,
						stderr,
					)
				}
				if !strings.Contains(stderr, tc.wantReplacement) {
					t.Errorf(
						"expected deny reason for %q to mention %q, got %q",
						tc.command,
						tc.wantReplacement,
						stderr,
					)
				}
			})
		}
	})

	t.Run("non-devgeta cwd allows", func(t *testing.T) {
		for _, dir := range []string{noGoMod, otherModule} {
			for _, tc := range worktreeCommands {
				t.Run(dir+"/"+tc.command, func(t *testing.T) {
					code, stderr := runTaskRedirectHookInDir(t, tc.command, dir, "")
					if code != 0 {
						t.Fatalf(
							"expected allow (exit 0) outside devgeta for %q in %q, got exit %d, stderr=%q",
							tc.command,
							dir,
							code,
							stderr,
						)
					}
				})
			}
		}
	})

	t.Run("no cwd field falls back to process PWD and allows outside devgeta", func(t *testing.T) {
		// No payload cwd; process cwd is a non-devgeta temp dir — the gate's
		// $PWD fallback must resolve to non-devgeta and allow.
		for _, tc := range worktreeCommands {
			t.Run(tc.command, func(t *testing.T) {
				code, stderr := runTaskRedirectHookInDir(t, tc.command, "", noGoMod)
				if code != 0 {
					t.Fatalf(
						"expected allow (exit 0) with no cwd and non-devgeta PWD for %q, got exit %d, stderr=%q",
						tc.command,
						code,
						stderr,
					)
				}
			})
		}
	})
}

// TestTaskRedirectHook_GlobalRulesUnaffectedByCwd proves the devgeta-repo
// scope gate added for the worktree/release rules (Task 1) didn't
// accidentally narrow the rules that were always global: gh pr checks and
// git diff <range> must still deny regardless of cwd, run here from a
// non-devgeta payload cwd to make that explicit.
func TestTaskRedirectHook_GlobalRulesUnaffectedByCwd(t *testing.T) {
	nonDevgetaDir := t.TempDir()

	cases := []struct {
		command         string
		wantReplacement string
	}{
		{"gh pr checks", "devgeta task pr-checks"},
		{"git diff main..feature", "devgeta task review-package"},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			code, stderr := runTaskRedirectHookInDir(t, tc.command, nonDevgetaDir, "")
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) outside devgeta for global rule %q, got exit %d, stderr=%q",
					tc.command,
					code,
					stderr,
				)
			}
			if !strings.Contains(stderr, tc.wantReplacement) {
				t.Errorf(
					"expected deny reason for %q to mention %q, got %q",
					tc.command,
					tc.wantReplacement,
					stderr,
				)
			}
		})
	}
}

func TestTaskRedirectHook_FailsOpenOnMalformedInput(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/task-redirect.sh")
	if err != nil {
		t.Fatalf("failed to read embedded task-redirect.sh: %v", err)
	}
	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "task-redirect.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	stdins := map[string]string{
		"empty stdin":             "",
		"not json":                "not json at all",
		"json without command":    `{"tool_input":{"file_path":"foo.go"}}`,
		"json with empty command": `{"tool_input":{"command":""}}`,
	}
	for name, stdin := range stdins {
		t.Run(name, func(t *testing.T) {
			cmd := exec.Command(scriptPath)
			cmd.Stdin = strings.NewReader(stdin)
			var stderrBuf bytes.Buffer
			cmd.Stderr = &stderrBuf
			if err := cmd.Run(); err != nil {
				t.Errorf(
					"expected the hook to fail open (exit 0) for %s, got error: %v (stderr=%q)",
					name,
					err,
					stderrBuf.String(),
				)
			}
		})
	}
}

func TestTaskRedirectHook_BypassEnvVarAllowsEverything(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/task-redirect.sh")
	if err != nil {
		t.Fatalf("failed to read embedded task-redirect.sh: %v", err)
	}
	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "task-redirect.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"tool_input": map[string]string{"command": "git worktree add ../wt -b x"},
	})
	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "DEVGETA_SKIP_TASK_REDIRECT=1")
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	if err := cmd.Run(); err != nil {
		t.Errorf(
			"expected bypass env var to allow a normally-denied command, got error: %v (stderr=%q)",
			err,
			stderrBuf.String(),
		)
	}
}

// denyMessageRe matches a double-quoted string starting with the literal
// prefix every deny message in both files uses ("Use: devgeta task"),
// regardless of whether it sits inline after `deny "..."` in bash, as a
// `message: "..."` field in the JS RULES array, or as an inline `return
// "..."` string for the two JS cases handled outside RULES.
var denyMessageRe = regexp.MustCompile(`"(Use: devgeta task[^"]*)"`)

// bypassHintRe matches BYPASS_HINT's value in both files. In bash this is
// `BYPASS_HINT="..."` on one line; in JS it's `const BYPASS_HINT =` on one
// line with the string literal on the next. Go's regexp \s already matches
// newlines (it is not restricted to non-dotall single-line semantics the way
// `.` is), so this single pattern works for both without a multiline flag —
// verified against both real files while developing this test, not assumed.
var bypassHintRe = regexp.MustCompile(`BYPASS_HINT\s*=\s*"([^"]+)"`)

// extractDenyMessageSet returns the set of every deny message found in text.
func extractDenyMessageSet(text string) map[string]bool {
	set := map[string]bool{}
	for _, match := range denyMessageRe.FindAllStringSubmatch(text, -1) {
		set[match[1]] = true
	}
	return set
}

// TestTaskRedirectShAndJsAgreeOnDenyMessages asserts
// configs/claude/task-redirect.sh and configs/opencode/plugin/task-redirect.js
// carry the same set of deny messages — the parity that CLAUDE.md's "Keeping
// the two AI agents in sync" rule requires, but that, before this test,
// nothing actually checked for this hook pair (unlike
// internal/apps/opencode/permissions_test.go, which only covers permission
// lists and formatter extensions).
//
// This is task-redirect-only, not table-driven like
// TestHookBypassTextParityAcrossAllPairs below: secret-guard.sh/.js and
// suppression-guard.sh/.js don't have a comparable "list of redirect
// messages" to diff — each has its own distinct deny-message shape (a staged
// filename/content match, a suppression-comment introduction) rather than a
// rule table of "Use: devgeta task ..." replacements.
//
// Scope of this claim, stated honestly: a set comparison catches the most
// likely drift — a rule added to one agent and not the other — but it does
// NOT prove the rule tables are equivalent. It cannot detect duplicate rules,
// rule ordering differences, divergent match patterns hiding behind identical
// messages, or one side's rule being gated to the devgeta repo while the
// other stays global. Those remain the job of the behavioral test suites
// elsewhere in this file and in task-redirect.test.mjs.
func TestTaskRedirectShAndJsAgreeOnDenyMessages(t *testing.T) {
	shBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/task-redirect.sh")
	if err != nil {
		t.Fatalf("failed to read embedded task-redirect.sh: %v", err)
	}
	jsBytes, err := fs.ReadFile(ConfigsFS, "configs/opencode/plugin/task-redirect.js")
	if err != nil {
		t.Fatalf("failed to read embedded task-redirect.js: %v", err)
	}
	shText, jsText := string(shBytes), string(jsBytes)

	shMessages := extractDenyMessageSet(shText)
	jsMessages := extractDenyMessageSet(jsText)
	if len(shMessages) == 0 || len(jsMessages) == 0 {
		t.Fatalf(
			"expected both files to yield at least one deny message, got sh=%d js=%d",
			len(shMessages),
			len(jsMessages),
		)
	}

	for msg := range shMessages {
		if !jsMessages[msg] {
			t.Errorf(
				"deny message present in task-redirect.sh but missing from task-redirect.js: %q",
				msg,
			)
		}
	}
	for msg := range jsMessages {
		if !shMessages[msg] {
			t.Errorf(
				"deny message present in task-redirect.js but missing from task-redirect.sh: %q",
				msg,
			)
		}
	}
}

// TestHookBypassTextParityAcrossAllPairs asserts that all three hook pairs —
// task-redirect, secret-guard, and suppression-guard — carry an IDENTICAL
// BYPASS_HINT string between their .sh and .js twins. Task 2 of this cycle
// reworded BYPASS_HINT in all six files (the bash trio and their JS twins),
// but until this test existed, only the task-redirect pair had a check that
// the two sides actually agree — a rewording fixed in one file and missed in
// its twin would have shipped silently for secret-guard or suppression-guard.
// bypassHintRe is reused as-is: it was already written generically (matches
// `BYPASS_HINT="..."` in bash and `const BYPASS_HINT = "..."` in JS), so
// widening this test needed no new regex.
func TestHookBypassTextParityAcrossAllPairs(t *testing.T) {
	pairs := []struct {
		name   string
		shPath string
		jsPath string
	}{
		{
			name:   "task-redirect",
			shPath: "configs/claude/task-redirect.sh",
			jsPath: "configs/opencode/plugin/task-redirect.js",
		},
		{
			name:   "secret-guard",
			shPath: "configs/claude/secret-guard.sh",
			jsPath: "configs/opencode/plugin/secret-guard.js",
		},
		{
			name:   "suppression-guard",
			shPath: "configs/claude/suppression-guard.sh",
			jsPath: "configs/opencode/plugin/suppression-guard.js",
		},
	}

	for _, pair := range pairs {
		t.Run(pair.name, func(t *testing.T) {
			shBytes, err := fs.ReadFile(ConfigsFS, pair.shPath)
			if err != nil {
				t.Fatalf("failed to read embedded %s: %v", pair.shPath, err)
			}
			jsBytes, err := fs.ReadFile(ConfigsFS, pair.jsPath)
			if err != nil {
				t.Fatalf("failed to read embedded %s: %v", pair.jsPath, err)
			}
			shText, jsText := string(shBytes), string(jsBytes)

			shBypassMatch := bypassHintRe.FindStringSubmatch(shText)
			if shBypassMatch == nil {
				t.Fatalf("failed to find BYPASS_HINT in %s", pair.shPath)
			}
			jsBypassMatch := bypassHintRe.FindStringSubmatch(jsText)
			if jsBypassMatch == nil {
				t.Fatalf("failed to find BYPASS_HINT in %s", pair.jsPath)
			}
			if shBypassMatch[1] != jsBypassMatch[1] {
				t.Errorf(
					"BYPASS_HINT differs between %s and %s:\n  sh: %q\n  js: %q",
					pair.shPath,
					pair.jsPath,
					shBypassMatch[1],
					jsBypassMatch[1],
				)
			}
		})
	}
}
