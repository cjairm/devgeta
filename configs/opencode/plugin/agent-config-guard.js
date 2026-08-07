// OpenCode plugin: deny an Edit or Write that would modify an agent's own
// configuration — permissions, hooks, agent/command/skill definitions, or
// plugins — rather than the source code it was asked to work on. Mirrors
// configs/claude/agent-config-guard.sh — see that file's header comment for
// the full rationale, ADR-0014, and the exact four-clause rule; both files
// must stay in sync.
//
// Scope: GLOBAL (see ADR-0006) — fires in every repo. It replaces a deny
// rule (`.claude/**` / `.opencode/**` in opencode.json.tmpl) that was
// already global, and must stay global to not silently narrow that
// coverage.
//
// This file introduces no import and no new plugin-loader export beyond its
// own factory — see secret-guard.js's header comment (and ADR-0006) for why
// a standalone "lib" module under plugin/ is never the right move here. It
// doesn't need one anyway: this guard shares no logic with the devgeta-repo
// gate the other three guards use, since it has no devgeta-repo scoping.
//
// CANONICALIZE: unlike the bash mirror, this file does not hand-roll a
// bounded symlink-chase — `fs.realpathSync` already resolves an EXISTING
// path's full symlink chain (file or directory, any depth) in one call,
// including the OS's own loop detection (throws ELOOP), which the bash
// version has to approximate with a hop counter. The only piece Node's
// stdlib doesn't provide is "resolve as much of a not-yet-existing path as
// exists" (`fs.realpathSync` throws ENOENT on the first missing component),
// so `canonicalize` below still does the deepest-existing-ancestor walk by
// hand before delegating to `realpathSync` — same two-step shape as the
// bash version (`lexical clean` is `path.resolve`, `resolve symlinks on the
// ancestor` is `realpathSync`, `fall back on any failure` is the try/catch).
//
// Escape hatch: set DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 in the shell that
// launches this agent (e.g. in the repo's .envrc or your shell profile)
// BEFORE invoking this plugin — this plugin reads its own environment, not
// one set inside the command.

import fs from "node:fs";
import path from "node:path";

const BYPASS_HINT =
  "bypass: export DEVGETA_SKIP_AGENT_CONFIG_GUARD=1 in the shell that launches this agent (e.g. the repo's .envrc), not inside the command — this hook reads its own environment";

// canonicalize resolves `target` (absolute or relative to `base`) to an
// absolute path with `.`/`..` collapsed and symlinks resolved on its
// deepest existing ancestor — the tail below that ancestor is appended
// unresolved, since a Write target usually doesn't exist yet. Falls back to
// the lexically-resolved (unresolved) path on any realpath failure —
// mirrors agent-config-guard.sh's canonicalize; see that file's comment for
// why this is the one place the guard family's fail-open stance doesn't
// apply to symlink resolution specifically.
function canonicalize(target, base) {
  const abs = path.resolve(base, target);
  let probe = abs;
  let tail = "";
  while (probe !== path.sep && !fs.existsSync(probe)) {
    tail = path.sep + path.basename(probe) + tail;
    probe = path.dirname(probe);
  }
  try {
    const resolved = fs.realpathSync(probe);
    return tail ? path.join(resolved, tail) : resolved;
  } catch {
    return abs;
  }
}

function pathSegments(p) {
  return p.split(path.sep).filter((s) => s.length > 0);
}

// A `.claude` segment is only harmless when what follows it is one of the two
// known non-config subtrees. `..`/symlinks are already collapsed by
// canonicalize, so an index past the end means the path genuinely ends there
// — an out-of-range read is `undefined` and matches neither name.
function claudeSegmentExcepted(segs, i) {
  // a. `.claude/worktrees/...` — an in-repo checkout of some repository.
  if (segs[i + 1] === "worktrees") {
    return true;
  }
  // b. `.claude/projects/<slug>/memory/<file>` — agent-authored memory. The
  //    length bound requires a file strictly BELOW memory/, so the directory
  //    itself (and a plain file named `memory`) stays denied.
  return (
    segs[i + 1] === "projects" &&
    segs[i + 3] === "memory" &&
    i + 4 < segs.length
  );
}

// Clause 1: a `.claude` segment whose following segments are neither
// `worktrees` nor `projects/<slug>/memory/<file>` — see the bash mirror's
// header comment for why memory is agent data, not agent config.
function clause1Denied(canon) {
  const segs = pathSegments(canon);
  for (let i = 0; i < segs.length; i++) {
    if (segs[i] === ".claude" && !claudeSegmentExcepted(segs, i)) {
      return true;
    }
  }
  return false;
}

// Clause 2: ANY `.opencode` segment — deliberately no `worktrees` exception.
function clause2Denied(canon) {
  return pathSegments(canon).includes(".opencode");
}

function isUnderOrEqual(p, root) {
  return p === root || p.startsWith(root + path.sep);
}

// Clause 3: under OpenCode's resolved global config root, or under
// $OPENCODE_CONFIG_DIR when set — both verified additive against the
// installed OpenCode binary (`opencode debug paths`), not relocating, so
// this guard (deployed to the default root) still loads under either.
function clause3Denied(canon, guardCwd) {
  const configHome =
    process.env.XDG_CONFIG_HOME || path.join(process.env.HOME || "", ".config");
  const defaultRoot = canonicalize(path.join(configHome, "opencode"), guardCwd);
  if (isUnderOrEqual(canon, defaultRoot)) {
    return true;
  }
  if (process.env.OPENCODE_CONFIG_DIR) {
    const extraRoot = canonicalize(process.env.OPENCODE_CONFIG_DIR, guardCwd);
    if (isUnderOrEqual(canon, extraRoot)) {
      return true;
    }
  }
  return false;
}

// Clause 4: equals the file named by $OPENCODE_CONFIG, resolved against
// every candidate base — OpenCode's own resolution base for a relative
// value could not be established from outside the binary (ADR-0014 §1), so
// this checks both the session's directory and this plugin's own process
// cwd rather than guessing one.
function clause4Denied(canon, sessionCwd, processCwd) {
  if (!process.env.OPENCODE_CONFIG) {
    return false;
  }
  if (canon === canonicalize(process.env.OPENCODE_CONFIG, sessionCwd)) {
    return true;
  }
  return canon === canonicalize(process.env.OPENCODE_CONFIG, processCwd);
}

export const AgentConfigGuard = async (ctx = {}) => {
  const sessionCwd = ctx.directory || ctx.worktree || process.cwd();

  return {
    "tool.execute.before": async (input, output) => {
      if (process.env.DEVGETA_SKIP_AGENT_CONFIG_GUARD) {
        return;
      }
      if (input.tool !== "edit" && input.tool !== "write") {
        return;
      }

      const filePath = output?.args?.filePath;
      if (!filePath || typeof filePath !== "string") {
        return;
      }

      const canon = canonicalize(filePath, sessionCwd);

      if (clause1Denied(canon)) {
        throw new Error(
          `Denies edits under .claude/ outside .claude/worktrees/ and .claude/projects/<slug>/memory/ — an agent must not rewrite its own permissions, hooks, or definitions — ${BYPASS_HINT}`,
        );
      }
      if (clause2Denied(canon)) {
        throw new Error(
          `Denies edits under any .opencode/ directory — an agent must not rewrite its own permissions, hooks, or definitions — ${BYPASS_HINT}`,
        );
      }
      if (clause3Denied(canon, sessionCwd)) {
        throw new Error(
          `Denies edits to OpenCode's global config root — an agent must not rewrite its own permissions or plugins — ${BYPASS_HINT}`,
        );
      }
      if (clause4Denied(canon, sessionCwd, process.cwd())) {
        throw new Error(
          `Denies edits to the file named by $OPENCODE_CONFIG — an agent must not rewrite its own permissions — ${BYPASS_HINT}`,
        );
      }
    },
  };
};
