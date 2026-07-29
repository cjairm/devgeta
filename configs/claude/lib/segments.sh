# Shared bash helpers for devgeta's PreToolUse hook scripts (task-redirect.sh,
# secret-guard.sh): splitting a shell command string into individual "command
# segments" so a rule can be checked against each one, not just the start of
# the whole string. Sourced, never executed directly — this file has no
# shebang-driven side effects, only function/constant definitions.
#
# Extracted here (rather than duplicated in every script that needs it —
# CLAUDE.md's DRY rule) because each caller needs the IDENTICAL segmentation
# algorithm to catch a target invocation (git worktree add, git commit, ...)
# anywhere in a compound command like `cd x && git commit -m "..."`. See
# task-redirect.sh's header comment for what this best-effort,
# non-adversarial split deliberately does not handle (escaped quotes, command
# substitution, heredocs).

# A leading run of shell VAR=value assignments a target invocation's anchor
# should tolerate (e.g. `GIT_PAGER=cat git diff a..b`).
DEVGETA_ENV_ASSIGN='[A-Za-z_][A-Za-z0-9_]*=[^[:space:]]*'

# DEVGETA_GIT_GLOBAL_OPT matches one git global option that can appear
# between `git` and its subcommand: a value-taking short flag (`-C <dir>`,
# `-c <name>=<value>`), a long flag with its value attached (`--long=value`)
# OR space-separated (`--long value`, e.g. `--git-dir /path`), a bare
# `--long` flag, or any other bare short flag. Used by task-redirect.sh's and
# secret-guard.sh's GIT_ANCHOR — without the value-taking alternatives, a
# plain `^git\s+<subcommand>` anchor missed the everyday
# `git -C ../other-repo <subcommand> ...`, letting that whole class of
# command bypass both hooks entirely. The space-separated long-flag
# alternative (added alongside the bare one) lets the engine pick whichever
# reading makes the overall anchored pattern match — e.g. for
# `--repo owner/repo pr checks`, only the "consumes a value" reading lets the
# rest of the pattern find the real subcommand, so that's the one that
# succeeds; for `--no-pager commit`, only the "bare flag" reading succeeds,
# since treating "commit" as --no-pager's value would leave no "commit"
# left for the pattern to require next. Not exhaustive — this is
# best-effort pattern-matching, not a git-arg parser (see task-redirect.sh's
# header comment for what this family of hooks deliberately does not
# handle).
DEVGETA_GIT_GLOBAL_OPT='(-[Cc][[:space:]]+[^[:space:]]+|--[A-Za-z-]+=[^[:space:]]+|--[A-Za-z-]+[[:space:]]+[^[:space:]]+|--[A-Za-z-]+|-[A-Za-z])'

# DEVGETA_GH_GLOBAL_OPT is the same shape for `gh`'s global options — most
# notably `-R <owner/repo>`/`--repo <owner/repo>` or `--repo=<owner/repo>`,
# the `gh` equivalent of git's `-C`. Used by task-redirect.sh's GH_ANCHOR.
# See DEVGETA_GIT_GLOBAL_OPT's comment for why both the `=value` and
# space-separated `value` forms of a long flag are needed.
DEVGETA_GH_GLOBAL_OPT='(-R[[:space:]]+[^[:space:]]+|--[A-Za-z-]+=[^[:space:]]+|--[A-Za-z-]+[[:space:]]+[^[:space:]]+|--[A-Za-z-]+|-[A-Za-z])'

# devgeta_trim strips leading/trailing whitespace so a segment that followed
# a separator (e.g. " git commit -m foo") anchors correctly against a `^`
# pattern.
devgeta_trim() {
	local s="$1"
	s="${s#"${s%%[![:space:]]*}"}"
	s="${s%"${s##*[![:space:]]}"}"
	printf '%s' "$s"
}

# devgeta_split_command_segments prints each shell "command segment" of its
# argument on its own line, splitting on unquoted &&, ||, ;, and | (a
# superset of everywhere a shell treats a new command as starting). A
# single- or double-quoted span is tracked so a separator character inside it
# (e.g. in a commit message) is not treated as a boundary.
devgeta_split_command_segments() {
	local command="$1"
	local -a segments=()
	local current=""
	local in_single=0 in_double=0
	local len=${#command}
	local i=0 c two
	while [ "$i" -lt "$len" ]; do
		c="${command:i:1}"
		if [ "$in_single" -eq 1 ]; then
			current+="$c"
			[ "$c" = "'" ] && in_single=0
			i=$((i + 1))
			continue
		fi
		if [ "$in_double" -eq 1 ]; then
			current+="$c"
			[ "$c" = '"' ] && in_double=0
			i=$((i + 1))
			continue
		fi
		if [ "$c" = "'" ]; then
			in_single=1
			current+="$c"
			i=$((i + 1))
			continue
		fi
		if [ "$c" = '"' ]; then
			in_double=1
			current+="$c"
			i=$((i + 1))
			continue
		fi
		two="${command:i:2}"
		if [ "$two" = "&&" ] || [ "$two" = "||" ]; then
			segments+=("$current")
			current=""
			i=$((i + 2))
			continue
		fi
		if [ "$c" = ";" ] || [ "$c" = "|" ]; then
			segments+=("$current")
			current=""
			i=$((i + 1))
			continue
		fi
		current+="$c"
		i=$((i + 1))
	done
	segments+=("$current")
	printf '%s\n' "${segments[@]}"
}
