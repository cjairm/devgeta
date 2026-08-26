package task

import (
	"fmt"
	"os"
	"strings"
)

// trailerLine is one "Key: value" line git interpret-trailers
// --only-trailers --parse emitted.
type trailerLine struct {
	Key   string
	Value string
}

// parseTrailerLines splits git's "Key: value" output into structured lines.
// Empty input — git parsed no trailers at all — yields nil, which is the
// expected result for an ordinary commit message with no trailer block,
// not an error.
func parseTrailerLines(raw string) []trailerLine {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []trailerLine
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			// git's own --only-trailers output always has a colon; a line
			// without one is not a shape this function claims to
			// understand, so it is kept verbatim rather than guessed at.
			out = append(out, trailerLine{Key: line})
			continue
		}
		out = append(out, trailerLine{Key: strings.TrimSpace(key), Value: strings.TrimSpace(value)})
	}
	return out
}

// checkRequiredTrailers fails when any of require's keys (case-insensitive,
// matching git's own trailer-key comparison) is not among parsed's keys.
//
// This is the ENTIRE enforcement mechanism (see CommitTrailers' doc
// comment for why): a key the caller did not name is never flagged,
// however prose-like or trailer-like it looks.
func checkRequiredTrailers(parsed []trailerLine, require []string) error {
	present := make(map[string]bool, len(parsed))
	for _, t := range parsed {
		present[strings.ToLower(t.Key)] = true
	}
	var missing []string
	for _, key := range require {
		if !present[strings.ToLower(key)] {
			missing = append(missing, key)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	return fmt.Errorf(
		"commit-trailers: missing required trailer(s): %s — if the message has "+
			"one of these near the end, git only recognizes it as a trailer when "+
			"it is separated from the body by a blank line",
		strings.Join(missing, ", "),
	)
}

// CommitTrailers parses message's trailer block via `git interpret-trailers
// --parse --only-trailers` and returns the parsed trailers as plain text.
//
// require (repeatable, case-insensitive) fails the command when a named key
// is not among the trailers git's own parser recognized. This is
// deliberately the ONLY enforcement this command does — no syntax scan for
// "a trailer-looking line that didn't parse." That earlier design is
// undecidable: verified against git 2.51.1, `Subject\n\nReason: this is
// needed\nMore explanation.` parses to ZERO trailers (a trailer line
// followed by prose is not a recognized block), yet a scanner counting
// trailer-looking lines would see one and reject an ordinary commit
// message. Syntax alone cannot tell "a trailer whose blank separator is
// missing" from "prose containing a label", so this command does not try —
// it only ever asks "is this DECLARED key among what git parsed," which
// correctly catches a required trailer glued to the body (confirmed: git
// parses nothing for `Subject\nCo-authored-by: A <a@x.com>`) with no
// false positive on a prose label the caller never named.
//
// message is written to a temp file rather than piped over stdin — the
// same pattern jq.runFilter already uses to hand arbitrary text to an
// external command through the app wrapper's argv-based ExecCommand — and
// because `git interpret-trailers` happily accepts a file path positionally,
// there is no need to add stdin support to the shared executor for this one
// caller.
func (tm *TaskManager) CommitTrailers(message string, require []string) (string, error) {
	tmp, err := os.CreateTemp("", "devgeta-commit-trailers-*.txt")
	if err != nil {
		return "", fmt.Errorf("commit-trailers: failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(message); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("commit-trailers: failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("commit-trailers: failed to close temp file: %w", err)
	}

	raw, err := tm.Git.RunCapture("interpret-trailers", "--parse", "--only-trailers", tmpName)
	if err != nil {
		return "", fmt.Errorf("commit-trailers: %w", err)
	}

	parsed := parseTrailerLines(raw)
	if err := checkRequiredTrailers(parsed, require); err != nil {
		return "", err
	}

	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "(no trailers)", nil
	}
	return trimmed, nil
}
