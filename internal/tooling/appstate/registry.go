// The profile registry sidecar: the file that carries a profile's directory,
// display name and avatar to the new machine (ADR-0046).
//
// It is beside the bundle rather than inside it, for the same reason the
// checksum manifest and the skip report are: that is the shape an export
// already writes, and an import already refuses a bundle whose manifest
// sidecar is missing, so these files travelling together is today's contract.
package appstate

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
)

// RegistryExt is the sidecar's extension, replacing BundleExt on the bundle's
// own name. One constant, derived in both directions, so the writer and the
// reader cannot drift apart.
const RegistryExt = ".profiles.json"

// registryFile is the sidecar's on-disk shape. A list rather than a map so the
// file reads in a stable order, and three fields per entry and no more — a
// registration is rebuilt from these, never transplanted (ADR-0046 §1).
type registryFile struct {
	Profiles []registryEntry `json:"profiles"`
}

type registryEntry struct {
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}

// RegistrySidecarPath is where the sidecar for bundlePath lives.
func RegistrySidecarPath(bundlePath string) string {
	return strings.TrimSuffix(bundlePath, BundleExt) + RegistryExt
}

// EncodeRegistry renders the registry for the sidecar. JSON rather than a line
// format because a profile name is free text a user typed: it can hold a
// quote, a tab or a newline, and the name is the only thing identifying the
// profile on the other machine.
func EncodeRegistry(profiles map[string]apps.StateProfileInfo) ([]byte, error) {
	file := registryFile{}
	for _, dir := range sortedProfileDirs(profiles) {
		info := profiles[dir]
		file.Profiles = append(file.Profiles, registryEntry{
			Dir:    dir,
			Name:   info.Name,
			Avatar: info.Avatar,
		})
	}
	encoded, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("encoding the profile registry: %w", err)
	}
	return append(encoded, '\n'), nil
}

// DecodeRegistry parses the sidecar. A file that does not parse is an error
// rather than an empty registry: proceeding would create every profile
// unnamed, which is the outcome the sidecar exists to prevent.
func DecodeRegistry(raw []byte) (map[string]apps.StateProfileInfo, error) {
	var file registryFile
	if err := json.Unmarshal(raw, &file); err != nil {
		return nil, fmt.Errorf("reading the profile registry: %w", err)
	}
	out := map[string]apps.StateProfileInfo{}
	for _, entry := range file.Profiles {
		out[entry.Dir] = apps.StateProfileInfo{
			Dir:    entry.Dir,
			Name:   entry.Name,
			Avatar: entry.Avatar,
		}
	}
	return out, nil
}

// sortedProfileDirs keeps the sidecar's order stable across runs, so two
// exports of an unchanged profile set produce an identical file.
func sortedProfileDirs(m map[string]apps.StateProfileInfo) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
