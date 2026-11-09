package agentpack

import "testing"

// sourceNamesPack decides whether a coder's plugin or package spec is another
// copy of a pack's upstream. It must catch the spellings real specs use and
// must not catch a different package that merely shares a prefix.
func TestSourceNamesPack(t *testing.T) {
	sp := Manifest{Name: "superpowers", Repo: "https://github.com/obra/superpowers.git"}
	pt := Manifest{Name: "ponytail", Repo: "https://github.com/DietrichGebert/ponytail.git"}

	yes := []struct {
		pack   Manifest
		source string
	}{
		{sp, "superpowers"},
		{sp, "Superpowers"},
		{sp, "npm:superpowers"},
		{sp, "npm:superpowers@7.0.2"},
		{sp, "npm:@obra/superpowers"},
		{sp, "npm:@obra/superpowers@^7"},
		{sp, "superpowers@git+https://github.com/obra/superpowers.git"},
		{sp, "git:github.com/obra/superpowers"},
		{sp, "git:github.com/obra/superpowers@v7.0.2"},
		{sp, "git+https://github.com/Obra/Superpowers"},
		{sp, "https://github.com/obra/superpowers.git"},
		{sp, "git@github.com:obra/superpowers.git"},
		{sp, "/home/u/code/superpowers"},
		{sp, "/home/u/code/superpowers/"},
		{sp, "/home/u/code/superpowers/.opencode/plugins/"},
		{pt, "npm:@dietrichgebert/ponytail"},
		{pt, "git:github.com/DietrichGebert/ponytail@v5"},
		{pt, "/work/ponytail/.opencode/plugins"},
	}
	for _, tc := range yes {
		if !sourceNamesPack(tc.source, tc.pack) {
			t.Errorf("%q should name %s", tc.source, tc.pack.Name)
		}
	}

	no := []struct {
		pack   Manifest
		source string
	}{
		{sp, ""},
		{sp, "superpowers-extras"},
		{sp, "npm:superpowers-extras@1.0.0"},
		{sp, "npm:@someone/superpowers-lite"},
		{sp, "/home/u/code/my-superpowers"},
		{sp, "git:github.com/obra/superpowers-fork-of-something-else"},
		{sp, "oh-my-opencode"},
		{sp, "npm:pi-lens@1.2.3"},
		{sp, "/data/agent-packs/ponytail"},
		{pt, "npm:superpowers"},
	}
	for _, tc := range no {
		if sourceNamesPack(tc.source, tc.pack) {
			t.Errorf("%q must not name %s", tc.source, tc.pack.Name)
		}
	}
}

func TestSourceNamesPack_APackWithoutARepoMatchesByNameOnly(t *testing.T) {
	m := Manifest{Name: "devgeta", Source: sourceEmbedded}
	if !sourceNamesPack("/x/devgeta", m) {
		t.Error("a folder called devgeta is still named devgeta")
	}
	if sourceNamesPack("git:github.com/cjairm/other", m) {
		t.Error("with no repo there is no slug to match")
	}
}
