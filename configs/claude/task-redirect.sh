#!/usr/bin/env bash
# PreToolUse hook: intercept a narrow set of raw-git command patterns that have
# a dedicated `devgeta task` equivalent, and deny with the exact replacement to
# run instead. See docs/apps/claude.md ("Command redirect (PreToolUse hook)")
# for the full contract.
#
# Claude Code delivers the hook payload as JSON on stdin; for a PreToolUse hook
# on the Bash tool, the command string lives at `.tool_input.command` (verified
# against Claude Code's current hook docs, code.claude.com/docs/en/hooks). To
# deny, this script exits 2 and writes a one-line reason to stderr — Claude
# Code blocks the call and shows the agent that stderr text. This is the
# simpler, exit-code-based deny mechanism (vs. printing structured
# hookSpecificOutput/permissionDecision JSON to stdout): it has no JSON-
# escaping failure mode that could silently fail open or closed, which matters
# more here than for format.sh's PostToolUse hook (which only ever surfaces
# advisory context, never blocks). Any other outcome — no command field, an
# unmatched command, jq missing — falls through to exit 0 (allow): this hook
# must never accidentally block all Bash calls.
#
# Escape hatch: set DEVGETA_SKIP_TASK_REDIRECT=1 for the session to bypass this
# hook entirely when raw git is genuinely needed. Every deny message repeats
# this so no flow dead-ends.
#
# NB: the command string is only ever pattern-matched (grep), never eval'd or
# executed — no command-injection surface from the JSON payload.
#
# Keep this file's pattern table and configs/opencode/plugin/task-redirect.js's
# pattern table in sync — they intentionally mirror each other one-for-one.
#
# Command segmentation (devgeta_split_command_segments/devgeta_trim) and the
# devgeta-repo gate (devgeta_is_repo) live in lib/segments.sh and
# lib/devgeta-repo.sh, sourced below — shared with secret-guard.sh and (for
# the repo gate) suppression-guard.sh. See ADR-0006.
#
# Rule scope (these hooks deploy to the user's GLOBAL config, so they fire in
# EVERY repo):
#   - Global rules fire everywhere: review-package (git diff/log <ref>..<ref>),
#     worktree-start (git worktree add), worktree-finish (git worktree remove),
#     and the gh redirects pr-checks / review-threads / submit-review. These
#     impose no repo-specific convention — they're better/compressed forms of
#     universal git/gh operations.
#   - The two release rules (git reset --soft HEAD~N, git tag -a v<semver>) are
#     devgeta-repo-only: they encode devgeta's own release policy and would be
#     wrong to steer in other repos. They fire only when is_devgeta_repo
#     confirms a go.mod with module github.com/cjairm/devgeta, and fail toward
#     NOT firing on any uncertainty (see is_devgeta_repo).
#
# Matching scope (read before touching the patterns below):
#   - Each rule is checked against every "command segment" of the input, not
#     just the start of the whole string. Segments are split on unquoted &&,
#     ||, ;, and | so that `cd x && git worktree add y`, `git status; git
#     worktree remove y`, and `git fetch && git diff a..b` are all caught —
#     not just a bare `git ...` with nothing else on the line.
#   - Each rule's `git` anchor also tolerates a leading run of shell
#     VAR=value assignments (e.g. `GIT_PAGER=cat git diff a..b`), so an
#     env-var prefix with NO separator character before `git` is caught too.
#   - Splitting respects single- and double-quoted spans (a `;` or `&&`
#     inside a quoted commit message is not treated as a boundary), but this
#     is a best-effort, non-adversarial split: it does not handle
#     backslash-escaped quotes, command substitution ($(...) / `...`), or
#     heredocs. A command deliberately crafted to defeat quote tracking is
#     out of scope — this hook pattern-matches agent-typed commands, it is
#     not a shell parser.

# Bypass first, before touching stdin, so it works even if stdin is malformed.
if [ -n "${DEVGETA_SKIP_TASK_REDIRECT:-}" ]; then
	exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/segments.sh"
source "$SCRIPT_DIR/lib/devgeta-repo.sh"

input=$(cat)
COMMAND=$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)
[ -z "$COMMAND" ] && exit 0

# Claude Code's PreToolUse payload includes the agent's working directory at
# top-level `.cwd`. It gates the release rules (rules 4 & 5) to the devgeta
# repo only (see is_devgeta_repo). If absent/empty, fall back to this shell's
# own $PWD; if that too is indeterminate, the gate fails toward NOT firing.
CWD=$(printf '%s' "$input" | jq -r '.cwd // empty' 2>/dev/null)

BYPASS_HINT="set DEVGETA_SKIP_TASK_REDIRECT=1 to bypass this session if raw git is genuinely needed"

deny() {
	echo "$1 — $BYPASS_HINT" >&2
	exit 2
}

# A ref..ref (or ref...ref) range token: non-space ref chars on both sides of
# the dot run, so a bare ref like HEAD~1 (no dots) or a lone dotted value like
# v1.2.0 (single dots, no run of 2-3) never matches.
RANGE_TOKEN='[A-Za-z0-9._/~^{}@:-]+\.\.\.?[A-Za-z0-9._/~^{}@:-]+'

# Matches the start of a segment that is a `git` invocation, optionally
# preceded by one or more shell VAR=value assignments (e.g.
# `GIT_PAGER=cat git diff ...`, `FOO=1 BAR=2 git worktree add ...`) AND
# tolerating git global options between `git` and the subcommand (e.g.
# `git -C ../wt worktree add x`, `git -R`-style equivalents for gh below) —
# without that second part, a plain `git\s+<subcommand>` anchor missed the
# everyday `git -C <dir> <subcommand> ...`, letting that whole class of
# command bypass these redirects entirely (found in review of the sibling
# secret-guard.sh hook, then fixed here too). DEVGETA_ENV_ASSIGN,
# DEVGETA_GIT_GLOBAL_OPT, and DEVGETA_GH_GLOBAL_OPT come from lib/segments.sh
# (sourced above) — shared with secret-guard.sh's GIT_ANCHOR.
GIT_ANCHOR="^(${DEVGETA_ENV_ASSIGN}[[:space:]]+)*git([[:space:]]+${DEVGETA_GIT_GLOBAL_OPT})*"
# Same anchor for `gh` invocations (the GitHub CLI rules below), tolerating
# the same leading VAR=value prefix run and gh's own global options (most
# notably `-R <owner/repo>`/`--repo <owner/repo>`).
GH_ANCHOR="^(${DEVGETA_ENV_ASSIGN}[[:space:]]+)*gh([[:space:]]+${DEVGETA_GH_GLOBAL_OPT})*"

# is_devgeta_repo answers "is the command running inside the devgeta repo?" —
# the gate for the release rules (rules 4 & 5). Those rules encode devgeta's
# OWN repo-specific release policy (CLAUDE.md §9), so redirecting the universal
# `git reset --soft`/`git tag -a` techniques must NOT happen in other repos.
#
# Delegates the actual go.mod walk (CWD, falling back to $PWD) to
# devgeta_is_repo (lib/devgeta-repo.sh, sourced above — shared with
# suppression-guard.sh, see ADR-0006), memoizing the result here so multiple
# release segments/rules in one invocation check at most once.
#
# CRITICAL: this fails TOWARD NOT firing. If the working dir is indeterminate,
# no go.mod is found, or the module doesn't match, it returns non-zero (repo is
# NOT devgeta) — so the raw git command is allowed through rather than a
# general reset/tag being wrongly blocked outside devgeta. It is only ever
# called AFTER a release pattern has already matched, so the common allow path
# never pays the go.mod-lookup cost.
DEVGETA_REPO_MEMO=""
is_devgeta_repo() {
	if [ -z "$DEVGETA_REPO_MEMO" ]; then
		if devgeta_is_repo "${CWD:-$PWD}"; then
			DEVGETA_REPO_MEMO="yes"
		else
			DEVGETA_REPO_MEMO="no"
		fi
	fi
	[ "$DEVGETA_REPO_MEMO" = "yes" ]
}

# check_segment applies every rule to a single trimmed command segment,
# denying (and exiting) on the first match. Identical rule logic to the
# pre-segmentation version of this script — only the anchor changed.
check_segment() {
	local segment="$1"

	# --- git diff <ref>..<ref> / git log <ref>..<ref> (the range review dance) ---
	# Only fires when a range-shaped token is present among the arguments,
	# regardless of flags before it (--stat, --oneline, -U10, ...). Bare
	# `git diff`, `git diff HEAD~1`, and `git log`/`git log -5` never match.
	if printf '%s\n' "$segment" |
		grep -qE "${GIT_ANCHOR}[[:space:]]+(diff|log)([[:space:]]+[^[:space:]]+)*[[:space:]]+${RANGE_TOKEN}([[:space:]]|\$)"; then
		deny "Use: devgeta task review-package <base> <head> (one call: verified range, commits, noise-filtered stats + full diff)"
	fi

	# --- git worktree add ... ---
	if printf '%s\n' "$segment" | grep -qE "${GIT_ANCHOR}[[:space:]]+worktree[[:space:]]+add([[:space:]]|\$)"; then
		deny "Use: devgeta task worktree-start <name> [--base <ref>]"
	fi

	# --- git worktree remove ... ---
	if printf '%s\n' "$segment" | grep -qE "${GIT_ANCHOR}[[:space:]]+worktree[[:space:]]+remove([[:space:]]|\$)"; then
		deny "Use: devgeta task worktree-finish [<name>] --merge|--discard"
	fi

	# --- git reset --soft HEAD~N (N >= 1) — devgeta-repo-only ---
	# `git reset --soft HEAD` (no ~N, e.g. amend-style staging) is never matched.
	# Gated by is_devgeta_repo (checked only after the pattern matches): the
	# release flow is devgeta's own policy, so this stays out of other repos.
	if printf '%s\n' "$segment" |
		grep -qE "${GIT_ANCHOR}[[:space:]]+reset[[:space:]]+--soft[[:space:]]+HEAD~[1-9][0-9]*([[:space:]]|\$)" &&
		is_devgeta_repo; then
		deny "Use: devgeta task release <version> --message-file <file> [--push] (squash + tag flow)"
	fi

	# --- git tag -a v<semver> ... — devgeta-repo-only ---
	# Requires all three, within THIS segment: "git tag", a "-a" flag, and a
	# v-prefixed semver-shaped token — so `git tag` (list) and `git tag
	# v1.0.0` (lightweight, no -a) are never matched. Also gated by
	# is_devgeta_repo (checked last, only after the three pattern checks pass).
	if printf '%s\n' "$segment" | grep -qE "${GIT_ANCHOR}[[:space:]]+tag\b" &&
		printf '%s\n' "$segment" | grep -qE '(^|[[:space:]])-a([[:space:]]|$)' &&
		printf '%s\n' "$segment" | grep -qE '(^|[[:space:]])v[0-9]+\.[0-9]+\.[0-9]+([[:space:]]|$)' &&
		is_devgeta_repo; then
		deny "Use: devgeta task release <version> --message-file <file> [--push] (squash + tag flow)"
	fi

	# --- gh pr checks — global ---
	# The gh PR CI-status view. `pr checks` only (never `gh pr view`, `gh pr
	# status`, `gh pr list`): the token after `pr` must be exactly `checks`.
	if printf '%s\n' "$segment" | grep -qE "${GH_ANCHOR}[[:space:]]+pr[[:space:]]+checks\b"; then
		deny "Use: devgeta task pr-checks (adds a failing-job log digest the raw command lacks)"
	fi

	# --- gh api graphql ... reviewThreads — global ---
	# A `gh` invocation carrying BOTH the `api` and `graphql` tokens AND the
	# literal reviewThreads (the field an agent hand-rolling the review-thread
	# fetch would name in its query). A bare `gh api graphql` for anything else,
	# or a bare `gh api ...`, never matches — reviewThreads must be present.
	# reviewThreads inside a quoted query=... span survives segment splitting
	# (the splitter keeps quoted spans intact), so it is still matchable here.
	if printf '%s\n' "$segment" | grep -qE "${GH_ANCHOR}([[:space:]]|\$)" &&
		printf '%s\n' "$segment" | grep -qE '(^|[[:space:]])api([[:space:]]|$)' &&
		printf '%s\n' "$segment" | grep -qE '(^|[[:space:]])graphql([[:space:]]|$)' &&
		printf '%s\n' "$segment" | grep -q 'reviewThreads'; then
		deny "Use: devgeta task review-threads (GraphQL payload rendered to compact markdown + dedup)"
	fi

	# --- gh pr review — global ---
	# Submitting a PR review. `pr review` only (the token after `pr` must be
	# exactly `review`) — never `gh pr view` (different subcommand) or `gh pr
	# checks`.
	if printf '%s\n' "$segment" | grep -qE "${GH_ANCHOR}[[:space:]]+pr[[:space:]]+review\b"; then
		deny "Use: devgeta task submit-review --event approve|request-changes|comment [--body ...] (posts body + inline comments atomically)"
	fi
}

while IFS= read -r raw_segment; do
	check_segment "$(devgeta_trim "$raw_segment")"
done < <(devgeta_split_command_segments "$COMMAND")

exit 0
