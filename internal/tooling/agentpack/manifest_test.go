package agentpack

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

const goodSHA = "22f0b463e7a5e579904dd6211a520321035526f7"

func upstreamYAML(overrides map[string]string) string {
	fields := map[string]string{
		"name": "superpowers",
		"repo": "https://github.com/obra/superpowers.git",
		"tag":  "v7.0.2",
		"sha":  goodSHA,
	}
	for k, v := range overrides {
		if v == "" {
			delete(fields, k)
		} else {
			fields[k] = v
		}
	}
	var b strings.Builder
	for _, k := range []string{"name", "repo", "tag", "sha", "source"} {
		if v, ok := fields[k]; ok {
			b.WriteString(k + ": " + v + "\n")
		}
	}
	return b.String()
}

func TestParseManifest_Upstream(t *testing.T) {
	m, err := ParseManifest([]byte(upstreamYAML(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if m.Name != "superpowers" || m.Tag != "v7.0.2" || m.SHA != goodSHA ||
		m.Repo != "https://github.com/obra/superpowers.git" {
		t.Errorf("unexpected manifest: %+v", m)
	}
	if m.Embedded() {
		t.Error("an upstream pack is not embedded")
	}
}

func TestParseManifest_Embedded(t *testing.T) {
	m, err := ParseManifest([]byte("name: devgeta\nsource: embedded\n"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !m.Embedded() || m.Name != "devgeta" {
		t.Errorf("unexpected manifest: %+v", m)
	}
}

func TestParseManifest_Rejects(t *testing.T) {
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"short sha", upstreamYAML(map[string]string{"sha": "22f0b46"}), "sha"},
		{"uppercase sha", upstreamYAML(map[string]string{"sha": strings.ToUpper(goodSHA)}), "sha"},
		{"non-hex sha", upstreamYAML(map[string]string{"sha": strings.Repeat("z", 40)}), "sha"},
		{"missing sha", upstreamYAML(map[string]string{"sha": ""}), "sha"},
		{"missing tag", upstreamYAML(map[string]string{"tag": ""}), "tag"},
		{"missing repo", upstreamYAML(map[string]string{"repo": ""}), "repo"},
		{"missing name", upstreamYAML(map[string]string{"name": ""}), "name"},
		{"tag that is a flag", upstreamYAML(map[string]string{"tag": "--upload-pack=x"}), "tag"},
		{"non-https repo", upstreamYAML(map[string]string{"repo": "file:///etc"}), "repo"},
		{"name with a slash", upstreamYAML(map[string]string{"name": "../evil"}), "name"},
		{"name in upper case", upstreamYAML(map[string]string{"name": "Superpowers"}), "name"},
		{"unknown key", upstreamYAML(nil) + "branch: main\n", "branch"},
		{"unknown source", "name: x\nsource: remote\n", "source"},
		{"embedded with a repo", "name: devgeta\nsource: embedded\nrepo: https://x.test/r.git\n", "embedded"},
		{"not yaml", "name: [", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected the error to mention %q, got: %v", tc.want, err)
			}
		})
	}
}

func writePack(t *testing.T, root, dir, yaml string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(root, dir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, dir, "pack.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoadManifests(t *testing.T) {
	root := t.TempDir()
	writePack(t, root, "superpowers", upstreamYAML(nil))
	writePack(t, root, "devgeta", "name: devgeta\nsource: embedded\n")
	// A folder without a pack.yaml (for example a stray directory) is not a pack.
	if err := os.MkdirAll(filepath.Join(root, "stray"), 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := LoadManifests(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var names []string
	for _, m := range got {
		names = append(names, m.Name)
	}
	if strings.Join(names, ",") != "devgeta,superpowers" {
		t.Errorf("expected packs sorted by name, got %v", names)
	}
}

func TestLoadManifests_NameMustMatchFolder(t *testing.T) {
	root := t.TempDir()
	writePack(t, root, "ponytail", upstreamYAML(nil)) // says "superpowers"

	_, err := LoadManifests(root)
	if err == nil || !strings.Contains(err.Error(), "ponytail") {
		t.Fatalf("expected an error naming the folder, got %v", err)
	}
}

func TestLoadManifests_InvalidManifestNamesThePack(t *testing.T) {
	root := t.TempDir()
	writePack(t, root, "superpowers", upstreamYAML(map[string]string{"sha": "abc"}))

	_, err := LoadManifests(root)
	if err == nil || !strings.Contains(err.Error(), "superpowers") {
		t.Fatalf("expected the error to name the pack, got %v", err)
	}
}

func TestLoadManifests_MissingRoot(t *testing.T) {
	_, err := LoadManifests(filepath.Join(t.TempDir(), "nope"))
	if err == nil {
		t.Fatal("expected an error for a missing configs directory")
	}
}

// TestShippedManifests keeps the real pins honest: they load, they pin the
// versions the cycle decided on, and every SHA is a full commit hash.
func TestShippedManifests(t *testing.T) {
	got, err := LoadManifests(filepath.Join("..", "..", "..", "configs", "agent-packs"))
	if err != nil {
		t.Fatalf("shipped manifests do not load: %v", err)
	}
	want := map[string][2]string{
		"superpowers": {"v7.0.2", "22f0b463e7a5e579904dd6211a520321035526f7"},
		"ponytail":    {"v5.3.3", "7801d061243d7b753bad0e0b3c057cd250b11755"},
	}
	seen := map[string]bool{}
	for _, m := range got {
		seen[m.Name] = true
		if m.Name == "devgeta" && !m.Embedded() {
			t.Error("the devgeta pack ships inside the binary and must say source: embedded")
		}
		if w, ok := want[m.Name]; ok && (m.Tag != w[0] || m.SHA != w[1]) {
			t.Errorf("%s pinned at %s %s, want %s %s", m.Name, m.Tag, m.SHA, w[0], w[1])
		}
	}
	want["devgeta"] = [2]string{} // present, embedded, no pin
	for _, m := range got {
		if m.Name == "superpowers" && len(m.Requires) != 2 {
			t.Errorf("superpowers needs bash (its Claude hook) and node (the visual companion), got %+v", m.Requires)
		}
		if m.Name == "ponytail" {
			if len(m.Requires) != 1 || m.Requires[0].Command != "node" {
				t.Errorf("ponytail's Claude hooks need node, got %+v", m.Requires)
			}
			var claude, opencode bool
			for _, n := range m.Notices {
				claude = claude || (n.Coder == "claude" && strings.Contains(n.Text, "band") && strings.Contains(n.Text, "ledger"))
				opencode = opencode || (n.Coder == "opencode" && strings.Contains(n.Text, "PONYTAIL_DEFAULT_MODE") && strings.Contains(n.Text, "service"))
			}
			if !claude || !opencode {
				t.Errorf("ponytail must tell Claude users about the ledger and OpenCode users about the service environment: %+v", m.Notices)
			}
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("shipped manifests are missing %s", name)
		}
	}
}

func TestParseManifest_RequiresAndNotices(t *testing.T) {
	m, err := ParseManifest([]byte(upstreamYAML(nil) + `requires:
  - command: node
    reason: the hooks it runs in Claude Code
  - command: bash
    reason: its session-start hook
notices:
  - text: Writes a row per task to ~/.config/x.
  - coder: opencode
    text: Read from the shared service's environment.
`))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(m.Requires) != 2 || m.Requires[0].Command != "node" || m.Requires[1].Reason != "its session-start hook" {
		t.Errorf("unexpected requires %+v", m.Requires)
	}
	if len(m.Notices) != 2 || m.Notices[0].Coder != "" || m.Notices[1].Coder != "opencode" {
		t.Errorf("unexpected notices %+v", m.Notices)
	}
}

func TestParseManifest_RejectsBadRequiresAndNotices(t *testing.T) {
	cases := []struct {
		name string
		tail string
		want string
	}{
		{"a command with a path", "requires:\n  - command: /usr/bin/node\n    reason: x\n", "command"},
		{"a command with a space", "requires:\n  - command: node --version\n    reason: x\n", "command"},
		{"a missing command", "requires:\n  - reason: x\n", "command"},
		{"a missing reason", "requires:\n  - command: node\n", "reason"},
		{"a notice with no text", "notices:\n  - coder: pi\n", "text"},
		{"a notice for an unknown coder", "notices:\n  - coder: vim\n    text: hi\n", "coder"},
		{"an unknown key in a requirement", "requires:\n  - command: node\n    reason: x\n    optional: true\n", "optional"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseManifest([]byte(upstreamYAML(nil) + tc.tail))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("expected an error mentioning %q, got %v", tc.want, err)
			}
		})
	}
}

func TestManifest_MissingRuntimes(t *testing.T) {
	m := Manifest{Requires: []Requirement{
		{Command: "node", Reason: "the hooks"},
		{Command: "bash", Reason: "the session hook"},
		{Command: "git", Reason: "cloning"},
	}}
	onPath := map[string]bool{"git": true}
	look := func(name string) (string, error) {
		if onPath[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}

	got := m.MissingRuntimes(look)

	if len(got) != 2 || got[0].Command != "node" || got[1].Command != "bash" {
		t.Errorf("expected node then bash missing, in the manifest's order, got %+v", got)
	}
	if got := (Manifest{}).MissingRuntimes(look); len(got) != 0 {
		t.Errorf("nothing required means nothing missing, got %v", got)
	}
}
