// GitHub CLI (gh) tool with devgeta integration
//
// GitHub CLI is the official command-line tool for GitHub, providing access to
// pull requests, issues, releases, repositories, gists, and more directly from
// the terminal. This module provides installation and configuration management
// for gh with devgeta integration.
//
// References:
// - GitHub CLI Documentation: https://cli.github.com/manual/
// - GitHub CLI Repository: https://github.com/cli/cli
//
// Common gh commands available through ExecuteCommand():
//   - gh --version - Show gh version information
//   - gh auth login - Authenticate with GitHub
//   - gh auth status - View authentication status
//   - gh repo clone <repo> - Clone a repository
//   - gh pr list - List pull requests
//   - gh pr create - Create a new pull request
//   - gh pr checkout <number> - Check out a pull request locally
//   - gh issue list - List issues
//   - gh issue create - Create a new issue
//   - gh release list - List releases
//   - gh api <endpoint> - Make authenticated GitHub API requests

package githubcli

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/pkg/constants"
)

type GithubCli struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func New() *GithubCli {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &GithubCli{Cmd: osCmd, Base: baseCmd}
}

func (g *GithubCli) Install() error {
	return g.Cmd.InstallPackage(constants.GithubCli)
}

func (g *GithubCli) SoftInstall() error {
	return g.Cmd.MaybeInstallPackage(constants.GithubCli)
}

func (g *GithubCli) ForceInstall() error {
	err := g.Uninstall()
	if err != nil {
		return fmt.Errorf("failed to uninstall gh: %w", err)
	}
	return g.Install()
}

func (g *GithubCli) Uninstall() error {
	return fmt.Errorf("gh uninstall not supported through devgeta")
}

func (g *GithubCli) ForceConfigure() error {
	// GitHub CLI configuration is typically handled via:
	// - gh auth login (interactive authentication)
	// - gh config set <key> <value> (setting configuration values)
	// Configuration is usually handled via command-line operations
	// rather than copying config files
	return nil
}

func (g *GithubCli) SoftConfigure() error {
	// GitHub CLI configuration is typically handled via:
	// - gh auth login (interactive authentication)
	// - gh config set <key> <value> (setting configuration values)
	// Configuration is usually handled via command-line operations
	// rather than copying config files
	return nil
}

// ExecuteCommand runs a gh command, discarding stdout, for callers that only
// care whether it succeeded (ApprovePR, RequestChangesPR, CommentPR, MergePR,
// UpdatePRDescription, RequestReviewPR — every outward-posting command). On
// failure it surfaces gh's stderr for the same reason as RunWithOutput and
// CurrentPRNumber: the Go-level error is never more than "exit status 1",
// while gh's own message ("HTTP 404: Not Found", a rate-limit message, auth
// advice) is the only actionable reason. The wrapped Go error is the fallback
// for the case gh produced no stderr at all (binary missing, killed, timed out).
func (g *GithubCli) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.GithubCli,
		Args:    args,
	}
	_, stderr, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			return fmt.Errorf("gh: %s", trimmed)
		}
		return fmt.Errorf("failed to run gh command: %w", err)
	}
	return nil
}

func (g *GithubCli) Update() error {
	return fmt.Errorf("gh update not implemented through devgeta")
}

// RunWithOutput runs a gh command and returns captured stdout.
//
// On failure it surfaces gh's stderr, because that is the only place the
// actionable reason exists: gh writes "HTTP 404: Not Found", a rate-limit
// message, or its auth advice there, while the Go-level error it exits with is
// never more than "exit status 1". Discarding stderr left every command routed
// through here — PRView, PRChecks, FetchReviewThreads, FetchPRDiscussion — with
// nothing a user could act on, which bites hardest in commands that run
// unattended (pr-review-state on a poll), where that string is all a human sees.
// The wrapped Go error is the fallback for the case gh produced no stderr at all
// (binary missing, killed, timed out). Same shape as CurrentPRNumber and
// AuthenticatedLogin below.
func (g *GithubCli) RunWithOutput(args ...string) (string, error) {
	execCommand := cmd.CommandParams{
		IsSudo:  false,
		Command: constants.GithubCli,
		Args:    args,
	}
	stdout, stderr, err := g.Base.ExecCommand(execCommand)
	if err != nil {
		if trimmed := strings.TrimSpace(stderr); trimmed != "" {
			return "", fmt.Errorf("gh: %s", trimmed)
		}
		return "", fmt.Errorf("failed to run gh command: %w", err)
	}
	return stdout, nil
}

// GraphQL runs `gh api graphql` with the given query and variables, returning raw JSON stdout.
// stringVars are passed as -f key=value; intVars are passed as -F key=value.
func (g *GithubCli) GraphQL(query string, stringVars, intVars map[string]string) (string, error) {
	args := []string{"api", "graphql", "-f", "query=" + query}
	// Sort keys so the assembled args are deterministic run-to-run — stabilizes
	// tests and produces consistent output for agent pipelines.
	for _, k := range sortedKeys(stringVars) {
		args = append(args, "-f", k+"="+stringVars[k])
	}
	for _, k := range sortedKeys(intVars) {
		args = append(args, "-F", k+"="+intVars[k])
	}
	return g.RunWithOutput(args...)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// reviewThreadsQuery selects every field jq.FormatReviewThreads needs: per
// thread id/isResolved/resolvedBy.login/path/line/originalLine, per comment
// id/author/body/createdAt, and the diff hunk via a dedicated firstComment
// alias so it isn't refetched for every comment. It paginates threads via
// $endCursor + pageInfo (driven by `gh api graphql --paginate`).
//
// resolvedBy is null for unresolved threads and lets the renderer show who
// resolved a thread. Comment createdAt lets the renderer show when feedback
// was posted, which the /review-pr dedup rule needs to decide whether code
// changed since a reply.
//
// Note: comments(first: 100) is NOT paginated — a thread with >100 comments
// truncates. That is far rarer than >100 threads and deferred for now.
const reviewThreadsQuery = `query($owner: String!, $name: String!, $pr: Int!, $endCursor: String) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $pr) {
      reviewThreads(first: 100, after: $endCursor) {
        nodes {
          id
          isResolved
          isOutdated
          resolvedBy { login }
          path
          line
          originalLine
          comments(first: 100) {
            nodes {
              id
              author { login }
              body
              createdAt
            }
          }
          firstComment: comments(first: 1) {
            nodes { diffHunk }
          }
        }
        pageInfo {
          hasNextPage
          endCursor
        }
      }
    }
  }
}`

// prDiscussionQuery selects a pull request's review summary bodies (the
// top-level body of a submitted review — Approve/Request-changes/Comment),
// each with submittedAt, and its top-level conversation comments (`gh pr
// comment`), each with createdAt. Both are feedback surfaces distinct from
// inline review threads (reviewThreadsQuery above), which are anchored to a
// diff line; these are not.
//
// submittedAt/createdAt let the renderer show when feedback was posted, which
// the /review-pr dedup rule needs to decide whether code changed since a reply.
//
// This is a deliberately SEPARATE, non-paginated query. reviewThreadsQuery is
// driven by `gh api graphql --paginate` over its reviewThreads connection,
// which re-runs the whole query once per page — adding reviews/comments as
// sibling fields there would refetch and re-render them on every thread page,
// duplicating output.
//
// Note: reviews(first: 100) and comments(first: 100) are NOT paginated — a PR
// with >100 reviews or >100 conversation comments truncates. That mirrors the
// deferred-truncation stance already taken on reviewThreadsQuery's per-thread
// comments(first: 100).
const prDiscussionQuery = `query($owner: String!, $name: String!, $pr: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $pr) {
      reviews(first: 100) {
        nodes { author { login } body state submittedAt }
      }
      comments(first: 100) {
        nodes { author { login } body createdAt }
      }
    }
  }
}`

// resolveReviewThreadMutation marks a single review thread as resolved.
const resolveReviewThreadMutation = `mutation($threadId: ID!) {
  resolveReviewThread(input: {threadId: $threadId}) {
    thread { id isResolved }
  }
}`

// FetchReviewThreads returns the raw GraphQL JSON for a pull request's review
// threads — all of them, across pages, resolved and unresolved. With
// --paginate, gh emits one JSON document per page; jq.FormatReviewThreads
// applies its filter to each document, so the rendered output covers every page.
func (g *GithubCli) FetchReviewThreads(owner, repo, prNumber string) (string, error) {
	if owner == "" || repo == "" || prNumber == "" {
		return "", fmt.Errorf("fetch review threads requires owner, repo, and pr number")
	}
	return g.RunWithOutput(
		"api", "graphql", "--paginate",
		"-f", "query="+reviewThreadsQuery,
		"-f", "owner="+owner,
		"-f", "name="+repo,
		"-F", "pr="+prNumber,
	)
}

// FetchPRDiscussion returns the raw GraphQL JSON for a pull request's review
// summary bodies and top-level conversation comments (see prDiscussionQuery).
// Unlike FetchReviewThreads, this call is NOT paginated.
func (g *GithubCli) FetchPRDiscussion(owner, repo, prNumber string) (string, error) {
	if owner == "" || repo == "" || prNumber == "" {
		return "", fmt.Errorf("fetch pr discussion requires owner, repo, and pr number")
	}
	return g.RunWithOutput(
		"api", "graphql",
		"-f", "query="+prDiscussionQuery,
		"-f", "owner="+owner,
		"-f", "name="+repo,
		"-F", "pr="+prNumber,
	)
}

// ResolveReviewThread marks one PR review thread as resolved and returns the
// mutation's JSON response.
func (g *GithubCli) ResolveReviewThread(threadID string) (string, error) {
	if threadID == "" {
		return "", fmt.Errorf("resolve review thread requires a thread id")
	}
	return g.GraphQL(
		resolveReviewThreadMutation,
		map[string]string{"threadId": threadID},
		nil,
	)
}

// CreateReview posts a single pull-request review via the REST reviews
// endpoint, optionally carrying inline comments. payloadJSON is the full
// request body ({body, event, comments[]}) already assembled by the caller — it
// is sent with --input (written to a temp file) because gh's -f/-F flags cannot
// express the nested comments array. Returns the API's JSON response.
func (g *GithubCli) CreateReview(owner, repo, prNumber, payloadJSON string) (string, error) {
	if owner == "" || repo == "" || prNumber == "" {
		return "", fmt.Errorf("create review requires owner, repo, and pr number")
	}
	if strings.TrimSpace(payloadJSON) == "" {
		return "", fmt.Errorf("create review requires a payload")
	}

	tmp, err := os.CreateTemp("", "devgeta-review-*.json")
	if err != nil {
		return "", fmt.Errorf("create review: failed to create temp file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if _, err := tmp.WriteString(payloadJSON); err != nil {
		_ = tmp.Close()
		return "", fmt.Errorf("create review: failed to write temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return "", fmt.Errorf("create review: failed to close temp file: %w", err)
	}

	endpoint := fmt.Sprintf("/repos/%s/%s/pulls/%s/reviews", owner, repo, prNumber)
	return g.RunWithOutput(
		"api", "--method", "POST",
		"-H", "Accept: application/vnd.github+json",
		endpoint, "--input", tmpName,
	)
}

// CreatePR opens a pull request from the current branch and returns the created
// PR URL. base is optional (defaults to the repo's default branch).
func (g *GithubCli) CreatePR(title, body, base string) (string, error) {
	if title == "" {
		return "", fmt.Errorf("create pr requires a title")
	}
	args := []string{"pr", "create", "--title", title, "--body", body}
	if base != "" {
		args = append(args, "--base", base)
	}
	return g.RunWithOutput(args...)
}

// UpdatePRDescription replaces a pull request's body. prNumber may be empty to
// target the PR associated with the current branch.
func (g *GithubCli) UpdatePRDescription(prNumber, body string) error {
	args := []string{"pr", "edit"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--body", body)
	return g.ExecuteCommand(args...)
}

// ApprovePR approves a pull request. prNumber may be empty (current branch);
// body is optional.
func (g *GithubCli) ApprovePR(prNumber, body string) error {
	args := []string{"pr", "review"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--approve")
	if body != "" {
		args = append(args, "--body", body)
	}
	return g.ExecuteCommand(args...)
}

// RequestChangesPR requests changes on a pull request. gh requires a body for
// request-changes reviews. prNumber may be empty (current branch).
func (g *GithubCli) RequestChangesPR(prNumber, body string) error {
	if body == "" {
		return fmt.Errorf("request changes requires a body")
	}
	args := []string{"pr", "review"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--request-changes", "--body", body)
	return g.ExecuteCommand(args...)
}

// RequestReviewPR adds reviewers to a pull request's requested-reviewers list.
// GitHub re-requests review from a reviewer who already reviewed, so this is
// how a PR is handed back after feedback is addressed. prNumber may be empty
// (current branch); at least one reviewer is required.
func (g *GithubCli) RequestReviewPR(prNumber string, reviewers []string) error {
	if len(reviewers) == 0 {
		return fmt.Errorf("request review requires at least one reviewer")
	}
	args := []string{"pr", "edit"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--add-reviewer", strings.Join(reviewers, ","))
	return g.ExecuteCommand(args...)
}

// replyReviewThreadMutation posts a reply comment onto an existing review thread.
const replyReviewThreadMutation = `mutation($threadId: ID!, $body: String!) {
  addPullRequestReviewThreadReply(input: {pullRequestReviewThreadId: $threadId, body: $body}) {
    comment { id url }
  }
}`

// unresolveReviewThreadMutation reopens a previously resolved review thread.
const unresolveReviewThreadMutation = `mutation($threadId: ID!) {
  unresolveReviewThread(input: {threadId: $threadId}) {
    thread { id isResolved }
  }
}`

// defaultPRViewFields is a compact, agent-oriented field set kept deliberately
// small for token economy. Callers needing more (e.g. body, statusCheckRollup)
// pass their own fields.
var defaultPRViewFields = []string{
	"number", "title", "state", "mergeable", "reviewDecision", "headRefName", "baseRefName",
}

// ReplyToReviewThread posts a reply onto a review thread and returns the
// mutation's JSON response. Pair this with ResolveReviewThread to respond
// before closing a conversation.
func (g *GithubCli) ReplyToReviewThread(threadID, body string) (string, error) {
	if threadID == "" || body == "" {
		return "", fmt.Errorf("reply requires a thread id and body")
	}
	return g.GraphQL(
		replyReviewThreadMutation,
		map[string]string{"threadId": threadID, "body": body},
		nil,
	)
}

// UnresolveReviewThread reopens a resolved review thread and returns the
// mutation's JSON response.
func (g *GithubCli) UnresolveReviewThread(threadID string) (string, error) {
	if threadID == "" {
		return "", fmt.Errorf("unresolve review thread requires a thread id")
	}
	return g.GraphQL(
		unresolveReviewThreadMutation,
		map[string]string{"threadId": threadID},
		nil,
	)
}

// PRView returns a pull request's metadata as JSON. prNumber may be empty
// (current branch). When no fields are given, defaultPRViewFields is used to
// keep the payload small; pass explicit fields to fetch more.
func (g *GithubCli) PRView(prNumber string, fields ...string) (string, error) {
	if len(fields) == 0 {
		fields = defaultPRViewFields
	}
	args := []string{"pr", "view"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--json", strings.Join(fields, ","))
	return g.RunWithOutput(args...)
}

// PRTitleAt returns the title of the pull request for the current branch of the
// repository checked out at dir. It is best-effort: it returns "" (never an
// error) when gh is absent, unauthenticated, times out, or the branch has no
// PR — all outcomes the caller treats as "no title". timeout bounds the gh
// call so a hung gh can't stall the caller; dir runs gh against a specific
// worktree without depending on the process's current directory.
func (g *GithubCli) PRTitleAt(dir string, timeout time.Duration) string {
	stdout, _, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.GithubCli,
		Args:    []string{"pr", "view", "--json", "title", "-q", ".title"},
		Dir:     dir,
		Timeout: timeout,
	})
	if err != nil {
		return ""
	}
	return strings.TrimSpace(stdout)
}

// PRChecks returns the CI check status for a pull request as JSON, for stable
// machine parsing. prNumber may be empty (current branch).
//
// `gh pr checks` exits non-zero when any check is failing or pending, but still
// prints valid JSON — exactly when a caller most wants the data. So this reads
// stdout directly: a non-zero exit with JSON present is returned as success, a
// "no checks" message returns an empty array, and only a genuinely empty
// failure surfaces as an error.
//
// `bucket` is requested alongside the fields jq.FormatPRChecks projects
// (name, state, link, workflow) because it is gh's own authoritative
// pass/fail/pending/skipping/cancel categorization of `state` (see `gh pr
// checks --help` and task.isFailingCheck) — cheaper and more future-proof
// than re-deriving "is this failing" from state's larger enum. jq's filter
// only reads name/state/link, so this extra field never changes the rendered
// per-check line.
func (g *GithubCli) PRChecks(prNumber string) (string, error) {
	args := []string{"pr", "checks"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--json", "name,state,link,workflow,bucket")

	stdout, stderr, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.GithubCli,
		Args:    args,
	})
	stdout = strings.TrimSpace(stdout)
	if err != nil {
		if stdout != "" {
			return stdout, nil // failing/pending checks: JSON is still valid
		}
		if strings.Contains(strings.ToLower(stderr), "no checks") {
			return "[]", nil // PR has no checks configured
		}
		if stderr != "" {
			return "", fmt.Errorf("gh: %s", stderr)
		}
		return "", fmt.Errorf("failed to get pr checks: %w", err)
	}
	return stdout, nil
}

// RunFailedJobLog returns the log for a single job's failed steps
// ("gh run view --job <jobID> --log-failed"), for embedding under a failing
// check's digest (see task.PRManager.PRChecks). It operates on the current
// repo (no owner/repo argument), matching PRChecks/PRView's convention of
// relying on gh's cwd-based repo resolution.
//
// Verified empirically against several public repos with real failing runs
// (junegunn/fzf, BurntSushi/ripgrep): gh's log-download API returns an
// empty, error-free stdout when the authenticated user has only read access
// to the check's repo — log archives are gated behind write access even for
// public repos. This method does not treat that as an error; the caller
// (task.PRManager) distinguishes "fetched but empty" from "failed to fetch"
// and reports both as an honest "log unavailable" note rather than a
// fabricated digest.
func (g *GithubCli) RunFailedJobLog(jobID string) (string, error) {
	if strings.TrimSpace(jobID) == "" {
		return "", fmt.Errorf("run failed job log requires a job id")
	}
	return g.RunWithOutput("run", "view", "--job", jobID, "--log-failed")
}

// CommentPR posts a top-level comment on a pull request (distinct from a
// review). prNumber may be empty (current branch); body is required.
func (g *GithubCli) CommentPR(prNumber, body string) error {
	if body == "" {
		return fmt.Errorf("comment requires a body")
	}
	args := []string{"pr", "comment"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, "--body", body)
	return g.ExecuteCommand(args...)
}

// MergePR merges a pull request. method selects the strategy: "squash"
// (default when empty), "merge", or "rebase". prNumber may be empty (current branch).
func (g *GithubCli) MergePR(prNumber, method string) error {
	var flag string
	switch method {
	case "", "squash":
		flag = "--squash"
	case "merge":
		flag = "--merge"
	case "rebase":
		flag = "--rebase"
	default:
		return fmt.Errorf("unknown merge method %q (use squash, merge, or rebase)", method)
	}
	args := []string{"pr", "merge"}
	if prNumber != "" {
		args = append(args, prNumber)
	}
	args = append(args, flag)
	return g.ExecuteCommand(args...)
}

// CurrentPRNumber returns the PR number for the current branch, or "" with a
// nil error when the branch has no associated pull request. Any other failure
// (auth, network, not a repo) is returned as an error.
//
// A PR number is a GitHub concept, not a git one, so this must go through gh —
// git cannot resolve a branch to its PR. gh handles the head-ref matching,
// forks, and remote selection.
func (g *GithubCli) CurrentPRNumber() (string, error) {
	stdout, stderr, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.GithubCli,
		Args:    []string{"pr", "view", "--json", "number", "--jq", ".number"},
	})
	if err != nil {
		// gh exits non-zero when the branch has no PR; treat that as "no PR"
		// (empty result) so callers can branch on it instead of error-handling.
		if strings.Contains(strings.ToLower(stderr), "no pull request") {
			return "", nil
		}
		if stderr != "" {
			return "", fmt.Errorf("gh: %s", stderr)
		}
		return "", fmt.Errorf("failed to resolve current pr number: %w", err)
	}
	return strings.TrimSpace(stdout), nil
}

// AuthenticatedLogin returns the login of the GitHub user gh is authenticated
// as, resolved by gh itself rather than from any local config.
//
// "Is this review requested from ME?" is a comparison against a PR's
// reviewRequests logins (see task.PRManager.PRReviewState), so the answer has
// to be the account whose token gh would actually review with — not a git
// user.name, not an email, and not a value a caller passes in and can get
// wrong. An empty answer is an error rather than a login that matches nothing.
func (g *GithubCli) AuthenticatedLogin() (string, error) {
	stdout, stderr, err := g.Base.ExecCommand(cmd.CommandParams{
		Command: constants.GithubCli,
		Args:    []string{"api", "user", "--jq", ".login"},
	})
	if err != nil {
		if strings.TrimSpace(stderr) != "" {
			return "", fmt.Errorf(
				"could not resolve the authenticated GitHub user (gh: %s) — check `gh auth status`",
				strings.TrimSpace(stderr),
			)
		}
		return "", fmt.Errorf(
			"could not resolve the authenticated GitHub user: %w — check `gh auth status`",
			err,
		)
	}
	login := strings.TrimSpace(stdout)
	if login == "" {
		return "", fmt.Errorf("gh reported no authenticated user; run `gh auth login`")
	}
	return login, nil
}

// CurrentRepo returns the current repository as "owner/name". It delegates
// repo resolution to gh (which reads the git remotes) rather than parsing
// remote URLs by hand, so SSH/HTTPS/Enterprise URL forms are all handled.
func (g *GithubCli) CurrentRepo() (string, error) {
	out, err := g.RunWithOutput(
		"repo", "view", "--json", "owner,name", "--jq", `.owner.login + "/" + .name`,
	)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}
