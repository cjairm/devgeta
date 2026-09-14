// Windows compatibility checks for `dg archive`. These never rename or skip
// a file — CLAUDE.md and the cycle doc both require warning, not altering the
// user's files — they only report why a name would fail to extract on
// Windows (learn.microsoft.com/en-us/windows/win32/fileio/naming-a-file).
package archive

import (
	"sort"
	"strings"
)

const windowsReservedChars = `<>:"/\|?*`

var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true, "COM5": true,
	"COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true, "LPT5": true,
	"LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// WindowsIncompatibleReason reports why name cannot be created as a file or
// directory name on Windows, or "" if it can.
func WindowsIncompatibleReason(name string) string {
	if name == "" {
		return ""
	}
	if strings.ContainsAny(name, windowsReservedChars) {
		return "contains a character Windows forbids in names (" + windowsReservedChars + ")"
	}
	switch name[len(name)-1] {
	case '.', ' ':
		return "ends with a dot or space, which Windows strips or rejects"
	}
	base := name
	if i := strings.IndexByte(name, '.'); i >= 0 {
		base = name[:i]
	}
	if windowsReservedNames[strings.ToUpper(base)] {
		return "is a reserved device name on Windows (" + strings.ToUpper(base) + ")"
	}
	return ""
}

// CaseInsensitiveCollisions groups names that differ only by case — Windows
// and macOS's default filesystem treat them as the same file, so extracting
// the archive there would silently drop all but one.
func CaseInsensitiveCollisions(names []string) [][]string {
	groups := map[string][]string{}
	var keys []string
	for _, n := range names {
		key := strings.ToLower(n)
		if _, ok := groups[key]; !ok {
			keys = append(keys, key)
		}
		groups[key] = append(groups[key], n)
	}
	sort.Strings(keys)

	var collisions [][]string
	for _, key := range keys {
		if len(groups[key]) > 1 {
			collisions = append(collisions, groups[key])
		}
	}
	return collisions
}
