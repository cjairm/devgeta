package opencode

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/internal/tooling/reviewjournal"
	"github.com/cjairm/devgeta/internal/tooling/worktree"
	"github.com/cjairm/devgeta/pkg/files"
	"gopkg.in/yaml.v3"
)

// OpenCode resolves a permission by sorting the rules by pattern LENGTH
// (ascending) and taking the last match — i.e. the longest matching pattern
// wins, and the order rules appear in the file is irrelevant. Two consequences
// these tests pin down:
//
//   - The catch-all "*" is length 1, so it loses to every other match. Keeping
//     it first is a readability convention (and what OpenCode's docs advise),
//     not a correctness requirement.
//   - Because longest wins, a broad deny is defeated by any LONGER matching
//     allow. Only deny/ask rules belong alongside the "*" allow.
//
// Pinned against the real embedded configs because a comment in the JSON or the
// agent frontmatter alone will not survive future edits (CLAUDE.md §12).

// renderedOpenCodeConfig renders the real embedded opencode.json.tmpl and
// returns its bytes.
func renderedOpenCodeConfig(t *testing.T) []byte {
	t.Helper()
	tmplPath := filepath.Join("..", "..", "..", "configs", "opencode", "opencode.json.tmpl")
	out := filepath.Join(t.TempDir(), "opencode.json")
	if err := files.GenerateFromTemplate(
		tmplPath,
		out,
		map[string]string{
			"Theme":          DEFAULT_THEME_NAME,
			"ScratchDirGlob": `"/tmp/placeholder-scratch/**"`,
		},
	); err != nil {
		t.Fatalf("failed to render opencode.json.tmpl: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatalf("rendered opencode.json is not valid JSON:\n%s", data)
	}
	return data
}

// orderedPairs returns a flat JSON object's key/value pairs in document order,
// which json.Unmarshal into a map would discard.
func orderedPairs(t *testing.T, raw json.RawMessage) [][2]string {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	tok, err := dec.Token()
	if err != nil {
		t.Fatalf("reading object start: %v", err)
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		t.Fatalf("expected a JSON object, got %v", tok)
	}
	var pairs [][2]string
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			t.Fatalf("reading key: %v", err)
		}
		key, ok := keyTok.(string)
		if !ok {
			t.Fatalf("expected a string key, got %v", keyTok)
		}
		valTok, err := dec.Token()
		if err != nil {
			t.Fatalf("reading value for %q: %v", key, err)
		}
		val, ok := valTok.(string)
		if !ok {
			t.Fatalf("expected a string value for %q, got %v", key, valTok)
		}
		pairs = append(pairs, [2]string{key, val})
	}
	return pairs
}

func permissionBlocks(t *testing.T) map[string][][2]string {
	t.Helper()
	var cfg struct {
		Permission struct {
			Read json.RawMessage `json:"read"`
			Bash json.RawMessage `json:"bash"`
			Edit json.RawMessage `json:"edit"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(renderedOpenCodeConfig(t), &cfg); err != nil {
		t.Fatalf("failed to unmarshal rendered opencode.json: %v", err)
	}
	blocks := map[string]json.RawMessage{
		"read": cfg.Permission.Read,
		"bash": cfg.Permission.Bash,
		"edit": cfg.Permission.Edit,
	}
	out := make(map[string][][2]string, len(blocks))
	for name, raw := range blocks {
		if len(raw) == 0 {
			t.Fatalf("permission.%s block is missing from opencode.json.tmpl", name)
		}
		out[name] = orderedPairs(t, raw)
	}
	return out
}

// TestEmbeddedConfigCatchAllIsFirst pins the ordering rule: each permission
// block opens with "*": "allow" so the specific deny/ask rules below it win.
func TestEmbeddedConfigCatchAllIsFirst(t *testing.T) {
	for name, pairs := range permissionBlocks(t) {
		if len(pairs) == 0 {
			t.Fatalf("permission.%s has no rules", name)
		}
		if pairs[0][0] != "*" {
			t.Errorf(
				"permission.%s should list the \"*\" catch-all first for readability, "+
					"got %q first",
				name,
				pairs[0][0],
			)
		}
		if pairs[0][1] != "allow" {
			t.Errorf(
				"permission.%s catch-all is %q, want \"allow\" — devgeta's policy is "+
					"allow everything, then deny/ask the dangerous cases",
				name,
				pairs[0][1],
			)
		}
		for _, p := range pairs[1:] {
			if p[0] == "*" {
				t.Errorf("permission.%s declares the \"*\" catch-all more than once", name)
			}
			if p[1] == "allow" {
				t.Errorf(
					"permission.%s rule %q is %q, but the catch-all already allows it — "+
						"OpenCode resolves longest-pattern-first, so a redundant allow can "+
						"only ever defeat a shorter deny",
					name,
					p[0],
					p[1],
				)
			}
		}
	}
}

// TestEmbeddedConfigGuardsDangerousCommands pins the danger list so a future
// edit cannot quietly drop a rule while the catch-all stays permissive.
func TestEmbeddedConfigGuardsDangerousCommands(t *testing.T) {
	blocks := permissionBlocks(t)

	want := map[string]map[string]string{
		"bash": {
			"sudo *":    "deny",
			"su *":      "deny",
			"rm -rf *":  "deny",
			"dd *":      "deny",
			"shred *":   "deny",
			"curl *":    "deny",
			"wget *":    "deny",
			"eval *":    "deny",
			"crontab *": "deny",
			"aws *":     "deny",
			"gcloud *":  "deny",
			"az *":      "deny",

			"ssh *":             "ask",
			"scp *":             "ask",
			"git push --force*": "ask",
			"terraform apply *": "ask",
			"kubectl apply *":   "ask",
		},
		"read": {
			"**/.env":      "deny",
			"*.pem":        "deny",
			"*.key":        "deny",
			"~/.ssh/**":    "deny",
			"~/.aws/**":    "deny",
			"~/.netrc":     "deny",
			"**/id_rsa":    "deny",
			"./secrets/**": "deny",
		},
		"edit": {
			".git/**":                        "deny",
			"**/.claude/settings.json":       "deny",
			"**/.claude/settings.local.json": "deny",
			"**/.claude/hooks/**":            "deny",
			"**/.opencode/opencode.json":     "deny",
			"**/.opencode/plugin/**":         "deny",
			"**/.mcp.json":                   "deny",
			"~/.config/opencode/**":          "deny",

			// The global Claude root is enumerated rather than covered by a
			// blanket `~/.claude/**`, so the memory directory underneath it
			// stays writable — see TestGlobalClaudeFloorLeavesMemoryWritable.
			"~/.claude/*.json":      "deny",
			"~/.claude/*.sh":        "deny",
			"~/.claude/*.md":        "deny",
			"~/.claude/agents/**":   "deny",
			"~/.claude/commands/**": "deny",
			"~/.claude/skills/**":   "deny",
			"~/.claude/plugins/**":  "deny",
			"~/.claude/hooks/**":    "deny",
			"~/.claude/lib/**":      "deny",
		},
	}

	for block, rules := range want {
		got := make(map[string]string, len(blocks[block]))
		for _, p := range blocks[block] {
			got[p[0]] = p[1]
		}
		for pattern, action := range rules {
			switch actual, ok := got[pattern]; {
			case !ok:
				t.Errorf("permission.%s is missing a rule for %q (want %q)", block, pattern, action)
			case actual != action:
				t.Errorf("permission.%s[%q] = %q, want %q", block, pattern, actual, action)
			}
		}
	}
}

// TestGlobalClaudeFloorLeavesMemoryWritable is the regression guard for the
// bug that made Claude Code's local memory unusable: a blanket
// `Edit(~/.claude/**)` deny in the settings floor also covered
// `~/.claude/projects/<slug>/memory/`, where the agent writes its own memory
// files. Nothing under `memory/` grants a permission, registers a hook, or
// defines an agent — it is data the agent is meant to write — so the deny
// blocked a feature while protecting nothing.
//
// It cannot be fixed with an allow carve-out. Claude Code evaluates "deny,
// then ask, then allow. The first match in that order determines the outcome,
// and rule specificity doesn't change the order", so a longer allow never
// defeats a shorter deny, and permission patterns have no negation. The only
// expressible fix is to stop the deny from matching in the first place, which
// is why the global Claude root is enumerated (agents/, commands/, skills/,
// plugins/, hooks/, lib/, and the direct-child config files by extension)
// instead of swept with `**`. The guard hook remains the default-deny layer
// for surface upstream has not shipped yet (ADR-0014 §3).
//
// This asserts the shape rather than the exact list: no `~/`-anchored edit
// deny may put `**` immediately after `.claude/`, in EITHER config. Re-adding
// `~/.claude/**` — the one edit that silently re-breaks memory — fails here.
func TestGlobalClaudeFloorLeavesMemoryWritable(t *testing.T) {
	const blanket = "~/.claude/**"

	check := func(t *testing.T, source string, patterns []string) {
		t.Helper()
		for _, pattern := range patterns {
			if pattern != blanket {
				continue
			}
			t.Errorf(
				"%s denies %q — that blanket also covers "+
					"~/.claude/projects/<slug>/memory/, Claude Code's memory "+
					"directory, and deny beats any allow carve-out. Enumerate the "+
					"config surfaces under ~/.claude/ instead (see ADR-0014's "+
					"memory amendment)",
				source, pattern,
			)
		}
	}

	claudeEdit := claudePermissions(t)["edit"]
	claudePatterns := make([]string, 0, len(claudeEdit))
	for pattern, action := range claudeEdit {
		if action == "deny" {
			claudePatterns = append(claudePatterns, pattern)
		}
	}
	check(t, "settings.json.tmpl", claudePatterns)

	openCodePatterns := make([]string, 0)
	for _, p := range permissionBlocks(t)["edit"] {
		if p[1] == "deny" {
			openCodePatterns = append(openCodePatterns, p[0])
		}
	}
	check(t, "opencode.json.tmpl", openCodePatterns)

	// The enumeration has to actually cover the escalation surface, or
	// "memory works" would be satisfiable by deleting the floor outright.
	// Parity means checking one config is enough here — the parity test
	// above fails if the other disagrees.
	for _, want := range []string{
		"~/.claude/*.json",
		"~/.claude/*.sh",
		"~/.claude/agents/**",
		"~/.claude/commands/**",
		"~/.claude/skills/**",
		"~/.claude/plugins/**",
		"~/.claude/hooks/**",
		"~/.claude/lib/**",
	} {
		if claudeEdit[want] != "deny" {
			t.Errorf(
				"settings.json.tmpl no longer denies %q — dropping the blanket "+
					"~/.claude/** deny is only safe while every config surface "+
					"under it is named explicitly",
				want,
			)
		}
	}
}

// TestSharedAgentsInheritGlobalBashPolicy is the regression guard for the bug
// that made every reviewer agent prompt on almost every command: an agent-level
// bash allowlist ("*": ask plus a handful of allows) overrides the global
// catch-all, so anything unlisted stopped for approval. Agents must leave bash
// to the host policy.
func TestSharedAgentsInheritGlobalBashPolicy(t *testing.T) {
	agentDir := filepath.Join("..", "..", "..", "configs", "shared", "agents")
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", agentDir, err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			fm := frontmatter(t, filepath.Join(agentDir, e.Name()))

			var parsed struct {
				Description string `yaml:"description"`
				Permission  struct {
					Bash any `yaml:"bash"`
				} `yaml:"permission"`
			}
			if err := yaml.Unmarshal(fm, &parsed); err != nil {
				t.Fatalf("frontmatter is not valid YAML: %v", err)
			}
			if strings.TrimSpace(parsed.Description) == "" {
				t.Error("agent frontmatter is missing a description")
			}
			if parsed.Permission.Bash != nil {
				t.Errorf(
					"agent declares permission.bash (%v); it must be omitted so the "+
						"global bash policy applies — an agent-level allowlist overrides "+
						"the \"*\" catch-all and prompts for every unlisted command",
					parsed.Permission.Bash,
				)
			}
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("no agent definitions found in %s", agentDir)
	}
}

// TestReviewerAgentsReadTheJournalAndCanApprove guards the two properties that
// decide whether a review can ever converge (ADR-0012). Both were missing from
// document-reviewer while its siblings had one of them, and the result was a
// reviewer that asked the same questions every run and could not approve
// anything:
//
//  1. Every reviewer reads the branch's review journal FIRST. Without it a
//     fresh session re-asks what was already answered, because a reviewer has
//     no memory of its own and the answers live in the journal.
//  2. Every reviewer ends on a verdict it can actually reach.
//     document-reviewer's output contract used to stop at a risk rating and a
//     questions section — there was no APPROVE anywhere in it, so "never
//     approves" was encoded in the template rather than being drift.
//  3. Every reviewer writes its blocking findings back, bound to severity, and
//     shows the settle command that closes them (ADR-0012's amendment). The
//     first version gated the write on "what you could not answer yourself",
//     which a competent reviewer never hits: three consecutive reviews of one
//     branch left the journal empty and re-raised the same findings.
//
// Asserted across all three rather than left to each prompt's own review: this
// asymmetry is invisible in any single file, since each agent reads fine alone
// and only a check over the set catches one of them missing a section.
func TestReviewerAgentsReadTheJournalAndCanApprove(t *testing.T) {
	// Substrings, not whole lines: the surrounding prose differs per agent by
	// design (each names its own subject), so this pins the contract each must
	// carry without freezing how it is worded.
	required := []struct {
		substr string
		why    string
	}{
		{
			substr: "devgeta task review-notes",
			why: "the agent never reads the branch's settled exchanges, so answered " +
				"questions come back on every re-review",
		},
		{
			substr: "devgeta task review-note --open",
			why: "the agent has no way to record an unanswered question, so nothing " +
				"the next run reads will contain it",
		},
		{
			substr: "devgeta task review-note --settle --id",
			why: "the agent never shows how its findings get closed, so they stay " +
				"open forever and every re-review raises them again (ADR-0012 " +
				"amendment: the settle line ends every report, because in a chat " +
				"review with no PR nothing downstream will print it)",
		},
		{
			substr: "[CRITICAL]` and `[IMPORTANT]",
			why: "the journal write is not bound to severity, so what gets recorded " +
				"is left to the agent's judgment — which produced empty journals " +
				"across three consecutive reviews (ADR-0012 amendment)",
		},
		{
			substr: "[STALE]",
			why: "without honoring the staleness marker the agent either trusts an " +
				"entry judged against code that has since changed, or ignores the " +
				"journal wholesale",
		},
		{
			substr: "**Status:** APPROVE",
			why: "the agent has no reachable verdict, so it can report findings " +
				"forever without ever approving",
		},
	}

	forEachReviewerAgent(t, func(t *testing.T, body string) {
		for _, req := range required {
			if !strings.Contains(body, req.substr) {
				t.Errorf("missing %q — %s", req.substr, req.why)
			}
		}
	})
}

// TestReviewerAgentsScopeTheJournalWhenTargeted guards the clause that lets a
// reviewer review something other than the checkout — a pull request whose head
// is not on any local branch. The journal is keyed by branch name (ADR-0012
// §5), so with unscoped commands the reviewer reads and writes the CHECKOUT's
// journal: it reconciles against a different branch's settled decisions and
// files this review's findings under that branch's name. A launch prompt cannot
// fix that on its own — it contradicts the agent's own written instructions,
// and the file wins.
//
// The clause has to reach EVERY journal call in the file, not just the read at
// the top. Scoping some calls and not others is worse than scoping none,
// because the round's findings then split across two journals and neither one
// is the record.
func TestReviewerAgentsScopeTheJournalWhenTargeted(t *testing.T) {
	// The flag pair, quoted exactly as the agent files render it, so the check
	// fails on a paraphrase that an agent could not copy verbatim.
	const scopedFlags = "`--branch <key> --rev <sha>`"

	// The clause's opening words and the fenced command it has to precede. Both
	// are matched verbatim: the ordering check below is only meaningful if it
	// finds the real clause and the real invocation, not a paraphrase.
	const (
		clauseOpening   = "**When your launch prompt gives you a journal key and a revision**"
		fencedFirstRead = "```bash\ndevgeta task review-notes"
	)

	required := []struct {
		substr string
		why    string
	}{
		{
			substr: clauseOpening,
			why: "nothing tells the agent when to scope its journal calls, so a review " +
				"of a target that is not the checkout lands in the checkout's journal",
		},
		{
			substr: scopedFlags,
			why:    "the agent is never told which flags carry the key and the revision",
		},
		{
			substr: "every `review-notes` and `review-note` command in this file",
			why: "the clause does not plainly reach every journal call, so the agent " +
				"scopes some and not others and the round's findings split across " +
				"two journals",
		},
		{
			substr: "run every command exactly as written",
			why: "the unprompted run — the ordinary branch review — is left ambiguous, " +
				"which invites the agent to invent a key or a revision it was not given",
		},
	}

	forEachReviewerAgent(t, func(t *testing.T, body string) {
		for _, req := range required {
			if !strings.Contains(body, req.substr) {
				t.Errorf("missing %q — %s", req.substr, req.why)
			}
		}

		// An agent acts on a file as it reads it, top to bottom, so the clause
		// only governs the first journal read if it is above it. Presence alone
		// is not enough — a clause sitting below the fence is read too late.
		clauseAt := strings.Index(body, clauseOpening)
		readAt := strings.Index(body, fencedFirstRead)
		if readAt < 0 {
			t.Fatalf(
				"no fenced %q command — the first journal read has no home, so the "+
					"scoped-journal clause has nothing to govern",
				"devgeta task review-notes",
			)
		}
		if clauseAt > readAt {
			t.Errorf(
				"the scoped-journal clause starting %q sits BELOW the first fenced "+
					"`devgeta task review-notes` command; an agent that executes as it "+
					"reads runs that unscoped read before it ever reaches the rule, so a "+
					"review targeting another branch reads the CHECKOUT's journal instead "+
					"of the target's — move the clause back above the fenced block",
				clauseOpening,
			)
		}

		// The journal WRITE commands sit ~150 lines below the clause, at the end
		// of a long report. Restating the rule where they are is what stops a
		// run from scoping its read and leaving its writes unscoped.
		const recordHeading = "## Record the blocking"
		i := strings.Index(body, recordHeading)
		if i < 0 {
			t.Fatalf("no %q section — the journal write commands have no home", recordHeading)
		}
		if !strings.Contains(body[i:], scopedFlags) {
			t.Errorf(
				"the %q section never repeats %s, so an agent that scoped its journal "+
					"read can still write its findings to the checkout's journal",
				recordHeading, scopedFlags,
			)
		}
	})
}

// forEachReviewerAgent runs fn as a subtest for every
// configs/shared/agents/*-reviewer.md file, handing it the file's full body.
// The reviewer-agent guards all assert over the SET rather than over one file:
// each agent reads fine alone, and only a check across all of them catches the
// one that dropped a shared contract.
func forEachReviewerAgent(t *testing.T, fn func(t *testing.T, body string)) {
	t.Helper()
	agentDir := filepath.Join("..", "..", "..", "configs", "shared", "agents")
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", agentDir, err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "-reviewer.md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(agentDir, e.Name()))
		if err != nil {
			t.Fatalf("failed to read agent %s: %v", e.Name(), err)
		}
		body := string(data)
		t.Run(e.Name(), func(t *testing.T) {
			fn(t, body)
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("no *-reviewer.md agents found in %s", agentDir)
	}
}

// commandFrontmatterAllowlist is OpenCode's real command-object schema
// (https://opencode.ai/config.json) — the only frontmatter keys a command
// file is allowed to set. "template" is the schema's name for the command
// body/prompt content; none of these files set it explicitly in frontmatter,
// but it stays in the allowlist because it is a valid schema key.
//
// This must stay an allowlist, not a denylist of "known bad keys": the cycle
// documented in docs/plans/cycles/2026-08-05-shared-command-permissions.md
// removed `permission`, `tools`, and `temperature` from every shared command
// file because OpenCode silently ignores keys outside this schema — they
// looked enforced but did nothing. A denylist of those three names would
// never have caught `temperature` before someone noticed by hand; only
// rejecting everything not on the real schema catches the next one too.
var commandFrontmatterAllowlist = map[string]bool{
	"template":    true,
	"description": true,
	"agent":       true,
	"model":       true,
	"variant":     true,
	"subtask":     true,
}

// forEachSharedCommand runs fn as a subtest for every
// configs/shared/commands/*.md file.
func forEachSharedCommand(t *testing.T, fn func(t *testing.T, name, path string)) {
	t.Helper()
	dir := filepath.Join("..", "..", "..", "configs", "shared", "commands")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", dir, err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		t.Run(e.Name(), func(t *testing.T) {
			fn(t, e.Name(), path)
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("no command files found in %s", dir)
	}
}

// TestSharedCommandsFrontmatterMatchesSchema guards against dead frontmatter
// keys creeping back into configs/shared/commands/*.md. OpenCode's command
// schema (https://opencode.ai/config.json) only recognizes template,
// description, agent, model, variant, and subtask — any other key is parsed
// but silently dropped at runtime, so it looks enforced in the file while
// doing nothing. That is exactly how `permission`, `tools`, and `temperature`
// went unnoticed until the cycle in
// docs/plans/cycles/2026-08-05-shared-command-permissions.md removed them.
func TestSharedCommandsFrontmatterMatchesSchema(t *testing.T) {
	forEachSharedCommand(t, func(t *testing.T, name, path string) {
		// OpenCode's command schema only requires `template` (the body) — a
		// command file with no frontmatter block at all is legal, so there is
		// nothing here for the allowlist to check.
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "---") {
			return
		}

		fm := frontmatter(t, path)

		var parsed map[string]any
		if err := yaml.Unmarshal(fm, &parsed); err != nil {
			t.Fatalf("frontmatter is not valid YAML: %v", err)
		}
		for key := range parsed {
			if !commandFrontmatterAllowlist[key] {
				t.Errorf(
					"%s frontmatter declares %q, which is outside OpenCode's real "+
						"command schema (https://opencode.ai/config.json: template, "+
						"description, agent, model, variant, subtask). OpenCode silently "+
						"drops unknown frontmatter keys at runtime, so %q is not merely "+
						"unused — it looks enforced but does nothing. Remove it or add it "+
						"to the schema allowlist if OpenCode has genuinely added it. This "+
						"also deliberately rejects Claude-Code-only keys (e.g. "+
						"allowed-tools, argument-hint): per-command restriction on the "+
						"Claude side is a separate decision that hasn't been made, not a "+
						"frontmatter edit — see "+
						"docs/plans/cycles/2026-08-05-shared-command-permissions.md.",
					name,
					key,
					key,
				)
			}
		}
	})
}

// frontmatter returns the YAML block delimited by the leading and next "---".
func frontmatter(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) == 0 || strings.TrimSpace(lines[0]) != "---" {
		t.Fatalf("%s does not open with a --- frontmatter delimiter", path)
	}
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(lines[i]) == "---" {
			return []byte(strings.Join(lines[1:i], "\n"))
		}
	}
	t.Fatal(fmt.Sprintf("%s has an unterminated frontmatter block", path))
	return nil
}

// ---------------------------------------------------------------------------
// Cross-tool parity: Claude Code and OpenCode must enforce the same policy.
//
// The two configs are hand-maintained in different formats (settings.json.tmpl
// uses "Bash(rm -rf *)" strings grouped into allow/ask/deny arrays; opencode.json
// .tmpl uses per-namespace pattern->action maps). Nothing structural stops one
// from gaining a rule the other lacks — which is exactly how the danger list
// drifted before. These tests compare the two and fail on any asymmetry.
// ---------------------------------------------------------------------------

// claudeRule matches "Bash(rm -rf *)" and captures namespace + pattern. A bare
// "Read" (no parens) means the whole tool, i.e. the "*" pattern.
var claudeRule = regexp.MustCompile(`^(Bash|Read|Edit)(?:\((.*)\))?$`)

// claudePermissions renders the real embedded settings.json.tmpl and returns
// namespace -> pattern -> action, using the same namespace names OpenCode uses.
func claudePermissions(t *testing.T) map[string]map[string]string {
	t.Helper()
	tmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")
	out := filepath.Join(t.TempDir(), "settings.json")
	// Mirrors claude.settingsTemplateData's shape (unexported, different
	// package) rather than importing it: the template only needs a
	// ScratchDir field alongside the promoted IntegrationsConfig fields.
	renderData := struct {
		config.IntegrationsConfig
		ScratchDir string
	}{ScratchDir: `"/tmp/placeholder-scratch"`}
	if err := files.GenerateFromTemplate(tmplPath, out, renderData); err != nil {
		t.Fatalf("failed to render settings.json.tmpl: %v", err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	var cfg struct {
		Permissions struct {
			Allow []string `json:"allow"`
			Ask   []string `json:"ask"`
			Deny  []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to unmarshal rendered settings.json: %v", err)
	}

	got := map[string]map[string]string{
		"bash": {},
		"read": {},
		"edit": {},
	}
	for action, entries := range map[string][]string{
		"allow": cfg.Permissions.Allow,
		"ask":   cfg.Permissions.Ask,
		"deny":  cfg.Permissions.Deny,
	} {
		for _, e := range entries {
			m := claudeRule.FindStringSubmatch(e)
			if m == nil {
				t.Fatalf(
					"permission entry %q in settings.json.tmpl is not a Bash/Read/Edit rule",
					e,
				)
			}
			ns := strings.ToLower(m[1])
			pattern := m[2]
			if pattern == "" {
				pattern = "*"
			}
			got[ns][pattern] = action
		}
	}
	return got
}

// TestClaudeAndOpenCodePermissionParity is the guard against the two configs
// drifting apart: every bash/read/edit rule must exist in both with the same
// action. If a rule genuinely cannot be expressed in one tool, remove it from
// both rather than weakening this test.
func TestClaudeAndOpenCodePermissionParity(t *testing.T) {
	claude := claudePermissions(t)

	openCode := map[string]map[string]string{}
	for ns, pairs := range permissionBlocks(t) {
		openCode[ns] = make(map[string]string, len(pairs))
		for _, p := range pairs {
			openCode[ns][p[0]] = p[1]
		}
	}

	for _, ns := range []string{"bash", "read", "edit"} {
		for pattern, action := range claude[ns] {
			switch got, ok := openCode[ns][pattern]; {
			case !ok:
				t.Errorf(
					"%s rule %q (%s) is in settings.json.tmpl but MISSING from opencode.json.tmpl",
					ns, pattern, action,
				)
			case got != action:
				t.Errorf(
					"%s rule %q disagrees: claude=%q opencode=%q",
					ns, pattern, action, got,
				)
			}
		}
		for pattern, action := range openCode[ns] {
			if _, ok := claude[ns][pattern]; !ok {
				t.Errorf(
					"%s rule %q (%s) is in opencode.json.tmpl but MISSING from settings.json.tmpl",
					ns, pattern, action,
				)
			}
		}
	}
}

// TestClaudeAndOpenCodeFormatterParity pins that both tools format the same file
// types. Claude formats via the format.sh PostToolUse hook (a shell `case` over
// glob patterns); OpenCode formats via the `formatter` block's extension lists.
// A language added to one must be added to the other.
func TestClaudeAndOpenCodeFormatterParity(t *testing.T) {
	// Extensions handled by format.sh, scraped from its `case` glob patterns.
	shPath := filepath.Join("..", "..", "..", "configs", "claude", "format.sh")
	sh, err := os.ReadFile(shPath)
	if err != nil {
		t.Fatal(err)
	}
	claudeExts := map[string]bool{}
	for _, m := range regexp.MustCompile(`\*(\.[a-zA-Z0-9]+)\)?\s*(?:\||\))`).
		FindAllStringSubmatch(string(sh), -1) {
		claudeExts[m[1]] = true
	}
	if len(claudeExts) == 0 {
		t.Fatalf("scraped no extensions from %s — has its `case` structure changed?", shPath)
	}

	var cfg struct {
		Formatter map[string]struct {
			Extensions []string `json:"extensions"`
		} `json:"formatter"`
	}
	if err := json.Unmarshal(renderedOpenCodeConfig(t), &cfg); err != nil {
		t.Fatalf("failed to unmarshal rendered opencode.json: %v", err)
	}
	openCodeExts := map[string]bool{}
	for _, f := range cfg.Formatter {
		for _, e := range f.Extensions {
			openCodeExts[e] = true
		}
	}

	for _, ext := range sortedKeys(claudeExts) {
		if !openCodeExts[ext] {
			t.Errorf("format.sh formats %s but opencode.json.tmpl has no formatter for it", ext)
		}
	}
	for _, ext := range sortedKeys(openCodeExts) {
		if !claudeExts[ext] {
			t.Errorf("opencode.json.tmpl formats %s but format.sh does not", ext)
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// TestScratchDirGrantParity is the guard against ADR-0015's scratch grant
// drifting to one agent only. The two configs express it in different
// shapes — Claude's permissions.additionalDirectories is a bare directory
// (no glob), OpenCode's permission.external_directory is a gitignore-style
// pattern map that needs a "/**" suffix to cover nested paths — so a plain
// string-equality check across the two files (the way
// TestClaudeAndOpenCodePermissionParity compares read/bash/edit) would be
// wrong here. This strips OpenCode's suffix back off and compares the
// underlying root instead.
func TestScratchDirGrantParity(t *testing.T) {
	const wantRoot = "/tmp/dg-grant-parity-scratch"

	claudeTmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")
	claudeOut := filepath.Join(t.TempDir(), "settings.json")
	claudeRenderData := struct {
		config.IntegrationsConfig
		ScratchDir string
	}{ScratchDir: mustJSONString(t, wantRoot)}
	if err := files.GenerateFromTemplate(claudeTmplPath, claudeOut, claudeRenderData); err != nil {
		t.Fatalf("failed to render settings.json.tmpl: %v", err)
	}
	claudeData, err := os.ReadFile(claudeOut)
	if err != nil {
		t.Fatal(err)
	}
	var claudeCfg struct {
		Permissions struct {
			AdditionalDirectories []string `json:"additionalDirectories"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(claudeData, &claudeCfg); err != nil {
		t.Fatalf("rendered settings.json is not valid JSON: %v\n%s", err, claudeData)
	}
	if len(claudeCfg.Permissions.AdditionalDirectories) != 1 ||
		claudeCfg.Permissions.AdditionalDirectories[0] != wantRoot {
		t.Fatalf(
			"expected additionalDirectories == [%q], got %v",
			wantRoot,
			claudeCfg.Permissions.AdditionalDirectories,
		)
	}

	openCodeTmplPath := filepath.Join("..", "..", "..", "configs", "opencode", "opencode.json.tmpl")
	openCodeOut := filepath.Join(t.TempDir(), "opencode.json")
	if err := files.GenerateFromTemplate(openCodeTmplPath, openCodeOut, map[string]string{
		"Theme":          DEFAULT_THEME_NAME,
		"ScratchDirGlob": mustJSONString(t, wantRoot+"/**"),
	}); err != nil {
		t.Fatalf("failed to render opencode.json.tmpl: %v", err)
	}
	openCodeData, err := os.ReadFile(openCodeOut)
	if err != nil {
		t.Fatal(err)
	}
	var openCodeCfg struct {
		Permission struct {
			ExternalDirectory map[string]string `json:"external_directory"`
		} `json:"permission"`
	}
	if err := json.Unmarshal(openCodeData, &openCodeCfg); err != nil {
		t.Fatalf("rendered opencode.json is not valid JSON: %v\n%s", err, openCodeData)
	}
	if len(openCodeCfg.Permission.ExternalDirectory) != 1 {
		t.Fatalf(
			"expected exactly one external_directory grant, got %v",
			openCodeCfg.Permission.ExternalDirectory,
		)
	}
	for pattern, action := range openCodeCfg.Permission.ExternalDirectory {
		if action != "allow" {
			t.Errorf("external_directory[%q] = %q, want %q", pattern, action, "allow")
		}
		gotRoot := strings.TrimSuffix(pattern, "/**")
		if gotRoot != wantRoot {
			t.Errorf(
				"external_directory grants %q, which strips to root %q — want %q (parity with additionalDirectories)",
				pattern,
				gotRoot,
				wantRoot,
			)
		}
	}
}

// TestScratchDirGrantRendersValidJSONForHostilePaths is the regression test
// for the class of bug ADR-0015 §2 exists to prevent: text/template does no
// JSON escaping, and ScratchDir/ScratchDirGlob were the first
// user-influenced (XDG_CACHE_HOME-derived) values either template ever
// interpolated. Renders both templates with a raw (unescaped) hostile root
// and asserts the OUTPUT is still valid JSON — proving the production code
// path (json.Marshal before templating, not the template itself) is what
// carries the escaping.
func TestScratchDirGrantRendersValidJSONForHostilePaths(t *testing.T) {
	hostileRoots := []string{
		"/tmp/dg cache/scratch",   // space
		`/tmp/dg"cache/scratch`,   // double quote
		`/tmp/dg\cache/scratch`,   // backslash
		"/tmp/dg\"ca\\pe/scratch", // quote and backslash together
	}

	claudeTmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")
	openCodeTmplPath := filepath.Join("..", "..", "..", "configs", "opencode", "opencode.json.tmpl")

	for _, root := range hostileRoots {
		t.Run(root, func(t *testing.T) {
			claudeOut := filepath.Join(t.TempDir(), "settings.json")
			claudeRenderData := struct {
				config.IntegrationsConfig
				ScratchDir string
			}{ScratchDir: mustJSONString(t, root)}
			if err := files.GenerateFromTemplate(
				claudeTmplPath,
				claudeOut,
				claudeRenderData,
			); err != nil {
				t.Fatalf("failed to render settings.json.tmpl: %v", err)
			}
			claudeData, err := os.ReadFile(claudeOut)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(claudeData) {
				t.Errorf(
					"rendered settings.json is not valid JSON for root %q:\n%s",
					root,
					claudeData,
				)
			}

			openCodeOut := filepath.Join(t.TempDir(), "opencode.json")
			if err := files.GenerateFromTemplate(openCodeTmplPath, openCodeOut, map[string]string{
				"Theme":          DEFAULT_THEME_NAME,
				"ScratchDirGlob": mustJSONString(t, root+"/**"),
			}); err != nil {
				t.Fatalf("failed to render opencode.json.tmpl: %v", err)
			}
			openCodeData, err := os.ReadFile(openCodeOut)
			if err != nil {
				t.Fatal(err)
			}
			if !json.Valid(openCodeData) {
				t.Errorf(
					"rendered opencode.json is not valid JSON for root %q:\n%s",
					root,
					openCodeData,
				)
			}
		})
	}
}

// mustJSONString renders v as a JSON-encoded value (e.g. a Go string ->
// `"..."` with quotes) — the pre-escaped form settings.json.tmpl and
// opencode.json.tmpl interpolate directly, since text/template does no
// JSON escaping of its own (ADR-0015 §2).
func mustJSONString(t *testing.T, v string) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("failed to marshal %q: %v", v, err)
	}
	return string(b)
}

// TestSharedCommandsNeverReferenceTmp is ADR-0015's constraint that
// devgeta-authored commands use the scratch directory (`devgeta task
// scratch`) rather than /tmp — enforced against the embedded configs
// themselves (CLAUDE.md §12), since a comment alone will not survive future
// edits. Only commands/ is checked: skills/ vendors upstream Superpowers
// content with its own /tmp paths, deliberately excluded (ADR-0015 §7).
func TestSharedCommandsNeverReferenceTmp(t *testing.T) {
	forEachSharedCommand(t, func(t *testing.T, name, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		if strings.Contains(string(data), "/tmp/") {
			t.Errorf(
				"%s references /tmp/ — use `devgeta task scratch` instead (ADR-0015)",
				name,
			)
		}
	})
}

// ---------------------------------------------------------------------------
// Standing authorization: a command that posts must say so in its own prose.
//
// The shared commands used to carry a `permission:` frontmatter block whose
// bash list read `"devgeta task *": allow`. OpenCode never enforced it, which
// is why commit 6bb17ab removed it — but an agent still READ that line as
// durable authorization to run the posting commands. Nothing replaced it in
// prose, so the base instinct to confirm outward-facing actions took over and
// /review-pr and /approve-pr began asking the human before posting their own
// verdict. Prose is now the only carrier of that authorization; these tests
// keep it in every file that needs it.
// ---------------------------------------------------------------------------

// outwardTaskVerbs are the `devgeta task` subcommands that act outside this
// machine — each one posts to a PR. A command file that runs any of them must
// declare standing authorization to do so without asking.
var outwardTaskVerbs = map[string]bool{
	"submit-review":  true,
	"approve-pr":     true,
	"comment-pr":     true,
	"create-pr":      true,
	"reply-thread":   true,
	"resolve-thread": true,
	"request-review": true,
}

// postingAuthorizationHeading is the stable anchor
// TestPostingCommandsDeclareStandingAuthorization reads. It must stay exactly
// this string in every shared command file that posts.
const postingAuthorizationHeading = "## Authority to post"

// devgetaTaskCall matches a `devgeta task <verb>` invocation in a command
// file's prose, capturing the verb.
var devgetaTaskCall = regexp.MustCompile(`devgeta task ([a-z][a-z-]*)`)

// gitStateChangingCall matches an instruction to commit or push. These are the
// local counterparts of an outward `devgeta task` verb: they change state the
// user cares about, so the agent's default is to confirm first, so the command
// file has to say it must not.
var gitStateChangingCall = regexp.MustCompile(`git (commit|push)\b`)

// requireStandingAuthorization asserts that `text` both grants the standing
// authorization and forbids the confirmation pause. Both halves are load
// bearing: granting the authority without forbidding the pause is not enough,
// because the default behavior being overridden is exactly the "want me to do
// this?" question. `where` names the part of the file being checked, so a
// failure says which prose to fix.
//
// Either phrasing of the prohibition is accepted ("do not ask" / "without
// asking") — the files are hand-wrapped prose and both say the same thing, so
// pinning one literal would fail on a legitimate reword.
func requireStandingAuthorization(t *testing.T, name, where, text string) {
	t.Helper()
	lower := strings.ToLower(text)
	if !strings.Contains(lower, "authorization") {
		t.Errorf(
			"%s's %s no longer says that running the command IS the authorization "+
				"to act — that sentence is the whole point of it",
			name, where,
		)
	}
	if !strings.Contains(lower, "do not ask") && !strings.Contains(lower, "without asking") {
		t.Errorf(
			"%s's %s no longer tells the agent not to ask first. Granting authority "+
				"without forbidding the confirmation step is not enough: the default "+
				"behavior it has to override is exactly that 'want me to do this?' pause",
			name, where,
		)
	}
}

// TestPostingCommandsDeclareStandingAuthorization requires every shared
// command that invokes an outward `devgeta task` verb to carry an "Authority
// to post" section that names each such verb and tells the agent not to ask
// first. Which files are covered is derived from the files themselves, not a
// hard-coded list, so a new posting command — or an existing one that gains a
// posting verb — fails the build until it declares the authorization too.
//
// What this catches: the authorization prose being dropped, moved out of its
// section, or written so generically that it never names the verb the file
// actually posts with.
// What this does NOT catch: wording that keeps the words but reverses the
// meaning, or an agent disobeying correctly-worded instructions — a substring
// check over prose cannot reach either.
func TestPostingCommandsDeclareStandingAuthorization(t *testing.T) {
	forEachSharedCommand(t, func(t *testing.T, name, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		body := string(data)

		// Collect the outward verbs this file actually invokes, in a stable
		// order so failures read the same way every run.
		var verbs []string
		seen := map[string]bool{}
		for _, m := range devgetaTaskCall.FindAllStringSubmatch(body, -1) {
			verb := m[1]
			if outwardTaskVerbs[verb] && !seen[verb] {
				seen[verb] = true
				verbs = append(verbs, verb)
			}
		}
		if len(verbs) == 0 {
			return
		}
		sort.Strings(verbs)

		if !strings.Contains(body, postingAuthorizationHeading) {
			t.Fatalf(
				"%s invokes the outward command(s) %v but has no %q section. Posting "+
					"authorization now lives only in prose: the `permission:` "+
					"frontmatter that used to grant it (`\"devgeta task *\": allow`) was "+
					"removed in 6bb17ab because OpenCode never enforced it, and without "+
					"a replacement the agent falls back to asking the human before it "+
					"posts. Add the section.",
				name, verbs, postingAuthorizationHeading,
			)
		}

		section := markdownSection(t, body, postingAuthorizationHeading)
		requireStandingAuthorization(
			t, name, fmt.Sprintf("%q section", postingAuthorizationHeading), section,
		)
		for _, verb := range verbs {
			if !strings.Contains(section, verb) {
				t.Errorf(
					"%s posts with `devgeta task %s` but its %q section never names "+
						"that command, so the authorization does not visibly cover it",
					name, verb, postingAuthorizationHeading,
				)
			}
		}
	})
}

// TestCommittingCommandsDeclareStandingAuthorization is the local counterpart
// of TestPostingCommandsDeclareStandingAuthorization. A command that tells the
// agent to run `git commit` or `git push` changes state the user cares about,
// so the same instinct that produced "want me to post this?" produces "want me
// to commit this?" — and the removed `permission:` block covered these too. Any
// shared command that instructs a commit or a push must therefore say in its
// own prose that running it IS the authorization and that the agent must not
// ask first.
//
// Which files are covered is derived from the files themselves, so a new
// command that commits or pushes fails the build until it declares the
// authorization too. Unlike the posting test this does not require a dedicated
// section: `smart-commit` carries the statement in its Rules list, where the
// rule it replaced already lived, and moving it would only make the file read
// worse.
//
// What this catches: the no-ask statement being dropped from a command that
// commits or pushes, and a new such command shipping without one.
// What this does NOT catch: wording that keeps the words but reverses the
// meaning, or an agent disobeying a correctly-worded instruction — a substring
// check over prose cannot execute the instructions. No harness in this repo
// can: reviewer runs are exercised against a scripted `opencode run` fixture
// (internal/tooling/task/reviewrun_test.go), never a live agent, because
// CLAUDE.md forbids tests that execute real commands.
func TestCommittingCommandsDeclareStandingAuthorization(t *testing.T) {
	forEachSharedCommand(t, func(t *testing.T, name, path string) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("failed to read %s: %v", name, err)
		}
		body := string(data)
		if !gitStateChangingCall.MatchString(body) {
			return
		}
		requireStandingAuthorization(t, name, "prose", body)
	})
}

// reviewLoopReportHeading is the stable anchor
// TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport checks against. It must
// stay exactly this string in configs/shared/commands/review-loop.md, above
// the report template and nowhere else.
const reviewLoopReportHeading = "## Terminal report"

// TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport guards ADR-0017 §6's
// human-only rule the only way prose can be guarded: `--ratify` and
// `--reopen` retire an agent's provisional rejection, and that decision
// belongs to a human, never to the loop itself. The permission model cannot
// enforce this — it has no way to tell who typed a `devgeta task` command —
// so the only thing standing between "the loop reports a rejection" and "the
// loop quietly ratifies its own rejection" is review-loop.md's wording. This
// test pins the one structural check available: every occurrence of either
// flag must fall after the report template's heading, so an instruction
// earlier in the file telling the loop to run one itself fails the build
// instead of shipping silently.
// markdownSection extracts the body of one section from a command file:
// everything from `heading` up to (but not including) the next line that
// starts with "#" — so a guard test can anchor on a single section's content
// without being tripped by wording changes in the rest of the file.
// readSharedCommand returns the path and body of one
// configs/shared/commands/<name> file — the two things every guard test below
// starts from.
func readSharedCommand(t *testing.T, name string) (string, string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "configs", "shared", "commands", name)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return path, string(data)
}

func markdownSection(t *testing.T, body, heading string) string {
	t.Helper()
	start := strings.Index(body, heading)
	if start < 0 {
		t.Fatalf(
			"%q heading not found — this is the anchor a guard test uses to "+
				"isolate one section's content",
			heading,
		)
	}
	rest := body[start+len(heading):]
	if next := strings.Index(rest, "\n#"); next >= 0 {
		return rest[:next]
	}
	return rest
}

// TestReviewLoopCleanApprovalRequiresNothingOpen guards the fix that closed a
// real correctness bug (see the cycle history around commit 0b39f28): a round
// where every reviewer said APPROVE but the journal still had open findings
// used to read as a clean approval. Step 3 must gate on BOTH every verdict
// being APPROVE AND nothing being open — drop the second half and the loop can
// declare victory while a finding sits unanswered in the journal.
//
// The open findings are read from the journal (`review-notes`, whose list is
// headed `open:`), not from review-run, which prints only verdict lines — so
// what this asserts is that step 3 still names the `open:` section and requires
// it to be empty, however that emptiness is worded.
//
// What this catches: the nothing-open condition (or the whole clause requiring
// it) being deleted from step 3, which would silently reintroduce the bug.
// What this does NOT catch: an executing agent misreading a correctly-worded
// instruction, or a reword that keeps the words in the section but no longer
// makes them a requirement (a substring match cannot distinguish "requires
// nothing open" from "ignores what is open" — it only proves the concept is
// still named in the right place).
func TestReviewLoopCleanApprovalRequiresNothingOpen(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	section := markdownSection(t, body, "### 3. Check for clean approval")

	if !strings.Contains(section, "open:") {
		t.Errorf(
			"%s step 3 no longer refers to the journal's `open:` section — without "+
				"it there is nothing tying clean approval to having no unanswered "+
				"findings, and an all-APPROVE round with open findings reads as clean",
			path,
		)
	}
	if !strings.Contains(section, "nothing under") {
		t.Errorf(
			"%s step 3 no longer requires the `open:` section to be EMPTY for a "+
				"clean approval (expected it to say nothing is listed under it) — "+
				"naming the section without requiring it empty is not a gate",
			path,
		)
	}
	if !strings.Contains(section, "APPROVE") {
		t.Errorf(
			"%s step 3 no longer checks that every reviewer's outcome is APPROVE — "+
				"the clean approval gate needs both conditions together, not the "+
				"empty journal alone",
			path,
		)
	}
}

// TestReviewLoopForwardsReviewerSelector guards the fix that closed the other
// real bug: `--reviewer <key>` was documented in the Usage section but never
// actually read from $ARGUMENTS or passed on to `devgeta task review-run`, so
// the documented selector silently did nothing. Step 0 must parse
// $ARGUMENTS, and step 1 must forward the resolved key to review-run on
// every round.
//
// What this catches: either half — the $ARGUMENTS parse in step 0, or the
// `--reviewer <key>` forwarding in step 1 — being deleted, which would
// silently restore "the selector is documented but does nothing".
// What this does NOT catch: the parse or forwarding being reworded to read
// plausibly while actually forwarding a stale or wrong value, since this is a
// substring check over prose, not an execution of the instructions.
func TestReviewLoopForwardsReviewerSelector(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	parseSection := markdownSection(t, body, "### 0. Resolve the reviewer selector")
	if !strings.Contains(parseSection, "$ARGUMENTS") {
		t.Errorf(
			"%s step 0 no longer mentions parsing $ARGUMENTS — without this the "+
				"documented --reviewer flag is never read from anywhere, so it is "+
				"just Usage-section text with no effect",
			path,
		)
	}

	runSection := markdownSection(t, body, "### 1. Run a round")
	if !strings.Contains(runSection, "--reviewer <key>") {
		t.Errorf(
			"%s step 1 no longer forwards --reviewer <key> to `devgeta task "+
				"review-run` — without this, --reviewer is parsed in step 0 but "+
				"never passed through, so the documented selector silently does "+
				"nothing",
			path,
		)
	}
}

// TestReviewLoopForwardsTheNote is the --note half of the same guard, for the
// same failure: a flag documented in the Usage section but never parsed or
// passed on does nothing, and the human's steering silently never reaches a
// reviewer. Step 0 must resolve it and step 1 must forward it.
//
// What this catches: `--note` being dropped from either step while the Usage
// section keeps advertising it.
// What this does NOT catch: the loop summarizing or answering the note instead
// of passing it verbatim — that is judgment, which the instruction states
// plainly but no substring check can verify.
func TestReviewLoopForwardsTheNote(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	if !strings.Contains(body, "--note <text>") {
		t.Fatalf("%s no longer documents --note <text> at all", path)
	}
	parseSection := markdownSection(t, body, "### 0. Resolve the reviewer selector")
	if !strings.Contains(parseSection, "--note") {
		t.Errorf(
			"%s step 0 no longer resolves --note — a note that is never parsed "+
				"never reaches a reviewer, so the flag is Usage-section text with "+
				"no effect",
			path,
		)
	}
	runSection := markdownSection(t, body, "### 1. Run a round")
	if !strings.Contains(runSection, "--note <text>") {
		t.Errorf(
			"%s step 1 no longer forwards --note <text> to `devgeta task "+
				"review-run` — without this the note is parsed and then dropped",
			path,
		)
	}
}

// TestReviewLoopAgentPrefixMatchesTheConstant guards the one thing
// reviewjournal.AgentNotePrefix's own doc comment demands and nothing else
// could enforce: the marker the loop is told to WRITE, and the marker step 3
// is told to DETECT, are both the constant's actual value.
//
// It has drifted once already. The constant carried a trailing space while
// step 3 looked for the marker without one, so a note settled as
// "agent:<evidence>" was reported to the human with a --ratify command that
// Ratify then refused. Prose cannot import a Go constant, so this test is the
// only thing tying the two together.
//
// What this catches: the constant changing without review-loop.md following,
// or either half of review-loop.md (the write instruction in step 4, the
// detection instruction in step 3) losing the marker.
// What this does NOT catch: an executing agent writing a note that omits the
// marker entirely — that is judgment, and the terminal report is where a
// human sees the rejection either way.
func TestReviewLoopAgentPrefixMatchesTheConstant(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	if want := `--note "` + reviewjournal.AgentNotePrefix; !strings.Contains(body, want) {
		t.Errorf(
			"%s no longer tells the loop to settle a rejection with %q — the marker "+
				"it writes must be reviewjournal.AgentNotePrefix verbatim, or the "+
				"rejection is not recognizable as the agent's and Ratify refuses it",
			path, want,
		)
	}

	section := markdownSection(t, body, "### 3. Check for clean approval")
	if !strings.Contains(section, reviewjournal.AgentNotePrefix) {
		t.Errorf(
			"%s step 3 no longer names %q as the marker of an unratified agent "+
				"rejection — without the constant's exact value there, an all-APPROVE "+
				"round with a pending agent rejection reads as a clean approval",
			path, reviewjournal.AgentNotePrefix,
		)
	}
	if !strings.Contains(section, "answer:") {
		t.Errorf(
			"%s step 3 no longer says the marker is read from the entry's `answer:` "+
				"line — renderNotes only ever puts it there, so a loop told to look at "+
				"the finding line finds no marker anywhere and declares a false approval",
			path,
		)
	}
}

func TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	headingAt := strings.Index(body, reviewLoopReportHeading)
	if headingAt < 0 {
		t.Fatalf(
			"%s has no %q heading — that heading is the anchor this test uses to "+
				"confirm --ratify/--reopen only appear in the human-facing report",
			path, reviewLoopReportHeading,
		)
	}

	for _, flag := range []string{"--ratify", "--reopen"} {
		for i := 0; i+len(flag) <= len(body); i++ {
			if body[i:i+len(flag)] != flag {
				continue
			}
			if i < headingAt {
				t.Errorf(
					"%s mentions %q at byte %d, before the %q heading at byte %d — "+
						"ratification is a human decision, and review-loop.md must never "+
						"instruct the loop to run %s itself; the only place it may appear "+
						"is inside the report template this loop prints for the human to "+
						"act on",
					path, flag, i, reviewLoopReportHeading, headingAt, flag,
				)
			}
		}
	}
}

// readReviewLoop returns review-loop.md's path and body, the two things every
// guard test below starts from.
func readReviewLoop(t *testing.T) (string, string) {
	t.Helper()
	path := filepath.Join("..", "..", "..", "configs", "shared", "commands", "review-loop.md")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("failed to read %s: %v", path, err)
	}
	return path, string(data)
}

// flowSection is markdownSection with the section's whitespace collapsed to
// single spaces, so a phrase a guard test looks for still matches when the
// prose is rewrapped. The command files are hand-wrapped at ~90 columns, and a
// phrase that happens to straddle a line break would otherwise read as deleted
// the next time a sentence above it grows by a word.
func flowSection(t *testing.T, body, heading string) string {
	t.Helper()
	return strings.Join(strings.Fields(markdownSection(t, body, heading)), " ")
}

// TestReviewLoopStopsWhenARoundLeavesNothingToActOn guards the second exit step
// 3 needs. A round can withhold approval and still record no journal finding —
// NEEDS DISCUSSION asks for a conversation, and a reviewer only journals its
// blocking findings — and with the single exit that used to be there, such a
// round fell through to step 4, which iterates the `open:` ids and so did
// nothing, then to step 5, which found nothing open and started another round.
// The loop changed nothing in between, so every remaining round bought the same
// verdict again and the human waited for the cap to expire.
//
// What this catches: step 3 losing either destination — the step 4 hand-off for
// a round that did leave findings, or the terminal-report stop for a round that
// did not.
// What this does NOT catch: an executing agent reading both branches correctly
// and still picking the wrong one; substring checks over prose cannot run the
// instructions.
func TestReviewLoopStopsWhenARoundLeavesNothingToActOn(t *testing.T) {
	path, body := readReviewLoop(t)
	section := flowSection(t, body, "### 3. Check for clean approval")

	if !strings.Contains(section, "continue to step 4") {
		t.Errorf(
			"%s step 3 no longer sends a round that DID leave open findings on to "+
				"step 4 — that hand-off is how findings reach triage at all",
			path,
		)
	}
	if !strings.Contains(section, "Do not run another round") {
		t.Errorf(
			"%s step 3 no longer stops a round that withheld approval without "+
				"recording a finding — such a round gives step 4 nothing to triage, "+
				"so the loop falls through and spins through every remaining round, "+
				"re-running the same reviewers against the same tree",
			path,
		)
	}
}

// TestReviewLoopTriageRoutesEveryOpenFinding guards step 4's routing: each open
// id goes to exactly one of two places, and neither place is "settled by the
// main session". A finding that needs a human is left open deliberately — the
// journal is what carries it to the human, so settling it on their behalf is
// the one thing that makes it disappear.
//
// What this catches: the two-pile split collapsing, the escalation pile losing
// its "leave it open" rule, or either settle outcome being dropped so a verified
// finding has no way to be closed.
// What this does NOT catch: a finding being sorted into the wrong pile — the
// test that separates "outside this loop's authority" from "hard" is judgment.
func TestReviewLoopTriageRoutesEveryOpenFinding(t *testing.T) {
	path, body := readReviewLoop(t)
	section := flowSection(
		t,
		body,
		"### 4. Otherwise, triage each open finding, then settle it",
	)

	if !strings.Contains(section, "two piles") {
		t.Errorf(
			"%s step 4 no longer sorts each open id into two piles — without the "+
				"split there is nothing separating a finding a subagent can fix from "+
				"one only the human can decide",
			path,
		)
	}
	if !strings.Contains(section, "Leave it open") {
		t.Errorf(
			"%s step 4 no longer says an escalated finding is left open — the "+
				"journal is the only thing carrying it to the human, so settling it "+
				"here removes it from their view entirely",
			path,
		)
	}
	for _, outcome := range []string{"--as fixed", "--as rejected"} {
		if !strings.Contains(section, outcome) {
			t.Errorf(
				"%s step 4 no longer gives the subagent the `%s` settle command — a "+
					"verified finding it cannot close comes back unchanged next round",
				path, outcome,
			)
		}
	}
}

// TestReviewLoopDispatchesOneSubagentPerRound guards step 6's dispatch shape.
// One fresh subagent carries the whole round: per-finding subagents each rebuild
// the same branch context, re-run the same suites, and cannot see each other's
// edits, so two fixes touching one file collide. Keeping the fix work out of the
// main session is the other half — it is why the main session's context stays
// free of diffs and test output, which is the entire point of the split.
//
// What this catches: a regression to one subagent per finding, or the main
// session being allowed back into the fix work.
// What this does NOT catch: a dispatch that says "one subagent" and then omits
// half the round's findings from it.
func TestReviewLoopDispatchesOneSubagentPerRound(t *testing.T) {
	path, body := readReviewLoop(t)
	section := flowSection(t, body, "### 6. The fix subagent")

	if !strings.Contains(section, "one fresh subagent per round") {
		t.Errorf(
			"%s step 6 no longer dispatches one fresh subagent per round — the "+
				"alternative is one per finding, where two fixes touching one file "+
				"collide because neither subagent can see the other's edits",
			path,
		)
	}
	if !strings.Contains(section, "never one subagent per finding") {
		t.Errorf(
			"%s step 6 no longer rules out one subagent per finding explicitly — "+
				"stating the rule without ruling out the alternative leaves the "+
				"collision available to anyone reading quickly",
			path,
		)
	}
	if !strings.Contains(section, "The main session stays out of the fix work entirely") {
		t.Errorf(
			"%s step 6 no longer keeps the main session out of the fix work — that "+
				"exclusion is what keeps diffs and test runs out of its context, and "+
				"is the reason the round is dispatched at all",
			path,
		)
	}
}

// TestReviewLoopTrustsTheJournalOverTheSubagent guards the settlement check. A
// subagent reports what it believes it did; the journal records what actually
// happened, and the two can differ — a settle command that failed still leaves
// the id under `open:`. So the main session re-reads the journal after the
// subagent returns and treats that as the round's state, and step 5 stops for
// anything still open there. Without both halves, a finding the subagent only
// claimed to settle is waved past and never reaches the human.
//
// What this catches: the post-dispatch journal re-read disappearing from step 4,
// or step 5 no longer stopping on anything left open.
// What this does NOT catch: the main session re-reading the journal and then
// reporting something other than what it says.
func TestReviewLoopTrustsTheJournalOverTheSubagent(t *testing.T) {
	path, body := readReviewLoop(t)

	fixSection := flowSection(
		t,
		body,
		"### 4. Otherwise, triage each open finding, then settle it",
	)
	if !strings.Contains(fixSection, "re-read") || !strings.Contains(fixSection, "review-notes") {
		t.Errorf(
			"%s step 4 no longer re-reads `devgeta task review-notes` after the "+
				"subagent returns — the subagent's report is a claim, and a settle "+
				"that failed leaves the id open while the report says it is closed",
			path,
		)
	}

	stopSection := flowSection(
		t,
		body,
		"### 5. Stop for anything escalated, enforce the phase cap, then route to the next phase",
	)
	if !strings.Contains(stopSection, "still open") ||
		!strings.Contains(stopSection, "any id still under") {
		t.Errorf(
			"%s step 5 no longer stops for anything still open after the round — "+
				"without it the loop runs more rounds over a finding it already knows "+
				"it cannot settle, and step 3 can never call the result clean anyway",
			path,
		)
	}
}

// TestReviewLoopFixedSettlementNamesTheTestEvidence keeps the two halves of one
// instruction aligned. The `--note` a subagent writes when it settles a finding
// `fixed` is specified twice: step 4 gives the command template it copies, and
// step 6's never-do list requires the test command and its result in that same
// note. When only the never-do list carries the requirement, the template a
// subagent actually copies invites "what changed and where" and nothing more,
// and the journal loses the evidence a human needs to check the fix.
//
// What this catches: either half drifting — the template dropping the evidence
// again, or the never-do rule that demands it disappearing.
// What this does NOT catch: a subagent that fills the placeholder in and names a
// test it never ran; no substring check over prose can verify that.
func TestReviewLoopFixedSettlementNamesTheTestEvidence(t *testing.T) {
	path, body := readReviewLoop(t)

	settleSection := flowSection(
		t,
		body,
		"### 4. Otherwise, triage each open finding, then settle it",
	)
	if !strings.Contains(settleSection, "test command") {
		t.Errorf(
			"%s step 4's `--as fixed` settle template no longer asks for the test "+
				"command and its result — the template is what a subagent copies, so "+
				"the evidence stops reaching the journal even though step 6 still "+
				"requires it",
			path,
		)
	}

	dispatchSection := flowSection(t, body, "### 6. The fix subagent")
	if !strings.Contains(dispatchSection, "name the command and its result") {
		t.Errorf(
			"%s step 6's never-do list no longer requires the test command and its "+
				"result in the `--note` — that rule is what makes the fix verifiable "+
				"by whoever reads the journal later",
			path,
		)
	}
}

// TestReviewLoopRestoresReviewerConfig guards the only thing this loop does
// that can damage something the user owns. Narrowing works by rewriting
// `review.reviewers` down to the reviewers still blocking, running one round,
// and putting the original list back, so a run that narrows and never restores
// leaves the human's configured reviewer list permanently replaced by whichever
// subset happened to be blocking when the loop stopped.
//
// The restore is specified in TWO places and both are required. Step 1 prints
// the recorded list and the exact restore command BEFORE the first narrowing
// write; the terminal report's reviewer-configuration block prints it again.
// The step 1 copy matters most: it is the only one an interrupted run ever
// reaches, because a session killed between the narrowing write and the restore
// emits no terminal report at all. The other half of that rule is anchored in
// both places too, since a later edit is most likely to drop it as an apparent
// inconsistency: when the loop holds no proved record, both places print NO
// restore command and say the key was never written instead.
//
// The remaining assertions are what keep the restore honest rather than merely
// present — the entries survive the shell as one argument each, the three
// screens that refuse narrowing outright rather than narrowing onto a record
// that cannot be put back, the two key-state checks that gate the writes, and
// the whole-string comparison of the write's own output.
//
// The negative assertion is the one rule a substring check can enforce properly
// rather than gesture at, so it is stated precisely: neither `global_config.yaml`
// nor `.config/devgeta` may appear anywhere in the shipped body, checked
// case-sensitively over the WHOLE file rather than one section so the failure
// names which literal appeared. Both halves are needed — the filename alone
// misses `~/.config/devgeta/*.yaml`, and the directory alone misses
// `$XDG_CONFIG_HOME/devgeta/global_config.yaml`, since that variable need not
// expand to anything containing `.config`. Between them they cover every
// path-shaped way to reach devgeta's stored config, which is the rule most
// likely to be undone by someone reaching for the "obvious" source of the
// reviewer list.
//
// Why that file is banned, and why the guard over-catches on purpose (an
// innocent mention fails too): the file sits outside the repository under
// review, and the only external directory either shipped agent grants is the
// disposable scratch root (`external_directory` in
// configs/opencode/opencode.json.tmpl, `additionalDirectories` in
// configs/claude/settings.json.tmpl). A read outside it prompts, and a headless
// run has nobody to answer, so it auto-rejects — the run this cycle came from
// produced four such auto-rejections. Widening the grant is an agent-permission
// change, which CLAUDE.md §10 lists as never-silently. Because the assertion is
// an exact substring check, the shipped command has to explain the ban while
// identifying that file DESCRIPTIVELY, never by name or path; that wording rule
// is Step 2 of
// docs/plans/cycles/2026-08-12-review-loop-narrows-to-blocking-reviewers.md.
// This comment is the only place a puzzled implementer can read either fact,
// because the shipped command may not carry devgeta's own reasoning
// (CLAUDE.md §12).
//
// What this catches: the restore, its pre-mutation print, its no-record branch,
// the quoting rule, any of the three refusal screens, either key-state check, or
// the captured-write comparison being deleted from the shipped command — and a
// path-shaped read of devgeta's stored config being written back in.
// What this does NOT catch: any of it being disobeyed at run time. An executing
// agent can skip the restore, print the command after the mutation instead of
// before it, run the checks and ignore the answer, quote the entries in prose
// while interpolating them bare in practice, or name the escaped rendering and
// then echo a value raw; a substring check over prose cannot execute the
// instructions, and no harness in this repo can (see
// TestCommittingCommandsDeclareStandingAuthorization). And one thing no test or
// prose reaches at all: `devgeta task review-run` prints each reviewer's label
// raw before the loop ever reads it, so these assertions bound the loop's own
// output, not everything a round puts on screen.
func TestReviewLoopRestoresReviewerConfig(t *testing.T) {
	path, body := readReviewLoop(t)

	// Step 1 is where the record is derived from the opening round's labels,
	// proved, screened, and printed with its restore command before the first
	// mutation — so every rule that keeps the round-trip exact lives there.
	roundSection := flowSection(t, body, "### 1. Run a round")
	roundRules := []struct{ want, why string }{
		{
			"devgeta config set review.reviewers <entry> <entry> …",
			"that command IS the restore; without it the loop can narrow the key " +
				"with no recorded way back to the user's own list",
		},
		{
			"before the first narrowing write",
			"the recorded list and its restore command must be on screen before " +
				"the key is mutated — stated only in the terminal report they are " +
				"lost for exactly the run that needs them, since a session killed " +
				"mid-round emits no report at all",
		},
		{
			"no restore command at all",
			"a run whose refusal turned narrowing off must print no restore " +
				"command; printing one anyway tells the human to paste back a " +
				"change nobody made",
		},
		{
			"never writes the key",
			"the no-command branch has to say the key was left untouched, or its " +
				"silence reads as the restore line having gone missing",
		},
		{
			"one argument per entry",
			"`devgeta config set` takes one argument per entry — a comma-joined " +
				"string is stored as one bogus model id, so the restore writes a " +
				"list the user never had",
		},
		{
			"single-quote each entry",
			"a stored entry may legally hold spaces, `;` or `$( )`, so an " +
				"unquoted one splits into two reviewers or runs as a second command",
		},
		{
			"cannot write back",
			"a recorded entry `devgeta config set` would reject must refuse " +
				"narrowing outright; narrowing first and restoring only the entries " +
				"that validate drops the rest permanently",
		},
		{
			`the labels joined with ", "`,
			"the record is accepted only when the joined labels are byte-identical " +
				"to the `get` line — without that comparison the loop restores a " +
				"list it never proved the user had",
		},
		{
			"Any mismatch turns narrowing off",
			"the join check has to be a condition on narrowing, not a note; a " +
				"check whose failure changes nothing is not a check",
		},
		{
			"contains a byte below `0x20`",
			"a recorded entry holding a control byte must refuse narrowing, " +
				"because its restore command cannot be both safe to print and " +
				"correct to paste",
		},
		{
			"escaped rendering",
			"a value the loop composed is displayed escaped — printed raw, an " +
				"entry ending in an erase-line sequence wipes the very line " +
				"reporting it, including the recovery line",
		},
		{
			"never run that `get` bare",
			"a `get` whose output is displayed goes through the filter; run bare, " +
				"the stored bytes have already reached the terminal before the loop " +
				"can screen them",
		},
		{
			"LC_ALL=C sed -n l",
			"that filter is the only route for a value the loop did not compose " +
				"and therefore cannot escape",
		},
		{
			"Narrow onto the recorded list",
			"a narrowing write may happen only while the key still holds the " +
				"recorded list, or it overwrites whatever the user changed it to " +
				"between rounds",
		},
		{
			"immediately before every narrowing write",
			"that check has to run before EVERY narrowing write, not once after " +
				"the opening round — the gap between rounds is the widest window " +
				"the key can change in",
		},
		{
			"Restore onto the narrowed list",
			"the restore may happen only while the key still holds the narrowed " +
				"list the loop wrote, or it clobbers a change made during the round",
		},
		{
			"capture the write's stdout and compare the whole string",
			"the write's own `<key>: <previous> -> <new>` line is the only reading " +
				"of the value it replaced, and comparing it whole is what makes a " +
				"write that landed on something else visible",
		},
		{
			"Never split a previous value out of it",
			"an entry may legally contain `\" -> \"`, so cutting that line at the " +
				"first one both hides a real change and invents a false one",
		},
	}
	for _, rule := range roundRules {
		if !strings.Contains(roundSection, rule.want) {
			t.Errorf(
				"%s step 1 no longer contains %q — %s",
				path, rule.want, rule.why,
			)
		}
	}

	// The report is the second of the two places the restore is required. It is
	// the one a human reading only the outcome sees; step 1's copy is the one an
	// interrupted run leaves behind. Both, or the story has a hole at one end.
	reportSection := flowSection(t, body, "### The reviewer-configuration block")
	reportRules := []struct{ want, why string }{
		{
			"devgeta config set review.reviewers <entry> <entry> …",
			"the terminal report no longer repeats the recorded restore command, " +
				"so a human reading only the outcome never learns how to put their " +
				"own reviewer list back",
		},
		{
			"no command",
			"the report must print no restore command when the loop held no proved " +
				"record — the same branch step 1 takes, and dropping it here " +
				"produces a command that writes a list the user may never have had",
		},
		{
			"was never written this run",
			"the no-command branch has to say `review.reviewers` was never written, " +
				"or the missing command reads as the report having lost it",
		},
	}
	for _, rule := range reportRules {
		if !strings.Contains(reportSection, rule.want) {
			t.Errorf(
				"%s's reviewer-configuration block no longer contains %q — %s",
				path, rule.want, rule.why,
			)
		}
	}

	// Case-sensitive, over the whole file, one assertion per literal so the
	// failure names which one appeared. See this test's doc comment for why the
	// shipped command must identify that file descriptively instead.
	for _, forbidden := range []string{"global_config.yaml", ".config/devgeta"} {
		if strings.Contains(body, forbidden) {
			t.Errorf(
				"%s names %q — the loop must learn everything about "+
					"`review.reviewers` from `devgeta config get`/`set`, never by "+
					"reading devgeta's stored config. That file is outside the "+
					"repository under review, the only external directory either "+
					"shipped agent grants is the disposable scratch root, and a read "+
					"outside it auto-rejects in a headless run instead of prompting — "+
					"so a step that reads it cannot reliably reach its first round. "+
					"If this fired on prose explaining the ban rather than on an "+
					"instruction to read the file, reword it to identify the file "+
					"descriptively, without its name or its path",
				path, forbidden,
			)
		}
	}
}

// TestReviewLoopQuotesEntriesInComparisonsToo guards the second half of the
// quoting rule, which used to be missing. Every entry stored in
// `review.reviewers` is untrusted input to a command line: the validator asks
// only for a `/` that is neither the first nor the last character, so a single
// quote is legal inside one. The rule listed exactly three places an entry has
// to be single-quoted — the narrowing write, the restore, and the restore
// command printed for a human — and all three are `devgeta config set`
// invocations. But the loop composes entries, and their `", "` join, into a
// fourth class of command line the enumeration missed: the `[ "$(devgeta config
// get review.reviewers)" = '<join>' ]` comparisons. There are five of them (the
// record's proof check, the pre-write check, the worked capture snippet, the
// post-round check, and the post-restore verification), and an entry such as
// `anthropic/x';touch /tmp/pwned;'` closes the test's quoted string early and
// runs the rest as a command — at the proof check, on the opening round, which
// is the earliest point in the protocol and before any refusal in the shipped
// file could have run.
//
// The fix is the enumeration, not the control-byte refusal: `'` is printable,
// so refusing it would cost narrowing on a config a human could plausibly have
// (a model id is free-form text), which is why the refusal branch deliberately
// triggers on control bytes only.
//
// What this catches: the enumeration narrowing back to the three writes,
// including the literal revert to "in **all three** places entries appear", and
// the POSIX escape for a single quote inside single quotes no longer being
// required inside a comparison string. (That escape sequence is spelled out in
// the asserted string below rather than here: a formatter in this environment
// rewrites two adjacent ASCII apostrophes in a comment into a curly quote, so a
// comment is not a safe place to write it.)
// What this does NOT catch: an executing agent that reads the rule and still
// interpolates a join bare — a substring check over prose cannot run the
// instructions (see TestCommittingCommandsDeclareStandingAuthorization). Nor
// does it reach the raw labels `devgeta task review-run` prints before the loop
// has composed anything.
func TestReviewLoopQuotesEntriesInComparisonsToo(t *testing.T) {
	path, body := readReviewLoop(t)
	section := flowSection(t, body, "### 1. Run a round")

	rules := []struct{ want, why string }{
		{
			"not just the ones that write the key",
			"the quoting rule has to say outright that it reaches past the three " +
				"`devgeta config set` invocations, or the next reader enumerates " +
				"the writes again and leaves the comparisons bare",
		},
		{
			"every comparison string",
			"the entries and their join are embedded in five `[ \"$(…)\" = '…' ]` " +
				"tests; an unquoted one runs the rest of the entry as a command at " +
				"the proof check, on the opening round",
		},
		{
			"needs the same `'\\''` escape in those comparison strings",
			"a `'` is legal inside a stored entry, so quoting a comparison string " +
				"without escaping the interior quote closes it early — the exact " +
				"hole the widened enumeration exists to close",
		},
	}
	for _, rule := range rules {
		if !strings.Contains(section, rule.want) {
			t.Errorf(
				"%s step 1's quoting rule no longer contains %q — %s",
				path, rule.want, rule.why,
			)
		}
	}

	if strings.Contains(section, "**all three** places") {
		t.Errorf(
			"%s step 1's quoting rule is back to naming three places entries "+
				"appear. All three are `devgeta config set` invocations, so the "+
				"comparison strings the loop also composes from those entries are "+
				"left unquoted and a stored `'` reaches the shell",
			path,
		)
	}
}

// TestReviewLoopRoutesARoundThatPrintedNoVerdicts guards the destination of a
// round that fails as a whole. `devgeta task review-run` abandons a round whose
// HEAD moved mid-flight and prints no verdict lines at all, so the state is
// reachable — step 6's never-do list is written around exactly that failure —
// but nothing routed it.
//
// The missing branch is not the dangerous half. Step 3's clean-approval gate
// requires "every reviewer's outcome this round is `APPROVE`", which is
// VACUOUSLY TRUE over an empty set of outcomes, so an abandoned confirming round
// satisfies every condition and reads as a clean approval. Step 3's fall-through
// cases have the mirror problem: "every non-approving outcome was `ERROR` or `NO
// VERDICT`" is vacuously true too, so a zero-line round matches two branches on
// nothing and none on its merits. The rule therefore lives in `### 1`, ahead of
// every consumer of the verdict lines, and step 3 carries a cross-reference
// rather than a second copy.
//
// What this catches: `### 1` losing the state, its not-vacuously-satisfied rule,
// or its destination; and step 3 losing the clause that defers to it, which is
// where the vacuous reading would actually be acted on.
// What this does NOT catch: an agent that reads both and still treats a silent
// round as unanimous. Substring checks over prose cannot execute the
// instructions.
func TestReviewLoopRoutesARoundThatPrintedNoVerdicts(t *testing.T) {
	path, body := readReviewLoop(t)

	roundSection := flowSection(t, body, "### 1. Run a round")
	roundRules := []struct{ want, why string }{
		{
			"no verdict lines at all did not complete",
			"a round that printed nothing has to be named as incomplete, or it " +
				"is read as a round whose every reviewer happened to agree",
		},
		{
			"empty set of outcomes",
			"the vacuous reading is the whole danger: without the sentence " +
				"ruling it out, \"every outcome is `APPROVE`\" is satisfied by a " +
				"round that produced no outcomes at all",
		},
		{
			"go to the terminal report",
			"the state needs a destination; a rule that says a round failed and " +
				"not where it goes leaves the loop to pick one of the branches " +
				"that match it vacuously",
		},
	}
	for _, rule := range roundRules {
		if !strings.Contains(roundSection, rule.want) {
			t.Errorf(
				"%s step 1 no longer contains %q — %s",
				path, rule.want, rule.why,
			)
		}
	}

	approvalSection := flowSection(t, body, "### 3. Check for clean approval")
	if !strings.Contains(approvalSection, "no verdict lines at all") {
		t.Errorf(
			"%s step 3 no longer excludes a round that printed no verdict lines. "+
				"Step 3 is where the vacuous reading gets acted on — its "+
				"clean-approval gate and its `ERROR`/`NO VERDICT` fall-through are "+
				"both satisfied by an empty set of outcomes — so the exclusion has "+
				"to be visible here, even though `### 1` states the rule",
			path,
		)
	}
}

// TestReviewLoopCleanApprovalRequiresConfirmingRound is the third condition on
// the same gate TestReviewLoopCleanApprovalRequiresNothingOpen guards. Narrowing
// made an approval cheap to collect and easy to over-trust: the opening round
// and every narrowing round hand out approvals while the branch is still being
// changed by the fix subagents, so by the time the loop would act on one, it is
// evidence about a version of the branch that no longer exists. A reviewer can
// approve during narrowing and find something real on its very next look. Only
// the confirming round — every configured reviewer, run again over the settled
// branch — can produce a clean approval, and step 3 has to say so, or the loop
// ends the run on the first unanimous narrowing round and ships whatever the
// later rounds would have caught.
//
// What this catches: the confirming-round condition being dropped from step 3,
// or kept as a description without being the exclusive one — which would let an
// all-APPROVE, nothing-open narrowing round read as clean.
// What this does NOT catch: the same limitation the other step 3 guard carries.
// This is a substring match over prose, so it proves the concept is still named
// in the right place and nothing more — it cannot distinguish "only the
// confirming round produces one" from a reword that mentions the confirming
// round and no longer requires it, and it cannot make an executing agent obey a
// correctly-worded instruction.
func TestReviewLoopCleanApprovalRequiresConfirmingRound(t *testing.T) {
	path, body := readReviewLoop(t)
	section := flowSection(t, body, "### 3. Check for clean approval")

	if !strings.Contains(section, "confirming round") {
		t.Errorf(
			"%s step 3 no longer names the confirming round at all — with only "+
				"all-APPROVE and nothing-open left, the first unanimous narrowing "+
				"round ends the run on approvals given to a branch the fix subagents "+
				"have since changed",
			path,
		)
	}
	if !strings.Contains(section, "Only the confirming round") {
		t.Errorf(
			"%s step 3 no longer makes the confirming round the ONLY round that can "+
				"produce a clean approval — naming the phase without making it "+
				"exclusive is not a gate, and an approval from the opening or a "+
				"narrowing round is provisional by construction",
			path,
		)
	}
	if !strings.Contains(section, "is not the confirming round") {
		t.Errorf(
			"%s step 3 no longer routes the round that met every other condition "+
				"but is not the confirming round — without that branch an "+
				"all-APPROVE, nothing-open narrowing round has nowhere to go, and "+
				"the reading that sends it to the clean-approval stop is exactly the "+
				"one this condition exists to forbid",
			path,
		)
	}
}

// TestReviewLoopRunsUnattendedWithoutAsking guards the standing authorization
// that makes this loop a loop. It invokes no outward `devgeta task` verb and no
// `git commit`, so neither derived authorization test above reaches it — yet it
// lost the same `permission:` frontmatter, and it is the command with the most
// to lose from the fallback: an unattended loop that stops to ask before each
// round is not unattended, and every pause costs the human the attention the
// loop exists to save.
//
// The carve-out is checked with it, not separately. "Do everything yourself
// without asking" and "ratification is the human's alone"
// (TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport) are in tension, so the
// exception has to sit in the same breath as the grant — an authorization
// stated without it invites the loop to ratify its own rejections.
//
// What this catches: the authorization bullet being dropped from the Notes
// section, or kept while its human-only exception is dropped.
// What this does NOT catch: wording that keeps the words but reverses the
// meaning, or an agent asking anyway despite a correctly-worded instruction —
// a substring check over prose cannot execute the instructions, and no harness
// in this repo can (see TestCommittingCommandsDeclareStandingAuthorization).
func TestReviewLoopRunsUnattendedWithoutAsking(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	section := markdownSection(t, body, "## Notes")
	requireStandingAuthorization(t, path, `"## Notes" section`, section)

	// The grant and its one exception must live in the same section, so a
	// reader of the grant cannot reach the end of it without meeting the limit.
	lower := strings.ToLower(section)
	if !strings.Contains(lower, "exception") {
		t.Errorf(
			"%s grants the loop standing authorization to run unattended but its "+
				"%q section no longer names the exception — retiring an agent's "+
				"rejection stays the human's call. Without the carve-out beside the "+
				"grant, 'settle findings yourself, without asking' reads as covering "+
				"--ratify too, which is exactly what "+
				"TestReviewLoopOnlyInvokesRatifyOrReopenInTheReport forbids",
			path, "## Notes",
		)
	}
}

// ---------------------------------------------------------------------------
// /pr-review-loop: the PR tick inherits the review-loop family's guards.
//
// It is the same shape of file — a documented flag list, a numbered flow, and a
// standing grant to act unattended — so it is exposed to the same two failures
// the three tests below already caught once in review-loop.md: a flag that is
// documented but never parsed or forwarded, and an authorization that goes
// missing so an unattended watch starts asking permission every tick.
//
// Step 0's closing bullet — "anything still left over … stops the tick before
// any state is read" — is what makes its flag list exhaustive, and getting
// that list right matters because of what already happened once:
// `/pr-review-loop --reviewer=document <n>` reviewed nothing on 2026-08-12,
// because step 0 back then only ever read bare words (`code document skill`) —
// `--reviewer <type>` is the sibling `/review-loop` command's spelling, and a
// human moving between the two typed the sibling's flag, which silently
// resolved to nothing instead of selecting the document reviewer. `--once` and
// `--on-request` are this cycle's own additions: they did not exist on
// 2026-08-12 and had nothing to do with that failure.
// TestPRReviewLoopForwardsReviewerTypes is the guard that pins the bare-word
// reviewer vocabulary to the review-run registry's own keys today, so it
// cannot silently drift the way the missing --reviewer spelling once did.
//
// The derived authorization tests above do not reach this file: it posts only
// through /review-pr and /approve-pr, so it contains no literal outward
// `devgeta task` verb for TestPostingCommandsDeclareStandingAuthorization's
// regex to find, and it instructs no commit or push. Its grant is pinned here
// instead.
// ---------------------------------------------------------------------------

// prReviewLoopParseHeading and prReviewLoopRunHeading are the two anchors the
// forwarding tests read. They are the tick's argument-parsing step and its
// reviewer-invocation step — the pair every "documented but does nothing" flag
// bug lives between.
const (
	prReviewLoopParseHeading = "### 0. Resolve the PR number, the reviewer types, the note, and the mode"
	prReviewLoopRunHeading   = "### 5. Run the reviewers, one run per type"
)

// TestPRReviewLoopForwardsReviewerTypes is the pr-review-loop analog of
// TestReviewLoopForwardsReviewerSelector, guarding the same bug in the same two
// places: the reviewer types a human passes must be READ from $ARGUMENTS in
// step 0 and FORWARDED to `devgeta task review-run` in step 5. Documented in
// Usage but missing from either step, they silently do nothing and every tick
// reviews with the runner's default lens instead of the one that was asked for.
//
// The type vocabulary is checked against worktree.BuiltinReviewerChoices()
// rather than against three hard-coded strings, because "no translation layer"
// is the decision this file rests on: the words a human types are the exact
// keys `--reviewer` validates against, so a friendlier alias invented here
// (`doc` for `document` is the tempting one) would be a second vocabulary that
// can drift from the registry it maps onto. Prose cannot import the registry,
// so this test is the only thing tying the two together.
//
// What this catches: either half being deleted, and the documented types
// drifting away from the runner's real keys — including a reviewer added to the
// registry that the tick's usage line never learns about.
// What this does NOT catch: prose that reads plausibly while forwarding a
// stale or wrong value — this is a substring check over prose, not an execution
// of it (see TestCommittingCommandsDeclareStandingAuthorization).
func TestPRReviewLoopForwardsReviewerTypes(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	parseSection := flowSection(t, body, prReviewLoopParseHeading)
	if !strings.Contains(parseSection, "$ARGUMENTS") {
		t.Errorf(
			"%s step 0 no longer mentions parsing $ARGUMENTS — without it the "+
				"reviewer types and the note are never read from anywhere, so the "+
				"Usage section advertises arguments that have no effect",
			path,
		)
	}
	for _, choice := range worktree.BuiltinReviewerChoices() {
		if !strings.Contains(parseSection, choice.Key) {
			t.Errorf(
				"%s step 0 no longer names %q as a reviewer type. The types are "+
					"passed through to `devgeta task review-run --reviewer` verbatim, "+
					"so the loop's vocabulary must be the registry's keys exactly — a "+
					"missing key is a reviewer a human cannot ask for, and a renamed "+
					"one is a translation layer this file deliberately does not have",
				path, choice.Key,
			)
		}
	}

	runSection := flowSection(t, body, prReviewLoopRunHeading)
	if !strings.Contains(runSection, "--reviewer <type>") {
		t.Errorf(
			"%s step 5 no longer forwards --reviewer <type> to `devgeta task "+
				"review-run` — without this the types are parsed in step 0 and then "+
				"dropped, and every tick runs the default reviewer",
			path,
		)
	}
}

// prReviewLoopDriverForm returns the repeat-driver invocation written inside
// section: everything from `/loop <interval> /pr-review-loop` up to the backtick
// that closes it, which is the closing backtick of an inline span in prose and
// the opening backtick of the closing fence in a fenced block. section must be
// whitespace-collapsed (flowSection), so a hand-rewrapped form still reads as one
// line.
//
// It fatals when the form is absent: every caller asserts something ABOUT that
// line, and a missing line is a different failure with its own guard
// (TestPRReviewLoopStartsTheWatchItPromises).
func prReviewLoopDriverForm(t *testing.T, path, where, section string) string {
	t.Helper()
	const driverForm = "/loop <interval> /pr-review-loop"
	formAt := strings.Index(section, driverForm)
	if formAt < 0 {
		t.Fatalf(
			"%s %s no longer names the driver form %q — see "+
				"TestPRReviewLoopStartsTheWatchItPromises",
			path, where, driverForm,
		)
	}
	span := section[formAt:]
	if end := strings.Index(span, "`"); end >= 0 {
		span = span[:end]
	}
	return span
}

// TestPRReviewLoopForwardsTheNote is the --note half of the same guard, for the
// same failure: a flag documented in Usage but never parsed or passed on does
// nothing, and the human's steering silently never reaches a reviewer. Step 0
// must resolve it and step 5 must forward it to every run of the tick.
//
// There is a third place the note can be dropped, and it is the worst of the
// three: the standing-watch handoff. A repeat driver re-runs the command line it
// was handed, so a driver form written as `/loop <interval> /pr-review-loop [n]
// [types]` silently strips the note from EVERY tick it ever fires — the parse
// and run steps stay correct and forward a note that no longer arrives, and the
// human who asked for the watch is by definition not watching. So the driver
// form's own code span must name the flag.
//
// The form is written twice, and both copies are checked, because the two have
// different jobs and only one of them is executed: Usage documents the shape,
// while the handoff step is the line the tick actually runs. A `--note` present
// in Usage and missing from the handoff step would read correctly and still
// strip the note from every tick.
//
// What this catches: `--note` being dropped from step 0, from step 5, or from
// the driver form in either Usage or the handoff step, while the Usage section
// keeps advertising it.
// What this does NOT catch: the tick summarizing or answering the note instead
// of passing it verbatim — that is judgment, which the instruction states
// plainly but no substring check can verify.
func TestPRReviewLoopForwardsTheNote(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	if !strings.Contains(body, "--note <text>") {
		t.Fatalf("%s no longer documents --note <text> at all", path)
	}

	for _, site := range []struct {
		heading string
		where   string
		why     string
	}{
		{
			heading: prReviewLoopUsageHeading,
			where:   "Usage",
			why: "the documented shape of the watch is missing the flag, so the next " +
				"hand that copies it into the handoff step copies the loss",
		},
		{
			heading: prReviewLoopHandoffHeading,
			where:   "the handoff step",
			why: "this is the line the tick actually runs, so the note is stripped " +
				"from every tick the watch fires no matter how Usage reads",
		},
	} {
		span := prReviewLoopDriverForm(t, path, site.where, flowSection(t, body, site.heading))
		if !strings.Contains(span, "--note") {
			t.Errorf(
				"%s hands the repeat driver `%s` in %s without --note. The driver "+
					"re-runs exactly this command line, so the human's reviewer emphasis "+
					"is stripped from every tick the watch ever fires — and step 0 and "+
					"step 5 look correct while it happens, because they forward a note "+
					"that is never passed in. Here, %s",
				path, span, site.where, site.why,
			)
		}
	}

	parseSection := flowSection(t, body, prReviewLoopParseHeading)
	if !strings.Contains(parseSection, "--note") {
		t.Errorf(
			"%s step 0 no longer resolves --note — a note that is never parsed "+
				"never reaches a reviewer, so the flag is Usage-section text with no "+
				"effect",
			path,
		)
	}
	runSection := flowSection(t, body, prReviewLoopRunHeading)
	if !strings.Contains(runSection, "--note <text>") {
		t.Errorf(
			"%s step 5 no longer forwards --note <text> to `devgeta task "+
				"review-run` — without this the note is parsed and then dropped",
			path,
		)
	}
}

// TestPRReviewLoopRunsUnattendedWithoutAsking guards the standing authorization
// that makes this tick usable at all, the analog of
// TestReviewLoopRunsUnattendedWithoutAsking. A tick reads GitHub state, fetches
// refs, runs reviewer agents, and then posts a review or an approval — every
// one of those is something an agent's default instinct is to confirm first,
// and the whole point of a watch running on an interval is that nobody is there
// to answer. The `permission:` frontmatter that used to carry this was removed
// in 6bb17ab because OpenCode never enforced it, so prose is the only carrier
// left.
//
// Unlike review-loop.md, no exception is required beside the grant. That file's
// carve-out exists because it settles journal findings and could reach for
// --ratify; this tick settles nothing itself — /review-pr does any settling —
// and it has no decision reserved for the human other than stopping, which is
// an absence of action rather than a permission it must not exercise.
//
// What this catches: the authorization section being dropped or reworded so it
// no longer grants the authority AND forbids the confirmation pause.
// What this does NOT catch: wording that keeps the words but reverses the
// meaning, or an agent asking anyway despite a correctly-worded instruction.
func TestPRReviewLoopRunsUnattendedWithoutAsking(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	section := markdownSection(t, body, postingAuthorizationHeading)
	requireStandingAuthorization(
		t, path, fmt.Sprintf("%q section", postingAuthorizationHeading), section,
	)
}

// prReviewLoopPostHeading and prReviewLoopScratchHeading complete the set of
// anchors the tick's guards read: the step that posts, and the step that
// allocates the reports directory.
const (
	prReviewLoopTargetHeading    = "### 3. Resolve the review target"
	prReviewLoopScratchHeading   = "### 4. Allocate the scratch directory"
	prReviewLoopAggregateHeading = "### 6. Aggregate every run's verdict, once"
	prReviewLoopPostHeading      = "### 8. Post exactly one review"
	prReviewLoopCleanupHeading   = "### 10. Clean up"
)

// TestPRReviewLoopCannotApproveWithoutRunningAReviewer guards the tick against
// approving a PR no model ever read. Two steps have to hold for that to be
// impossible, and each closed half of one real hole:
//
//   - Step 3 assigns the reviewer types from the PR's changed files. It used to
//     name three buckets — code, docs/prose, agent prompts — with no bucket for
//     anything else, so a PR touching only a workflow YAML and a shell script
//     could resolve to no type at all. `code` is now the catch-all, which is
//     what the code reviewer already is: it reads the whole diff of its range,
//     not a list of source extensions.
//   - Step 6 then aggregates. Its approval rule is universally quantified over
//     the runs ("every run APPROVE"), and a universal over an empty set is TRUE
//     — so zero runs used to satisfy the approval branch outright. Counting the
//     runs before weighing them is what makes that rule mean something.
//
// Both halves are asserted because either one alone leaves the failure
// reachable: patch only step 3 and any other route to zero runs re-enters the
// vacuous branch; patch only step 6 and every such PR escalates to a human
// instead of being reviewed.
//
// What this catches: the catch-all bucket leaving step 3, or step 6's approval
// branch going back to a bare "every run approved" with no count.
// What this does NOT catch: an agent classifying a file wrongly, or approving
// despite a correctly-worded count — substring checks over prose cannot execute
// the instructions (see TestCommittingCommandsDeclareStandingAuthorization).
func TestPRReviewLoopCannotApproveWithoutRunningAReviewer(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	resolve := flowSection(t, body, prReviewLoopTargetHeading)
	for _, req := range []struct {
		substr string
		why    string
	}{
		{
			substr: "`code` for **everything else**",
			why: "the three named buckets stop covering every changed file, so a PR " +
				"of config, build, or script files resolves to no reviewer type — and " +
				"a tick with no type runs no reviewer",
		},
		{
			substr: "at least one type",
			why: "nothing states the invariant the buckets exist to produce, so a " +
				"later edit can narrow them back down without anything noticing",
		},
	} {
		if !strings.Contains(resolve, req.substr) {
			t.Errorf(
				"%s step 3 is missing %q — %s",
				path, req.substr, req.why,
			)
		}
	}

	aggregate := flowSection(t, body, prReviewLoopAggregateHeading)
	if !strings.Contains(aggregate, "No run happened this tick") {
		t.Errorf(
			"%s step 6 no longer treats \"no run happened\" as its own outcome. "+
				"Every other rule in that step quantifies over the runs, and each is "+
				"trivially true of nothing, so without this case a tick that reviewed "+
				"no code falls straight through to the approval path",
			path,
		)
	}
	if !strings.Contains(
		aggregate,
		"At least one run happened and every run's outcome is `APPROVE`",
	) {
		t.Errorf(
			"%s step 6's approval branch no longer requires a run to have happened. "+
				"\"Every run approved\" is a universal, and a universal over an empty "+
				"set is true — so the count is not decoration, it is the whole guard",
			path,
		)
	}
}

// TestPRReviewLoopForwardsTheRangeFlags guards the four flags that make a
// reviewer run read the PR instead of the checkout. `review-run` requires
// --base/--head/--journal/--report-dir as a group, so dropping one is an error
// the runner reports — with one exception that is silent and severe: --journal
// is what keys the review journal to the PR (ADR-0012 §5), and a run that
// somehow loses it files a PR's findings under whatever branch happens to be
// checked out. The types and the note already have guards above; the range
// flags carry the actual target, and had none.
//
// What this catches: any of the four being dropped from step 5's invocation
// while the rest of the file still describes a PR review.
// What this does NOT catch: the flags carrying wrong values — step 3 resolves
// those, and a substring check over prose cannot follow a value.
func TestPRReviewLoopForwardsTheRangeFlags(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	runSection := flowSection(t, body, prReviewLoopRunHeading)
	required := []struct {
		flag string
		why  string
	}{
		{
			flag: "--base <base>",
			why: "the reviewers would read a range with no start, so the diff under " +
				"review is not the PR's",
		},
		{
			flag: "--head <head>",
			why: "the reviewed commit stops being the one step 3 pinned, so a push " +
				"mid-tick changes what gets reviewed",
		},
		{
			flag: "--journal <key>",
			why: "the journal falls back to the checked-out branch, so this PR's " +
				"findings are filed under an unrelated branch's name — the failure " +
				"ADR-0012 §5's keying exists to prevent, and the one range-mode flag " +
				"whose absence is silent rather than an error",
		},
		{
			flag: "--report-dir",
			why: "the reports have nowhere to land, so step 8 has nothing to read and " +
				"a review composed from journal one-liners throws the findings away",
		},
	}
	for _, req := range required {
		if !strings.Contains(runSection, req.flag) {
			t.Errorf(
				"%s step 5 no longer forwards %s to `devgeta task review-run` — %s",
				path, req.flag, req.why,
			)
		}
	}
}

// invocationFlags returns the flag text of every `/<command> …` inline-code span
// in section — the argument list each posting invocation is written with. The
// spans are matched inside whitespace-collapsed prose, so a hand-rewrapped
// invocation still reads as one span.
func invocationFlags(t *testing.T, section, command string) []string {
	t.Helper()
	re := regexp.MustCompile("`/" + regexp.QuoteMeta(command) + " ([^`]*)`")
	matches := re.FindAllStringSubmatch(section, -1)
	flags := make([]string, 0, len(matches))
	for _, m := range matches {
		flags = append(flags, m[1])
	}
	return flags
}

// TestPRReviewLoopPostingInvocationsCarryTheirScope pins the shape of step 8's
// two invocations, which is where the tick hands a PR that is not the checkout
// to a command that defaults to the checkout. Every one of these was a
// maintainer decision that nothing held in place, and each has a distinct
// failure:
//
//   - `/review-pr` needs --base: a head alone cannot yield a merge base, and a
//     tip-based diff reads every commit merged since the PR opened as a finding
//     against it.
//   - `/review-pr` needs --journal: without the key its own settle commands fall
//     back to the checkout's journal, where the same sequential id is a
//     different finding — so a settle closes a real, unrelated open finding with
//     a note about this PR.
//   - `/approve-pr` must NOT get --base: it judges threads at a commit and never
//     diffs a range, so a base sha there is a flag its file does not document.
//     It never reads the journal either, so it takes no key.
//   - Both need --target: it is the whole reason either command can judge a PR
//     that is not checked out.
//
// What this catches: any of the four being dropped or added while the tick's
// prose still reads plausibly.
// What this does NOT catch: the values being wrong (step 3 resolves them), or
// the invoked command ignoring a flag it was correctly handed — the companion
// guards over review-pr.md and approve-pr.md cover that side.
func TestPRReviewLoopPostingInvocationsCarryTheirScope(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	section := flowSection(t, body, prReviewLoopPostHeading)

	reviewCalls := invocationFlags(t, section, "review-pr")
	if len(reviewCalls) == 0 {
		t.Fatalf(
			"%s step 8 no longer invokes `/review-pr` — the review path has no way to "+
				"reach the PR",
			path,
		)
	}
	for _, flags := range reviewCalls {
		for _, want := range []string{"--base", "--target", "--journal"} {
			if !strings.Contains(flags, want) {
				t.Errorf(
					"%s step 8's `/review-pr %s` does not pass %s. The PR is not the "+
						"checkout here, so every part of the target has to travel in the "+
						"invocation: --base and --target name the reviewed range, and "+
						"--journal names the journal a settle writes to — without the key, "+
						"`/review-pr` settles an id in the checked-out branch's journal, "+
						"where that id is someone else's open finding",
					path, flags, want,
				)
			}
		}
	}

	approveCalls := invocationFlags(t, section, "approve-pr")
	if len(approveCalls) == 0 {
		t.Fatalf(
			"%s step 8 no longer invokes `/approve-pr` — the approval path has no way "+
				"to reach the PR",
			path,
		)
	}
	for _, flags := range approveCalls {
		if !strings.Contains(flags, "--target") {
			t.Errorf(
				"%s step 8's `/approve-pr %s` does not pass --target, so the approval "+
					"is decided against the working tree instead of the reviewed commit",
				path, flags,
			)
		}
		for _, unwanted := range []string{"--base", "--journal"} {
			if strings.Contains(flags, unwanted) {
				t.Errorf(
					"%s step 8's `/approve-pr %s` passes %s. `/approve-pr` documents "+
						"neither: it judges threads at one commit rather than diffing a "+
						"range, and it never reads or writes the journal — so this is a "+
						"flag its own file has no rule for",
					path, flags, unwanted,
				)
			}
		}
	}

	if !strings.Contains(section, "review-notes --branch <key> --rev <head>") {
		t.Errorf(
			"%s step 8 no longer reads `devgeta task review-notes --branch <key> --rev "+
				"<head>` before composing the review. That read is the only place open "+
				"findings are listed — review-run prints verdicts and report paths, never "+
				"ids — and unscoped it would list the checkout branch's findings instead",
			path,
		)
	}
}

// TestPRReviewLoopScratchVariableCannotCollideWithReviewPR guards the tick's
// reports directory against the one name it must not use. /review-pr, which
// this tick invokes at step 8, allocates and cleans its own scratch directory in
// a variable literally named SCRATCH. Sharing that name across the two files in
// one session means step 10 cleans a directory /review-pr already removed and
// leaves this tick's reports behind.
//
// What this catches: the tick going back to a bare SCRATCH, or step 5 and step
// 10 drifting onto a different variable than step 4 allocated.
// What this does NOT catch: a shell that never exports either variable — this
// is prose, and the collision is about the name, not the value.
func TestPRReviewLoopScratchVariableCannotCollideWithReviewPR(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	alloc := flowSection(t, body, prReviewLoopScratchHeading)
	m := regexp.MustCompile(`([A-Za-z_][A-Za-z0-9_]*)=\$\(devgeta task scratch\)`).
		FindStringSubmatch(alloc)
	if m == nil {
		t.Fatalf(
			"%s step 4 no longer allocates a scratch directory from `devgeta task "+
				"scratch` — the reviewer reports have nowhere to land",
			path,
		)
	}
	name := m[1]
	if name == "SCRATCH" {
		t.Errorf(
			"%s step 4 allocates $SCRATCH, the same variable /review-pr allocates and "+
				"cleans for itself. This tick invokes /review-pr at step 8, between this "+
				"allocation and step 10's cleanup, so one name for two directories makes "+
				"step 10 clean a path that is already gone and leak this tick's reports",
			path,
		)
	}

	ref := "\"$" + name + "\""
	for _, use := range []struct {
		heading string
		what    string
	}{
		{prReviewLoopRunHeading, "step 5 writes the reviewer reports into it"},
		{prReviewLoopCleanupHeading, "step 10 removes it"},
	} {
		if !strings.Contains(flowSection(t, body, use.heading), ref) {
			t.Errorf(
				"%s %s, but no longer names %s — the step that allocates the directory "+
					"and the step that uses it have drifted apart, so one of them acts on "+
					"a variable nothing set",
				path, use.what, ref,
			)
		}
	}
}

// These four anchor the sections the tests below read: where the driver handoff
// is described, where the generic command-failure rule lives, the step that
// actually starts the driver, and where the tick reports what happens next.
//
// The handoff is its own numbered step, and it has to stay numbered ABOVE the
// report step: the report names the driver this tick just started, which it can
// only do once the handoff has run. That is also why the report step is 12 and
// not 11 — the handoff was inserted directly before it so steps 0 through 10
// kept their numbers.
const (
	prReviewLoopUsageHeading   = "## Usage"
	prReviewLoopNotesHeading   = "## Notes"
	prReviewLoopHandoffHeading = "### 11. Start the watch, unless this tick was one"
	prReviewLoopReportHeading  = "### 12. Report the tick"
)

// prReviewLoopReportSection returns the report step's content, whitespace-collapsed
// the way flowSection does it. It cannot go through markdownSection: the step's
// template is a fenced block whose first line starts with `## PR #<n>`, which reads
// as the next heading and truncates the section before the rules that follow it. So
// this anchors on everything after the heading instead — which also takes in the
// "Notes" section below it, harmless for the phrases its callers look for.
func prReviewLoopReportSection(t *testing.T, path, body string) string {
	t.Helper()
	reportAt := strings.Index(body, prReviewLoopReportHeading)
	if reportAt < 0 {
		t.Fatalf("%s no longer has a %q section", path, prReviewLoopReportHeading)
	}
	return strings.Join(strings.Fields(body[reportAt:]), " ")
}

// TestPRReviewLoopStartsTheWatchItPromises guards the gap that made a real user
// think a watch was running when none was. The command's trigger offers to watch
// a PR "unattended", but one invocation is deliberately one tick — so a bare
// invocation that only described the driver in passing answered once and then
// nothing ever ticked again, silently.
//
// Four things close that, and all are asserted here because each alone leaves the
// same surprise:
//
//   - Usage must make the handoff an obligation and name the driver form,
//     otherwise the file only mentions a driver a human has to know to type.
//   - The handoff step must actually start one. That is a step of its own, and it
//     is the only place in the flow that starts a driver.
//   - That step must come AFTER the posting step (ADR-0025 §4). A driver is
//     cron-backed and fires at its next scheduled match, never on creation, so a
//     handoff placed before the review answers the human with a state read now and
//     their review one whole interval later — the failure that moved it here. The
//     check is an index comparison rather than a phrase, because prose can claim
//     "after the outcome is known" from anywhere in the file.
//   - Step 0 must NOT start one. Two places that both hand off start two drivers
//     on one invocation, and the second is invisible until the PR gets reviewed
//     twice per interval. This is checked two ways, and both are required: a
//     negative (the driver form `/loop <interval> /pr-review-loop` is absent from
//     step 0) and a positive (step 0 still carries its own "nothing is handed off
//     here" clause). The negative alone pins nothing against the bug that
//     motivated it: the handoff step 1 of this cycle deleted was prose ("hand it
//     this command with every argument just resolved … then exit") that contained
//     no `/loop` literal, so a regression back to that wording would pass the
//     negative untouched. The positive clause fails against both that old prose
//     and a re-added literal form.
//
// The report step then has to name the result either way: the driver that will
// run the next tick, or — when no driver is repeating this command — that
// nothing will. (A terminal exit starts nothing too and says so as "the watch is
// over", which the report template already carries.) A lone tick must never read
// as a watch. That "nothing repeats" branch has to be conditioned on no driver
// repeating the command, not on step 11 having started none: on a watch tick
// step 11 always starts none, but a driver IS repeating the command — the one
// that fired the tick — so the weaker condition is true on every watch tick and
// would have the report claim nothing repeats while the watch keeps going.
//
// The condition is stated twice in the step — once naming the "nothing" branch
// of what runs next, once introducing the instruction to actually say so — and
// each of the two sites is worded differently and asserted on its own below.
// A single check for one phrase would still pass if only the other site were
// reverted, because the surviving site's wording would still match it.
//
// What this catches: the handoff being reduced back to a passing mention (by
// dropping the driver form from Usage or from the handoff step), the handoff
// drifting back above the posting step, step 0 growing a second handoff (in
// either its literal or its old prose form), the report losing either branch of
// what happens next, and either of the two "nothing repeats" sites drifting
// back onto "step 11 started none" so it fires on a watch tick too.
// What this does NOT catch: whether the harness's driver actually starts, an
// agent reading a correct instruction and skipping it, or a step 0 that keeps
// "Nothing is handed off for repetition here" while adding a second, prose-only
// handoff beneath it — the negative check above finds no `/loop` literal and
// the positive check finds the sentence still there, so a self-contradictory
// step 0 like that passes both. This is a substring check over prose, not an
// execution of it.
func TestPRReviewLoopStartsTheWatchItPromises(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	usage := flowSection(t, body, prReviewLoopUsageHeading)
	if !strings.Contains(usage, "/loop <interval> /pr-review-loop") {
		t.Errorf(
			"%s no longer names the driver form `/loop <interval> /pr-review-loop` in "+
				"its Usage section. One invocation is one tick, so that form is the only "+
				"thing that turns this command into the standing watch its description "+
				"offers",
			path,
		)
	}
	if !strings.Contains(usage, "standing watch") {
		t.Errorf(
			"%s Usage no longer distinguishes a standing watch from a single tick. "+
				"Without that distinction the file reads as if invoking it once starts a "+
				"watch, which is exactly the failure a human hit: one answer, then silence",
			path,
		)
	}

	handoff := flowSection(t, body, prReviewLoopHandoffHeading)
	if !strings.Contains(handoff, "/loop <interval> /pr-review-loop") {
		t.Errorf(
			"%s's handoff step (%q) no longer starts the harness's repeat driver. "+
				"Describing the driver in Usage without any step acting on it leaves the "+
				"watch un-started, and no other step starts one",
			path, prReviewLoopHandoffHeading,
		)
	}

	// flowSection above already fataled if the handoff heading is gone, so only
	// the posting heading still needs checking before the comparison.
	handoffAt := strings.Index(body, prReviewLoopHandoffHeading)
	postAt := strings.Index(body, prReviewLoopPostHeading)
	if postAt < 0 {
		t.Fatalf("%s no longer has a %q section", path, prReviewLoopPostHeading)
	}
	if handoffAt < postAt {
		t.Errorf(
			"%s puts the handoff step (%q) BEFORE the posting step (%q). A repeat "+
				"driver fires at its next scheduled match and never on creation, so a "+
				"handoff that runs before the review answers the human who asked for one "+
				"with a state read now and their review a whole interval later. The "+
				"handoff also cannot know whether a watch is still wanted until the "+
				"outcome exists — a first-look approval must start nothing",
			path, prReviewLoopHandoffHeading, prReviewLoopPostHeading,
		)
	}

	parseSection := flowSection(t, body, prReviewLoopParseHeading)
	if strings.Contains(parseSection, "/loop <interval> /pr-review-loop") {
		t.Errorf(
			"%s step 0 starts a repeat driver again. The handoff belongs to one step "+
				"only (%q); two places that both hand off start two drivers on one "+
				"invocation, and the second one is invisible until the PR is reviewed "+
				"twice per interval",
			path, prReviewLoopHandoffHeading,
		)
	}
	// The check above is a negative and pins nothing on its own: the step 0
	// handoff this cycle deleted was prose with no `/loop` literal in it at all
	// ("hand it this command with every argument just resolved … then exit"), so
	// a regression back to that wording would pass the negative untouched. Pin
	// the positive too: step 0 must still say plainly that nothing is handed off
	// there. This fails against both the old prose form and a re-added literal
	// one.
	if !strings.Contains(parseSection, "Nothing is handed off for repetition here") {
		t.Errorf(
			"%s step 0 no longer says that nothing is handed off for repetition "+
				"there. Without that clause, a step 0 rewritten to hand the command to "+
				"a repeat driver in prose — with no `/loop` literal for the negative "+
				"check above to catch — would pass this test while starting a second "+
				"driver alongside %q",
			path, prReviewLoopHandoffHeading,
		)
	}

	report := prReviewLoopReportSection(t, path, body)
	if !strings.Contains(report, "what will run the next tick") {
		t.Errorf(
			"%s's report step no longer requires the report to say what will run the "+
				"next tick. That line is what tells a human a lone tick left the PR "+
				"unwatched; without it, a report about what the next tick expects promises "+
				"a watch the invocation never started",
			path,
		)
	}
	if !strings.Contains(report, "nothing will run another tick") {
		t.Errorf(
			"%s's report step no longer has the branch where nothing repeats. Plenty of "+
				"ticks start no driver — --once, and any harness with no repeat driver — "+
				"and for those the report has to say so in as many words. (A terminal exit "+
				"starts nothing either, and says it as \"the watch is over\"; this is the "+
				"non-terminal case, which the next-tick line above would otherwise leave "+
				"reading as a watch that was never started — the original failure in a new "+
				"place)",
			path,
		)
	}
	// The "no driver repeating this command" condition is stated at two
	// separate sites in the step, worded differently, and each is asserted on
	// its own here — a single check for one phrase would still pass if only
	// the other site were reverted, because the surviving site's wording would
	// still match it (see the comment above this test).
	if !strings.Contains(report, "no driver is repeating this command") {
		t.Errorf(
			"%s's report step no longer conditions \"what will run the next tick\" "+
				"on no driver repeating this command. Conditioning it on step 11 "+
				"having started none instead makes the branch true on a watch tick "+
				"too — step 11 starts no driver on a watch tick, but a driver IS "+
				"repeating the command (the one that fired this tick), so the report "+
				"would claim nothing repeats while the driver keeps firing",
			path,
		)
	}
	if !strings.Contains(report, "this command has no driver behind it") {
		t.Errorf(
			"%s's report step no longer conditions the \"nothing will run another "+
				"tick\" instruction on this command having no driver behind it. "+
				"Wording it as \"step 11 started none\" instead makes the instruction "+
				"fire on a watch tick too — step 11 always starts none there, but a "+
				"driver (the one that fired this tick) is still repeating the "+
				"command, so the report would tell the human nothing repeats while "+
				"the watch keeps going, and point them at the very form that starts "+
				"a second driver",
			path,
		)
	}
}

// TestPRReviewLoopDescriptionCarriesTriggersNotTheHandoff keeps the driver
// handoff out of the frontmatter `description`. The description is the one part
// of a command an agent reads BEFORE — and without — the body: it is what the
// `/` menu and the command listing show, and what an agent picks the command
// from. So it can only carry when to reach for this command, never how the
// command works.
//
// The handoff is the case where that distinction bites. Starting a repeat driver
// on this command is something an agent can do straight from the listing, by
// typing `/loop … /pr-review-loop …` without ever loading this file — and every
// rule that makes the handoff correct lives in the body it skipped: carry the PR
// number, the types and `--note` through verbatim (see
// TestPRReviewLoopForwardsTheNote — a dropped flag is dropped from every tick the
// watch fires), add `--on-request` so the fired ticks are request-gated and start
// no further drivers, start the driver only after the tick's own review, and
// treat the handoff as impossible rather than optional on a harness with no
// driver (OpenCode). A description that names the driver form invites exactly the
// ungated handoff those rules exist to prevent. Internal step numbers have no
// place there either: nothing in a menu entry resolves "step 11", and
// renumbering the flow would silently falsify it.
//
// What this catches: the driver form or a step reference migrating back into the
// description.
// What this does NOT catch: the description drifting away from what the command
// actually does, or an agent starting a driver on its own initiative — neither is
// reachable from a substring check over metadata.
func TestPRReviewLoopDescriptionCarriesTriggersNotTheHandoff(t *testing.T) {
	path, _ := readSharedCommand(t, "pr-review-loop.md")

	var parsed struct {
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(frontmatter(t, path), &parsed); err != nil {
		t.Fatalf("%s frontmatter is not valid YAML: %v", path, err)
	}
	if parsed.Description == "" {
		t.Fatalf(
			"%s frontmatter has no description — that is what the agent picks the command from",
			path,
		)
	}

	if strings.Contains(parsed.Description, "/loop") {
		t.Errorf(
			"%s description names the repeat-driver form. An agent can act on the "+
				"description alone, so the handoff would run without the body's rules "+
				"about it — forwarding every argument, marking the fired ticks with "+
				"--on-request, starting the driver only after this tick's own review, and "+
				"having no driver at all on OpenCode. Keep the form in Usage and in the "+
				"handoff step; leave the description to say when this command is the "+
				"right one",
			path,
		)
	}
	if stepReference.MatchString(parsed.Description) {
		t.Errorf(
			"%s description points at %q. The description is shown without the body, "+
				"so a step number resolves to nothing there and any renumbering makes it "+
				"a lie",
			path, stepReference.FindString(parsed.Description),
		)
	}
}

// stepReference matches a pointer to one of the flow's numbered steps, e.g.
// "step 0" — meaningful inside the body, meaningless in metadata read on its own.
var stepReference = regexp.MustCompile(`(?i)\bstep \d+`)

// TestPRReviewLoopCleansScratchOnEveryExitAfterAllocation guards the tick's one
// leak path. Step 4 allocates a reports directory and step 10 removes it, but
// the generic rule in "Notes" — a `devgeta task` command that refuses to run
// ends the tick — fires for the reviewer runs, both step 7 reads and step 8's
// journal read, all of which happen AFTER that allocation. Read as "stop here",
// it walks out past step 10 and leaves the directory behind on every failing
// tick.
//
// So two things must hold: step 10's condition must be the allocation itself
// rather than a list of exit shapes that a new exit can fall outside of, and the
// refusal rule must route through step 10 instead of around it.
//
// What this catches: step 10 going back to enumerating which exits clean up, and
// the refusal rule losing its pointer to step 10.
// What this does NOT catch: a process killed mid-tick, which runs no cleanup at
// all — that one is swept by the scratch sweep step 10 names.
func TestPRReviewLoopCleansScratchOnEveryExitAfterAllocation(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	cleanup := flowSection(t, body, prReviewLoopCleanupHeading)
	if !strings.Contains(cleanup, "on every exit taken after step 4 allocated it") {
		t.Errorf(
			"%s step 10 no longer conditions the scratch cleanup on step 4 having "+
				"allocated the directory. A list of exit shapes instead of that one "+
				"condition leaks the reports directory for every exit the list forgot",
			path,
		)
	}

	notes := flowSection(t, body, prReviewLoopNotesHeading)
	if !strings.Contains(notes, "goes through step 10") {
		t.Errorf(
			"%s no longer routes the command-refusal rule in Notes through step 10. "+
				"Every command that can refuse after step 4 — the reviewer runs, both "+
				"step 7 reads, step 8's journal read — then ends the tick without "+
				"cleaning the reports directory it allocated",
			path,
		)
	}
}

// ---------------------------------------------------------------------------
// The explicit/watch split (ADR-0025).
//
// A tick is either explicit (a human typed it) or a watch tick (a repeat driver
// fired it, and the line it fired carries `--on-request`). The split lives
// entirely in this file's prose — there is no code path to test — and its
// Consequences/Negative section names four clauses that can rot without any
// visible failure. The five guards below are those clauses plus the flag
// spellings the split is carried by.
//
// Two of the sections they read had no test reader before this, so the anchors
// are new.
// ---------------------------------------------------------------------------

const (
	prReviewLoopDecisionHeading = "### 2. Take exactly one row of the table"
	prReviewLoopPrePostHeading  = "### 7. Re-check the state and the head before posting"
)

// TestPRReviewLoopMarksTheDriversTicksAsWatchTicks pins the one argument on the
// driver's line that the invocation itself never had: `--on-request` (ADR-0025
// §1). It is the whole carrier of the explicit/watch split — a flag rather than
// an inference about the surrounding prompt, precisely so a guard test can read
// it — and it does two jobs on every tick the driver fires: it request-gates the
// tick, and it stops that tick from starting a driver of its own.
//
// So a driver form written without it starts a watch made of EXPLICIT ticks, and
// both jobs fail at once: every fired tick reviews and posts regardless of
// whether a review was requested, and every fired tick starts another driver.
// Nothing about the file reads wrong while that happens, and the human who asked
// for the watch is by definition not watching it.
//
// The form is written at three sites and each is asserted on its own, because a
// revert of one leaves the others matching: Usage documents the shape, the
// handoff step is the line the tick actually runs, and the report step is the
// form a human is handed when no driver is repeating this command (so a form
// missing the marker there hands them the self-multiplying watch by hand).
//
// What this catches: `--on-request` being dropped from any one of the three
// driver forms.
// What this does NOT catch: the flag being present but the tick ignoring it —
// that is what the two guards below pin from the receiving end, and neither can
// execute the prose.
func TestPRReviewLoopMarksTheDriversTicksAsWatchTicks(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	for _, site := range []struct {
		where   string
		section string
		why     string
	}{
		{
			where:   "Usage",
			section: flowSection(t, body, prReviewLoopUsageHeading),
			why: "this is the documented shape of the watch, so the next hand that " +
				"copies it into the handoff step copies the loss",
		},
		{
			where:   "the handoff step",
			section: flowSection(t, body, prReviewLoopHandoffHeading),
			why: "this is the line the tick actually starts, so every tick the watch " +
				"fires is an explicit tick: each reviews and posts with no request, and " +
				"each starts a driver of its own",
		},
		{
			where:   "the report step",
			section: prReviewLoopReportSection(t, path, body),
			why: "this is the form the report hands a human when nothing is watching " +
				"the PR, so they would start the multiplying watch themselves",
		},
	} {
		span := prReviewLoopDriverForm(t, path, site.where, site.section)
		if !strings.Contains(span, "--on-request") {
			t.Errorf(
				"%s writes the driver form `%s` in %s without --on-request. That flag is "+
					"the only thing that tells a fired tick it is a watch tick (ADR-0025 §1): "+
					"without it the driver's ticks are explicit, so each one reviews and posts "+
					"whether or not a review was requested, and each one starts another driver. "+
					"Here, %s",
				path, span, site.where, site.why,
			)
		}
	}
}

// TestPRReviewLoopWatchTickStartsNoDriverOfItsOwn pins the receiving end of the
// marker: only an explicit tick ever starts a driver (ADR-0025 §4). The driver
// that fired a watch tick is already running, so a handoff there starts a second
// driver on every tick — and each of those ticks starts another. The failure is
// exponential and completely silent from inside the file: every individual tick
// behaves correctly, and the human sees only a PR reviewed more and more often.
//
// Two things are asserted in the handoff step and they are not the same
// assertion. The precondition is what an agent evaluates ("this tick is
// explicit — `--on-request` was not on its command line"); the restated rule is
// what survives a rewrite of the precondition list into prose. The rule sentence
// is written twice in the file — once in Usage, once in the handoff step — so
// each site is asserted separately: a revert of one alone would leave a single
// whole-file check green while the two halves of the file contradicted each
// other.
//
// What this catches: the handoff step losing its explicit-only precondition,
// either statement of the no-second-driver rule being deleted, or a fourth
// occurrence of the driver form `/loop <interval> /pr-review-loop` appearing
// anywhere in the file. That count is the one structural signal available for
// "no other site hands this command to a driver" (ADR-0025 §4): the three
// legitimate sites — Usage, the handoff step, the report step — put the count
// at exactly 3, so a fourth site anywhere raises it and fails here, whether or
// not that fourth site's own prose reads correctly on its own.
// What this does NOT catch: an agent reading the rule and starting a driver
// anyway, or a fourth site that hands off in prose with no `/loop` literal in
// it — the count only sees the literal form, not a paraphrase of it.
func TestPRReviewLoopWatchTickStartsNoDriverOfItsOwn(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	const rule = "A `--on-request` tick starts no driver"

	handoff := flowSection(t, body, prReviewLoopHandoffHeading)
	if !strings.Contains(handoff, "`--on-request` was not on its command line") {
		t.Errorf(
			"%s's handoff step (%q) no longer requires the tick to be explicit before "+
				"it starts a driver. That precondition is the one an agent actually "+
				"evaluates, and without it a watch tick starts a second driver — then each "+
				"tick of each driver starts another (ADR-0025 §4)",
			path, prReviewLoopHandoffHeading,
		)
	}
	if !strings.Contains(handoff, rule) {
		t.Errorf(
			"%s's handoff step (%q) no longer states that %q, whatever its outcome. The "+
				"precondition list above it can be rewritten; this sentence is what says "+
				"the rule outright, at the one step that could break it",
			path, prReviewLoopHandoffHeading, rule,
		)
	}

	usage := flowSection(t, body, prReviewLoopUsageHeading)
	if !strings.Contains(usage, rule) {
		t.Errorf(
			"%s's Usage section no longer states that %q. This is the second of the "+
				"two places the rule is written, and it is the one a reader meets before "+
				"the flow — the handoff step keeping it is not a reason to drop it here, "+
				"because the reverse revert is just as easy",
			path, rule,
		)
	}

	// The one structural signal available for "no other site hands this command
	// to a driver": the three legitimate sites (Usage, the handoff step, the
	// report step) write the exact form `/loop <interval> /pr-review-loop`
	// once each, so the count over the whole file is 3. A fourth site anywhere
	// — a new handoff added in a future step, a stray copy left over from an
	// edit — raises that count even if its own local prose reads correctly.
	// Whitespace is collapsed over the whole body first, the same as
	// flowSection does per-section, so a hand-rewrap that splits the form
	// across a line break still counts.
	const driverForm = "/loop <interval> /pr-review-loop"
	flatBody := strings.Join(strings.Fields(body), " ")
	if got := strings.Count(flatBody, driverForm); got != 3 {
		t.Errorf(
			"%s writes the driver form %q %d times; want exactly 3 (Usage, the "+
				"handoff step, and the report step — ADR-0025 §4). If the count is "+
				"below 3, one of those three legitimate sites lost the form. If it is "+
				"above 3, a new site now writes it — that is not necessarily a bug: "+
				"check what the new site is before changing anything. If it hands this "+
				"command to a driver, that starts a second driver on every tick the new "+
				"site fires, so remove it. If it is a legitimate mention instead (a new "+
				"example, or prose describing the driver), update the count this test "+
				"wants and say in the same change which site is new",
			path, driverForm, got,
		)
	}
}

// markdownTableRows returns the rows of the pipe table in section, each split
// into its cells, trimmed, with the outer empties dropped and the `---`
// separator skipped. The header row comes back as a row like any other; callers
// look rows up by their leading cells, which no header matches.
//
// The section is read uncollapsed on purpose: a markdown table row is already
// one line, so nothing here needs flowSection's rewrap tolerance, and collapsing
// would run the rows together into one string with no cell boundaries left.
func markdownTableRows(section string) [][]string {
	var rows [][]string
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if strings.HasPrefix(cells[0], "---") {
			continue
		}
		rows = append(rows, cells)
	}
	return rows
}

// TestPRReviewLoopExplicitRowsAreNotRequestGated pins step 2's decision table
// column by column, which is where ADR-0025's central reversal lives: an
// explicit tick reviews unless the pull request is over, and GitHub's request
// field gates the watch only (§1, §2).
//
// This is the clause the ADR was written for. Before it, every tick was
// request-gated, so `/pr-review-loop <n>` on a PR nobody had requested a review
// on printed three lines of state and stopped — the human had already said what
// they wanted by typing the command, and the file made them say it again in
// prose (ADR-0025 Context, 2026-08-12). Two of the rows an explicit tick now
// reviews existed to protect the AUTHOR from a review they did not ask to
// receive — a draft, and a PR this user already approved — and neither reason
// survives a human asking for one deliberately.
//
// The check is per-cell rather than per-phrase because the property is
// structural: each state row must carry the right action in the right column,
// and prose elsewhere can claim the split while the table contradicts it. The
// watch column is asserted too, in the same pass — it is what ADR-0025 §3 keeps
// unchanged from ADR-0022, and a table edited to review on every tick regardless
// of mode would otherwise pass a check that only looked at the explicit side.
// The negative on the explicit column is the ADR's actual claim: those cells may
// not consult `requested:` at all.
//
// What this catches: any row's action changing in either column, a row
// disappearing or its state cells being reworded, an explicit cell growing a
// request condition, a cell naming two actions instead of exactly one, and —
// because the `approved` row and the anything-else row are the only two of the
// five whose state cells overlap ("anything else" subsumes `approved`) — those
// two being swapped. A state-cell match alone cannot see the swap, since both
// rows still exist afterward with the right action in the right column; only
// their relative order does, and first-match-wins makes that order the thing
// that actually decides which row a watch tick lands on.
// What this does NOT catch: an agent reading the right row and taking the wrong
// action, the prose around the table contradicting it in some wording this does
// not name, or a sixth row added below (the table is evaluated first-match-wins,
// so a row appended after these five is unreachable for the states they already
// match — but a row inserted ABOVE them is not, and nothing here notices it).
func TestPRReviewLoopExplicitRowsAreNotRequestGated(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	section := markdownSection(t, body, prReviewLoopDecisionHeading)

	// The generalization the rows are an instance of. Pinned as well as the cells
	// because it is what an agent applies to a state the table's rows do not
	// obviously cover, and a table-only revert and a prose-only revert are
	// separate edits.
	const rule = "An explicit tick reviews unless the pull request is over"
	if !strings.Contains(flowSection(t, body, prReviewLoopDecisionHeading), rule) {
		t.Errorf(
			"%s step 2 no longer says that %q. The table's cells are the mechanism; "+
				"this sentence is the rule they implement, and it is what tells an agent "+
				"that only merged and closed stop an explicit tick (ADR-0025 §2)",
			path, rule,
		)
	}

	rows := markdownTableRows(section)
	if len(rows) == 0 {
		t.Fatalf(
			"%s step 2 no longer has a decision table at all — the state-to-action "+
				"mapping is the whole of what this step does",
			path,
		)
	}

	// Column 3 is the explicit tick's action, column 4 the watch tick's, after
	// the three state columns (`pr:`, `requested:`, `my-review:`).
	const explicitCol, watchCol = 3, 4

	// Row indexes of the two overlapping rows, filled in as the loop below
	// finds them, and compared once the loop is done.
	approvedRowIndex, catchAllRowIndex := -1, -1
	for _, want := range []struct {
		state    [3]string
		explicit string
		watch    string
		why      string
	}{
		{
			state:    [3]string{"`merged`/`closed`", "any", "any"},
			explicit: "Terminal",
			watch:    "Terminal",
			why: "a review posted on a finished pull request is noise nobody can act " +
				"on, and this row is the ONLY thing that stops an explicit tick — lose " +
				"it and \"reviews unless the pull request is over\" has no over",
		},
		{
			state:    [3]string{"`draft`", "any", "any"},
			explicit: "Review",
			watch:    "Wait",
			why: "a draft is exactly when an author wants a private read of unfinished " +
				"work, so a human asking for one gets it; the watch still waits, so " +
				"nothing unattended lands on a draft",
		},
		{
			state:    [3]string{"`open`", "`yes`", "any"},
			explicit: "Review",
			watch:    "Review",
			why: "this is the one row both modes share — the requested review the watch " +
				"exists for. It is also read BEFORE the standing-approval row below, " +
				"which is what makes an author's re-request reach a PR this user already " +
				"approved",
		},
		{
			state:    [3]string{"`open`", "`no`", "`approved`"},
			explicit: "Review",
			watch:    "Terminal",
			why: "\"review it again\" after a rebase or a late doubt is a normal ask, and " +
				"answering it with a three-line refusal is the failure ADR-0025 exists to " +
				"remove; for the watch the standing approval is still the end of the loop",
		},
		{
			state:    [3]string{"`open`", "`no`", "anything else"},
			explicit: "Review",
			watch:    "Wait",
			why: "this is the row the 2026-08-12 failure landed on: the human typed the " +
				"command, and the tick answered that the ball was with the author",
		},
	} {
		var cells []string
		matchedIndex := -1
		for i, row := range rows {
			if len(row) > watchCol &&
				row[0] == want.state[0] && row[1] == want.state[1] && row[2] == want.state[2] {
				cells = row
				matchedIndex = i
				break
			}
		}
		if cells == nil {
			t.Errorf(
				"%s step 2's table has no row for pr=%q requested=%q my-review=%q. The "+
					"rows are matched here by their state cells, so a reworded state is a "+
					"test change in the same commit — but a DELETED row leaves that state "+
					"with no action at all in either mode",
				path, want.state[0], want.state[1], want.state[2],
			)
			continue
		}

		switch want.state[2] {
		case "`approved`":
			approvedRowIndex = matchedIndex
		case "anything else":
			catchAllRowIndex = matchedIndex
		}

		if !strings.Contains(cells[explicitCol], want.explicit) {
			t.Errorf(
				"%s step 2's row for pr=%q requested=%q my-review=%q no longer gives an "+
					"EXPLICIT tick %q — it now reads %q (ADR-0025 §1). Why the row is what it "+
					"is: %s",
				path, want.state[0], want.state[1], want.state[2],
				want.explicit, cells[explicitCol], want.why,
			)
		}
		if !strings.Contains(cells[watchCol], want.watch) {
			t.Errorf(
				"%s step 2's row for pr=%q requested=%q my-review=%q no longer gives a "+
					"WATCH tick %q — it now reads %q. ADR-0025 §3 keeps the watch column "+
					"exactly as ADR-0022 decided it, because GitHub's request field is still "+
					"the only memory an unattended loop has: a watch that stops taking these "+
					"rows reposts on every tick. Why the row is what it is: %s",
				path, want.state[0], want.state[1], want.state[2],
				want.watch, cells[watchCol], want.why,
			)
		}

		// Exactly one action per cell, not merely the wanted one present: a cell
		// carrying two actions ("**Review** — steps 3 to 9, but **Wait** if …")
		// would pass the two Contains checks above while leaving the actual
		// choice between them to whichever agent reads it.
		for _, other := range []string{"Review", "Wait", "Terminal"} {
			if other != want.explicit && strings.Contains(cells[explicitCol], other) {
				t.Errorf(
					"%s step 2's row for pr=%q requested=%q my-review=%q's EXPLICIT cell "+
						"names %q in addition to its %q action (cell reads %q). A row is "+
						"supposed to pick exactly one action; a cell naming two leaves the "+
						"choice between them to whichever agent reads it",
					path, want.state[0], want.state[1], want.state[2],
					other, want.explicit, cells[explicitCol],
				)
			}
			if other != want.watch && strings.Contains(cells[watchCol], other) {
				t.Errorf(
					"%s step 2's row for pr=%q requested=%q my-review=%q's WATCH cell "+
						"names %q in addition to its %q action (cell reads %q). A row is "+
						"supposed to pick exactly one action; a cell naming two leaves the "+
						"choice between them to whichever agent reads it",
					path, want.state[0], want.state[1], want.state[2],
					other, want.watch, cells[watchCol],
				)
			}
		}

		// This fires on any occurrence of "request" in the explicit cell, not
		// just a condition on `requested:` — a cell reading "**Review** — steps
		// 3 to 9, request button or not" would trip it too. Tighten the cell's
		// wording rather than weakening this check.
		if strings.Contains(cells[explicitCol], "request") {
			t.Errorf(
				"%s step 2's row for pr=%q requested=%q my-review=%q makes the EXPLICIT "+
					"tick's action depend on a request (%q). A human typing the command IS "+
					"the request — addressed to this tool, by the person running it, about "+
					"the PR they named — so gating the explicit column on GitHub's field "+
					"makes this file ask for a permission it was just handed (ADR-0025 §1)",
				path, want.state[0], want.state[1], want.state[2], cells[explicitCol],
			)
		}
	}

	// The `approved` row and the anything-else row are the only two of the five
	// whose state cells overlap: "anything else" matches every `my-review:`
	// value the approved row already claimed, so which one is checked FIRST is
	// the only thing that tells them apart (ADR-0025 §1's first-match-wins).
	// Both rows existing with the right action in the right column — which is
	// all the checks above can see — is not enough: swap their order in the
	// file and every check above still passes, while a watch tick on a PR this
	// user already approved now matches the catch-all row first and waits
	// instead of going terminal.
	if approvedRowIndex < 0 || catchAllRowIndex < 0 {
		t.Fatalf(
			"%s step 2's table is missing the `approved` row, the anything-else "+
				"row, or both — the order check below has nothing to compare",
			path,
		)
	}
	if approvedRowIndex >= catchAllRowIndex {
		t.Errorf(
			"%s step 2's table has the `open`/`requested: no`/`approved` row (row "+
				"index %d) at or after the `open`/`requested: no`/anything-else row "+
				"(row index %d). Rows are evaluated first-match-wins (ADR-0025 §1), "+
				"and \"anything else\" subsumes `approved`, so with this order a watch "+
				"tick on a PR this user already approved now matches the catch-all "+
				"row first and WAITS instead of going terminal — ADR-0025 §3's \"a "+
				"standing approval is terminal\" is gone, and the driver §4 promised "+
				"would stop at an approval never stops: it ticks forever on a pull "+
				"request that is already done. Move the `approved` row back above "+
				"the anything-else row",
			path, approvedRowIndex, catchAllRowIndex,
		)
	}
}

// TestPRReviewLoopPrePostGateIsModeAware pins step 7, which ADR-0025 §6 calls
// the place the whole change would die silently. The gate re-reads the state
// after the reviewer runs, and while every tick was request-gated it could
// simply demand the Review row again. Left that way, an explicit tick does all
// the work — fetches the refs, runs every reviewer over every model, spends the
// minutes — reaches a gate demanding `requested: yes`, and posts nothing. No
// error, no missing section, nothing in the report that reads as a bug.
//
// So the gate has to carry both modes, and the two branches are asserted from
// their own slices of the section rather than from the section as a whole:
// `requested: yes` is REQUIRED in the watch branch and must not appear in the
// explicit one, which a whole-section check could not tell apart.
//
// The gate's other condition — head unchanged since the reviewers read it — is
// mode-independent and is not this guard's clause; ADR-0023 and step 7's own
// prose carry it.
//
// What this catches: either branch of the mode split disappearing, the explicit
// branch losing what it now requires instead (neither merged nor closed), the
// clause that says an absent request cannot cancel an explicit post, the
// request requirement migrating into the explicit branch directly
// (`requested: yes`), and the same migration done additively instead — a
// rewrite that keeps both pinned explicit phrases but also requires the fresh
// state to land on the watch branch's Review row, re-gating the explicit tick
// under a different name.
// What this does NOT catch: an agent applying the wrong branch, and — because
// the negative check fires on any mention of `requested: yes` in the explicit
// branch — a future rewrite that names the flag only to disclaim it will trip
// this too. The file's own wording for that disclaimer is the phrase pinned
// below; keep using it rather than weakening the check. The "Review row" check
// has the same shape: an explicit branch that mentions the Review row only to
// contrast it (as in "unlike the watch's Review row, …") trips it too. That
// check is also case-sensitive — a rewrite that lowercases it to "review row"
// evades it silently.
func TestPRReviewLoopPrePostGateIsModeAware(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	gate := flowSection(t, body, prReviewLoopPrePostHeading)

	const (
		explicitMarker = "**Explicit tick:**"
		watchMarker    = "**Watch tick (`--on-request`):**"
		// The paragraph shared by both branches, immediately after whichever
		// sub-bullet comes second. Bounding the second branch here — not just
		// at the other branch's marker — keeps the slicing symmetric: without
		// it, whichever branch comes second in the file runs into "When the
		// condition fails", a paragraph that is mode-independent and follows
		// BOTH sub-bullets, so a required substring could be satisfied by that
		// shared tail instead of by the branch it is supposed to pin.
		tailMarker = "When the condition fails"
	)
	explicitAt := strings.Index(gate, explicitMarker)
	watchAt := strings.Index(gate, watchMarker)
	if explicitAt < 0 || watchAt < 0 {
		t.Fatalf(
			"%s step 7's pre-post gate no longer splits by mode: it is missing %q "+
				"(found=%t) or %q (found=%t). One gate for both modes is where ADR-0025 "+
				"dies silently — an explicit tick runs the whole cross-model review, then "+
				"fails a gate that wants a review request, and posts nothing (§6)",
			path, explicitMarker, explicitAt >= 0, watchMarker, watchAt >= 0,
		)
	}
	// Slice each branch off at the other's marker, in whichever order they appear
	// — their order is not a property this pins.
	explicitBranch, watchBranch := gate[explicitAt:], gate[watchAt:]
	secondAt := explicitAt
	if watchAt > explicitAt {
		explicitBranch = gate[explicitAt:watchAt]
		secondAt = watchAt
	} else {
		watchBranch = gate[watchAt:explicitAt]
	}
	// Bound whichever branch runs second at the shared tail paragraph instead,
	// so neither branch's required substring can be satisfied by prose that
	// applies to both modes. The anchor is required, not optional: a rewrite
	// that drops or relocates it must fail this test loudly rather than widen
	// the slice back to the rest of the section silently, and the ordering
	// check (tailAt > secondAt) stops a future edit that moves this paragraph
	// ABOVE the mode branches from turning the slice below into a panic.
	tailAt := strings.Index(gate, tailMarker)
	if tailAt < 0 || tailAt <= secondAt {
		t.Fatalf(
			"%s step 7's pre-post gate no longer has %q positioned after both mode "+
				"sub-bullets (found at byte %d; explicit branch starts at %d, watch "+
				"branch starts at %d — want the anchor after whichever of those is "+
				"larger). This guard needs that anchor to stop the second branch's "+
				"slice before the paragraph shared by both modes; without it, either "+
				"the slice runs to the end of the section and a required substring "+
				"could be satisfied by that shared tail instead of by the branch it "+
				"pins, or the anchor moved above the branches and slicing on it would "+
				"panic",
			path, tailMarker, tailAt, explicitAt, watchAt,
		)
	}
	if watchAt > explicitAt {
		watchBranch = gate[watchAt:tailAt]
	} else {
		explicitBranch = gate[explicitAt:tailAt]
	}

	for _, req := range []struct {
		branch string
		substr string
		why    string
	}{
		{
			branch: explicitBranch,
			substr: "neither `merged` nor `closed`",
			why: "this is what the explicit branch requires INSTEAD of a review request, " +
				"so without it the branch names a mode and no condition — and the tick " +
				"either posts onto a pull request that closed mid-review or falls back to " +
				"the watch's requirement",
		},
		{
			branch: explicitBranch,
			substr: "`requested: no` cannot",
			why: "nothing then states that an absent review request cannot cancel an " +
				"explicit post, which is the exact re-gating ADR-0025 §6 warns about and " +
				"the one that shows up as a silent no-op rather than an error",
		},
		{
			branch: watchBranch,
			substr: "`requested: yes`",
			why: "the watch branch stops requiring the request, so an unattended tick " +
				"posts again on a PR whose request someone else already answered — the " +
				"dedup ADR-0022 rests on and ADR-0025 §3 keeps",
		},
	} {
		if !strings.Contains(req.branch, req.substr) {
			t.Errorf(
				"%s step 7's pre-post gate is missing %q from the branch that needs it: %s",
				path, req.substr, req.why,
			)
		}
	}

	if strings.Contains(explicitBranch, "`requested: yes`") {
		t.Errorf(
			"%s step 7's EXPLICIT branch names `requested: yes`. On an explicit tick "+
				"the request was usually never `yes` — the human's own invocation is the "+
				"request — so a gate that asks for it here runs every reviewer over every "+
				"model and then posts nothing at all (ADR-0025 §6). The branch reads: %q",
			path, strings.TrimSpace(explicitBranch),
		)
	}

	// The additive form of the same re-gating: a rewrite can keep both phrases
	// pinned above word-for-word and still require the fresh state to land on
	// the watch branch's "Review row" beside them, which re-gates the explicit
	// post on `requested: yes` under a name this guard's other checks do not
	// name.
	if strings.Contains(explicitBranch, "Review row") {
		t.Errorf(
			"%s step 7's EXPLICIT branch mentions the \"Review row\" — that is the "+
				"watch branch's own gate (\"the state must still land on the Review "+
				"row\"), and requiring it in the explicit branch too re-gates the post "+
				"on `requested: yes` under a different name. This guard's other checks "+
				"can stay word-for-word correct while this is added beside them, and "+
				"the tick still runs the whole cross-model review and then silently "+
				"posts nothing (ADR-0025 §6). The branch reads: %q",
			path, strings.TrimSpace(explicitBranch),
		)
	}
}

// TestPRReviewLoopParsesBothReviewerSpellingsAndOnce pins the flag spellings the
// explicit/watch split is driven by, in the two places a flag has to appear to
// have any effect: the usage line a human copies from, and step 0, the only step
// that reads $ARGUMENTS.
//
// `--reviewer` is here because of a failure that already happened.
// `/pr-review-loop --reviewer=document <n>` selected no reviewer on 2026-08-12:
// step 0 read bare words only, `--reviewer <type>` is the sibling `/review-loop`
// command's spelling, and a human moving between the two typed the sibling's
// flag — which resolved to nothing, silently (ADR-0025 Context). Both spellings
// are one vocabulary, not a translation layer: the value still reaches
// `devgeta task review-run --reviewer` verbatim, which is what
// TestPRReviewLoopForwardsReviewerTypes pins.
//
// `--once` has no such history. It is this cycle's own addition and it is
// asserted for the structural reason: it is the only way to ask for the single
// look a bare invocation used to be, and a flag documented but not parsed does
// nothing while reading as if it works. Its parse-step assertion is the sentence
// that classifies it — a flag, not a value — because that is what stops `--once`
// being read as a reviewer type and rejected as an unknown one.
//
// What this catches: either `--reviewer` spelling or `--once` leaving the usage
// line or step 0.
// What this does NOT catch: step 0 naming a flag and then doing nothing with it,
// or the two spellings drifting to mean different things — prose can say either
// and this is a substring check.
func TestPRReviewLoopParsesBothReviewerSpellingsAndOnce(t *testing.T) {
	path, body := readSharedCommand(t, "pr-review-loop.md")

	// The synopsis line itself, not the Usage prose around it: it is what a human
	// copies, and it is the one line in the file that has to list every flag.
	// This reads the section uncollapsed and splits on "\n", so it assumes the
	// synopsis stays one physical line inside its fence — a fence is never
	// hand-rewrapped the way the prose sections are, so nothing here tolerates
	// it being split. If a future edit ever wraps it, this extraction needs
	// flowSection's whitespace-collapsing to keep finding it.
	usage := markdownSection(t, body, prReviewLoopUsageHeading)
	synopsis := ""
	for _, line := range strings.Split(usage, "\n") {
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "/pr-review-loop") {
			synopsis = line
			break
		}
	}
	if synopsis == "" {
		t.Fatalf(
			"%s's Usage section no longer opens with a `/pr-review-loop …` synopsis "+
				"line — that line is the command's whole documented shape",
			path,
		)
	}
	for _, req := range []struct {
		flag string
		why  string
	}{
		{
			flag: "--reviewer <type>",
			why: "this is the spelling the sibling `/review-loop` takes, so it is the one " +
				"a human moving between the two types; unlisted here it looks like it was " +
				"never accepted",
		},
		{
			flag: "--once",
			why: "nothing then documents how to ask for a single look, and since any " +
				"other explicit invocation starts a standing watch, the human is left " +
				"with a driver they never asked for",
		},
	} {
		if !strings.Contains(synopsis, req.flag) {
			t.Errorf(
				"%s's usage line (`%s`) no longer lists %s — %s",
				path, synopsis, req.flag, req.why,
			)
		}
	}

	const equalsSpelling = "--reviewer=<type>"
	if !strings.Contains(flowSection(t, body, prReviewLoopUsageHeading), equalsSpelling) {
		t.Errorf(
			"%s's Usage section no longer documents the %s spelling. The synopsis line "+
				"shows the space form only, so this is the sole place a reader learns the "+
				"`=` form is accepted — and it is the exact form that silently selected no "+
				"reviewer on 2026-08-12",
			path, equalsSpelling,
		)
	}

	parse := flowSection(t, body, prReviewLoopParseHeading)
	for _, req := range []struct {
		substr string
		why    string
	}{
		{
			substr: "`--reviewer <type>` or `--reviewer=<type>`",
			why: "step 0 is the only place $ARGUMENTS is read, so a spelling missing " +
				"here is a spelling Usage advertises and nothing resolves — which is the " +
				"2026-08-12 failure exactly: the flag was typed and the tick reviewed " +
				"nothing. Both forms are named in one clause on purpose; a step 0 that " +
				"parses only one of them accepts half of what Usage documents",
		},
		{
			substr: "`--once` and `--on-request` are flags, not values",
			why: "these two are the only arguments that are neither a PR number nor a " +
				"reviewer type. Unclassified, `--once` falls through to the type check " +
				"and stops the tick as an unknown value — and `--on-request` with it, " +
				"which breaks every tick a driver fires",
		},
	} {
		if !strings.Contains(parse, req.substr) {
			t.Errorf("%s step 0 is missing %q — %s", path, req.substr, req.why)
		}
	}
}

// ---------------------------------------------------------------------------
// The posting commands' own --target contracts.
//
// /pr-review-loop hands a PR that is not the checkout to /review-pr and
// /approve-pr. Both default to the checkout — for the journal, for the PR
// number — and a launch prompt cannot override that on its own: it contradicts
// the command file's own written instructions, and the file wins. That is the
// same lesson TestReviewerAgentsScopeTheJournalWhenTargeted pins for the
// reviewer agents, and it had to be learned twice.
// ---------------------------------------------------------------------------

// TestPostingCommandsScopeTheJournalWhenTargeted is the /review-pr analog of
// TestReviewerAgentsScopeTheJournalWhenTargeted, for the same defect one file
// downstream. /review-pr settles a journal entry whenever it drops a finding as
// genuinely handled. Unscoped, that settle goes to the checked-out branch's
// journal, and because ids are per-journal and sequential, `--settle --id n4`
// closes whatever n4 that branch already has — a real, unrelated open finding
// marked answered with a note about someone else's PR. Run from a default-branch
// checkout, which reviewing another person's PR normally means, it also creates
// the `main` journal ADR-0018 exists to prevent.
//
// approve-pr.md is asserted from the other side: it must stay journal-free. It
// never reads or writes the journal, so a scoping rule there would be a rule
// for a command that does not exist — and if it ever gains a journal call, this
// test fails and says which clause it now needs.
//
// What this catches: the --target section losing the scoping rule, the flags
// being paraphrased into something an agent cannot copy, the fenced settle
// dropping its scoped form, and approve-pr.md growing an unscoped journal call.
// What this does NOT catch: an agent reading a correct rule and settling
// unscoped anyway; substring checks over prose cannot execute it.
func TestPostingCommandsScopeTheJournalWhenTargeted(t *testing.T) {
	// The flag pair, quoted as review-pr.md renders it, so a paraphrase an agent
	// could not copy verbatim fails here.
	const scopedFlags = "`--branch <key> --rev <head-sha>`"

	path, body := readSharedCommand(t, "review-pr.md")
	target := flowSection(t, body, "### `--target <head-sha>`")

	required := []struct {
		substr string
		why    string
	}{
		{
			substr: "--journal <key>",
			why: "nothing names the flag that carries the key, so a caller reviewing " +
				"another branch's PR has no way to say which journal is the PR's",
		},
		{
			substr: scopedFlags,
			why:    "the rule never says which flags carry the key and the revision",
		},
		{
			substr: "every `review-notes` and `review-note` command in this file",
			why: "the rule does not plainly reach every journal call, so some are " +
				"scoped and some are not and the round's record splits across two " +
				"journals",
		},
		{
			substr: "run every journal command exactly as written",
			why: "the ordinary run — a PR checked out here, no key passed — is left " +
				"ambiguous, which invites inventing a key that was never given",
		},
	}
	for _, req := range required {
		if !strings.Contains(target, req.substr) {
			t.Errorf(
				"%s's `--target` section is missing %q — %s",
				path, req.substr, req.why,
			)
		}
	}

	// The settle sits ~60 lines below the rule, in step 3. Restating the scope
	// where the command actually is is what stops a run from reading the rule and
	// then settling unscoped anyway — the same reason the reviewer agents repeat
	// it in their recording section.
	dedup := flowSection(t, body, "### 3. Fetch existing threads and dedup")
	if !strings.Contains(dedup, "--branch <key> --rev <head-sha>") {
		t.Errorf(
			"%s step 3's settle no longer shows its scoped form (`--branch <key> --rev "+
				"<head-sha>`). This is the only journal WRITE in the file, and the one "+
				"that corrupts another branch's record: ids are per-journal and "+
				"sequential, so an unscoped `--settle --id n4` closes that branch's n4",
			path,
		)
	}

	// The other half of the contract: approve-pr.md is journal-free, which is why
	// the loop passes it no key. If that ever stops being true, the scoping rule
	// has to arrive with the first journal call, not after it.
	approvePath, approveBody := readSharedCommand(t, "approve-pr.md")
	for _, call := range []string{"review-notes", "review-note "} {
		if strings.Contains(approveBody, call) {
			t.Errorf(
				"%s now runs `devgeta task %s`, so it touches the review journal. "+
					"/pr-review-loop passes it no journal key precisely because it did "+
					"not, so that call resolves the journal from the checked-out branch — "+
					"which in --target mode is not this PR. Give this file the same "+
					"scoping rule review-pr.md's `--target` section carries, and pass the "+
					"key from the loop's step 8",
				approvePath, strings.TrimSpace(call),
			)
		}
	}
}

// TestApprovePRSubmitNamesThePRWhenTargeted guards the second half of the same
// defect class: a command that defaults to the checkout, invoked from a checkout
// that is not the PR. `devgeta task approve-pr` with no --pr resolves the PR
// from the current branch, and that does not error — it approves whatever PR the
// checkout branch has open. A real, misdirected approval on someone else's PR.
//
// The rule cannot live only at step 1, ~80 lines above the submit: it was there
// all along, and the submit still omitted the flag. It has to be at the submit,
// the way review-pr.md's already is.
//
// What this catches: the --target section or the fenced approve losing the
// restatement.
// What this does NOT catch: an agent omitting the flag despite reading the rule.
func TestApprovePRSubmitNamesThePRWhenTargeted(t *testing.T) {
	path, body := readSharedCommand(t, "approve-pr.md")

	target := flowSection(t, body, "### `--target <head-sha>`")
	if !strings.Contains(target, "--pr <n>") {
		t.Errorf(
			"%s's `--target` section never says the submit must name the PR (`--pr "+
				"<n>`). With --target the checkout is not this PR, so an omitted number "+
				"does not fail — `devgeta task approve-pr` resolves the checkout "+
				"branch's own PR and approves that one instead",
			path,
		)
	}

	decide := flowSection(t, body, "### 4. Decide")
	if !strings.Contains(decide, "--pr PR_NUMBER") {
		t.Errorf(
			"%s step 4 no longer restates `--pr PR_NUMBER` at the approve submit. "+
				"Step 1 saying to pass it is not enough — it is ~80 lines above the "+
				"fenced command, and review-pr.md restates it at its own submit for "+
				"exactly this reason",
			path,
		)
	}
}

// TestReviewPRSubmitNamesThePRWhenTargeted is the /review-pr half of the same
// defect approve-pr.md was already guarded against. `devgeta task submit-review`
// with no --pr resolves the PR from the current branch, and that does not error:
// under --target the checkout is a different PR entirely, so the review is
// composed correctly against the target commit and then posted, in full, on
// somebody else's pull request.
//
// The file's own opening line — "The PR is resolved from the current branch
// unless you pass a number" — is what makes an omitted number silently plausible,
// so the --target section has to override it explicitly. And the rule cannot live
// only there: it is ~200 lines above the submit, which is the same distance that
// let approve-pr.md's submit omit the flag with the rule already written.
//
// What this catches: the --target section losing the mandate, or step 6's submit
// losing the local restatement.
// What this does NOT catch: an agent omitting the flag despite reading both.
func TestReviewPRSubmitNamesThePRWhenTargeted(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	target := flowSection(t, body, "### `--target <head-sha>`")
	for _, req := range []struct {
		substr string
		why    string
	}{
		{
			substr: "--pr <n>",
			why: "nothing names the flag that carries the PR number, so the review " +
				"is posted to whichever PR the checkout branch has open",
		},
		{
			substr: "mandatory",
			why: "the number reads as optional, which is exactly what step 1's " +
				"branch inference then supplies — a wrong number, without an error",
		},
	} {
		if !strings.Contains(target, req.substr) {
			t.Errorf(
				"%s's `--target` section is missing %q — %s",
				path, req.substr, req.why,
			)
		}
	}

	submit := flowSection(t, body, "### 6. Submit one review")
	if !strings.Contains(submit, "not optional") {
		t.Errorf(
			"%s step 6 no longer restates at the submit that `--pr PR_NUMBER` is "+
				"not optional under --target. \"Add it when you resolved a number in "+
				"step 1\" is a conditional, and under --target there is no condition: "+
				"the checkout is not this PR",
			path,
		)
	}
}

// assertReadsNameThePR checks that every `devgeta task <cmd>` line inside a
// section carries a --pr flag. The two targeted-read guards below both need it,
// and a section can hold the same command twice (approve-pr.md reads threads
// once unresolved and once resolved), so every occurrence is checked rather
// than the first one found.
func assertReadsNameThePR(t *testing.T, path, section, cmd, why string) {
	t.Helper()

	found := false
	for _, line := range strings.Split(section, "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "devgeta task "+cmd) {
			continue
		}
		found = true
		if !strings.Contains(trimmed, "--pr") {
			t.Errorf("%s runs `%s` with no --pr in sight. %s", path, trimmed, why)
		}
	}
	if !found {
		t.Errorf(
			"%s no longer runs `devgeta task %s` where this guard looks for it — "+
				"if the read moved, move the guard with it rather than dropping it",
			path, cmd,
		)
	}
}

// TestReviewPRReadsNameThePRWhenTargeted extends the submit-side guard above to
// the reads that feed it. Pinning only the post left the same defect alive one
// step earlier: under --target, a `pr-view` or `review-threads` with no --pr
// resolves the checkout branch's own PR, so the review is composed from another
// PR's description and deduplicated against another PR's threads. That is worse
// than a misdirected post, because it is silent — the review still lands on the
// right PR, carrying findings this PR already addressed and missing ones an
// unrelated thread happened to look like.
//
// What this catches: either read losing its --pr treatment, or the --target
// section going back to naming only the post.
// What this does NOT catch: an agent omitting the flag despite reading the rule.
func TestReviewPRReadsNameThePRWhenTargeted(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	target := flowSection(t, body, "### `--target <head-sha>`")
	for _, cmd := range []string{"pr-view", "review-threads"} {
		if !strings.Contains(target, cmd) {
			t.Errorf(
				"%s's `--target` section never names `%s` among the calls that must "+
					"carry --pr. Listing only the post is what let the reads keep "+
					"resolving the checkout branch's PR",
				path, cmd,
			)
		}
	}

	for _, sec := range []struct {
		heading string
		cmd     string
		why     string
	}{
		{
			heading: "### 2. Load context",
			cmd:     "pr-view",
			why: "under --target that reads the checkout branch's PR, so the review " +
				"is written against another PR's purpose and description",
		},
		{
			heading: "### 3. Fetch existing threads and dedup",
			cmd:     "review-threads",
			why: "under --target that dedups against another PR's threads — already " +
				"addressed findings get raised again, and an unrelated thread can " +
				"look like a duplicate and drop a live one",
		},
	} {
		assertReadsNameThePR(t, path, markdownSection(t, body, sec.heading), sec.cmd, sec.why)

		if !strings.Contains(flowSection(t, body, sec.heading), "not optional") {
			t.Errorf(
				"%s %q no longer says the flag is not optional under --target. "+
					"\"Add it if you have one\" reads as a convenience, which is exactly "+
					"how these reads kept defaulting to the checkout's PR",
				path, sec.heading,
			)
		}
	}
}

// TestApprovePRReadsNameThePRWhenTargeted is the same guard on the approval
// side, where the stakes are higher: every gate in step 3 is a read. With no
// --pr under --target, the threads, the resolutions, and the CI status all come
// from the checkout branch's PR, so the approval is decided on evidence
// belonging to a different pull request and then posted on this one.
//
// What this catches: any of the four gate reads losing its --pr treatment, or
// the --target section going back to naming only the post.
// What this does NOT catch: an agent omitting the flag despite reading the rule.
func TestApprovePRReadsNameThePRWhenTargeted(t *testing.T) {
	path, body := readSharedCommand(t, "approve-pr.md")

	target := flowSection(t, body, "### `--target <head-sha>`")
	for _, cmd := range []string{"pr-view", "review-threads", "pr-checks"} {
		if !strings.Contains(target, cmd) {
			t.Errorf(
				"%s's `--target` section never names `%s` among the calls that must "+
					"carry --pr. Every gate in step 3 is a read, so naming only the "+
					"approve leaves the decision itself resolving the wrong PR",
				path, cmd,
			)
		}
	}

	assertReadsNameThePR(t, path,
		markdownSection(t, body, "### 2. Confirm it's reviewable"), "pr-view",
		"under --target that reports whether some other PR is open and already "+
			"reviewed, which is the precondition this whole command rests on")

	gates := markdownSection(t, body, "### 3. Check the gates")
	assertReadsNameThePR(t, path, gates, "review-threads",
		"under --target the gates are checked against another PR's threads — and "+
			"\"No unresolved review threads.\" from the wrong PR reads exactly like "+
			"a clean bill of health for this one")
	assertReadsNameThePR(t, path, gates, "pr-checks",
		"under --target that reports another PR's CI, so a red check here is "+
			"invisible and a green one there is reported as this PR's")

	for _, heading := range []string{"### 2. Confirm it's reviewable", "### 3. Check the gates"} {
		if !strings.Contains(flowSection(t, body, heading), "not optional") {
			t.Errorf(
				"%s %q no longer says the flag is not optional under --target — the "+
					"same weak \"add it if you have one\" wording the submit already "+
					"had to override",
				path, heading,
			)
		}
	}
}

// TestReviewPRRereviewBlockerSubmitsAReviewNotAComment guards a mismatch that
// was live in the file: step 6 defines case 1 (a live blocker someone else
// already raised) as a review submitted with `--event comment`, while the
// re-review bullet at the end of the same step pointed at that case and named
// `comment-pr` instead.
//
// Those are not two spellings of one action. `devgeta task comment-pr` runs
// `gh pr comment` — its own doc comment in githubcli.go calls it "distinct from
// a review" — so it posts no review at all. ADR-0022 rests on the opposite:
// submitting any review, comment-only included, removes the user from the PR's
// reviewRequests, and that removal is the entire de-trigger for
// /pr-review-loop. Follow the bullet instead of the case definition it cites
// and the request never clears, so the next tick reads `requested: yes` again
// and posts the same note — indefinitely.
//
// What this catches: the bullet reverting to comment-pr, or losing the reason
// it must not.
// What this does NOT catch: an agent reading the bullet and reaching for
// comment-pr anyway.
func TestReviewPRRereviewBlockerSubmitsAReviewNotAComment(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	rereview := flowSection(t, body, "**Re-review with nothing new to add**")

	if !strings.Contains(rereview, "submit-review --event comment") {
		t.Errorf(
			"%s's re-review section no longer posts the still-live blocker as "+
				"`devgeta task submit-review --event comment`. Step 6 defines that "+
				"case as a review with event comment, so anything else contradicts "+
				"the definition the bullet points back to",
			path,
		)
	}

	if !strings.Contains(rereview, "not a review") {
		t.Errorf(
			"%s's re-review section no longer says why a top-level comment is the "+
				"wrong tool here — that it is not a review, so it leaves the review "+
				"request pending. Without the reason, `comment-pr` reads like a "+
				"lighter-weight equivalent and the swap comes back",
			path,
		)
	}
}

// ---------------------------------------------------------------------------
// Dedup suppresses duplicate comments. It never moves the verdict.
//
// A real review approved a PR whose route coverage was genuinely missing: the
// finding was deduplicated against an existing Copilot comment that raised the
// same point, and "already raised" was then read as "not blocking". Both
// halves of that are wrong, and each is guarded below — the drop must not
// change the verdict (review-pr step 6, approve-pr step 4), and it must not
// close the journal entry that tracks the concern (review-pr step 3,
// review-loop step 4).
// ---------------------------------------------------------------------------

// TestReviewPRDedupDoesNotDecideTheVerdict pins the separation itself in the
// two places it can be lost: the dedup step, where a finding is dropped, and
// the verdict step, which must weigh the dropped findings anyway.
//
// What this catches: the verdict step losing its "judge on the dropped ones
// too" instruction, or the dedup step losing the rule that a still-live
// duplicate is not settled in the journal — either one restores the approval
// bug on its own.
// What this does NOT catch: an agent dropping a finding and then forgetting it
// despite the instruction. A substring check over prose cannot execute the
// instructions (see TestCommittingCommandsDeclareStandingAuthorization).
func TestReviewPRDedupDoesNotDecideTheVerdict(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	dedup := strings.ToLower(markdownSection(t, body, "### 3. Fetch existing threads and dedup"))
	if !strings.Contains(dedup, "never decides the verdict") {
		t.Errorf(
			"%s step 3 no longer says dedup decides what you post but never the "+
				"verdict — without that sentence, dropping a duplicate finding "+
				"silently removes a blocker from the decision, which is the bug "+
				"this rule exists to prevent",
			path,
		)
	}
	if !strings.Contains(dedup, "never settle an entry") {
		t.Errorf(
			"%s step 3 no longer forbids settling the journal entry of a finding "+
				"dropped only because someone else raised the same still-live "+
				"point. Settling it `answered` tells the next review the concern is "+
				"gone, so the blocker disappears from the only record tracking it",
			path,
		)
	}

	verdict := strings.ToLower(markdownSection(t, body, "### 6. Submit one review"))
	if !strings.Contains(verdict, "dedup dropped") {
		t.Errorf(
			"%s step 6 no longer requires the verdict to be judged on the findings "+
				"dedup dropped as well as the ones posted — the drop list is exactly "+
				"where a live blocker hides",
			path,
		)
	}
}

// TestReviewPRNamesTheThreeVerdictCases keeps the three decided cases spelled
// out where the verdict is picked, rather than left to inference from the
// severity table. They are the cases that actually recur on a reviewed PR, and
// the first one is the one that was getting decided wrongly.
//
// What this catches: any of the three cases being dropped from step 6, or case
// 1 being softened from "do not approve" into an approval.
// What this does NOT catch: the cases being present but misapplied to a real
// PR — that is judgment, which prose states and no test here executes.
func TestReviewPRNamesTheThreeVerdictCases(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	verdict := strings.ToLower(markdownSection(t, body, "### 6. Submit one review"))

	if !strings.Contains(verdict, "do not approve") {
		t.Errorf(
			"%s step 6 no longer states that a live blocker someone else already "+
				"raised means NOT approving. Whoever raised it, a blocker that is "+
				"still in the code blocks",
			path,
		)
	}
	if !strings.Contains(verdict, "i'll approve once") {
		t.Errorf(
			"%s step 6 no longer tells the reviewer to say they will approve once "+
				"the outstanding item is addressed. That clause is what makes the "+
				"withheld approval actionable instead of a silent non-verdict",
			path,
		)
	}
	if !strings.Contains(verdict, "lgwc") {
		t.Errorf(
			"%s step 6 no longer names LGWC — approving while a non-blocking "+
				"comment from someone else still stands is one of the three cases "+
				"this step has to decide",
			path,
		)
	}
	if !strings.Contains(verdict, "pr-checks") {
		t.Errorf(
			"%s step 6 no longer says where failing CI is judged. This command "+
				"does not fetch check status, so it must point at /approve-pr "+
				"instead of leaving a red check to decide the verdict here",
			path,
		)
	}
}

// TestReviewPRSubmitCommandLeavesTheVerdictOpen keeps the one runnable submit
// command in step 6 free of a literal verdict. The three cases above it decide
// `--event`, and case 1 requires `comment` for a blocker someone else already
// raised — so a copyable command reading `--event request-changes` hands the
// agent the exact stacked request-changes that case told it not to post, and
// it is the last thing in the step a reader sees before acting.
//
// What this catches: the example regaining a hard-coded `--event` value, so
// the command a reader copies contradicts the case they just read.
// What this does NOT catch: the agent substituting the wrong verdict into the
// placeholder — which verdict fits the PR is judgment, not a string.
func TestReviewPRSubmitCommandLeavesTheVerdictOpen(t *testing.T) {
	path, body := readSharedCommand(t, "review-pr.md")

	verdict := markdownSection(t, body, "### 6. Submit one review")

	const marker = "devgeta task submit-review"
	start := strings.Index(verdict, marker)
	if start < 0 {
		t.Fatalf(
			"%s step 6 no longer shows a %q command — this test guards the "+
				"verdict that command carries",
			path, marker,
		)
	}
	command := verdict[start:]
	if end := strings.Index(command, "```"); end >= 0 {
		command = command[:end]
	}

	for _, event := range []string{"approve", "request-changes", "comment"} {
		if strings.Contains(command, "--event "+event) {
			t.Errorf(
				"%s step 6 hard-codes `--event %s` in the submit command. The "+
					"verdict is decided by the three cases above it — case 1 posts "+
					"`comment` for a blocker someone else already raised — so a "+
					"literal value here is copied over the case that was just read",
				path, event,
			)
		}
	}
	if !strings.Contains(command, "--event <") {
		t.Errorf(
			"%s step 6's submit command no longer leaves `--event` as a "+
				"placeholder to substitute. Without one, the reader has nothing "+
				"marking the verdict as theirs to pick",
			path,
		)
	}
}

// TestApprovePRVerdictIgnoresWhoRaisedIt guards the same separation at the
// approving end. /approve-pr never dedups, but it reaches the identical wrong
// answer by a shorter route: a thread it did not open, whose concern is a
// blocker, gets sorted as somebody else's comment and approved over.
//
// What this catches: the "who raised it" rule leaving the triage step, or any
// of the three decided cases leaving the decide step.
// What this does NOT catch: a blocker being triaged as non-blocking despite
// the rule — the severity call is judgment.
func TestApprovePRVerdictIgnoresWhoRaisedIt(t *testing.T) {
	path, body := readSharedCommand(t, "approve-pr.md")

	gates := strings.ToLower(markdownSection(t, body, "### 3. Check the gates"))
	if !strings.Contains(gates, "who raised it") {
		t.Errorf(
			"%s step 3 no longer says authorship stays out of the blocker/"+
				"non-blocking triage. 'Someone else already flagged this' is a "+
				"reason not to repeat a comment, never a reason to downgrade it",
			path,
		)
	}

	decide := strings.ToLower(markdownSection(t, body, "### 4. Decide"))
	if !strings.Contains(decide, "do not approve") {
		t.Errorf(
			"%s step 4 no longer states that an unresolved blocker raised by "+
				"someone else means NOT approving",
			path,
		)
	}
	if !strings.Contains(decide, "i'll approve once") {
		t.Errorf(
			"%s step 4 no longer carries the comment that names the outstanding "+
				"item and commits to approving once it is addressed — the author "+
				"otherwise cannot tell whether anything else is left",
			path,
		)
	}
	if !strings.Contains(decide, "lgwc") {
		t.Errorf(
			"%s step 4 no longer names LGWC for the two approve-anyway cases "+
				"(a live non-blocking comment, and a failing check)",
			path,
		)
	}
	if !strings.Contains(decide, "check") {
		t.Errorf(
			"%s step 4 no longer covers failing checks. A red check is flagged and "+
				"named, not treated as a gate — leaving it unstated invites the "+
				"opposite",
			path,
		)
	}
}

// TestReviewLoopNeverSettlesOnAlreadyRaised is the journal-side half. The loop
// posts no verdict, so the damage lands differently: a finding settled because
// it duplicates another one leaves `open:` empty, and step 3 then reads an
// all-APPROVE round as a clean approval while the code is still wrong.
//
// What this catches: the rule leaving step 4, or leaving the fix subagent's
// never-do list — the dispatch carries that list verbatim, so a rule missing
// there never reaches the agent doing the settling.
// What this does NOT catch: a subagent settling on those grounds anyway.
func TestReviewLoopNeverSettlesOnAlreadyRaised(t *testing.T) {
	path, body := readSharedCommand(t, "review-loop.md")

	triage := strings.ToLower(
		markdownSection(t, body, "### 4. Otherwise, triage each open finding"),
	)
	if !strings.Contains(triage, "already raised") {
		t.Errorf(
			"%s step 4 no longer says that a point being already raised is never "+
				"grounds for settling a finding. `fixed` needs a code change and "+
				"`rejected` needs disproving evidence; a duplicate is neither",
			path,
		)
	}

	dispatch := strings.ToLower(markdownSection(t, body, "### 6. The fix subagent"))
	if !strings.Contains(dispatch, "already known") {
		t.Errorf(
			"%s step 6's never-do list no longer forbids settling a finding "+
				"because it is already known. The dispatch carries that list to the "+
				"subagent, so a rule that is only in step 4 never reaches the agent "+
				"that does the settling",
			path,
		)
	}
}
