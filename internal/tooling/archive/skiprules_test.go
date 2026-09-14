package archive

import (
	"os"
	"path/filepath"
	"testing"
)

func writeFile(t *testing.T, path string, content []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("WriteFile(%q): %v", path, err)
	}
}

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("MkdirAll(%q): %v", path, err)
	}
}

func TestMatchCachedirTag(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "some-cache")
	mkdir(t, cacheDir)
	writeFile(
		t,
		filepath.Join(cacheDir, "CACHEDIR.TAG"),
		[]byte("Signature: 8a477f597d28d172789f06886806bc55\n# comment"),
	)

	rule, skip := Match(cacheDir)
	if !skip {
		t.Fatalf("expected %q to be skipped as a CACHEDIR.TAG directory", cacheDir)
	}
	if rule != "CACHEDIR.TAG" {
		t.Errorf("rule = %q, want %q", rule, "CACHEDIR.TAG")
	}
}

func TestMatchCachedirTagRequiresExactSignature(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "some-cache")
	mkdir(t, cacheDir)
	writeFile(t, filepath.Join(cacheDir, "CACHEDIR.TAG"), []byte("Signature: not-the-real-one"))

	if _, skip := Match(cacheDir); skip {
		t.Fatalf("expected %q to be kept: CACHEDIR.TAG signature does not match", cacheDir)
	}
}

func TestMatchPyvenvCfg(t *testing.T) {
	root := t.TempDir()
	venvDir := filepath.Join(root, "myenv")
	mkdir(t, venvDir)
	writeFile(t, filepath.Join(venvDir, "pyvenv.cfg"), []byte("home = /usr/bin"))

	rule, skip := Match(venvDir)
	if !skip {
		t.Fatalf("expected %q to be skipped as a virtualenv", venvDir)
	}
	if rule == "" {
		t.Error("expected a non-empty rule name")
	}
}

func TestMatchPyvenvCfgAbsentIsKept(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "myenv")
	mkdir(t, dir)

	if _, skip := Match(dir); skip {
		t.Fatalf("expected %q to be kept: no pyvenv.cfg present", dir)
	}
}

func TestMatchAppleDouble(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "._notes.txt")
	writeFile(
		t,
		path,
		append([]byte{0x00, 0x05, 0x16, 0x07}, []byte("rest of appledouble data")...),
	)

	rule, skip := Match(path)
	if !skip {
		t.Fatalf("expected %q to be skipped as an AppleDouble file", path)
	}
	if rule == "" {
		t.Error("expected a non-empty rule name")
	}
}

func TestMatchAppleDoubleRequiresMagicBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "._notes.txt")
	writeFile(
		t,
		path,
		[]byte("just a regular file that happens to start with an underscore-dot name"),
	)

	if _, skip := Match(path); skip {
		t.Fatalf("expected %q to be kept: no AppleDouble magic bytes", path)
	}
}

func TestMatchUnambiguousNames(t *testing.T) {
	names := []string{"__pycache__", ".pytest_cache", ".mypy_cache", ".ruff_cache", ".DS_Store"}
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, name)
			if name == ".DS_Store" {
				writeFile(t, path, []byte("binary junk"))
			} else {
				mkdir(t, path)
			}
			if _, skip := Match(path); !skip {
				t.Fatalf("expected %q to always be skipped", path)
			}
		})
	}
}

func TestMatchManifestRules(t *testing.T) {
	cases := []struct {
		name       string
		entryName  string
		proofFiles []string // files to create in the parent, relative to parent
		absentFile string   // file that must be absent inside the entry itself (vendor/modules.txt case)
	}{
		{
			name:       "node_modules with package.json",
			entryName:  "node_modules",
			proofFiles: []string{"package.json"},
		},
		{name: ".next with package.json", entryName: ".next", proofFiles: []string{"package.json"}},
		{name: ".nuxt with package.json", entryName: ".nuxt", proofFiles: []string{"package.json"}},
		{
			name:       ".svelte-kit with package.json",
			entryName:  ".svelte-kit",
			proofFiles: []string{"package.json"},
		},
		{
			name:       ".turbo with package.json",
			entryName:  ".turbo",
			proofFiles: []string{"package.json"},
		},
		{
			name:       ".parcel-cache with package.json",
			entryName:  ".parcel-cache",
			proofFiles: []string{"package.json"},
		},
		{name: "target with Cargo.toml", entryName: "target", proofFiles: []string{"Cargo.toml"}},
		{name: "Pods with Podfile", entryName: "Pods", proofFiles: []string{"Podfile"}},
		{
			name:       ".gradle with settings.gradle",
			entryName:  ".gradle",
			proofFiles: []string{"settings.gradle"},
		},
		{
			name:       ".gradle with settings.gradle.kts",
			entryName:  ".gradle",
			proofFiles: []string{"settings.gradle.kts"},
		},
		{
			name:       ".gradle with build.gradle",
			entryName:  ".gradle",
			proofFiles: []string{"build.gradle"},
		},
		{
			name:       ".gradle with build.gradle.kts",
			entryName:  ".gradle",
			proofFiles: []string{"build.gradle.kts"},
		},
		{
			name:       ".dart_tool with pubspec.yaml",
			entryName:  ".dart_tool",
			proofFiles: []string{"pubspec.yaml"},
		},
		{name: "_build with mix.exs", entryName: "_build", proofFiles: []string{"mix.exs"}},
		{name: "deps with mix.exs", entryName: "deps", proofFiles: []string{"mix.exs"}},
		{
			name:       ".zig-cache with build.zig",
			entryName:  ".zig-cache",
			proofFiles: []string{"build.zig"},
		},
		{
			name:       "zig-cache with build.zig",
			entryName:  "zig-cache",
			proofFiles: []string{"build.zig"},
		},
		{name: ".tox with tox.ini", entryName: ".tox", proofFiles: []string{"tox.ini"}},
		{name: ".tox with setup.cfg", entryName: ".tox", proofFiles: []string{"setup.cfg"}},
		{
			name:       ".tox with pyproject.toml",
			entryName:  ".tox",
			proofFiles: []string{"pyproject.toml"},
		},
		{
			name:       ".terraform with a .tf file",
			entryName:  ".terraform",
			proofFiles: []string{"main.tf"},
		},
		{
			name:       "vendor with composer.json, no modules.txt",
			entryName:  "vendor",
			proofFiles: []string{"composer.json"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name+"/skipped", func(t *testing.T) {
			root := t.TempDir()
			entry := filepath.Join(root, tc.entryName)
			mkdir(t, entry)
			for _, f := range tc.proofFiles {
				writeFile(t, filepath.Join(root, f), []byte("proof"))
			}
			rule, skip := Match(entry)
			if !skip {
				t.Fatalf("expected %q to be skipped given proof %v", entry, tc.proofFiles)
			}
			if rule == "" {
				t.Error("expected a non-empty rule name")
			}
		})

		t.Run(tc.name+"/kept without proof", func(t *testing.T) {
			root := t.TempDir()
			entry := filepath.Join(root, tc.entryName)
			mkdir(t, entry)
			// deliberately do not create the proof files
			if _, skip := Match(entry); skip {
				t.Fatalf("expected %q to be kept: no proof file present", entry)
			}
		})
	}
}

func TestMatchVendorKeptWhenModulesTxtPresent(t *testing.T) {
	root := t.TempDir()
	vendor := filepath.Join(root, "vendor")
	mkdir(t, vendor)
	writeFile(t, filepath.Join(root, "composer.json"), []byte("{}"))
	writeFile(t, filepath.Join(vendor, "modules.txt"), []byte("# Go vendor modules"))

	if _, skip := Match(vendor); skip {
		t.Fatalf(
			"expected %q to be kept: vendor/modules.txt proves this is a Go vendor dir",
			vendor,
		)
	}
}

func TestMatchNeverSkipsExplicitlyProtectedNames(t *testing.T) {
	// These names must never be skipped, even in a directory containing every
	// proof file our manifest rules look for (ADR-0041).
	protected := []string{".git", "build", "dist", "out", "coverage", "bin"}
	proofFiles := []string{
		"package.json", "Cargo.toml", "composer.json", "Podfile",
		"settings.gradle", "pubspec.yaml", "mix.exs", "build.zig",
		"tox.ini", "main.tf",
	}

	for _, name := range protected {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			entry := filepath.Join(root, name)
			mkdir(t, entry)
			for _, f := range proofFiles {
				writeFile(t, filepath.Join(root, f), []byte("proof"))
			}
			if rule, skip := Match(entry); skip {
				t.Fatalf("expected %q to never be skipped, got rule %q", entry, rule)
			}
		})
	}
}

func TestMatchKeepsOrdinaryFilesAndDirs(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "my-notes")
	mkdir(t, dir)
	file := filepath.Join(root, "todo.txt")
	writeFile(t, file, []byte("buy milk"))

	if _, skip := Match(dir); skip {
		t.Fatalf("expected ordinary directory %q to be kept", dir)
	}
	if _, skip := Match(file); skip {
		t.Fatalf("expected ordinary file %q to be kept", file)
	}
}
