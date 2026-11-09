package files

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

// JSONSetting is one value devgeta writes into a JSON settings file that it
// shares with the file's owner: a coder's own settings, which also hold the
// user's preferences.
type JSONSetting struct {
	// Section is the top-level object the key lives in, or "" for a
	// top-level key.
	Section string
	Key     string
	// Value is the raw JSON to write, e.g. `"default"` or `true`.
	Value json.RawMessage
}

// MergeJSONSettings writes settings into the JSON object at path and keeps
// everything else in it.
//
// Without force, a key that is already present is the user's choice and
// stays; with force, every setting is written. Every other key round-trips as
// raw JSON, and HTML escaping is off, because the default would rewrite "&"
// in a URL as &. A missing file starts empty. A file that does not parse
// is an error, never a reset: devgeta does not own it. Nothing is written when
// nothing changed.
func MergeJSONSettings(path string, settings []JSONSetting, force bool) error {
	root := map[string]json.RawMessage{}
	data, err := os.ReadFile(path)
	switch {
	case err == nil:
		if err := json.Unmarshal(data, &root); err != nil {
			return fmt.Errorf("failed to parse %s: %w", path, err)
		}
	case !os.IsNotExist(err):
		return fmt.Errorf("failed to read %s: %w", path, err)
	}

	changed := false
	for _, setting := range settings {
		if setting.Section == "" {
			if _, exists := root[setting.Key]; exists && !force {
				continue
			}
			root[setting.Key] = setting.Value
			changed = true
			continue
		}
		fields := map[string]json.RawMessage{}
		if raw, ok := root[setting.Section]; ok && string(raw) != "null" {
			if err := json.Unmarshal(raw, &fields); err != nil {
				return fmt.Errorf("failed to parse %q in %s: %w", setting.Section, path, err)
			}
		}
		if _, exists := fields[setting.Key]; exists && !force {
			continue
		}
		fields[setting.Key] = setting.Value
		raw, err := json.Marshal(fields)
		if err != nil {
			return fmt.Errorf("failed to encode %q: %w", setting.Section, err)
		}
		root[setting.Section] = raw
		changed = true
	}
	if !changed {
		return nil
	}

	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(root); err != nil {
		return fmt.Errorf("failed to encode %s: %w", path, err)
	}
	return WriteFileAtomic(path, out.Bytes(), FilePermission)
}
