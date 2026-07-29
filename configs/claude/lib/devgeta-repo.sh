# Shared bash helper for devgeta's PreToolUse hook scripts that gate a rule to
# the devgeta repo only (task-redirect.sh's worktree-add/worktree-remove and
# release rules, suppression-guard.sh's lint-suppression ban — see ADR-0006
# for why that ban must NOT fire in other repos). Sourced, never executed
# directly.

# devgeta_is_repo answers "is <dir> (or an ancestor of it) the devgeta repo?"
# by walking UP from <dir> looking for the FIRST go.mod and checking its
# module path.
#
# CRITICAL: fails TOWARD false on any uncertainty — no dir, no go.mod found,
# an unreadable go.mod, or a non-matching module path. The unacceptable
# outcome is a devgeta-only rule wrongly firing in someone else's repo, never
# "the rule didn't fire here." Callers that check this more than once per
# invocation should memoize the result themselves (this function does not
# cache anything).
devgeta_is_repo() {
	local dir="$1"
	if [ -z "$dir" ]; then
		return 1
	fi
	while [ -n "$dir" ] && [ "$dir" != "/" ]; do
		if [ -f "$dir/go.mod" ]; then
			grep -qE '^module[[:space:]]+github\.com/cjairm/devgeta($|/)' "$dir/go.mod" 2>/dev/null
			return
		fi
		dir=$(dirname "$dir")
	done
	return 1
}
