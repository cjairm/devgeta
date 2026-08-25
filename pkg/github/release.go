package github

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// maxReleaseResponseSize bounds how much of the GitHub releases-API response
// body we will read. A real release JSON payload (tag name, asset list,
// changelog body, etc.) is a few KB to a few tens of KB; 1 MiB is generous
// enough to comfortably cover that while still bounding a misbehaving or
// malicious server instead of buffering an unbounded response.
const maxReleaseResponseSize = 1 << 20 // 1 MiB

// httpClient has a whole-request timeout because this call is a small,
// bounded JSON API response, not a large file stream — unlike
// pkg/downloader, which has to bound body inactivity instead of the whole
// request so a slow-but-progressing multi-megabyte download isn't killed at
// an arbitrary time limit.
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// FetchLatestRelease queries the GitHub API to get the latest release version
// for the given owner/repo. Returns the version string without the "v" prefix
// (e.g. "0.44.1").
func FetchLatestRelease(owner, repo string) (string, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, repo)
	return fetchLatestReleaseFromURL(url)
}

// fetchLatestReleaseFromURL holds the actual HTTP + parsing logic behind
// FetchLatestRelease, taking the full URL directly so tests can point it at
// an httptest server instead of the real GitHub API.
func fetchLatestReleaseFromURL(url string) (string, error) {
	resp, err := httpClient.Get(url) // #nosec G107 - URL is constructed from known constants
	if err != nil {
		return "", fmt.Errorf("failed to query GitHub API: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// Drain the body so the underlying connection can return to the idle
		// pool instead of being discarded along with an unread response.
		_, _ = io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("GitHub API returned status %d", resp.StatusCode)
	}

	var release struct {
		TagName string `json:"tag_name"`
	}
	decoder := json.NewDecoder(io.LimitReader(resp.Body, maxReleaseResponseSize))
	if err := decoder.Decode(&release); err != nil {
		return "", fmt.Errorf("failed to parse GitHub API response: %w", err)
	}

	if release.TagName == "" {
		return "", fmt.Errorf("no tag_name in GitHub API response")
	}

	version := release.TagName
	if len(version) > 0 && version[0] == 'v' {
		version = version[1:]
	}
	return version, nil
}
