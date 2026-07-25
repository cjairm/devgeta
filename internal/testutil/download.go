package testutil

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/downloader"
)

// ChecksumAwareDownloadFn returns a downloadFn for commands.InstallGitHubBinary
// tests: it writes fake archive bytes to the archive destination and a matching
// sha256sum-format checksums file to the checksums destination, so the helper's
// mandatory SHA-256 verification passes with zero network access. The checksums
// destination is recognized by the "-checksums.txt" suffix InstallGitHubBinary
// uses; the archive is always requested first, so its file name is known by the
// time the checksums file is written.
func ChecksumAwareDownloadFn(
	t *testing.T,
) func(context.Context, string, string, downloader.RetryConfig) error {
	t.Helper()
	content := []byte("devgeta-test-archive")
	sum := sha256.Sum256(content)
	var assetName string
	return func(_ context.Context, url, dest string, _ downloader.RetryConfig) error {
		if strings.HasSuffix(dest, "-checksums.txt") {
			if assetName == "" {
				return fmt.Errorf("checksums requested before archive download")
			}
			line := fmt.Sprintf("%s  %s\n", hex.EncodeToString(sum[:]), assetName)
			return os.WriteFile(dest, []byte(line), 0o644)
		}
		assetName = path.Base(url)
		return os.WriteFile(dest, content, 0o644)
	}
}
