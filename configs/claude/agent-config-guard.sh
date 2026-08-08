#!/usr/bin/env bash
# PreToolUse hook: deny an Edit or Write that would modify an agent's own
# configuration — permissions, hooks, agent/command/skill definitions, or
# plugins — rather than the source code it was asked to work on. CLAUDE.md
# §4: an agent that can rewrite its own permissions can grant itself any
# permission the deny list withholds.
#
# Scope: GLOBAL (see ADR-0006's test — "an agent should not rewrite its own
# configuration" has no devgeta-specific opinion in it, unlike
# suppression-guard.sh's lint-suppression ban). It also MUST be global: it
# replaces a deny rule (`Edit(.claude/**)` / `Edit(.opencode/**)`) that was
# already global.
#
# See ADR-0014 for the full design and why a settings.json deny rule cannot
# express this rule on its own (deny is evaluated before allow with no
# specificity tiebreak, so a `.claude/worktrees/**` allow carve-out is a
# no-op). This hook is a fourth member of the guard family ADR-0006
# established (secret-guard.sh, suppression-guard.sh, task-redirect.sh) and
# follows its conventions: PreToolUse deny via exit 2, a one-line reason on
# stderr, and a bypass env var read from the launching shell.
#
# THE RULE (mirrored exactly in configs/opencode/plugin/agent-config-guard.js
# — keep both in sync). Canonicalize the target path, then deny when it:
#
#   1. has a `.claude` segment NOT immediately followed by either
#        a. `worktrees` (Claude Code's own worktree location — ADR-0010), or
#        b. `projects/<one segment>/memory/` with a file below it — Claude
#           Code's per-project memory directory. Memory files are notes the
#           agent is DESIGNED to write; they carry no permission, hook, or
#           definition surface, so denying them blocked a feature without
#           protecting anything. Only `memory/` is excepted, not the rest of
#           `projects/<slug>/`, and only for a file strictly under it;
#   2. has ANY `.opencode` segment — no exception; nothing creates
#      `.opencode/worktrees/`, so granting one would only widen the rule;
#   3. falls under OpenCode's resolved global config root
#      (`${XDG_CONFIG_HOME:-$HOME/.config}/opencode`) or under
#      `$OPENCODE_CONFIG_DIR` when set — both are real, OpenCode-defined
#      config sources, verified empirically against the installed OpenCode
#      binary (`opencode debug paths`): both are ADDITIVE, not relocating,
#      so a guard deployed to the default root still loads and can enforce
#      this;
#   4. equals the file named by `$OPENCODE_CONFIG` when set, resolved
#      against every candidate base (see CANONICALIZE below) since
#      OpenCode's own resolution base for a relative value could not be
#      established from outside the binary.
#
# `$CLAUDE_CONFIG_DIR` is deliberately NOT handled: devgeta deploys this very
# hook to a fixed `~/.claude/`, and settings.json.tmpl's own hook commands
# are literal `~/.claude/*.sh` — under a relocated Claude root, Claude Code
# reads a settings.json devgeta never wrote, so this hook does not even run.
# A clause for that root would assert protection that cannot exist. This is
# not a guard-specific gap: nothing devgeta ships for Claude reaches the
# agent under a relocated CLAUDE_CONFIG_DIR.
#
# CANONICALIZE: this is what makes the `worktrees` exception (and clause 4's
# equality check) correct rather than trivially escapable. Two escapes a
# naive segment walk would miss:
#   - Lexically: `.claude/worktrees/../agents/x.md` has a `.claude` followed
#     by `worktrees` on a naive walk, while resolving to `.claude/agents/x.md`.
#   - Through a symlink: a link under `worktrees/` pointing at `.claude/agents/`
#     puts an allowed-looking path inside a denied directory — and the
#     reverse (an innocent-looking path reaching a denied target through a
#     symlink) is exactly the same mechanism.
# Claude Code's own deny rules resolve symlinks on both the link and its
# target; a hook receives the raw `tool_input.file_path` and gets none of
# that for free, so skipping this step would make the guard WEAKER than the
# deny rule it replaces.
#
# Order: make the path absolute against the payload's cwd, lexically clean
# `.`/`..` (pure string work, always succeeds), then resolve symlinks on the
# deepest EXISTING ancestor and re-append the not-yet-existing tail — a
# Write target usually does not exist yet, so resolving the full path is not
# an option. Symlink resolution does I/O and is bounded (16 hops) against a
# self-referential loop; on any failure this hook falls back to the
# lexically-cleaned (unresolved) path rather than skipping the check — the
# one place this guard family's usual fail-open stance is wrong, so `..` is
# closed unconditionally and only the symlink-resolution step is best-effort.
#
# Claude Code delivers the hook payload as JSON on stdin: `.tool_name`
# ("Edit" or "Write"), `.tool_input.file_path`, and top-level `.cwd` — the
# same fields suppression-guard.sh already relies on. Denies via exit 2 with
# a one-line reason on stderr. A missing/unparseable payload, or `jq` being
# unavailable, falls through to exit 0 (allow) for the GUARD itself — the
# settings.json floor (ADR-0014 §3) is what still holds when this happens.
#
# Escape hatch: set DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 in the shell that
# launches this agent (e.g. in the repo's .envrc or your shell profile)
# BEFORE invoking this hook — this hook reads its own environment, not one
# set inside the command.

if [ -n "${DEVGETA_SKIP_AGENT_CONFIG_GUARD:-}" ]; then
	exit 0
fi

input=$(cat)
TOOL=$(printf '%s' "$input" | jq -r '.tool_name // empty' 2>/dev/null)
case "$TOOL" in
Edit | Write) ;;
*) exit 0 ;;
esac

FILE_PATH=$(printf '%s' "$input" | jq -r '.tool_input.file_path // empty' 2>/dev/null)
[ -z "$FILE_PATH" ] && exit 0

CWD=$(printf '%s' "$input" | jq -r '.cwd // empty' 2>/dev/null)
CWD="${CWD:-$PWD}"

BYPASS_HINT="bypass: export DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 in the shell that launches this agent (e.g. the repo's .envrc), not inside the command — this hook reads its own environment"

# --- Lexical `.`/`..` collapse — pure string work, always succeeds. -------

lexical_clean() {
	local target="$1" rest seg
	local -a stack=()
	rest="${target#/}"
	while [ -n "$rest" ]; do
		case "$rest" in
		*/*)
			seg="${rest%%/*}"
			rest="${rest#*/}"
			;;
		*)
			seg="$rest"
			rest=""
			;;
		esac
		case "$seg" in
		"" | .) ;;
		..)
			[ "${#stack[@]}" -gt 0 ] && unset "stack[$((${#stack[@]} - 1))]"
			;;
		*)
			stack+=("$seg")
			;;
		esac
	done
	local out="" s
	for s in "${stack[@]}"; do
		out="$out/$s"
	done
	printf '%s' "${out:-/}"
}

# --- Deepest-existing-ancestor walk. Sets OUT_ANCESTOR / OUT_TAIL. --------

find_existing_ancestor() {
	local cleaned="$1" probe tail=""
	probe="$cleaned"
	while [ "$probe" != "/" ] && [ ! -e "$probe" ]; do
		tail="/${probe##*/}$tail"
		probe="${probe%/*}"
		[ -z "$probe" ] && probe="/"
	done
	OUT_ANCESTOR="$probe"
	OUT_TAIL="$tail"
}

# --- Resolve an EXISTING path's symlinks (bounded), directory or file. ---

resolve_existing() {
	local p="$1" hops=0 dir target
	while [ -L "$p" ]; do
		hops=$((hops + 1))
		if [ "$hops" -gt 16 ]; then
			printf '%s' "$p"
			return 1
		fi
		dir=$(cd "$(dirname "$p")" 2>/dev/null && pwd -P) || {
			printf '%s' "$p"
			return 1
		}
		target=$(readlink "$p")
		case "$target" in
		/*) p="$target" ;;
		*) p="$dir/$target" ;;
		esac
	done
	if [ -d "$p" ]; then
		(cd "$p" 2>/dev/null && pwd -P) || {
			printf '%s' "$p"
			return 1
		}
	else
		dir=$(cd "$(dirname "$p")" 2>/dev/null && pwd -P) || {
			printf '%s' "$p"
			return 1
		}
		printf '%s/%s' "$dir" "$(basename "$p")"
	fi
}

# --- canonicalize: the full pipeline for one path against one base. ------

canonicalize() {
	local target="$1" base="$2" cleaned resolved
	case "$target" in
	/*) ;;
	*) target="$base/$target" ;;
	esac
	cleaned=$(lexical_clean "$target")

	find_existing_ancestor "$cleaned"

	resolved=$(resolve_existing "$OUT_ANCESTOR")
	if [ $? -ne 0 ]; then
		printf '%s' "$cleaned"
		return
	fi

	if [ "$resolved" = "/" ]; then
		printf '%s' "${OUT_TAIL:-/}"
	else
		printf '%s' "$resolved$OUT_TAIL"
	fi
}

# --- Segment access for clauses 1-2. Sets PATH_SEGS. ----------------------

split_segments() {
	local p="${1#/}" rest seg
	PATH_SEGS=()
	rest="$p"
	while [ -n "$rest" ]; do
		case "$rest" in
		*/*)
			seg="${rest%%/*}"
			rest="${rest#*/}"
			;;
		*)
			seg="$rest"
			rest=""
			;;
		esac
		[ -n "$seg" ] && PATH_SEGS+=("$seg")
	done
}

# A `.claude` segment is only harmless when what follows it is one of the two
# known non-config subtrees. `..`/symlinks are already collapsed by
# canonicalize, so an out-of-range index here means the path genuinely ends
# there — an unset PATH_SEGS entry expands to "" and matches neither name.
claude_segment_excepted() {
	local idx="$1" n="$2"
	# a. `.claude/worktrees/...` — an in-repo checkout of some repository.
	[ "${PATH_SEGS[$((idx + 1))]:-}" = "worktrees" ] && return 0
	# b. `.claude/projects/<slug>/memory/<file>` — agent-authored memory.
	#    The `n` bound requires a file strictly BELOW memory/, so the
	#    directory itself (and a plain file named `memory`) stays denied.
	if [ "${PATH_SEGS[$((idx + 1))]:-}" = "projects" ] &&
		[ "${PATH_SEGS[$((idx + 3))]:-}" = "memory" ] &&
		[ "$((idx + 4))" -lt "$n" ]; then
		return 0
	fi
	return 1
}

clause1_denied() {
	split_segments "$1"
	local n="${#PATH_SEGS[@]}" idx=0
	while [ "$idx" -lt "$n" ]; do
		if [ "${PATH_SEGS[$idx]}" = ".claude" ] && ! claude_segment_excepted "$idx" "$n"; then
			return 0
		fi
		idx=$((idx + 1))
	done
	return 1
}

clause2_denied() {
	split_segments "$1"
	local seg
	for seg in "${PATH_SEGS[@]}"; do
		[ "$seg" = ".opencode" ] && return 0
	done
	return 1
}

# --- Prefix check for clause 3. -------------------------------------------

is_under_or_equal() {
	local path="$1" root="$2"
	[ "$path" = "$root" ] && return 0
	case "$path" in
	"$root"/*) return 0 ;;
	esac
	return 1
}

clause3_denied() {
	local canon="$1" default_root extra_root
	default_root=$(canonicalize "${XDG_CONFIG_HOME:-$HOME/.config}/opencode" "$PWD")
	is_under_or_equal "$canon" "$default_root" && return 0
	if [ -n "${OPENCODE_CONFIG_DIR:-}" ]; then
		extra_root=$(canonicalize "$OPENCODE_CONFIG_DIR" "$PWD")
		is_under_or_equal "$canon" "$extra_root" && return 0
	fi
	return 1
}

clause4_denied() {
	local canon="$1" payload_cwd="$2" cand
	[ -z "${OPENCODE_CONFIG:-}" ] && return 1
	cand=$(canonicalize "$OPENCODE_CONFIG" "$payload_cwd")
	[ "$canon" = "$cand" ] && return 0
	cand=$(canonicalize "$OPENCODE_CONFIG" "$PWD")
	[ "$canon" = "$cand" ] && return 0
	return 1
}

# --- Evaluate. -------------------------------------------------------------

CANON=$(canonicalize "$FILE_PATH" "$CWD")

if clause1_denied "$CANON"; then
	echo "Denies edits under .claude/ outside .claude/worktrees/ and .claude/projects/<slug>/memory/ — an agent must not rewrite its own permissions, hooks, or definitions — $BYPASS_HINT" >&2
	exit 2
fi
if clause2_denied "$CANON"; then
	echo "Denies edits under any .opencode/ directory — an agent must not rewrite its own permissions, hooks, or definitions — $BYPASS_HINT" >&2
	exit 2
fi
if clause3_denied "$CANON"; then
	echo "Denies edits to OpenCode's global config root — an agent must not rewrite its own permissions or plugins — $BYPASS_HINT" >&2
	exit 2
fi
if clause4_denied "$CANON" "$CWD"; then
	echo "Denies edits to the file named by \$OPENCODE_CONFIG — an agent must not rewrite its own permissions — $BYPASS_HINT" >&2
	exit 2
fi

exit 0
