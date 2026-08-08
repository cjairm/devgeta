package main

import (
	"io/fs"
	"path"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
)

// TestShellConfigTemplateRendersCoderAliasesFromConstants pins devgeta.zsh's
// cc/oc alias lines to the launch recipes in pkg/constants, which are also what
// a created tmux pane execs (ADR-0020: the pane runs through a non-interactive
// shell that has no aliases, so the recipe is the definition and the alias is a
// rendering of it).
//
// It reads the template out of the EMBEDDED ConfigsFS rather than off disk,
// because that is what `dg configure` actually deploys - an on-disk-only test
// would pass while a stale binary shipped the old alias (CLAUDE.md's "Changing
// an embedded config" rule). It lives in package main for the same reason
// TestBuiltinReviewersAgentNamesMatchEmbeddedAgentFiles does: ConfigsFS is
// declared here, and internal packages cannot import it.
//
// It fails in both directions:
//
//   - Change the template's alias line - hardcode a value back, rename the
//     alias, drop the env prefix - and the rendered line stops matching
//     AliasLine().
//   - Change a recipe's binary, alias or env prefix while the template still
//     holds an old literal, and the same comparison fails.
//   - Change a recipe AND AliasLine() together (or hardcode today's value back
//     into the template) and the two comparisons above still agree with each
//     other - the template's placeholder IS AliasLine(), so on its own that pair
//     can only catch template drift. The LITERAL expectation beside each recipe is
//     what makes the deployed line itself the thing under test, and it fails here,
//     where a reader looks, rather than incidentally in the worktree package.
//   - Move an alias line outside its {{if}} guard, and the all-features-off
//     render below reports it.
//
// Rendering the whole template with the production data type also proves every
// {{if .X}} guard in it still resolves: text/template fails execution on a
// struct field it cannot find, so a data change that orphaned a guard would
// error here rather than silently emitting nothing.
func TestShellConfigTemplateRendersCoderAliasesFromConstants(t *testing.T) {
	recipes := []struct {
		recipe constants.CoderLaunch
		// wantLine is the exact line that must reach a user's devgeta.zsh. It is
		// spelled out rather than derived so that changing the recipe, or
		// AliasLine()'s rendering, is a deliberate edit here too.
		wantLine string
	}{
		{constants.OpenCodeLaunch, `alias oc="opencode"`},
		{constants.ClaudeLaunch, `alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"`},
	}

	enabled := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())
	for _, tt := range recipes {
		lines := aliasLinesFor(enabled, tt.recipe.Alias)
		if len(lines) != 1 {
			t.Errorf(
				"expected exactly one `alias %s=` line in the rendered shell config, got %q",
				tt.recipe.Alias, lines,
			)
			continue
		}
		if lines[0] != tt.recipe.AliasLine() {
			t.Errorf(
				"rendered alias line is %q, want the launch recipe's %q",
				lines[0], tt.recipe.AliasLine(),
			)
		}
		if lines[0] != tt.wantLine {
			t.Errorf("rendered alias line is %q, want exactly %q", lines[0], tt.wantLine)
		}
	}

	// Every feature off: a coder's alias must not appear at all. This is what
	// catches an alias line written outside its {{if}} guard, which would
	// otherwise still match above.
	disabled := renderEmbeddedShellConfig(t, config.ShellFeatures{})
	for _, tt := range recipes {
		if lines := aliasLinesFor(disabled, tt.recipe.Alias); len(lines) != 0 {
			t.Errorf(
				"`alias %s=` must only be rendered when the coder's feature is enabled, got %q",
				tt.recipe.Alias, lines,
			)
		}
	}
}

// TestShellConfigCoderSectionHeaderRendersForEitherCoder covers the one-coder
// installs. The `# ---- AI coders ----` header used to sit inside the opencode
// guard, so a claude-only user got a bare `alias cc=` line while every other
// section in the generated file carried its header.
func TestShellConfigCoderSectionHeaderRendersForEitherCoder(t *testing.T) {
	const header = "# ---- AI coders ----"

	tests := []struct {
		name     string
		features config.ShellFeatures
		wantHead bool
	}{
		{"opencode only", config.ShellFeatures{Opencode: true}, true},
		{"claude only", config.ShellFeatures{Claude: true}, true},
		{"both", config.ShellFeatures{Opencode: true, Claude: true}, true},
		{"neither", config.ShellFeatures{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderEmbeddedShellConfig(t, tt.features)
			if got := strings.Contains(rendered, header); got != tt.wantHead {
				t.Errorf("rendered contains %q = %v, want %v", header, got, tt.wantHead)
			}
			if strings.Count(rendered, header) > 1 {
				t.Errorf("expected at most one %q header, got %d", header,
					strings.Count(rendered, header))
			}
		})
	}
}

// TestShellConfigCoderSectionIsContiguous pins the AI-coders section's shape
// against its neighbors (e.g. the Eza block a few lines above it): a header
// immediately followed by its alias line(s), with no blank line in between.
//
// Before this, each `{{if}}` body started with its own leading newline and
// each `{{end}}` was followed by one, so the section rendered as header, a
// blank line, `alias oc=...`, two more blank lines, then `alias cc=...` (and,
// for a claude-only install, header then three blank lines before `alias
// cc=...`). Whitespace-trimming markers (`{{-` / `-}}`) on the section's
// three `{{if}}`/`{{end}}` pairs fixed the spacing without changing which
// lines render - see configs/templates/devgeta.zsh.tmpl.
func TestShellConfigCoderSectionIsContiguous(t *testing.T) {
	tests := []struct {
		name     string
		features config.ShellFeatures
		want     []string
	}{
		{
			"both",
			config.ShellFeatures{Opencode: true, Claude: true},
			[]string{
				"# ---- AI coders ----",
				`alias oc="opencode"`,
				`alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"`,
			},
		},
		{
			"opencode only",
			config.ShellFeatures{Opencode: true},
			[]string{
				"# ---- AI coders ----",
				`alias oc="opencode"`,
			},
		},
		{
			"claude only",
			config.ShellFeatures{Claude: true},
			[]string{
				"# ---- AI coders ----",
				`alias cc="CLAUDE_CODE_NO_FLICKER=1 claude"`,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rendered := renderEmbeddedShellConfig(t, tt.features)
			got := contiguousLinesFollowing(rendered, "# ---- AI coders ----", len(tt.want))
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf(
					"AI-coders section rendered as %q, want the header immediately "+
						"followed by %q with no blank line between them",
					got, tt.want,
				)
			}
		})
	}
}

// contiguousLinesFollowing returns header and the count-1 non-empty lines
// immediately after it in rendered, stopping (and returning a short slice) at
// the first blank line - so a caller comparing against a want slice sees
// exactly where a blank line crept back in.
func contiguousLinesFollowing(rendered, header string, count int) []string {
	lines := strings.Split(rendered, "\n")
	for i, line := range lines {
		if line != header {
			continue
		}
		got := []string{header}
		for _, line := range lines[i+1:] {
			if len(got) >= count || line == "" {
				break
			}
			got = append(got, line)
		}
		return got
	}
	return nil
}

// TestShellConfigTemplateKeepsMaintainerNotesOutOfTheOutput: the template
// explains WHY its alias lines are rendered from pkg/constants, and that
// explanation is for whoever edits the template - it must not be shipped into
// every user's generated devgeta.zsh as `#` comments.
func TestShellConfigTemplateKeepsMaintainerNotesOutOfTheOutput(t *testing.T) {
	rendered := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())

	for _, leaked := range []string{"pkg/constants", "ADR-0020", "non-interactive"} {
		if strings.Contains(rendered, leaked) {
			t.Errorf("maintainer note mentioning %q reached the generated shell config", leaked)
		}
	}
}

// renderEmbeddedShellConfig renders the embedded devgeta.zsh template from the
// same data production uses (config.NewShellTemplateData), so this test cannot
// pass by re-deriving the alias itself.
func renderEmbeddedShellConfig(t *testing.T, features config.ShellFeatures) string {
	t.Helper()

	templatePath := path.Join(
		constants.App.Dir.Configs,
		constants.Templates,
		constants.App.Template.ShellConfig,
	)
	raw, err := fs.ReadFile(ConfigsFS, templatePath)
	if err != nil {
		t.Fatalf("failed to read embedded %s: %v", templatePath, err)
	}

	tmpl, err := template.New(constants.App.Template.ShellConfig).Parse(string(raw))
	if err != nil {
		t.Fatalf("failed to parse embedded %s: %v", templatePath, err)
	}

	var out strings.Builder
	if err := tmpl.Execute(&out, config.NewShellTemplateData(features)); err != nil {
		t.Fatalf("failed to render embedded %s: %v", templatePath, err)
	}
	return out.String()
}

// allShellFeaturesEnabled turns on every ShellFeatures flag, by reflection so a
// flag added later is covered without editing this test.
func allShellFeaturesEnabled() config.ShellFeatures {
	var features config.ShellFeatures
	value := reflect.ValueOf(&features).Elem()
	for i := 0; i < value.NumField(); i++ {
		if field := value.Field(i); field.Kind() == reflect.Bool {
			field.SetBool(true)
		}
	}
	return features
}

// aliasLinesFor returns every rendered line that defines the named alias.
func aliasLinesFor(rendered, alias string) []string {
	prefix := "alias " + alias + "="
	var found []string
	for _, line := range strings.Split(rendered, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), prefix) {
			found = append(found, strings.TrimSpace(line))
		}
	}
	return found
}
