package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/downloader"
)

func TestVerifySHA256(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "tool.tar.gz")
	content := []byte("payload")
	if err := os.WriteFile(archive, content, 0o644); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	hexSum := hex.EncodeToString(sum[:])

	writeChecksums := func(t *testing.T, body string) string {
		t.Helper()
		p := filepath.Join(t.TempDir(), "checksums.txt")
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}

	t.Run("match", func(t *testing.T) {
		p := writeChecksums(t, fmt.Sprintf("%s  tool.tar.gz\n", hexSum))
		if err := verifySHA256(archive, p, "tool.tar.gz"); err != nil {
			t.Fatalf("expected match, got: %v", err)
		}
	})

	t.Run("binary-mode marker", func(t *testing.T) {
		p := writeChecksums(t, fmt.Sprintf("%s *tool.tar.gz\n", hexSum))
		if err := verifySHA256(archive, p, "tool.tar.gz"); err != nil {
			t.Fatalf("expected match with * marker, got: %v", err)
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		p := writeChecksums(t, fmt.Sprintf("%s  tool.tar.gz\n", strings.Repeat("0", 64)))
		err := verifySHA256(archive, p, "tool.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "mismatch") {
			t.Fatalf("expected mismatch error, got: %v", err)
		}
	})

	t.Run("missing entry", func(t *testing.T) {
		p := writeChecksums(t, fmt.Sprintf("%s  other.tar.gz\n", hexSum))
		err := verifySHA256(archive, p, "tool.tar.gz")
		if err == nil || !strings.Contains(err.Error(), "no checksum entry") {
			t.Fatalf("expected missing-entry error, got: %v", err)
		}
	})
}

// TestRunInstallScript_StagedShape asserts the successful path calls curl
// (download to a temp file, carrying InstallScriptDownloadTimeout) followed
// by the interpreter (executing that file, carrying InstallScriptExecTimeout)
// — the shape that replaces a raw `curl | sh` pipeline whose exit status was
// the interpreter's, not curl's.
func TestRunInstallScript_StagedShape(t *testing.T) {
	base := NewMockBaseCommand()

	if err := RunInstallScript(base, "opencode", "https://opencode.ai/install", "sh"); err != nil {
		t.Fatalf("RunInstallScript error: %v", err)
	}

	calls := base.ExecCommandCalls
	if len(calls) != 2 {
		t.Fatalf("expected 2 ExecCommand calls (download, execute), got %d", len(calls))
	}

	download := calls[0]
	if download.Command != "curl" {
		t.Errorf("expected download step command 'curl', got %q", download.Command)
	}
	if download.Timeout != InstallScriptDownloadTimeout {
		t.Errorf(
			"expected download step Timeout %v, got %v",
			InstallScriptDownloadTimeout, download.Timeout,
		)
	}
	if !download.NoStdin {
		t.Error("expected download step to set NoStdin")
	}

	execute := calls[1]
	if execute.Command != "sh" {
		t.Errorf("expected execute step command 'sh', got %q", execute.Command)
	}
	if execute.Timeout != InstallScriptExecTimeout {
		t.Errorf(
			"expected execute step Timeout %v, got %v",
			InstallScriptExecTimeout, execute.Timeout,
		)
	}
	if !execute.NoStdin {
		t.Error("expected execute step to set NoStdin")
	}
}

// TestRunInstallScript_FailedDownloadBlocksInstall is the regression test for
// the defect this step fixes: `bash -c 'false | bash'; echo $?` prints 0, so
// a failed or truncated download used to be reported as a successful
// install. Staging must fail the install and must never reach the execute
// step.
func TestRunInstallScript_FailedDownloadBlocksInstall(t *testing.T) {
	base := NewMockBaseCommand()
	base.SetExecCommandResults(
		ExecCommandResult(
			"",
			"curl: (22) The requested URL returned error: 404",
			errors.New("exit status 22"),
		),
	)

	err := RunInstallScript(base, "opencode", "https://opencode.ai/install", "sh")
	if err == nil {
		t.Fatal("expected RunInstallScript to fail when the download step fails")
	}
	if !strings.Contains(err.Error(), "failed to download install script") {
		t.Errorf("expected download-failure error, got: %v", err)
	}
	if got := base.GetExecCommandCallCount(); got != 1 {
		t.Errorf(
			"expected the execute step to be skipped after a failed download, got %d calls",
			got,
		)
	}
}

// TestRunInstallScript_FailedExecuteFailsInstall covers the execute step's
// own failure path, distinct from the download failure above.
func TestRunInstallScript_FailedExecuteFailsInstall(t *testing.T) {
	base := NewMockBaseCommand()
	base.SetExecCommandResults(
		ExecCommandResult("", "", nil),                                // download succeeds
		ExecCommandResult("", "install failed", errors.New("exit 1")), // execute fails
	)

	err := RunInstallScript(base, "opencode", "https://opencode.ai/install", "sh")
	if err == nil {
		t.Fatal("expected RunInstallScript to fail when the execute step fails")
	}
	if !strings.Contains(err.Error(), "install script failed") {
		t.Errorf("expected execute-failure error, got: %v", err)
	}
	if got := base.GetExecCommandCallCount(); got != 2 {
		t.Errorf("expected both steps to run, got %d calls", got)
	}
}

// TestInstallScriptStrategy_Install exercises the strategy wrapper end to
// end: it is the first test for InstallScriptStrategy, which had none before
// this step. A mocked 404 on the download step must fail the install rather
// than being silently reported as a success.
func TestInstallScriptStrategy_Install(t *testing.T) {
	t.Run("success stages download then execute", func(t *testing.T) {
		base := NewMockBaseCommand()
		strategy := &InstallScriptStrategy{cmd: base, scriptURL: "https://opencode.ai/install"}

		if err := strategy.Install("opencode"); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		if got := base.GetExecCommandCallCount(); got != 2 {
			t.Fatalf("expected 2 ExecCommand calls, got %d", got)
		}
		last := base.GetLastExecCommandCall()
		if last.Command != "sh" {
			t.Errorf("expected execute step via 'sh', got %q", last.Command)
		}
	})

	t.Run("404 on download blocks install", func(t *testing.T) {
		base := NewMockBaseCommand()
		base.SetExecCommandResults(
			ExecCommandResult("", "404", errors.New("exit status 22")),
		)
		strategy := &InstallScriptStrategy{cmd: base, scriptURL: "https://opencode.ai/install"}

		err := strategy.Install("opencode")
		if err == nil {
			t.Fatal("expected Install to fail when the download step returns a 404")
		}
		if got := base.GetExecCommandCallCount(); got != 1 {
			t.Errorf(
				"expected the execute step to be skipped after a failed download, got %d calls",
				got,
			)
		}
	})
}

// TestNerdFontStrategy_Install_RoutesExtractAndCacheThroughExecutor asserts
// the tar extraction and fc-cache steps run through the mocked executor
// (rather than a raw, unmockable exec.Command) and that a failing tar
// extraction fails the install while a failing fc-cache does not (matching
// the pre-existing non-fatal behavior for fc-cache).
func TestNerdFontStrategy_Install_RoutesExtractAndCacheThroughExecutor(t *testing.T) {
	// fakeDownload stands in for the real network download (Step 11's
	// concern, not this task's) so these tests never make a real HTTP
	// request just to reach the tar/fc-cache steps this task changed.
	fakeDownload := func(_ context.Context, _, dest string, _ downloader.RetryConfig) error {
		return os.WriteFile(dest, []byte("fake archive"), 0o644)
	}

	t.Run("tar failure fails the install", func(t *testing.T) {
		base := NewMockBaseCommand()
		base.SetExecCommandResults(
			ExecCommandResult("", "tar: unexpected EOF", errors.New("exit 2")),
		)
		strategy := &NerdFontStrategy{
			cmd:        base,
			archiveURL: "https://github.com/example/font.tar.xz",
			downloadFn: fakeDownload,
		}

		err := strategy.Install("FiraCode")
		if err == nil || !strings.Contains(err.Error(), "failed to extract font archive") {
			t.Fatalf("expected extract failure error, got: %v", err)
		}
	})

	t.Run("fc-cache failure is non-fatal", func(t *testing.T) {
		base := NewMockBaseCommand()
		base.SetExecCommandResults(
			ExecCommandResult("", "", nil),                                   // tar succeeds
			ExecCommandResult("", "fc-cache exploded", errors.New("exit 1")), // fc-cache fails
		)
		strategy := &NerdFontStrategy{
			cmd:        base,
			archiveURL: "https://github.com/example/font.tar.xz",
			downloadFn: fakeDownload,
		}

		if err := strategy.Install("FiraCode"); err != nil {
			t.Fatalf("expected fc-cache failure to be non-fatal, got: %v", err)
		}
		if got := base.GetExecCommandCallCount(); got != 2 {
			t.Errorf("expected both tar and fc-cache to run, got %d calls", got)
		}
	})
}

// TestGitCloneStrategy_Install_RoutesThroughExecutor asserts the clone runs
// through the mocked executor with a non-zero Timeout — a git clone is a
// network operation that can otherwise hang indefinitely — and that a
// failure is surfaced as an install error.
func TestGitCloneStrategy_Install_RoutesThroughExecutor(t *testing.T) {
	dir := t.TempDir()
	installPath := filepath.Join(dir, "does-not-exist-yet")

	t.Run("success", func(t *testing.T) {
		base := NewMockBaseCommand()
		strategy := &GitCloneStrategy{
			cmd:         base,
			repoURL:     "https://github.com/example/repo.git",
			installPath: installPath,
		}

		if err := strategy.Install("repo"); err != nil {
			t.Fatalf("Install error: %v", err)
		}
		last := base.GetLastExecCommandCall()
		if last == nil || last.Command != "git" {
			t.Fatalf("expected a 'git' ExecCommand call, got %v", last)
		}
		if last.Timeout == 0 {
			t.Error("expected git clone to carry a non-zero Timeout")
		}
	})

	t.Run("clone failure fails the install", func(t *testing.T) {
		base := NewMockBaseCommand()
		base.SetExecCommandResults(
			ExecCommandResult("", "fatal: unable to access", errors.New("exit 128")),
		)
		strategy := &GitCloneStrategy{
			cmd:         base,
			repoURL:     "https://github.com/example/repo.git",
			installPath: installPath,
		}

		err := strategy.Install("repo")
		if err == nil || !strings.Contains(err.Error(), "git clone failed") {
			t.Fatalf("expected git clone failure error, got: %v", err)
		}
	})
}

func TestInstallGitHubBinary_RefusesUnverified(t *testing.T) {
	t.Run("checksum mismatch blocks install", func(t *testing.T) {
		base := NewMockBaseCommand()
		dl := func(_ context.Context, _, dest string, _ downloader.RetryConfig) error {
			if strings.HasSuffix(dest, "-checksums.txt") {
				line := fmt.Sprintf("%s  tool.tar.gz\n", strings.Repeat("0", 64))
				return os.WriteFile(dest, []byte(line), 0o644)
			}
			return os.WriteFile(dest, []byte("payload"), 0o644)
		}

		err := InstallGitHubBinary(
			base, "tool",
			"https://example.com/releases/tool.tar.gz",
			"https://example.com/releases/checksums.txt",
			dl,
		)
		if err == nil || !strings.Contains(err.Error(), "checksum verification") {
			t.Fatalf("expected checksum verification error, got: %v", err)
		}
		if got := base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no extract/install commands after failed verification, got %d", got)
		}
	})

	t.Run("failed checksums download blocks install", func(t *testing.T) {
		base := NewMockBaseCommand()
		dl := func(_ context.Context, _, dest string, _ downloader.RetryConfig) error {
			if strings.HasSuffix(dest, "-checksums.txt") {
				return fmt.Errorf("404 not found")
			}
			return os.WriteFile(dest, []byte("payload"), 0o644)
		}

		err := InstallGitHubBinary(
			base, "tool",
			"https://example.com/releases/tool.tar.gz",
			"https://example.com/releases/checksums.txt",
			dl,
		)
		if err == nil || !strings.Contains(err.Error(), "refusing to install unverified binary") {
			t.Fatalf("expected unverified-binary refusal, got: %v", err)
		}
		if got := base.GetExecCommandCallCount(); got != 0 {
			t.Errorf("expected no extract/install commands without checksums, got %d", got)
		}
	})
}
