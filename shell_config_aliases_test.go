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
//   - Move an alias line outside its {{if}} guard, and the all-features-off
//     render below reports it.
//
// Rendering the whole template with the production data type also proves every
// {{if .X}} guard in it still resolves: text/template fails execution on a
// struct field it cannot find, so a data change that orphaned a guard would
// error here rather than silently emitting nothing.
func TestShellConfigTemplateRendersCoderAliasesFromConstants(t *testing.T) {
	recipes := []constants.CoderLaunch{constants.OpenCodeLaunch, constants.ClaudeLaunch}

	enabled := renderEmbeddedShellConfig(t, allShellFeaturesEnabled())
	for _, recipe := range recipes {
		lines := aliasLinesFor(enabled, recipe.Alias)
		if len(lines) != 1 {
			t.Errorf(
				"expected exactly one `alias %s=` line in the rendered shell config, got %q",
				recipe.Alias, lines,
			)
			continue
		}
		if lines[0] != recipe.AliasLine() {
			t.Errorf(
				"rendered alias line is %q, want the launch recipe's %q",
				lines[0], recipe.AliasLine(),
			)
		}
	}

	// Every feature off: a coder's alias must not appear at all. This is what
	// catches an alias line written outside its {{if}} guard, which would
	// otherwise still match above.
	disabled := renderEmbeddedShellConfig(t, config.ShellFeatures{})
	for _, recipe := range recipes {
		if lines := aliasLinesFor(disabled, recipe.Alias); len(lines) != 0 {
			t.Errorf(
				"`alias %s=` must only be rendered when the coder's feature is enabled, got %q",
				recipe.Alias, lines,
			)
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
