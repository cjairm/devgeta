package brave

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
)

// localStateFile is Chromium's per-installation registry: the profile list,
// each profile's display name, and the counter it hands the next profile
// directory. It sits beside the profile directories, not inside one.
//
// It is on the denylist and stays there: no export packs it and no bundle
// member may restore to it. What this file does is different in kind — it
// merges named fields into *this machine's own* copy, having copied nothing
// from the source (ADR-0046).
const localStateFile = "Local State"

// ReadProfileRegistry reads the profile list out of Local State.
func (b *Brave) ReadProfileRegistry() (map[string]apps.StateProfileInfo, error) {
	path := filepath.Join(b.dataDir(), localStateFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]apps.StateProfileInfo{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}

	var root struct {
		Profile struct {
			InfoCache map[string]struct {
				Name   string `json:"name"`
				Avatar string `json:"avatar_icon"`
			} `json:"info_cache"`
		} `json:"profile"`
	}
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}

	out := map[string]apps.StateProfileInfo{}
	for dir, entry := range root.Profile.InfoCache {
		out[dir] = apps.StateProfileInfo{Dir: dir, Name: entry.Name, Avatar: entry.Avatar}
	}
	return out, nil
}

// EnsureProfiles creates and registers the profiles want names that this
// machine does not have yet, and returns the directories it created.
func (b *Brave) EnsureProfiles(want map[string]apps.StateProfileInfo) ([]string, error) {
	root, err := b.readLocalStateTree()
	if err != nil {
		return nil, err
	}
	cache := infoCache(root)

	var created []string
	for _, dir := range sortedProfileKeys(want) {
		// An entry that is already registered is left exactly as it is. An
		// import restores state into a profile; it never relabels one the
		// user is already using (ADR-0046).
		if _, already := cache[dir]; already {
			continue
		}
		info := want[dir]
		if err := os.MkdirAll(filepath.Join(b.dataDir(), dir), 0o700); err != nil {
			return created, fmt.Errorf("creating profile directory %s: %w", dir, err)
		}
		entry := map[string]any{"name": info.Name}
		if info.Avatar != "" {
			entry["avatar_icon"] = info.Avatar
		}
		cache[dir] = entry
		appendToOrder(root, dir)
		raiseCreatedPast(root, dir)
		created = append(created, dir)
	}

	if len(created) == 0 {
		return nil, nil
	}
	if err := b.writeLocalStateTree(root); err != nil {
		return created, err
	}
	return created, nil
}

// readLocalStateTree parses Local State whole, so a write puts back every key
// Chromium keeps there that devgeta does not model.
func (b *Brave) readLocalStateTree() (map[string]any, error) {
	path := filepath.Join(b.dataDir(), localStateFile)
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var root map[string]any
	if err := json.Unmarshal(raw, &root); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return root, nil
}

// writeLocalStateTree replaces the file through a temporary sibling and a
// rename, so a crash mid-write cannot leave Chromium's registry truncated.
func (b *Brave) writeLocalStateTree(root map[string]any) error {
	path := filepath.Join(b.dataDir(), localStateFile)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(path), err)
	}
	encoded, err := json.Marshal(root)
	if err != nil {
		return fmt.Errorf("encoding %s: %w", path, err)
	}
	tmp := path + ".dg-tmp"
	if err := os.WriteFile(tmp, encoded, 0o600); err != nil {
		return fmt.Errorf("writing %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replacing %s: %w", path, err)
	}
	return nil
}

// infoCache reaches profile.info_cache, creating the intermediate maps when
// Local State has never held a profile list.
func infoCache(root map[string]any) map[string]any {
	profile, ok := root["profile"].(map[string]any)
	if !ok {
		profile = map[string]any{}
		root["profile"] = profile
	}
	cache, ok := profile["info_cache"].(map[string]any)
	if !ok {
		cache = map[string]any{}
		profile["info_cache"] = cache
	}
	return cache
}

// sortedProfileKeys keeps creation deterministic, so a failure part-way
// through leaves the same profiles created on every run.
func sortedProfileKeys(m map[string]apps.StateProfileInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// raiseCreatedPast keeps profiles_created above the index in "Profile N", so
// the next profile Chromium names cannot collide with a directory this import
// just created. The counter only ever rises: it records how many profiles this
// machine has made, and walking it backwards would reissue names already on
// disk here. A directory with no index ("Default") has no effect.
func raiseCreatedPast(root map[string]any, dir string) {
	rest, ok := strings.CutPrefix(dir, "Profile ")
	if !ok {
		return
	}
	n, err := strconv.Atoi(rest)
	if err != nil {
		return
	}
	profile, ok := root["profile"].(map[string]any)
	if !ok {
		return
	}
	current, _ := profile["profiles_created"].(float64)
	if float64(n+1) > current {
		profile["profiles_created"] = float64(n + 1)
	}
}

// RegistryPaths is the one file EnsureProfiles writes, so the caller can back
// it up before the first write and roll it back with everything else.
func (b *Brave) RegistryPaths() []string {
	return []string{filepath.Join(b.dataDir(), localStateFile)}
}

// Brave satisfies the optional registrar contract. Asserted here rather than
// in a test so a signature drift fails the build.
var _ apps.StateProfileRegistrar = (*Brave)(nil)

// appendToOrder adds dir to profiles_order when that key is present. Chromium
// shows only the profiles the order lists once it exists, so a new profile
// left out of it is registered but invisible.
//
// An absent key is left absent on purpose: Chromium writes it only once a
// user has arranged their profiles, so creating one here would invent a
// preference the user never set.
func appendToOrder(root map[string]any, dir string) {
	profile, ok := root["profile"].(map[string]any)
	if !ok {
		return
	}
	order, ok := profile["profiles_order"].([]any)
	if !ok {
		return
	}
	for _, existing := range order {
		if name, _ := existing.(string); name == dir {
			return
		}
	}
	profile["profiles_order"] = append(order, dir)
}
