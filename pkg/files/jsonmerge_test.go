package files

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return out
}

func TestMergeJSONSettings(t *testing.T) {
	theme := []JSONSetting{{Section: "theme", Key: "name", Value: json.RawMessage(`"gruvbox"`)}}

	t.Run("missing file starts empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		if err := MergeJSONSettings(path, theme, false); err != nil {
			t.Fatal(err)
		}
		got := readJSON(t, path)["theme"].(map[string]any)["name"]
		if got != "gruvbox" {
			t.Errorf("theme.name = %v, want gruvbox", got)
		}
	})

	t.Run("keeps sibling keys and the section's other fields", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		writeFile(t, path, `{"theme":{"name":"old","mode":"dark"},"animations":true,"url":"a&b"}`)
		if err := MergeJSONSettings(path, theme, true); err != nil {
			t.Fatal(err)
		}
		got := readJSON(t, path)
		section := got["theme"].(map[string]any)
		if section["name"] != "gruvbox" || section["mode"] != "dark" {
			t.Errorf("theme = %v, want name gruvbox and mode kept", section)
		}
		if got["animations"] != true || got["url"] != "a&b" {
			t.Errorf("siblings not kept: %v", got)
		}
		raw, _ := os.ReadFile(path)
		if json.Valid(raw) && containsEscapedAmpersand(raw) {
			t.Errorf("& was HTML-escaped: %s", raw)
		}
	})

	t.Run("without force an existing value stays", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		writeFile(t, path, `{"theme":{"name":"mine"}}`)
		if err := MergeJSONSettings(path, theme, false); err != nil {
			t.Fatal(err)
		}
		if got := readJSON(t, path)["theme"].(map[string]any)["name"]; got != "mine" {
			t.Errorf("theme.name = %v, want mine", got)
		}
	})

	t.Run("a null section is treated as empty", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		writeFile(t, path, `{"theme":null}`)
		if err := MergeJSONSettings(path, theme, false); err != nil {
			t.Fatal(err)
		}
		if got := readJSON(t, path)["theme"].(map[string]any)["name"]; got != "gruvbox" {
			t.Errorf("theme.name = %v, want gruvbox", got)
		}
	})

	t.Run("top-level key", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "settings.json")
		writeFile(t, path, `{"packages":["x"]}`)
		top := []JSONSetting{{Key: "theme", Value: json.RawMessage(`"devgeta"`)}}
		if err := MergeJSONSettings(path, top, false); err != nil {
			t.Fatal(err)
		}
		got := readJSON(t, path)
		if got["theme"] != "devgeta" || got["packages"] == nil {
			t.Errorf("got %v", got)
		}
	})

	t.Run("an unparseable file is an error and is left alone", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		writeFile(t, path, `{not json`)
		if err := MergeJSONSettings(path, theme, true); err == nil {
			t.Fatal("expected an error")
		}
		if raw, _ := os.ReadFile(path); string(raw) != `{not json` {
			t.Errorf("file was rewritten: %s", raw)
		}
	})

	t.Run("nothing changed writes nothing", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "cli.json")
		original := `{"theme":{"name":"mine"}}`
		writeFile(t, path, original)
		if err := MergeJSONSettings(path, theme, false); err != nil {
			t.Fatal(err)
		}
		if raw, _ := os.ReadFile(path); string(raw) != original {
			t.Errorf("file was rewritten: %s", raw)
		}
	})
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsEscapedAmpersand(raw []byte) bool {
	for i := 0; i+6 <= len(raw); i++ {
		if string(raw[i:i+6]) == `&` {
			return true
		}
	}
	return false
}
