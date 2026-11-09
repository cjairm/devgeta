package opencode

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/files"
)

// OpenCode 2 resolves a permission from the ordered `permissions` list: the
// LAST matching rule wins ("The last matching rule wins, so the specific
// exceptions follow the broad rule", opencode.ai/docs/permissions). Two
// consequences these tests pin down:
//
//   - The catch-all "*" must come FIRST for its action. Anywhere later, it
//     would override every rule above it.
//   - An allow after a deny defeats it. Only deny/ask rules belong after the
//     "*" allow.
//
// Pinned against the real embedded configs because a comment in the JSON alone
// will not survive future edits (CLAUDE.md §12).

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

type permissionRule struct {
	Action   string `json:"action"`
	Resource string `json:"resource"`
	Effect   string `json:"effect"`
}

// permissionRules returns the rendered config's ordered permissions list.
func permissionRules(t *testing.T, data []byte) []permissionRule {
	t.Helper()
	var cfg struct {
		Permissions []permissionRule `json:"permissions"`
	}
	if err := json.Unmarshal(data, &cfg); err != nil {
		t.Fatalf("failed to unmarshal rendered opencode.json: %v", err)
	}
	return cfg.Permissions
}

// permissionBlocks groups the read, shell and edit rules by action, as
// resource/effect pairs in document order. Shell rules are keyed "bash", the
// name Claude Code uses, so the parity test compares like with like.
func permissionBlocks(t *testing.T) map[string][][2]string {
	t.Helper()
	out := map[string][][2]string{}
	for _, r := range permissionRules(t, renderedOpenCodeConfig(t)) {
		ns := r.Action
		switch ns {
		case "shell":
			ns = "bash"
		case "read", "edit":
		default:
			continue
		}
		out[ns] = append(out[ns], [2]string{r.Resource, r.Effect})
	}
	for _, name := range []string{"read", "bash", "edit"} {
		if len(out[name]) == 0 {
			t.Fatalf("opencode.json.tmpl has no %s permission rules", name)
		}
	}
	return out
}

// TestEmbeddedConfigCatchAllIsFirst pins the ordering rule: each action opens
// with "*" → allow, so the specific deny/ask rules after it win.
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

// TestEmbeddedConfigPermissionFloor pins the floor from ADR-0067 so a future
// edit cannot quietly drop a secret-read deny or an irreversible-action ask
// while the catch-all stays permissive. It is not a list of everything that is
// allowed: nothing else is denied, and a new rule needs its own reason.
func TestEmbeddedConfigPermissionFloor(t *testing.T) {
	blocks := permissionBlocks(t)

	want := map[string]map[string]string{
		"bash": {
			"git push --force*":   "ask",
			"git push -f*":        "ask",
			"sudo *":              "ask",
			"gh repo delete *":    "ask",
			"gh release delete *": "ask",
		},
		"read": {
			"**/.env":                "deny",
			"**/.env.*":              "deny",
			"*.env":                  "deny",
			"**/envs/*.js":           "deny",
			"./secrets/**":           "deny",
			"*.pem":                  "deny",
			"*.key":                  "deny",
			"~/.ssh/**":              "deny",
			"**/.ssh/**":             "deny",
			"**/id_rsa":              "deny",
			"**/id_ed25519":          "deny",
			"**/id_ecdsa":            "deny",
			"~/.aws/**":              "deny",
			"**/.aws/**":             "deny",
			"~/.kube/**":             "deny",
			"**/.kube/**":            "deny",
			"~/.config/gcloud/**":    "deny",
			"**/.config/gcloud/**":   "deny",
			"~/.azure/**":            "deny",
			"**/.azure/**":           "deny",
			"~/.npmrc":               "deny",
			"**/.npmrc":              "deny",
			"~/.pypirc":              "deny",
			"**/.pypirc":             "deny",
			"~/.netrc":               "deny",
			"**/.netrc":              "deny",
			"~/.git-credentials":     "deny",
			"**/.git-credentials":    "deny",
			"~/.docker/config.json":  "deny",
			"**/.docker/config.json": "deny",
			"~/.pgpass":              "deny",
			"**/.pgpass":             "deny",
			"~/.zsh_history":         "deny",
			"**/.zsh_history":        "deny",
			"~/.bash_history":        "deny",
			"**/.bash_history":       "deny",
		},
		"edit": {
			".git/**": "deny",
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
		// The floor is exact: a rule beyond it was added without the ADR.
		for pattern, action := range got {
			if pattern == "*" {
				continue
			}
			if _, ok := rules[pattern]; !ok {
				t.Errorf(
					"permission.%s has %q = %q, which is not in the ADR-0067 floor",
					block, pattern, action,
				)
			}
		}
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
func TestHomeAnchoredDeniesHaveGlobstarTwins(t *testing.T) {
	for _, ns := range []string{"read", "edit"} {
		rules := claudePermissions(t)[ns]
		for pattern, action := range rules {
			tail, ok := strings.CutPrefix(pattern, "~/")
			if !ok || action != "deny" {
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

// claudeRenderData is the data for Claude's settings.json.tmpl in these
// parity tests. It is a map, not a mirror of claude.settingsTemplateData
// (unexported, in another package): a mirrored struct breaks every time that
// template gains a field, while a map leaves the optional sections it does not
// set (the agent-pack entries) switched off. scratchDir is already
// JSON-encoded.
func claudeRenderData(scratchDir string) map[string]any {
	return map[string]any{"ScratchDir": scratchDir, "Theme": DEFAULT_THEME_NAME}
}

// claudePermissions renders the real embedded settings.json.tmpl and returns
// namespace -> pattern -> action, using the same namespace names OpenCode uses.
func claudePermissions(t *testing.T) map[string]map[string]string {
	t.Helper()
	tmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")
	out := filepath.Join(t.TempDir(), "settings.json")
	if err := files.GenerateFromTemplate(
		tmplPath, out, claudeRenderData(`"/tmp/placeholder-scratch"`),
	); err != nil {
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
// (no glob), OpenCode's external_directory rule is a glob that needs a "/**" suffix to cover nested paths — so a plain
// string-equality check across the two files (the way
// TestClaudeAndOpenCodePermissionParity compares read/bash/edit) would be
// wrong here. This strips OpenCode's suffix back off and compares the
// underlying root instead.
func TestScratchDirGrantParity(t *testing.T) {
	const wantRoot = "/tmp/dg-grant-parity-scratch"

	claudeTmplPath := filepath.Join("..", "..", "..", "configs", "claude", "settings.json.tmpl")
	claudeOut := filepath.Join(t.TempDir(), "settings.json")
	if err := files.GenerateFromTemplate(
		claudeTmplPath, claudeOut, claudeRenderData(mustJSONString(t, wantRoot)),
	); err != nil {
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
		"ScratchDirGlob": mustJSONString(t, wantRoot+"/**"),
	}); err != nil {
		t.Fatalf("failed to render opencode.json.tmpl: %v", err)
	}
	openCodeData, err := os.ReadFile(openCodeOut)
	if err != nil {
		t.Fatal(err)
	}
	var external []permissionRule
	for _, r := range permissionRules(t, openCodeData) {
		if r.Action == "external_directory" {
			external = append(external, r)
		}
	}
	// OpenCode 2 allows every outside path when no rule matches (1.x asked),
	// so the floor needs an explicit "*" → ask, and it must come first so the
	// scratch grant after it wins for the scratch dir only.
	if len(external) != 2 {
		t.Fatalf("expected exactly two external_directory rules (* ask, then the grant), got %v", external)
	}
	if external[0].Resource != "*" || external[0].Effect != "ask" {
		t.Errorf("first external_directory rule = %+v, want * → ask", external[0])
	}
	grant := external[1]
	if grant.Effect != "allow" {
		t.Errorf("external_directory grant %q = %q, want allow", grant.Resource, grant.Effect)
	}
	if gotRoot := strings.TrimSuffix(grant.Resource, "/**"); gotRoot != wantRoot {
		t.Errorf(
			"external_directory grants %q, which strips to root %q — want %q (parity with additionalDirectories)",
			grant.Resource, gotRoot, wantRoot,
		)
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
			if err := files.GenerateFromTemplate(
				claudeTmplPath,
				claudeOut,
				claudeRenderData(mustJSONString(t, root)),
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
