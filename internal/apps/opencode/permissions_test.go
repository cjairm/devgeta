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
			"~/.claude/**":                   "deny",
			"~/.config/opencode/**":          "deny",
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
	agentDir := filepath.Join("..", "..", "..", "configs", "shared", "agents")
	entries, err := os.ReadDir(agentDir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", agentDir, err)
	}

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

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), "-reviewer.md") {
			continue
		}
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(agentDir, e.Name()))
			if err != nil {
				t.Fatalf("failed to read agent: %v", err)
			}
			body := string(data)
			for _, req := range required {
				if !strings.Contains(body, req.substr) {
					t.Errorf("missing %q — %s", req.substr, req.why)
				}
			}
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
// This must stay an allowlist, not a denylist of "known bad keys": commit
// 3d813f4 removed `permission`, `tools`, and `temperature` from every shared
// command file because OpenCode silently ignores keys outside this schema —
// they looked enforced but did nothing. A denylist of those three names would
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

// TestSharedCommandsFrontmatterMatchesSchema guards against dead frontmatter
// keys creeping back into configs/shared/commands/*.md. OpenCode's command
// schema (https://opencode.ai/config.json) only recognizes template,
// description, agent, model, variant, and subtask — any other key is parsed
// but silently dropped at runtime, so it looks enforced in the file while
// doing nothing. That is exactly how `permission`, `tools`, and `temperature`
// went unnoticed until commit 3d813f4 removed them.
func TestSharedCommandsFrontmatterMatchesSchema(t *testing.T) {
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
		t.Run(e.Name(), func(t *testing.T) {
			fm := frontmatter(t, filepath.Join(dir, e.Name()))

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
							"to the schema allowlist if OpenCode has genuinely added it.",
						e.Name(),
						key,
						key,
					)
				}
			}
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("no command files found in %s", dir)
	}
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
		t.Run(e.Name(), func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				t.Fatalf("failed to read %s: %v", e.Name(), err)
			}
			if strings.Contains(string(data), "/tmp/") {
				t.Errorf(
					"%s references /tmp/ — use `devgeta task scratch` instead (ADR-0015)",
					e.Name(),
				)
			}
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("no command files found in %s", dir)
	}
}
