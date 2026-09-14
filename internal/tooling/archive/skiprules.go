// Skip rules for `dg archive`, per ADR-0041: a directory or file is skipped
// only when a rule below finds proof a tool generated it and can generate it
// again. Anything a rule does not match is archived — when in doubt, keep it.
package archive

import (
	"os"
	"path/filepath"
	"strings"
)

const cachedirTagSignature = "Signature: 8a477f597d28d172789f06886806bc55"

var appleDoubleMagic = [4]byte{0x00, 0x05, 0x16, 0x07}

// unambiguousNames are always skipped: no tool or person uses these names for
// anything but a cache (ADR-0041).
var unambiguousNames = map[string]string{
	"__pycache__":   "__pycache__ (cache)",
	".pytest_cache": ".pytest_cache (cache)",
	".mypy_cache":   ".mypy_cache (cache)",
	".ruff_cache":   ".ruff_cache (cache)",
	".DS_Store":     ".DS_Store (Finder metadata)",
}

// manifestRule skips a directory whose name is in names only when proof
// finds, next to it, the file that drives the tool which generates it.
type manifestRule struct {
	names []string
	label string
	proof func(parentDir, entryPath string) bool
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func anyFileExists(parentDir string, names ...string) bool {
	for _, n := range names {
		if fileExists(filepath.Join(parentDir, n)) {
			return true
		}
	}
	return false
}

var manifestRules = []manifestRule{
	{
		names: []string{"node_modules", ".next", ".nuxt", ".svelte-kit", ".turbo", ".parcel-cache"},
		label: "package.json",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "package.json")
		},
	},
	{
		names: []string{"target"},
		label: "Cargo.toml",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "Cargo.toml")
		},
	},
	{
		names: []string{"vendor"},
		label: "composer.json (Go vendor/modules.txt absent)",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "composer.json") &&
				!fileExists(filepath.Join(entryPath, "modules.txt"))
		},
	},
	{
		names: []string{"Pods"},
		label: "Podfile",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "Podfile")
		},
	},
	{
		names: []string{".gradle"},
		label: "settings.gradle(.kts) or build.gradle(.kts)",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(
				parentDir,
				"settings.gradle",
				"settings.gradle.kts",
				"build.gradle",
				"build.gradle.kts",
			)
		},
	},
	{
		names: []string{".dart_tool"},
		label: "pubspec.yaml",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "pubspec.yaml")
		},
	},
	{
		names: []string{"_build", "deps"},
		label: "mix.exs",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "mix.exs")
		},
	},
	{
		names: []string{".zig-cache", "zig-cache"},
		label: "build.zig",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "build.zig")
		},
	},
	{
		names: []string{".tox"},
		label: "tox.ini, setup.cfg or pyproject.toml",
		proof: func(parentDir, entryPath string) bool {
			return anyFileExists(parentDir, "tox.ini", "setup.cfg", "pyproject.toml")
		},
	},
	{
		names: []string{".terraform"},
		label: "a *.tf file",
		proof: func(parentDir, entryPath string) bool {
			matches, err := filepath.Glob(filepath.Join(parentDir, "*.tf"))
			return err == nil && len(matches) > 0
		},
	},
}

// hasCachedirTag reports whether dir contains a CACHEDIR.TAG file whose first
// bytes are exactly the standard signature (https://bford.info/cachedir/).
func hasCachedirTag(dir string) bool {
	data, err := os.ReadFile(filepath.Join(dir, "CACHEDIR.TAG"))
	if err != nil || len(data) < len(cachedirTagSignature) {
		return false
	}
	return string(data[:len(cachedirTagSignature)]) == cachedirTagSignature
}

// isAppleDouble reports whether path is a regular file starting with the
// AppleDouble magic number 0x00051607.
func isAppleDouble(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }()

	var buf [4]byte
	n, err := f.Read(buf[:])
	if err != nil || n < len(buf) {
		return false
	}
	return buf == appleDoubleMagic
}

// Match reports whether path should be skipped per ADR-0041, and the rule
// that matched. Every rule requires proof next to or inside the entry, so an
// I/O error while looking for that proof is treated as "no proof found" —
// when in doubt, Match keeps the entry.
func Match(path string) (rule string, skip bool) {
	name := filepath.Base(path)

	if label, ok := unambiguousNames[name]; ok {
		return label, true
	}

	info, err := os.Lstat(path)
	if err != nil {
		return "", false
	}

	if !info.IsDir() {
		if strings.HasPrefix(name, "._") && isAppleDouble(path) {
			return "AppleDouble metadata", true
		}
		return "", false
	}

	if hasCachedirTag(path) {
		return "CACHEDIR.TAG", true
	}
	if fileExists(filepath.Join(path, "pyvenv.cfg")) {
		return "pyvenv.cfg (virtualenv)", true
	}

	parentDir := filepath.Dir(path)
	for _, r := range manifestRules {
		for _, n := range r.names {
			if n == name && r.proof(parentDir, path) {
				return name + " (" + r.label + ")", true
			}
		}
	}
	return "", false
}
