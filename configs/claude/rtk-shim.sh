#!/usr/bin/env bash
# PreToolUse hook: forwards a Bash tool call's payload to `rtk hook claude`
# (rtk's own PreToolUse hook, run when the rtk integration is enabled — see
# ADR-0038) and re-emits its JSON output with `permissionDecision` and
# `permissionDecisionReason` stripped, leaving `updatedInput` intact. This
# keeps rtk's token-saving command rewrite while stopping it from also
# deciding the permission — a PreToolUse hook's `permissionDecision: "allow"`
# overrides both the `ask` and the `deny` list (ADR-0038's probe table), so
# without this shim devgeta's own allow/ask/deny lists are not the policy in
# force for anything rtk speaks for.
#
# Fail-open to silence (no stdout, exit 0) — the call then proceeds through
# the normal permission flow unaffected, exactly as if this hook were not
# installed — whenever: the rtk binary is missing from PATH, `rtk hook
# claude` exits non-zero, its stdout is empty, or its stdout is not valid
# JSON. Same fail-open posture as task-redirect.sh's contract comment
# (configs/claude/task-redirect.sh:17), except this hook never denies — it
# only ever rewrites or stays silent.
#
# Escape hatch: set DEVGETA_SKIP_RTK_SHIM=1 in the shell that launches this
# agent (e.g. the repo's .envrc), not inside the command — this hook reads
# its own environment, checked before touching stdin so it works even if
# stdin is malformed.
set -u

if [ -n "${DEVGETA_SKIP_RTK_SHIM:-}" ]; then
	exit 0
fi

command -v rtk >/dev/null 2>&1 || exit 0
command -v jq >/dev/null 2>&1 || exit 0

input=$(cat)

output=$(printf '%s' "$input" | rtk hook claude 2>/dev/null)
rc=$?
[ "$rc" -eq 0 ] || exit 0
[ -n "$output" ] || exit 0

printf '%s' "$output" | jq -e . >/dev/null 2>&1 || exit 0

printf '%s' "$output" | jq 'del(.hookSpecificOutput.permissionDecision, .hookSpecificOutput.permissionDecisionReason)'
