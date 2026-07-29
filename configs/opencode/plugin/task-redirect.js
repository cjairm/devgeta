// OpenCode plugin: intercept a narrow set of raw-git command patterns that
// have a dedicated `devgeta task` equivalent, and deny with the exact
// replacement to run instead. See docs/apps/claude.md ("Command redirect
// (PreToolUse hook)") for the full contract — this is the OpenCode-side
// mirror of configs/claude/task-redirect.sh's PreToolUse hook. Keep the two
// pattern tables in sync — they intentionally mirror each other one-for-one
// (deliberately not sharing code — different languages, different plugin
// runtimes).
//
// Rule scope (these hooks deploy to the user's GLOBAL config, so they fire in
// EVERY repo):
//   - Global rules fire everywhere: review-package (git diff/log
//     <ref>..<ref>), worktree-start (git worktree add), worktree-finish (git
//     worktree remove), and the gh redirects pr-checks / review-threads /
//     submit-review. They impose no repo-specific convention.
//   - The two release rules (git reset --soft HEAD~N, git tag -a v<semver>)
//     are devgeta-repo-only: they encode devgeta's own release policy and
//     would be wrong to steer in other repos. They fire only when
//     isDevgetaRepo confirms a go.mod with module github.com/cjairm/devgeta,
//     and fail toward NOT firing on any uncertainty (see isDevgetaRepo).
//
// Working directory for the release gate: the plugin factory receives an
// OpenCode context object with `directory` (current working dir) and
// `worktree` (git worktree path) per opencode.ai/docs/plugins/. We prefer
// `directory`, fall back to `worktree`, then process.cwd(). isDevgetaRepo
// walks up from there for a matching go.mod; if none of these yield a devgeta
// go.mod, the release rules simply do not fire.
//
// API shape (tool.execute.before: async (input, output) => {...}, deny by
// throwing) is per OpenCode's plugin docs (opencode.ai/docs/plugins/) as of
// research done for this change. Plugin files are loaded from
// ~/.config/opencode/plugin/ (or .opencode/plugin/ per-project); OpenCode's
// docs primarily use the plural "plugins/" but explicitly state singular
// directory names are also supported for backwards compatibility, so this
// file's deploy path (configs/opencode/plugin/, singular, mirroring the
// Claude Code side) loads correctly either way.
//
// Escape hatch: set DEVGETA_SKIP_TASK_REDIRECT=1 in the environment to bypass
// this plugin entirely when raw git is genuinely needed. Every deny message
// repeats this so no flow dead-ends.
//
// NB: the command string is only ever pattern-matched (regex), never eval'd
// or executed — no command-injection surface from the tool call payload.
//
// Matching scope (read before touching RULES below):
//   - Each rule is checked against every "command segment" of the input, not
//     just the start of the whole string. Segments are split on unquoted &&,
//     ||, ;, and | so that `cd x && git worktree add y`, `git status; git
//     worktree remove y`, and `git fetch && git diff a..b` are all caught —
//     not just a bare `git ...` with nothing else on the line.
//   - Each rule's `git` anchor (GIT_PREFIX) also tolerates a leading run of
//     shell VAR=value assignments (e.g. `GIT_PAGER=cat git diff a..b`), so an
//     env-var prefix with NO separator character before `git` is caught too.
//   - Splitting respects single- and double-quoted spans (a `;` or `&&`
//     inside a quoted commit message is not treated as a boundary), but this
//     is a best-effort, non-adversarial split: it does not handle
//     backslash-escaped quotes, command substitution ($(...) / `...`), or
//     heredocs. A command deliberately crafted to defeat quote tracking is
//     out of scope — this plugin pattern-matches agent-typed commands, it is
//     not a shell parser.

import { existsSync, readFileSync } from "node:fs";
import { dirname, join } from "node:path";

const BYPASS_HINT =
  "set DEVGETA_SKIP_TASK_REDIRECT=1 to bypass this session if raw git is genuinely needed";

// isDevgetaRepo answers "is the command running inside the devgeta repo?" —
// the gate for the release rules (git reset --soft HEAD~N, git tag -a
// v<semver>), which encode devgeta's OWN release policy and must not steer the
// universal git techniques in other repos. It walks UP from startDir looking
// for the FIRST go.mod; the repo IS devgeta only if that go.mod's module path
// is exactly github.com/cjairm/devgeta.
//
// CRITICAL: it fails TOWARD false (release rules do NOT fire) on any
// uncertainty — no startDir, no go.mod found, an unreadable go.mod, or a
// non-matching module path. The unacceptable outcome is a general reset/tag
// being wrongly blocked outside devgeta; "the release redirect didn't fire" is
// always the safe fallback. Exported so it is unit-testable with injected
// paths.
const DEVGETA_MODULE_RE = /^module\s+github\.com\/cjairm\/devgeta($|\/)/m;
export function isDevgetaRepo(startDir) {
  if (!startDir || typeof startDir !== "string") {
    return false;
  }
  let dir = startDir;
  // Bounded walk up to the filesystem root (dirname is idempotent at root).
  for (;;) {
    const goMod = join(dir, "go.mod");
    if (existsSync(goMod)) {
      try {
        return DEVGETA_MODULE_RE.test(readFileSync(goMod, "utf8"));
      } catch {
        return false;
      }
    }
    const parent = dirname(dir);
    if (parent === dir) {
      return false;
    }
    dir = parent;
  }
}

// A ref..ref (or ref...ref) range token: non-space ref chars on both sides of
// the dot run, so a bare ref like HEAD~1 (no dots) or a lone dotted value like
// v1.2.0 (single dots, no run of 2-3) never matches.
const REF_CHARS = "[A-Za-z0-9._/~^{}@:-]+";
const RANGE_TOKEN = `${REF_CHARS}\\.\\.\\.?${REF_CHARS}`;

// GIT_GLOBAL_OPT mirrors secret-guard.js's — a git global option that can
// appear between `git` and its subcommand (`-C <dir>`, `-c name=value`,
// `--git-dir=<path>` or `--git-dir /path`, a bare `--long-flag`, or a bare
// short `-x` flag). Without the value-taking alternatives, a plain
// `git\s+<subcommand>` anchor missed the everyday
// `git -C ../other-repo <subcommand> ...`, bypassing these redirects
// entirely (found in review, then fixed here too — see secret-guard.js). The
// space-separated long-flag alternative (alongside the bare one) lets the
// engine pick whichever reading makes the overall anchored pattern match —
// see secret-guard.js's GIT_GLOBAL_OPT comment for the `--repo owner/repo`
// vs. `--no-pager` worked example.
const GIT_GLOBAL_OPT =
  "(?:-[Cc]\\s+\\S+|--[A-Za-z-]+=\\S+|--[A-Za-z-]+\\s+\\S+|--[A-Za-z-]+|-[A-Za-z])";
// GH_GLOBAL_OPT is the same shape for `gh`'s global options — most notably
// `-R <owner/repo>`/`--repo owner/repo`/`--repo=owner/repo`, the `gh`
// equivalent of git's `-C`.
const GH_GLOBAL_OPT =
  "(?:-R\\s+\\S+|--[A-Za-z-]+=\\S+|--[A-Za-z-]+\\s+\\S+|--[A-Za-z-]+|-[A-Za-z])";

// Matches the start of a segment that is a `git` invocation, optionally
// preceded by one or more shell VAR=value assignments (e.g.
// `GIT_PAGER=cat git diff ...`, `FOO=1 BAR=2 git worktree add ...`) and
// tolerating git global options between `git` and the subcommand.
const GIT_PREFIX = `(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*git(?:\\s+${GIT_GLOBAL_OPT})*`;
// Same anchor for `gh` invocations (the GitHub CLI rules below), tolerating
// gh's own global options.
const GH_PREFIX = `(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*gh(?:\\s+${GH_GLOBAL_OPT})*`;

// Each rule: { pattern, message, scope }. `pattern` is tested against a single
// command segment (see splitCommandSegments below), anchored with `^` at the
// start of that segment; `message` is the deny reason (bypass hint is
// appended by the caller). `scope` is "global" (fires everywhere) or "devgeta"
// (fires only inside the devgeta repo — see isDevgetaRepo). Order mirrors
// task-redirect.sh's case order.
const RULES = [
  {
    // git diff <ref>..<ref> / git log <ref>..<ref> (the range review dance).
    // Only fires when a range-shaped token is present among the arguments,
    // regardless of flags before it (--stat, --oneline, -U10, ...). Bare
    // `git diff`, `git diff HEAD~1`, and `git log`/`git log -5` never match.
    pattern: new RegExp(
      `^${GIT_PREFIX}\\s+(diff|log)(\\s+\\S+)*\\s+${RANGE_TOKEN}(\\s|$)`,
    ),
    message:
      "Use: devgeta task review-package <base> <head> (one call: verified range, commits, noise-filtered stats + full diff)",
    scope: "global",
  },
  {
    // git worktree add ...
    pattern: new RegExp(`^${GIT_PREFIX}\\s+worktree\\s+add(\\s|$)`),
    message: "Use: devgeta task worktree-start <name> [--base <ref>]",
    scope: "global",
  },
  {
    // git worktree remove ...
    pattern: new RegExp(`^${GIT_PREFIX}\\s+worktree\\s+remove(\\s|$)`),
    message: "Use: devgeta task worktree-finish [<name>] --merge|--discard",
    scope: "global",
  },
  {
    // git reset --soft HEAD~N (N >= 1). `git reset --soft HEAD` (no ~N,
    // e.g. amend-style staging) is never matched. devgeta-repo-only.
    pattern: new RegExp(
      `^${GIT_PREFIX}\\s+reset\\s+--soft\\s+HEAD~[1-9][0-9]*(\\s|$)`,
    ),
    message:
      "Use: devgeta task release <version> --message-file <file> [--push] (squash + tag flow)",
    scope: "devgeta",
  },
  {
    // gh pr checks — the PR CI-status view. `pr checks` only (never `gh pr
    // view`, `gh pr status`, `gh pr list`): the token after `pr` must be
    // exactly `checks`.
    pattern: new RegExp(`^${GH_PREFIX}\\s+pr\\s+checks\\b`),
    message:
      "Use: devgeta task pr-checks (adds a failing-job log digest the raw command lacks)",
    scope: "global",
  },
  {
    // gh pr review — submitting a PR review. `pr review` only (the token
    // after `pr` must be exactly `review`) — never `gh pr view` (different
    // subcommand) or `gh pr checks`.
    pattern: new RegExp(`^${GH_PREFIX}\\s+pr\\s+review\\b`),
    message:
      "Use: devgeta task submit-review --event approve|request-changes|comment [--body ...] (posts body + inline comments atomically)",
    scope: "global",
  },
];

// git tag -a v<semver> ... needs three independent checks (git tag, a -a
// flag, and a v-prefixed semver-shaped token) so `git tag` (list) and
// `git tag v1.0.0` (lightweight, no -a) are never matched. Kept separate from
// RULES since it isn't a single regex match, mirroring task-redirect.sh's
// three-grep combination for the same rule. All three checks run against the
// SAME segment, so a `-a` flag in one compound-command segment can't combine
// with a `git tag` in another.
const GIT_TAG_PATTERN = new RegExp(`^${GIT_PREFIX}\\s+tag\\b`);
function matchesReleaseTag(segment) {
  return (
    GIT_TAG_PATTERN.test(segment) &&
    /(^|\s)-a(\s|$)/.test(segment) &&
    /(^|\s)v[0-9]+\.[0-9]+\.[0-9]+(\s|$)/.test(segment)
  );
}

// gh api graphql ... reviewThreads needs three independent checks (a `gh`
// invocation, the `api` and `graphql` tokens, and the literal reviewThreads)
// so a bare `gh api graphql` for anything else, or a bare `gh api ...`, never
// matches — reviewThreads must be present. Kept separate from RULES since it
// isn't a single regex match, mirroring task-redirect.sh's grep combination.
// All checks run against the SAME segment. Global scope. reviewThreads inside
// a quoted query=... span survives segment splitting (splitCommandSegments
// keeps quoted spans intact), so it is still matchable here.
const GH_ANCHOR_PATTERN = new RegExp(`^${GH_PREFIX}(\\s|$)`);
function matchesReviewThreads(segment) {
  return (
    GH_ANCHOR_PATTERN.test(segment) &&
    /(^|\s)api(\s|$)/.test(segment) &&
    /(^|\s)graphql(\s|$)/.test(segment) &&
    segment.includes("reviewThreads")
  );
}

// splitCommandSegments splits a shell command string into "command segments"
// on unquoted &&, ||, ;, and | (a superset of everywhere a shell treats a new
// command as starting). A single-quoted or double-quoted span is tracked so
// a separator character inside it (e.g. in a commit message) is not treated
// as a boundary. This is a best-effort, non-adversarial split — see the
// matching-scope comment at the top of this file for what it deliberately
// does not handle (escaped quotes, command substitution, heredocs).
//
// Exported (like isDevgetaRepo above) so secret-guard.js can reuse it
// instead of duplicating it — see ADR-0006 for why that plugin imports this
// named export rather than the two of them sharing a new standalone helper
// file under plugin/ (OpenCode's plugin loader invokes every export of every
// file there as if it were a plugin factory).
export function splitCommandSegments(command) {
  const segments = [];
  let current = "";
  let inSingle = false;
  let inDouble = false;

  for (let i = 0; i < command.length; i++) {
    const c = command[i];

    if (inSingle) {
      current += c;
      if (c === "'") inSingle = false;
      continue;
    }
    if (inDouble) {
      current += c;
      if (c === '"') inDouble = false;
      continue;
    }
    if (c === "'") {
      inSingle = true;
      current += c;
      continue;
    }
    if (c === '"') {
      inDouble = true;
      current += c;
      continue;
    }

    const two = command.slice(i, i + 2);
    if (two === "&&" || two === "||") {
      segments.push(current);
      current = "";
      i += 1; // extra skip; the for-loop's i++ covers the second char
      continue;
    }
    if (c === ";" || c === "|") {
      segments.push(current);
      current = "";
      continue;
    }

    current += c;
  }
  segments.push(current);

  return segments.map((s) => s.trim());
}

// findDenyMessage returns the deny message for the first matching rule across
// all segments, or null. `isDevgetaRepoFn` is a zero-arg memoized predicate:
// a devgeta-scoped rule (release) only denies when it returns true, and it is
// called ONLY after a release pattern has already matched — so a command with
// no release pattern, or a repo where release patterns never match, pays zero
// go.mod-lookup cost.
function findDenyMessage(command, isDevgetaRepoFn) {
  const segments = splitCommandSegments(command);
  for (const segment of segments) {
    for (const rule of RULES) {
      if (rule.pattern.test(segment)) {
        if (rule.scope === "devgeta" && !isDevgetaRepoFn()) {
          continue;
        }
        return rule.message;
      }
    }
    if (matchesReviewThreads(segment)) {
      return "Use: devgeta task review-threads (GraphQL payload rendered to compact markdown + dedup)";
    }
    // git tag -a v<semver> — devgeta-repo-only (checked after the pattern).
    if (matchesReleaseTag(segment) && isDevgetaRepoFn()) {
      return "Use: devgeta task release <version> --message-file <file> [--push] (squash + tag flow)";
    }
  }
  return null;
}

export const TaskRedirect = async (ctx = {}) => {
  // Prefer the OpenCode context's working dir, then the git worktree path,
  // then this process's cwd — used only by the devgeta-repo release gate.
  const projectDir = ctx.directory || ctx.worktree || process.cwd();
  return {
    "tool.execute.before": async (input, output) => {
      if (process.env.DEVGETA_SKIP_TASK_REDIRECT) {
        return;
      }
      if (input.tool !== "bash") {
        return;
      }
      const command = output?.args?.command;
      if (!command || typeof command !== "string") {
        return;
      }

      // Memoize the repo check per invocation: computed at most once, and
      // only if a release pattern actually matches a segment.
      let repoMemo;
      const isDevgetaRepoFn = () => {
        if (repoMemo === undefined) {
          repoMemo = isDevgetaRepo(projectDir);
        }
        return repoMemo;
      };

      const message = findDenyMessage(command, isDevgetaRepoFn);
      if (message) {
        throw new Error(`${message} — ${BYPASS_HINT}`);
      }
    },
  };
};
