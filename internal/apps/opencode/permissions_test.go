package opencode

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/files"
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

// bashPatternMatches implements the trailing-`*` prefix match that is the
// whole of Bash(...) / permission.bash matching for a pattern with no other
// glob metacharacter — verified empirically for both agents: ADR-0038's probe
// table for Claude Code, and this cycle's step 1 probe-notes for OpenCode
// (e.g. `action.pattern="gh pr merge *"` matching `gh pr merge 12 --squash`,
// logged verbatim by `--print-logs --log-level DEBUG`). A pattern ending in
// "*" matches iff the command starts with the literal text before it; a
// pattern with no "*" must equal the command exactly.
func bashPatternMatches(pattern, command string) bool {
	prefix, hasStar := strings.CutSuffix(pattern, "*")
	if hasStar {
		return strings.HasPrefix(command, prefix)
	}
	return pattern == command
}

// TestGhWriteGatesResolve pins the four write gates this cycle adds
// (docs/plans/cycles/2026-09-10-gh-permission-granularity.md step 7): each
// must actually match a representative real invocation of the command it
// names, in both configs — string presence alone (TestEmbeddedConfigGuardsDangerousCommands's
// style) proves the pattern exists, not that it matches anything
// (docs/guides/agent-permission-matching.md is the whole reason that
// distinction gets its own test here). Also confirms none of the four is
// shadowed by the existing `gh repo delete *` deny: Claude Code resolves
// deny -> ask -> allow with no specificity tiebreak, so a broader deny
// landing first would silently swallow a narrower ask.
func TestGhWriteGatesResolve(t *testing.T) {
	cases := []struct {
		pattern string
		command string
	}{
		{"gh pr merge *", "gh pr merge 12 --squash"},
		{"gh release delete *", "gh release delete v1.0.0"},
		{"gh pr close *", "gh pr close 5"},
		{"gh issue delete *", "gh issue delete 5"},
	}

	claudeBash := claudePermissions(t)["bash"]
	openCodeBash := make(map[string]string)
	for _, p := range permissionBlocks(t)["bash"] {
		openCodeBash[p[0]] = p[1]
	}

	for _, tc := range cases {
		t.Run(tc.pattern, func(t *testing.T) {
			if !bashPatternMatches(tc.pattern, tc.command) {
				t.Fatalf(
					"pattern %q does not match its own target command %q — the matcher or the pattern is wrong",
					tc.pattern,
					tc.command,
				)
			}
			if got := claudeBash[tc.pattern]; got != "ask" {
				t.Errorf("Claude Code: permission.bash[%q] = %q, want \"ask\"", tc.pattern, got)
			}
			if got := openCodeBash[tc.pattern]; got != "ask" {
				t.Errorf("OpenCode: permission.bash[%q] = %q, want \"ask\"", tc.pattern, got)
			}
			if bashPatternMatches("gh repo delete *", tc.command) {
				t.Fatalf(
					"command %q unexpectedly matches the unrelated \"gh repo delete *\" deny",
					tc.command,
				)
			}
		})
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
// This asserts the shape rather than the exact list, in EITHER config, for
// both spellings of the Claude root:
//
//   - `~/.claude/**` — the original blanket. Re-adding it silently re-breaks
//     memory on Claude Code, where `~/` rules are live.
//   - `**/.claude/<anything containing *>` — the same bug in the spelling that
//     OpenCode actually honours. Probed 2026-08-25 against opencode 1.18.23:
//     an edit deny `**/.dgprobe-claude/*.md` matched
//     `…/.dgprobe-claude/projects/slug/memory/MEMORY.md`, because OpenCode's
//     `*` crosses `/`. So a `**/`-anchored rule may only wildcard *below* a
//     literal first segment (`**/.claude/agents/**`), never in it
//     (`**/.claude/*.md`, `**/.claude/**`).
//
// The `~/`-spelled trio (`~/.claude/*.json`, `*.sh`, `*.md`) is exempt from the
// second rule and must stay: on Claude Code `*` does NOT cross `/` (probed the
// same day against 2.1.245), so it denies the direct children while leaving
// memory writable, and on OpenCode it is inert. See
// docs/guides/agent-permission-matching.md.
func TestGlobalClaudeFloorLeavesMemoryWritable(t *testing.T) {
	const blanket = "~/.claude/**"
	const globstarRoot = "**/.claude/"

	check := func(t *testing.T, source string, patterns []string) {
		t.Helper()
		for _, pattern := range patterns {
			if pattern == blanket {
				t.Errorf(
					"%s denies %q — that blanket also covers "+
						"~/.claude/projects/<slug>/memory/, Claude Code's memory "+
						"directory, and deny beats any allow carve-out. Enumerate the "+
						"config surfaces under ~/.claude/ instead (see ADR-0014's "+
						"memory amendment)",
					source, pattern,
				)
				continue
			}
			tail, ok := strings.CutPrefix(pattern, globstarRoot)
			if !ok {
				continue
			}
			first, _, _ := strings.Cut(tail, "/")
			if strings.Contains(first, "*") {
				t.Errorf(
					"%s denies %q — OpenCode's %q crosses %q, so a wildcard in the "+
						"segment straight after %q also matches "+
						"~/.claude/projects/<slug>/memory/*, Claude Code's memory "+
						"directory. Name the file exactly (%q) or wildcard only below a "+
						"literal subdirectory (%q)",
					source, pattern, "*", "/", globstarRoot,
					globstarRoot+"CLAUDE.md", globstarRoot+"agents/**",
				)
			}
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
		// The `~/` spelling: live on Claude Code, inert on OpenCode.
		"~/.claude/*.json",
		"~/.claude/*.sh",
		"~/.claude/*.md",
		"~/.claude/agents/**",
		"~/.claude/commands/**",
		"~/.claude/skills/**",
		"~/.claude/plugins/**",
		"~/.claude/hooks/**",
		"~/.claude/lib/**",

		// The `**/` spelling: the only one OpenCode honours for the global
		// Claude root. The trio's `*.json`/`*.sh`/`*.md` cannot be re-spelled
		// this way without sweeping memory (see the comment above), so its
		// counterpart is the enumeration of the direct children devgeta and
		// Claude Code actually deploy there.
		"**/.claude/settings.json",
		"**/.claude/settings.local.json",
		"**/.claude/CLAUDE.md",
		"**/.claude/statusline.sh",
		"**/.claude/format.sh",
		"**/.claude/task-redirect.sh",
		"**/.claude/secret-guard.sh",
		"**/.claude/suppression-guard.sh",
		"**/.claude/agent-config-guard.sh",
		"**/.claude/agent-state.sh",
		"**/.claude/agents/**",
		"**/.claude/commands/**",
		"**/.claude/skills/**",
		"**/.claude/plugins/**",
		"**/.claude/hooks/**",
		"**/.claude/lib/**",
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

// TestGlobalClaudeFloorEnumerationStaysCurrent stops the exact-filename half of
// the floor going stale. The `~/.claude/*.sh` wildcard used to cover every
// deployed hook script at once, but it is inert on OpenCode, and its live
// counterpart cannot be a wildcard (that would sweep memory — see
// TestGlobalClaudeFloorLeavesMemoryWritable). Naming each script is the only
// expressible shape, and a hand-written list of filenames is exactly the kind of
// enumeration that silently falls behind the thing it mirrors, so this derives
// the expected set from the scripts devgeta actually ships instead.
func TestGlobalClaudeFloorEnumerationStaysCurrent(t *testing.T) {
	configDir := filepath.Join("..", "..", "..", "configs", "claude")
	entries, err := os.ReadDir(configDir)
	if err != nil {
		t.Fatalf("failed to read %s: %v", configDir, err)
	}

	claudeEdit := claudePermissions(t)["edit"]
	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".sh") {
			continue
		}
		checked++
		want := "**/.claude/" + e.Name()
		if claudeEdit[want] != "deny" {
			t.Errorf(
				"configs/claude/%s is deployed to ~/.claude/ but nothing denies %q. "+
					"~/.claude/*.sh does not cover it on OpenCode (a `~/` rule never "+
					"matches there), and `**/.claude/*.sh` would also match every "+
					"memory file, so each script has to be named",
				e.Name(), want,
			)
		}
	}
	if checked == 0 {
		t.Fatalf("found no hook scripts in %s — has the layout changed?", configDir)
	}
}

// TestHomeAnchoredDeniesHaveGlobstarTwins is the structural guard for the fact
// that started this whole page: a `~/`-anchored rule is live on Claude Code and
// completely dead on OpenCode (probed 2026-08-25, opencode 1.18.23 — the rule
// falls through to the `*` catch-all). Shipping one without a `**/`-anchored
// twin therefore ships half a policy, and nothing about the config's appearance
// says so.
//
// The `**/` twin is not a replacement: `**/` does NOT reach outside the project
// on Claude Code (probed the same day, 2.1.245 — a `**/`-anchored deny left a
// home-directory read allowed). Both spellings have to ship together.
//
// The trio is the one exemption, and it is a real one rather than a loophole:
// `**/.claude/*.md` would match memory on OpenCode, so the trio's counterpart is
// the exact-filename enumeration pinned by the two tests above.
func TestHomeAnchoredDeniesHaveGlobstarTwins(t *testing.T) {
	exempt := map[string]string{
		"~/.claude/*.json": "covered by the **/.claude/<name>.json enumeration",
		"~/.claude/*.sh":   "covered by the **/.claude/<name>.sh enumeration",
		"~/.claude/*.md":   "covered by the **/.claude/<name>.md enumeration",
	}

	for _, ns := range []string{"read", "edit"} {
		rules := claudePermissions(t)[ns]
		for pattern, action := range rules {
			tail, ok := strings.CutPrefix(pattern, "~/")
			if !ok || action != "deny" {
				continue
			}
			if why, skip := exempt[pattern]; skip {
				if rules["**/"+tail] == "deny" {
					t.Errorf(
						"%s deny %q is on the exemption list (%s) but now has a "+
							"literal twin too — drop the exemption",
						ns, pattern, why,
					)
				}
				continue
			}
			if rules["**/"+tail] != "deny" {
				t.Errorf(
					"%s denies %q with no %q alongside it. A `~/` rule never fires on "+
						"OpenCode, so this protects Claude Code only; a `**/` rule never "+
						"reaches the home directory on Claude Code, so it cannot replace "+
						"the `~/` one. Ship both (see docs/guides/agent-permission-matching.md)",
					ns, pattern, "**/"+tail,
				)
			}
		}
	}
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
		ScratchDir          string
		OutputBudgetEnabled bool
		Theme               string
	}{ScratchDir: `"/tmp/placeholder-scratch"`, Theme: DEFAULT_THEME_NAME}
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
// types. Claude and Pi format via the shared format-file.sh (a shell `case` over
// glob patterns); OpenCode formats via the `formatter` block's extension lists.
// A language added to one must be added to the other.
func TestClaudeAndOpenCodeFormatterParity(t *testing.T) {
	// Extensions handled by format-file.sh, scraped from its `case` glob patterns.
	shPath := filepath.Join("..", "..", "..", "configs", "devgeta", "format-file.sh")
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
		ScratchDir          string
		OutputBudgetEnabled bool
		Theme               string
	}{ScratchDir: mustJSONString(t, wantRoot), Theme: DEFAULT_THEME_NAME}
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
				ScratchDir          string
				OutputBudgetEnabled bool
				Theme               string
			}{ScratchDir: mustJSONString(t, root), Theme: DEFAULT_THEME_NAME}
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
