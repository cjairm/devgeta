package commands

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
