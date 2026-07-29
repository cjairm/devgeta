// OpenCode plugin: before a Bash `git commit` runs, scan what's actually
// staged for filenames and content that look like a committed secret, and
// deny with what to unstage. Mirrors configs/claude/secret-guard.sh — see
// that file's header comment for the full rationale and the (deliberately
// narrow) pattern lists, which must stay in sync with the ones below.
//
// Scope: GLOBAL (see ADR-0006) — fires in every repo. This is a safety net,
// not a substitute for a real secret scanner or CI pre-commit hooks.
//
// API shape (tool.execute.before, deny by throwing) matches
// task-redirect.js's, whose header comment documents the research source.
//
// Reuses task-redirect.js's exported splitCommandSegments instead of
// duplicating it. See that export's comment and ADR-0006 for why this file
// does NOT introduce its own standalone "lib" module under plugin/:
// OpenCode's plugin loader (packages/opencode/src/plugin/index.ts's
// getLegacyPlugins) invokes EVERY exported value of EVERY .js file under the
// plugin directory as if it were a plugin factory, throwing if any export
// isn't callable. Importing a named export from an already-loaded plugin
// file (without re-exporting it) adds nothing to either file's own export
// surface, so it carries none of that risk.
//
// Escape hatch: set DEVGETA_SKIP_SECRET_GUARD=1 in the shell that launches this
// agent (e.g. in the repo's .envrc or your shell profile) BEFORE invoking this
// plugin — this plugin reads its own environment, not one set inside the command.
//
// Three things beyond simple pattern-matching, all closing bypasses found
// in review or in manual end-to-end verification:
//   - `cd <dir> && git commit` is checked against <dir> (chained through
//     every `cd` segment before the commit — see effectiveDir below), not
//     ctx.directory — otherwise the hook would silently check the wrong
//     repository. Unlike staging, `cd` only ever changes where LATER
//     segments in the SAME command run, so this is knowable from the
//     command text alone even though nothing has executed yet. Confirmed
//     as a real, live bypass (not just theoretical) while manually
//     verifying this hook end-to-end.
//   - `git -C <dir> commit` is checked against <dir>'s staged index (see
//     targetDir below), same reasoning as `cd`.
//   - A compound "stage, then commit" in ONE Bash call (`git add -A && git
//     commit`), or a bare `git commit -a`/`-am`/`--all`, is denied outright
//     rather than checked: this is a tool.execute.before hook, so it fires
//     BEFORE any part of the Bash command has run — `git diff --cached` at
//     check time cannot reflect a staging action that is part of the SAME,
//     not-yet-run command. Splitting into two separate Bash calls is the
//     only way this hook can make a real assessment.

import { execFileSync } from "node:child_process";
import { splitCommandSegments } from "./task-redirect.js";

const BYPASS_HINT =
  "bypass: export DEVGETA_SKIP_SECRET_GUARD=1 in the shell that launches this agent (e.g. the repo's .envrc), not inside the command — this hook reads its own environment";

const STAGE_SEPARATELY_MESSAGE =
  "This hook can only check what is ALREADY staged — run staging (git add/mv) and git commit as two separate commands so a staged secret can be caught before it's committed";

// GIT_GLOBAL_OPT mirrors secret-guard.sh's DEVGETA_GIT_GLOBAL_OPT — see that
// file's comment for the space-separated-long-flag reasoning
// (`--git-dir /path`, not just `--git-dir=/path`). Without the
// value-taking alternatives, a bare anchor requiring "commit" to
// immediately follow "git" missed the everyday `git -C ../other-repo
// commit ...`, bypassing this guard.
const GIT_GLOBAL_OPT =
  "(?:-[Cc]\\s+\\S+|--[A-Za-z-]+=\\S+|--[A-Za-z-]+\\s+\\S+|--[A-Za-z-]+|-[A-Za-z])";
const GIT_COMMIT_PATTERN = new RegExp(
  `^(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*git(?:\\s+${GIT_GLOBAL_OPT})*\\s+commit(\\s|$)`,
);
// GIT_MUTATION_PATTERN matches an index-mutating command (add/mv/stage) —
// NOT git rm, which only removes content, never introduces it.
const GIT_MUTATION_PATTERN = new RegExp(
  `^(?:[A-Za-z_][A-Za-z0-9_]*=\\S*\\s+)*git(?:\\s+${GIT_GLOBAL_OPT})*\\s+(?:add|mv|stage)(\\s|$)`,
);
// SELF_STAGE_PATTERN matches a short-option cluster containing the letter
// 'a' (-a, -am, -qam, ...) or the long form --all — git commit's own
// auto-stage flags. A long flag like --author=x or --amend is a different,
// unrelated option and does not match (requires a single leading dash).
const SELF_STAGE_PATTERN = /(^|\s)-[a-zA-Z]*a[a-zA-Z]*(\s|$)|(^|\s)--all(\s|$)/;
// CD_PATTERN matches a bare `cd` (optionally with a path argument) segment,
// e.g. `cd ../other-repo`. Not exhaustive — no `pushd`/`popd`, no `cd -P`
// flags; agents overwhelmingly just type plain `cd <path>`.
const CD_PATTERN = /^cd(\s+\S+)?(\s|$)/;

// isSensitiveFilename mirrors secret-guard.sh's is_sensitive_filename: a
// small, high-confidence set of filename shapes that are essentially always
// a committed secret, never ordinary code. *.pub is a PUBLIC key (never a
// secret); .env.example/.env.sample/.env.template are common, legitimate
// committed templates — both are explicitly excluded.
function isSensitiveFilename(path) {
  const base = path.split("/").pop() ?? path;
  if (base.endsWith(".pub")) {
    return false;
  }
  if (
    base === ".env.example" ||
    base === ".env.sample" ||
    base === ".env.template"
  ) {
    return false;
  }
  if (base === ".env" || base.startsWith(".env.")) {
    return true;
  }
  if (/\.(pem|p12|pfx|keystore|jks)$/.test(base)) {
    return true;
  }
  return ["id_rsa", "id_ed25519", "id_ecdsa"].includes(base);
}

// CONTENT_PATTERNS mirrors secret-guard.sh's CONTENT_PATTERNS exactly — keep
// the two lists in sync. Kept deliberately narrow (private-key headers,
// AWS/GitHub/Slack token shapes) to avoid false positives on ordinary code.
const CONTENT_PATTERNS = [
  /-----BEGIN (RSA |EC |OPENSSH |DSA |PGP )?PRIVATE KEY-----/,
  /AKIA[0-9A-Z]{16}/,
  /gh[pousr]_[A-Za-z0-9]{36}/,
  /xox[baprs]-[A-Za-z0-9-]{10,}/,
];

// runGit executes read-only git introspection (never staging/committing/
// mutating anything) and returns its stdout, or null on any failure (git
// missing, not a repo, ...) — the caller treats null as "allow, ambiguous".
function runGit(dir, args) {
  try {
    return execFileSync("git", ["-C", dir, ...args], {
      encoding: "utf8",
      stdio: ["ignore", "pipe", "ignore"],
    });
  } catch {
    return null;
  }
}

// hasStagedContentMatch asks git itself whether the staged diff contains an
// added/removed line matching `pattern`, via `git diff --cached --quiet
// -G<pattern>` — instead of capturing the diff text and testing it here.
// Found in review: execFileSync's default 1 MiB output buffer silently
// dropped large staged diffs (runGit returned null, treated as "allow,
// ambiguous"), letting a real match through untested. `--quiet` never
// materializes the patch text at all, so this holds regardless of diff
// size — confirmed against a ~2.7 MB staged diff. -G matches an
// ADDED-OR-REMOVED line (not added-only, unlike the previous filter) — a
// secret-shaped line being REMOVED can also trigger this, which is the
// safe direction (more cautious, never a bypass).
//
// execFileSync throws on a non-zero exit; the exit code is checked
// explicitly on the thrown error rather than treating any throw as a
// match: status 1 = match found (deny), status 0 never throws (no match,
// allow), anything else (an unrelated git error, no `status` at all, ...)
// falls open — same fail-toward-allow posture as the rest of this hook.
function hasStagedContentMatch(dir, pattern) {
  try {
    execFileSync(
      "git",
      ["-C", dir, "diff", "--cached", "--quiet", "-G", pattern],
      {
        stdio: "ignore",
      },
    );
    return false;
  } catch (err) {
    return err.status === 1;
  }
}

export const SecretGuard = async (ctx = {}) => {
  const projectDir = ctx.directory || ctx.worktree || process.cwd();
  return {
    "tool.execute.before": async (input, output) => {
      if (process.env.DEVGETA_SKIP_SECRET_GUARD) {
        return;
      }
      if (input.tool !== "bash") {
        return;
      }
      const command = output?.args?.command;
      if (!command || typeof command !== "string") {
        return;
      }

      const segments = splitCommandSegments(command);
      // Scan segments in order for the FIRST `git commit` (a second commit
      // in the same compound command is an accepted, unhandled edge case),
      // tracking whether a mutation segment appears BEFORE it, and tracking
      // effectiveDir through any `cd` segment seen before it (chained: each
      // resolved relative to the previous effectiveDir).
      let commitSegment = null;
      let sawMutationBeforeCommit = false;
      let effectiveDir = projectDir;
      for (const segment of segments) {
        if (!commitSegment && GIT_COMMIT_PATTERN.test(segment)) {
          commitSegment = segment;
          continue;
        }
        if (!commitSegment && GIT_MUTATION_PATTERN.test(segment)) {
          sawMutationBeforeCommit = true;
          continue;
        }
        if (!commitSegment && CD_PATTERN.test(segment)) {
          const target = segment.replace(/^cd\s*/, "");
          if (target === "" || target === "~") {
            effectiveDir = process.env.HOME || effectiveDir;
          } else if (target === "-") {
            // cd - needs the PREVIOUS dir, which isn't tracked; best-effort:
            // leave unchanged.
          } else if (target.startsWith("/")) {
            effectiveDir = target;
          } else {
            effectiveDir = `${effectiveDir}/${target}`;
          }
        }
      }
      if (!commitSegment) {
        return;
      }

      if (sawMutationBeforeCommit || SELF_STAGE_PATTERN.test(commitSegment)) {
        throw new Error(`${STAGE_SEPARATELY_MESSAGE} — ${BYPASS_HINT}`);
      }

      // Resolve the ACTUAL git target directory, starting from effectiveDir
      // (the result of any `cd` chain above): `git -C <dir> commit` runs
      // against <dir>, resolved relative to effectiveDir if relative
      // (`cd X && git -C Y commit` operates in X/Y). The LAST `-C` in the
      // commit segment wins (uppercase only — `-c` is git's unrelated
      // inline-config-value flag); multiple chained -C's, each relative to
      // the previous, are not handled — rare enough in practice that this
      // best-effort approximation is acceptable.
      let targetDir = effectiveDir;
      const dashCMatches = [...commitSegment.matchAll(/(^|\s)-C\s+(\S+)/g)];
      if (dashCMatches.length > 0) {
        const value = dashCMatches[dashCMatches.length - 1][2];
        targetDir = value.startsWith("/") ? value : `${effectiveDir}/${value}`;
      }

      if (runGit(targetDir, ["rev-parse", "--is-inside-work-tree"]) === null) {
        return;
      }

      // --diff-filter=ACMR excludes Deleted: staging the REMOVAL of a
      // sensitive file (the correct remediation for an already-committed
      // secret) must not be denied as if it were being added.
      const namesOut = runGit(targetDir, [
        "diff",
        "--cached",
        "--name-only",
        "--diff-filter=ACMR",
      ]);
      const names = (namesOut ?? "").split("\n").filter(Boolean);
      for (const name of names) {
        if (isSensitiveFilename(name)) {
          throw new Error(
            `Staged file looks like a secret: ${name} — unstage it first: git restore --staged "${name}" — ${BYPASS_HINT}`,
          );
        }
      }

      for (const pattern of CONTENT_PATTERNS) {
        if (hasStagedContentMatch(targetDir, pattern.source)) {
          throw new Error(
            `Staged changes contain what looks like a secret (matched: ${pattern}) — unstage the offending file and rotate the credential if it was ever committed — ${BYPASS_HINT}`,
          );
        }
      }
    },
  };
};
