#!/usr/bin/env bash
# PreToolUse hook: before a Bash `git commit` runs, scan what's actually
# staged for filenames and content that look like a committed secret, and
# deny with what to unstage. This is a safety net, not a substitute for a
# real secret scanner (gitleaks, trufflehog) or CI pre-commit hooks — see the
# deliberately narrow pattern lists below for exactly what it catches.
#
# Scope: GLOBAL — deploys to every repo (see ADR-0006). "Never commit a
# secret" is universal practice, not a devgeta-specific convention, unlike
# the release rules in task-redirect.sh or the lint-suppression ban in
# suppression-guard.sh.
#
# Claude Code delivers the hook payload as JSON on stdin, the same shape
# task-redirect.sh already relies on (.tool_input.command, top-level .cwd).
# Denies via exit 2 with a one-line reason on stderr (same mechanism, same
# reason — no JSON-escaping failure mode). A missing/unparseable command, jq
# being unavailable, git being unavailable, the target not being a git repo,
# or any ambiguity reading the staged diff falls through to exit 0 (allow) —
# this hook must never accidentally block all commits.
#
# Escape hatch: set DEVGETA_SKIP_SECRET_GUARD=1 for the session to bypass this
# hook when a match is a known false positive.
#
# NB: `git diff --cached`/`git rev-parse` are read-only introspection of what
# is already staged — this hook never stages, commits, or mutates anything.
#
# Keep this file's SENSITIVE filename/content pattern lists and
# configs/opencode/plugin/secret-guard.js's in sync — they mirror each other
# one-for-one, the same convention task-redirect.sh/.js already use.

# Two things beyond simple pattern-matching, both closing bypasses found in
# review:
#   - `git -C <dir> commit` is checked against <dir>'s staged index, not
#     this hook's own payload cwd — otherwise the hook would silently check
#     the wrong repository entirely (see TARGET_DIR below).
#   - A compound "stage, then commit" in ONE Bash call (`git add -A && git
#     commit`), or a bare `git commit -a`/`-am`/`--all`, is denied outright
#     rather than checked: this is a PreToolUse hook, so it fires BEFORE any
#     part of the Bash command has run — `git diff --cached` at check time
#     cannot reflect a staging action that is part of the SAME, not-yet-run
#     command. Splitting into two separate Bash calls is the only way this
#     hook can make a real assessment (see STAGE_SEPARATELY_MSG below).

if [ -n "${DEVGETA_SKIP_SECRET_GUARD:-}" ]; then
	exit 0
fi

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/lib/segments.sh"

input=$(cat)
COMMAND=$(printf '%s' "$input" | jq -r '.tool_input.command // empty' 2>/dev/null)
[ -z "$COMMAND" ] && exit 0

CWD=$(printf '%s' "$input" | jq -r '.cwd // empty' 2>/dev/null)
DIR="${CWD:-$PWD}"
[ -z "$DIR" ] && exit 0

BYPASS_HINT="set DEVGETA_SKIP_SECRET_GUARD=1 to bypass this session if this is a false positive"

# DEVGETA_GIT_GLOBAL_OPT (from lib/segments.sh, shared with
# task-redirect.sh's GIT_ANCHOR) tolerates git global options between `git`
# and `commit` — without it, a bare anchor requiring "commit" to immediately
# follow "git" missed the everyday `git -C ../other-repo commit ...`,
# entirely bypassing this guard.
GIT_ANCHOR="^(${DEVGETA_ENV_ASSIGN}[[:space:]]+)*git([[:space:]]+${DEVGETA_GIT_GLOBAL_OPT})*"

STAGE_SEPARATELY_MSG="This hook can only check what is ALREADY staged — run staging (git add/mv) and git commit as two separate commands so a staged secret can be caught before it's committed — $BYPASS_HINT"

# Scan segments in order for the FIRST `git commit` (a second commit in the
# same compound command is an accepted, unhandled edge case — see below),
# tracking whether an index-mutating command (git add/mv/stage — NOT git rm,
# which only removes content, never introduces it) appears BEFORE it. This
# is a PreToolUse hook: it runs before ANY part of the Bash command has
# executed, so for `git add -A && git commit`, `git diff --cached` right now
# reflects neither command — the add hasn't happened yet, so its effect is
# invisible to this check. Denying and asking for two separate calls is the
# only reliable fix: by the time a standalone `git commit` call runs alone,
# the prior `git add` call has actually completed, so `git diff --cached`
# then faithfully reflects reality.
# EFFECTIVE_DIR tracks where the command is ACTUALLY running as segments are
# scanned left to right, updated by a `cd <path>` segment seen before the
# commit (chained: each resolved relative to the previous EFFECTIVE_DIR,
# mirroring real shell semantics). Without this, `cd /other/repo && git
# commit` would be checked against payload cwd — not /other/repo, the
# command's REAL target — even though PreToolUse fires before any of this
# has run: unlike staging (which needs an actual git operation to take
# effect), `cd` only ever changes where LATER segments in the SAME command
# run, and that is knowable by reading the command text alone. Confirmed as
# a real, live bypass (not just a theoretical one) while manually verifying
# this hook end-to-end.
EFFECTIVE_DIR="$DIR"
COMMIT_SEGMENT=""
saw_mutation_before_commit=0
while IFS= read -r raw_segment; do
	segment="$(devgeta_trim "$raw_segment")"
	if [ -z "$COMMIT_SEGMENT" ] &&
		printf '%s\n' "$segment" | grep -qE "${GIT_ANCHOR}[[:space:]]+commit([[:space:]]|\$)"; then
		COMMIT_SEGMENT="$segment"
		continue
	fi
	if [ -z "$COMMIT_SEGMENT" ] &&
		printf '%s\n' "$segment" | grep -qE "${GIT_ANCHOR}[[:space:]]+(add|mv|stage)([[:space:]]|\$)"; then
		saw_mutation_before_commit=1
		continue
	fi
	if [ -z "$COMMIT_SEGMENT" ] &&
		printf '%s\n' "$segment" | grep -qE '^cd([[:space:]]+[^[:space:]]+)?([[:space:]]|$)'; then
		cd_target=$(printf '%s\n' "$segment" | sed -E 's/^cd[[:space:]]*//')
		case "$cd_target" in
		"" | "~") EFFECTIVE_DIR="$HOME" ;;
		"-") : ;; # cd - needs the PREVIOUS dir, which isn't tracked; best-effort: leave unchanged
		/*) EFFECTIVE_DIR="$cd_target" ;;
		*) EFFECTIVE_DIR="$EFFECTIVE_DIR/$cd_target" ;;
		esac
	fi
done < <(devgeta_split_command_segments "$COMMAND")
[ -n "$COMMIT_SEGMENT" ] || exit 0

if [ "$saw_mutation_before_commit" -eq 1 ]; then
	echo "$STAGE_SEPARATELY_MSG" >&2
	exit 2
fi

# `git commit -a`/`-am`/`--all` auto-stages tracked working-tree changes AT
# COMMIT TIME — the identical "not visible to `git diff --cached` yet"
# problem as the compound case above, without needing a compound command at
# all. Matches a short-option cluster containing the letter 'a' (-a, -am,
# -qam, ...) or the long form --all; a long flag like --author=x or --amend
# is a different, unrelated option and does not match (see lib/segments.sh's
# comment on why a single leading dash is required for this class of check).
if printf '%s\n' "$COMMIT_SEGMENT" |
	grep -qE '(^|[[:space:]])-[a-zA-Z]*a[a-zA-Z]*([[:space:]]|$)|(^|[[:space:]])--all([[:space:]]|$)'; then
	echo "$STAGE_SEPARATELY_MSG" >&2
	exit 2
fi

command -v git >/dev/null 2>&1 || exit 0

# Resolve the ACTUAL git target directory, starting from EFFECTIVE_DIR (the
# result of any `cd` chain above): `git -C <dir> commit` runs against <dir>,
# resolved relative to EFFECTIVE_DIR if relative (`cd X && git -C Y commit`
# operates in X/Y). The LAST `-C` in the commit segment wins (uppercase
# only — `-c` is git's unrelated inline-config-value flag, never a
# directory); multiple chained -C's, each relative to the previous, are not
# handled — rare enough in practice that this best-effort approximation is
# acceptable.
TARGET_DIR="$EFFECTIVE_DIR"
LAST_DASH_C=$(printf '%s\n' "$COMMIT_SEGMENT" | grep -oE -- '(^|[[:space:]])-C[[:space:]]+[^[:space:]]+' | tail -n1)
if [ -n "$LAST_DASH_C" ]; then
	LAST_DASH_C="$(devgeta_trim "${LAST_DASH_C#*-C}")"
	case "$LAST_DASH_C" in
	/*) TARGET_DIR="$LAST_DASH_C" ;;
	*) TARGET_DIR="$EFFECTIVE_DIR/$LAST_DASH_C" ;;
	esac
fi

git -C "$TARGET_DIR" rev-parse --is-inside-work-tree >/dev/null 2>&1 || exit 0

# is_sensitive_filename: a small, high-confidence set of filename shapes that
# are essentially always a committed secret, never ordinary code. *.pub is a
# PUBLIC key (never a secret); .env.example/.env.sample/.env.template are
# common, legitimate committed templates — both are explicitly excluded.
is_sensitive_filename() {
	local base="${1##*/}"
	case "$base" in
	*.pub) return 1 ;;
	esac
	case "$base" in
	.env.example | .env.sample | .env.template) return 1 ;;
	.env | .env.*) return 0 ;;
	*.pem | id_rsa | id_ed25519 | id_ecdsa | *.p12 | *.pfx | *.keystore | *.jks) return 0 ;;
	*) return 1 ;;
	esac
}

# --diff-filter=ACMR (Added/Copied/Modified/Renamed) excludes Deleted: staging
# the REMOVAL of a sensitive file — the correct remediation for a secret
# that's already committed — must not be denied as if it were being added.
while IFS= read -r file; do
	[ -z "$file" ] && continue
	if is_sensitive_filename "$file"; then
		echo "Staged file looks like a secret: $file — unstage it first: git restore --staged \"$file\" — $BYPASS_HINT" >&2
		exit 2
	fi
done < <(git -C "$TARGET_DIR" diff --cached --name-only --diff-filter=ACMR 2>/dev/null)

# Content check: a high-confidence secret signature anywhere in the staged
# diff. Kept deliberately narrow (private-key headers, AWS/GitHub/Slack
# token shapes) to avoid false positives on ordinary code; a generic
# "password=..."/"Bearer ..." check is NOT included here for that reason.
#
# Uses `git diff --cached --quiet -G<pattern>` — asking git itself whether
# the staged diff contains an added/removed line matching the pattern —
# instead of capturing the diff text and grepping it. Two reasons, both
# found in review:
#   - No pathspec restriction (earlier code had `-- .`, scoping the scan to
#     the invoking cwd's subtree — but `git commit` with no pathspec
#     commits the ENTIRE staged index regardless of cwd, so a secret staged
#     elsewhere in the repo, committed from a subdirectory, went unchecked).
#   - No size limit: capturing the full diff text (bash command
#     substitution here; execFileSync's default 1 MiB buffer in the
#     OpenCode mirror) silently drops or truncates a large staged diff. -G
#     with --quiet asks git the yes/no question directly and never
#     materializes the patch text, so this holds regardless of diff size —
#     confirmed against a ~2.7 MB staged diff.
# -G matches an ADDED-OR-REMOVED line (not added-only, unlike the previous
# grep-based check) — a secret-shaped line being REMOVED can also trigger
# this, which is the safe direction (more cautious, never a bypass), not a
# security concern.
#
# Exit code is checked explicitly rather than treating "anything non-zero"
# as a match: 0 = no match (allow), 1 = match found (deny), anything else
# (an unrelated git error) falls open — same fail-toward-allow posture as
# the rest of this hook.
CONTENT_PATTERNS=(
	'-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----'
	'AKIA[0-9A-Z]{16}'
	'gh[pousr]_[A-Za-z0-9]{36}'
	'xox[baprs]-[A-Za-z0-9-]{10,}'
)

for pattern in "${CONTENT_PATTERNS[@]}"; do
	git -C "$TARGET_DIR" diff --cached --quiet -G "$pattern" >/dev/null 2>&1
	rc=$?
	if [ "$rc" -eq 1 ]; then
		echo "Staged changes contain what looks like a secret (matched: $pattern) — unstage the offending file and rotate the credential if it was ever committed — $BYPASS_HINT" >&2
		exit 2
	fi
done

exit 0
