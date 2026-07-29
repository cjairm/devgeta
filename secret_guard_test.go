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

// initGitRepoWithStaged creates a fresh git repo in a temp dir, writes each
// file in `files` (path -> content) and stages it, then returns the repo
// dir. Real `git` is required and genuinely exercised here — same posture as
// task_redirect_test.go's real-`jq` requirement — because this hook's whole
// job is to read `git diff --cached`, not a mockable devgeta app boundary.
func initGitRepoWithStaged(t *testing.T, files map[string]string) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not found on PATH")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "test")
	for path, content := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("failed to create dir for %s: %v", path, err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
		run("add", path)
	}
	return dir
}

// initGitRepo creates a fresh, empty git repo (no staged files) and returns
// its dir — used as the "clean, unrelated" side of the -C target-resolution
// tests below.
func initGitRepo(t *testing.T) string {
	t.Helper()
	return initGitRepoWithStaged(t, nil)
}

// runSecretGuardHook extracts configs/claude/secret-guard.sh and its lib/
// dependencies from the embedded ConfigsFS, runs it with a PreToolUse-shaped
// JSON payload (command + cwd) on stdin, and returns its exit code and
// stderr. Mirrors runTaskRedirectHookInDir's pattern.
func runSecretGuardHook(t *testing.T, command, cwd string) (exitCode int, stderr string) {
	t.Helper()

	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH; skipping secret-guard.sh behavioral test")
	}

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/secret-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded secret-guard.sh: %v", err)
	}

	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "secret-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payload, err := json.Marshal(map[string]any{
		"tool_input": map[string]string{"command": command},
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
		t.Fatalf("failed to run secret-guard.sh for command %q: %v", command, runErr)
	}
	return exitErr.ExitCode(), stderrBuf.String()
}

func TestSecretGuardHook_AllowsNonCommitCommands(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	for _, command := range []string{"git status", "git add .env", "git diff --cached", "git log"} {
		t.Run(command, func(t *testing.T) {
			code, stderr := runSecretGuardHook(t, command, dir)
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

func TestSecretGuardHook_DeniesSensitiveStagedFilenames(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"dotenv", ".env"},
		{"dotenv with suffix", ".env.production"},
		{"pem", "certs/server.pem"},
		{"id_rsa", "id_rsa"},
		{"id_ed25519", "id_ed25519"},
		{"pfx", "cert.pfx"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := initGitRepoWithStaged(t, map[string]string{tc.path: "secret-shaped-content"})
			code, stderr := runSecretGuardHook(t, `git commit -m "add file"`, dir)
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) for staged %q, got exit %d, stderr=%q",
					tc.path,
					code,
					stderr,
				)
			}
			if !strings.Contains(stderr, "DEVGETA_SKIP_SECRET_GUARD") {
				t.Errorf("expected deny reason to state the bypass escape hatch, got %q", stderr)
			}
		})
	}
}

func TestSecretGuardHook_AllowsExcludedFilenameShapes(t *testing.T) {
	cases := []struct {
		name string
		path string
	}{
		{"env example template", ".env.example"},
		{"env sample template", ".env.sample"},
		{"env template template", ".env.template"},
		{"public key", "id_rsa.pub"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := initGitRepoWithStaged(t, map[string]string{tc.path: "not actually secret"})
			code, stderr := runSecretGuardHook(t, `git commit -m "add file"`, dir)
			if code != 0 {
				t.Errorf(
					"expected allow (exit 0) for staged %q, got exit %d, stderr=%q",
					tc.path,
					code,
					stderr,
				)
			}
		})
	}
}

func TestSecretGuardHook_DeniesSecretShapedContent(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			"private key header",
			"-----BEGIN RSA PRIVATE KEY-----\nMIIBogIBAAJ...\n-----END RSA PRIVATE KEY-----\n",
		},
		{"aws access key", "AWS_ACCESS_KEY_ID=AKIAABCDEFGHIJKLMNOP\n"},
		{"github token", "TOKEN=ghp_" + strings.Repeat("a", 36) + "\n"},
		{"slack token", "SLACK_TOKEN=xox" + "b-1234567890-abcdefghij\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := initGitRepoWithStaged(t, map[string]string{"config.go": tc.content})
			code, stderr := runSecretGuardHook(t, `git commit -m "add config"`, dir)
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) for content %q, got exit %d, stderr=%q",
					tc.content,
					code,
					stderr,
				)
			}
			if !strings.Contains(stderr, "DEVGETA_SKIP_SECRET_GUARD") {
				t.Errorf("expected deny reason to state the bypass escape hatch, got %q", stderr)
			}
		})
	}
}

// TestSecretGuardHook_RecognizesCommitDespiteGlobalOptions is the regression
// test for the original reviewer-found bypass: a bare anchor requiring
// "commit" to immediately follow "git" missed `git -c`/`--git-dir=`-prefixed
// invocations entirely. Neither of these cases overrides the scan target
// (only uppercase -C does — see the next test), so payload cwd (which holds
// the staged secret) is what gets scanned, and both must still deny.
func TestSecretGuardHook_RecognizesCommitDespiteGlobalOptions(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	cases := []string{
		`git -c user.name=x commit -m "x"`,
		`git --git-dir=/tmp/unrelated/.git commit -m "x"`,
	}
	for _, command := range cases {
		t.Run(command, func(t *testing.T) {
			code, stderr := runSecretGuardHook(t, command, dir)
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) for %q, got exit %d, stderr=%q",
					command,
					code,
					stderr,
				)
			}
		})
	}
}

// TestSecretGuardHook_ScansDashCTargetNotPayloadCwd is the regression test
// for a reviewer-found bypass: after the anchor fix above, `git -C <dir>
// commit` was correctly RECOGNIZED as a commit, but the hook still scanned
// its own payload cwd instead of <dir> — so it could check entirely the
// wrong repository. Both directions are proven: -C pointing AT the secret
// (from a clean cwd) must deny, and -C pointing AWAY from the secret (from
// the secret's own repo) must allow.
func TestSecretGuardHook_ScansDashCTargetNotPayloadCwd(t *testing.T) {
	secretDir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	cleanDir := initGitRepo(t)

	t.Run("-C targets the secret repo from a clean cwd: denies", func(t *testing.T) {
		command := `git -C ` + secretDir + ` commit -m "x"`
		code, stderr := runSecretGuardHook(t, command, cleanDir)
		if code != 2 {
			t.Fatalf(
				"expected deny (exit 2) for %q run from clean cwd %q, got exit %d, stderr=%q",
				command,
				cleanDir,
				code,
				stderr,
			)
		}
	})

	t.Run("-C targets a clean repo from the secret's own cwd: allows", func(t *testing.T) {
		command := `git -C ` + cleanDir + ` commit -m "x"`
		code, stderr := runSecretGuardHook(t, command, secretDir)
		if code != 0 {
			t.Fatalf(
				"expected allow (exit 0) for %q run from secret cwd %q, got exit %d, stderr=%q",
				command,
				secretDir,
				code,
				stderr,
			)
		}
	})
}

// TestSecretGuardHook_ScansCdTargetNotPayloadCwd is the regression test for
// a bypass found while manually verifying this hook end-to-end (not by
// external review): `cd <dir> && git commit` was checked against payload
// cwd, never <dir> — the command's REAL target, since PreToolUse fires
// before the `cd` (or anything else in the command) has run. Reproduced
// live: a Bash call of exactly this shape committed a staged .env straight
// through. Both directions are proven, same as the -C test above.
func TestSecretGuardHook_ScansCdTargetNotPayloadCwd(t *testing.T) {
	secretDir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	cleanDir := initGitRepo(t)

	t.Run("cd targets the secret repo from a clean cwd: denies", func(t *testing.T) {
		command := `cd ` + secretDir + ` && git commit -m "x"`
		code, stderr := runSecretGuardHook(t, command, cleanDir)
		if code != 2 {
			t.Fatalf(
				"expected deny (exit 2) for %q run from clean cwd %q, got exit %d, stderr=%q",
				command,
				cleanDir,
				code,
				stderr,
			)
		}
	})

	t.Run("cd targets a clean repo from the secret's own cwd: allows", func(t *testing.T) {
		command := `cd ` + cleanDir + ` && git commit -m "x"`
		code, stderr := runSecretGuardHook(t, command, secretDir)
		if code != 0 {
			t.Fatalf(
				"expected allow (exit 0) for %q run from secret cwd %q, got exit %d, stderr=%q",
				command,
				secretDir,
				code,
				stderr,
			)
		}
	})

	t.Run("chained cd resolves relative segments", func(t *testing.T) {
		// cd secretDir/.. (parent of secretDir) then cd into the basename —
		// proves relative `cd` segments chain off the PREVIOUS effective dir,
		// not off the original payload cwd.
		parent := filepath.Dir(secretDir)
		base := filepath.Base(secretDir)
		command := `cd ` + parent + ` && cd ` + base + ` && git commit -m "x"`
		code, stderr := runSecretGuardHook(t, command, cleanDir)
		if code != 2 {
			t.Fatalf(
				"expected deny (exit 2) for chained cd %q, got exit %d, stderr=%q",
				command,
				code,
				stderr,
			)
		}
	})
}

// TestSecretGuardHook_DeniesCompoundStagingWithCommit is the regression test
// for a reviewer-found bypass: this is a PreToolUse hook, so it runs BEFORE
// any part of the Bash command executes — for `git add -A && git commit`,
// `git diff --cached` at check time reflects neither command, since the add
// hasn't happened yet. The fix denies the compound shape outright (asking
// for two separate calls) rather than attempting to check it, so this must
// deny EVEN with a clean index (nothing secret is staged at all).
func TestSecretGuardHook_DeniesCompoundStagingWithCommit(t *testing.T) {
	dir := initGitRepo(t)
	if err := os.WriteFile(filepath.Join(dir, "readme.txt"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("failed to write readme.txt: %v", err)
	}

	code, stderr := runSecretGuardHook(t, `git add -A && git commit -m "x"`, dir)
	if code != 2 {
		t.Fatalf(
			"expected deny (exit 2) for compound add+commit even with a clean index, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
	if !strings.Contains(stderr, "separate commands") {
		t.Errorf(
			"expected deny reason to explain the two-separate-commands requirement, got %q",
			stderr,
		)
	}
}

// TestSecretGuardHook_DeniesSelfStagingCommitFlag is the regression test for
// the bare-command variant of the same bug: `git commit -a`/`-am`/`--all`
// auto-stages tracked working-tree changes AT COMMIT TIME, which is equally
// invisible to `git diff --cached` at check time — no compound command
// needed. Denies outright, same as the compound case, even with a clean
// index. `--amend` (no -a) must NOT be caught by this check.
func TestSecretGuardHook_DeniesSelfStagingCommitFlag(t *testing.T) {
	dir := initGitRepo(t)

	denyCases := []string{
		`git commit -a -m "x"`,
		`git commit -am "x"`,
		`git commit --all -m "x"`,
	}
	for _, command := range denyCases {
		t.Run(command, func(t *testing.T) {
			code, stderr := runSecretGuardHook(t, command, dir)
			if code != 2 {
				t.Fatalf(
					"expected deny (exit 2) for %q even with a clean index, got exit %d, stderr=%q",
					command,
					code,
					stderr,
				)
			}
			if !strings.Contains(stderr, "separate commands") {
				t.Errorf(
					"expected deny reason to explain the two-separate-commands requirement, got %q",
					stderr,
				)
			}
		})
	}

	t.Run(`git commit --amend -m "x" is not mistaken for self-staging`, func(t *testing.T) {
		code, stderr := runSecretGuardHook(t, `git commit --amend -m "x"`, dir)
		if code != 0 {
			t.Fatalf("expected allow (exit 0) for --amend, got exit %d, stderr=%q", code, stderr)
		}
	})
}

// TestSecretGuardHook_AllowsStagedDeletionOfSensitiveFile is the regression
// test for a reviewer-found false positive: `git diff --cached --name-only`
// lists deleted paths too, so staging the REMOVAL of a secret (the correct
// remediation for one already committed) was denied as if it were being
// added.
func TestSecretGuardHook_AllowsStagedDeletionOfSensitiveFile(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, out)
		}
	}
	run("commit", "-q", "-m", "add secret (test fixture)")
	run("rm", "--cached", "-q", ".env")

	code, stderr := runSecretGuardHook(t, `git commit -m "remove secret"`, dir)
	if code != 0 {
		t.Errorf(
			"expected allow (exit 0) for staging a deletion of a sensitive file, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

func TestSecretGuardHook_AllowsCleanCommit(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{"readme.txt": "hello world\n"})
	code, stderr := runSecretGuardHook(t, `git commit -m "add readme"`, dir)
	if code != 0 {
		t.Errorf("expected allow (exit 0) for a clean commit, got exit %d, stderr=%q", code, stderr)
	}
}

// TestSecretGuardHook_ScansWholeRepoNotJustCwdSubtree is the regression test
// for a reviewer-found bypass: the content check restricted its scan to the
// invoking cwd's subtree (`git diff --cached -- .`), but `git commit` with
// no pathspec commits the ENTIRE staged index regardless of cwd — so a
// secret staged at the repo root, committed from a subdirectory, went
// unchecked. The filename check never had this restriction; this proves
// the content check now matches it.
func TestSecretGuardHook_ScansWholeRepoNotJustCwdSubtree(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{
		"secret.txt": "TOKEN=ghp_" + strings.Repeat("a", 36) + "\n",
	})
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("failed to create subdirectory: %v", err)
	}
	code, stderr := runSecretGuardHook(t, `git commit -m "add secret from subdir"`, sub)
	if code != 2 {
		t.Fatalf(
			"expected deny (exit 2) for a secret staged at repo root when committing from a subdirectory, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

// TestSecretGuardHook_DeniesSecretInLargeStagedDiff is the regression test
// for a reviewer-found bypass specific to the OpenCode mirror (not this
// bash hook, which never buffered the diff into a bounded structure the
// same way) but tested here too for parity: a large staged diff must not
// let a real secret signature slip through unscanned.
func TestSecretGuardHook_DeniesSecretInLargeStagedDiff(t *testing.T) {
	var sb strings.Builder
	sb.WriteString("AKIAABCDEFGHIJKLMNOP\n")
	line := strings.Repeat("x", 200) + "\n"
	for sb.Len() < 2*1024*1024 {
		sb.WriteString(line)
	}
	dir := initGitRepoWithStaged(t, map[string]string{"big.txt": sb.String()})
	code, stderr := runSecretGuardHook(t, `git commit -m "add big file"`, dir)
	if code != 2 {
		t.Fatalf(
			"expected deny (exit 2) for a secret buried in a %d-byte staged diff, got exit %d, stderr=%q",
			sb.Len(),
			code,
			stderr,
		)
	}
}

func TestSecretGuardHook_CompoundCommandStillDenies(t *testing.T) {
	dir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})
	code, stderr := runSecretGuardHook(t, `git add -A && git commit -m "x"`, dir)
	if code != 2 {
		t.Fatalf(
			"expected deny (exit 2) for compound command, got exit %d, stderr=%q",
			code,
			stderr,
		)
	}
}

func TestSecretGuardHook_BypassEnvVarAllowsEverything(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}
	dir := initGitRepoWithStaged(t, map[string]string{".env": "SECRET=1"})

	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/secret-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded secret-guard.sh: %v", err)
	}
	scriptDir := t.TempDir()
	writeClaudeHookLib(t, scriptDir)
	scriptPath := filepath.Join(scriptDir, "secret-guard.sh")
	if err := os.WriteFile(scriptPath, scriptBytes, 0o755); err != nil {
		t.Fatalf("failed to write script: %v", err)
	}

	payload, _ := json.Marshal(map[string]any{
		"tool_input": map[string]string{"command": `git commit -m "x"`},
		"cwd":        dir,
	})
	cmd := exec.Command(scriptPath)
	cmd.Stdin = bytes.NewReader(payload)
	cmd.Env = append(os.Environ(), "DEVGETA_SKIP_SECRET_GUARD=1")
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

func TestSecretGuardHook_FailsOpenOnMalformedInput(t *testing.T) {
	if _, err := exec.LookPath("jq"); err != nil {
		t.Skip("jq not found on PATH")
	}
	scriptBytes, err := fs.ReadFile(ConfigsFS, "configs/claude/secret-guard.sh")
	if err != nil {
		t.Fatalf("failed to read embedded secret-guard.sh: %v", err)
	}
	dir := t.TempDir()
	writeClaudeHookLib(t, dir)
	scriptPath := filepath.Join(dir, "secret-guard.sh")
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
					"expected fail-open (exit 0) for %s, got error: %v (stderr=%q)",
					name,
					err,
					stderrBuf.String(),
				)
			}
		})
	}
}
