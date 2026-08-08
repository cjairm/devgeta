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

// reviewLoopFlowSection is reviewLoopSection with the section's whitespace
// collapsed to single spaces, so a phrase a guard test looks for still matches
// when the prose is rewrapped. The file is hand-wrapped at ~90 columns, and a
// phrase that happens to straddle a line break would otherwise read as deleted
// the next time a sentence above it grows by a word.
func reviewLoopFlowSection(t *testing.T, body, heading string) string {
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
	section := reviewLoopFlowSection(t, body, "### 3. Check for clean approval")

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
	section := reviewLoopFlowSection(
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
	section := reviewLoopFlowSection(t, body, "### 6. The fix subagent")

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

	fixSection := reviewLoopFlowSection(
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

	stopSection := reviewLoopFlowSection(
		t,
		body,
		"### 5. Stop for anything escalated, then enforce the round cap",
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

	settleSection := reviewLoopFlowSection(
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

	dispatchSection := reviewLoopFlowSection(t, body, "### 6. The fix subagent")
	if !strings.Contains(dispatchSection, "name the command and its result") {
		t.Errorf(
			"%s step 6's never-do list no longer requires the test command and its "+
				"result in the `--note` — that rule is what makes the fix verifiable "+
				"by whoever reads the journal later",
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
