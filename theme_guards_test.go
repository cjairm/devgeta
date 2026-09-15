package main

import (
	"encoding/base64"
	"io/fs"
	"net/http"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"gopkg.in/yaml.v3"
)

// rasterExtensions catches an image committed under a name whose bytes don't
// sniff as one (a truncated or empty file, or a format outside Go's sniff
// table) - a cheap second net, not the primary check. See
// checkNoEmbeddedImageBytes's doc comment for why the content sniff is what
// actually makes ADR-0044 point 2 structural.
var rasterExtensions = []string{
	".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".ico", ".tiff", ".heic", ".avif",
}

// shippedWallpapersDir is the ONE directory under configs/ an image may live
// in. Confining the exception to a single directory is what keeps it from
// becoming "images are fine anywhere now": an image added under, say,
// configs/claude/ still fails the guard even if someone adds it to the
// allowlist below.
const shippedWallpapersDir = "configs/themes/wallpapers/"

// shippedWallpapers is the explicit allowlist of image files devgeta ships,
// per ADR-0044's 2026-09-14 amendment (the maintainer reversed the original
// "never ships one" rule so a fresh install carries the themes' wallpapers).
// Everything NOT listed here is still rejected, so the guard keeps doing its
// original job - catching an image that lands under configs/ by accident -
// while a deliberate addition has to be written down here, where a reviewer
// sees it.
//
// Adding an entry is a decision, not a formality: it puts the file in every
// release binary and redistributes it to every person who installs devgeta.
// Confirm the project has the right to do that for the image in question.
var shippedWallpapers = map[string]bool{
	"configs/themes/wallpapers/default.jpg":    true,
	"configs/themes/wallpapers/tokyonight.jpg": true,
}

// checkNoEmbeddedImageBytes walks fsys and returns every path that is
// either sniffed as an image by its leading bytes (http.DetectContentType,
// stdlib - covers PNG, JPEG, GIF, WebP, BMP, ICO) or carries a raster
// extension regardless of what its bytes sniff as. Takes an fs.FS rather
// than reading ConfigsFS directly so a test can prove the check actually
// flags something, against a fixture the production call never enounters.
func checkNoEmbeddedImageBytes(fsys fs.FS) ([]string, error) {
	var offenders []string
	err := fs.WalkDir(fsys, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			return err
		}

		flagged := false
		head := data
		if len(head) > 512 {
			head = head[:512]
		}
		if strings.HasPrefix(http.DetectContentType(head), "image/") {
			flagged = true
		}
		lower := strings.ToLower(path)
		for _, ext := range rasterExtensions {
			if strings.HasSuffix(lower, ext) {
				flagged = true
				break
			}
		}
		if flagged {
			offenders = append(offenders, path)
		}
		return nil
	})
	return offenders, err
}

// TestEmbeddedConfigsShipOnlyAllowlistedImages is the guard ADR-0044 rests
// on after its 2026-09-14 amendment: devgeta ships image bytes ONLY for the
// wallpapers named in shippedWallpapers, and nothing else under configs/
// may be an image. The original rule was "no images at all"; relaxing it to
// an allowlist keeps the part that was actually doing the work — an image
// landing under configs/ by accident (a screenshot dropped next to a config,
// a logo added to a skill) still fails the build.
func TestEmbeddedConfigsShipOnlyAllowlistedImages(t *testing.T) {
	found, err := checkNoEmbeddedImageBytes(ConfigsFS)
	if err != nil {
		t.Fatalf("failed to walk embedded configs: %v", err)
	}

	var offenders []string
	for _, p := range found {
		if !shippedWallpapers[p] {
			offenders = append(offenders, p)
		}
	}
	if len(offenders) > 0 {
		t.Errorf(
			"found image file(s) under configs/ that are not in shippedWallpapers: %v\n"+
				"Shipping an image puts it in every release binary and redistributes it to "+
				"every person who installs devgeta. If that is intended, add it to the "+
				"allowlist in this file; if not, remove the file.",
			offenders,
		)
	}
}

// TestShippedWallpaperAllowlistHasNoStaleEntries stops the allowlist from
// rotting: an entry naming a file that no longer exists is a permission
// granted to nothing, and the next person to read it would reasonably
// assume the file is still shipped.
func TestShippedWallpaperAllowlistHasNoStaleEntries(t *testing.T) {
	for p := range shippedWallpapers {
		if _, err := fs.Stat(ConfigsFS, p); err != nil {
			t.Errorf("shippedWallpapers names %q, which is not in ConfigsFS: %v", p, err)
		}
	}
}

// TestShippedWallpaperAllowlistIsConfinedToTheWallpapersDir keeps the
// exception narrow. Without this, the allowlist is a general-purpose
// "ship any image from anywhere" escape hatch, and the next image lands
// wherever was convenient rather than in the one directory that is
// obviously about wallpapers.
func TestShippedWallpaperAllowlistIsConfinedToTheWallpapersDir(t *testing.T) {
	for p := range shippedWallpapers {
		if !strings.HasPrefix(p, shippedWallpapersDir) {
			t.Errorf(
				"shippedWallpapers names %q, which is outside %s — an image may only ship from there",
				p,
				shippedWallpapersDir,
			)
		}
	}
}

// TestShippedThemesWallpapersResolveToShippedFiles closes the other half of
// the loop: a theme file naming a relative wallpaper must name one that is
// actually shipped. Without this, a typo in a theme's wallpaper: key fails
// silently at runtime on every machine (the setter is best-effort, so it
// warns into a log nobody reads) rather than failing here once.
func TestShippedThemesWallpapersResolveToShippedFiles(t *testing.T) {
	entries, err := fs.ReadDir(ConfigsFS, "configs/themes")
	if err != nil {
		t.Fatalf("failed to read configs/themes: %v", err)
	}

	checked := 0
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		data, err := fs.ReadFile(ConfigsFS, "configs/themes/"+e.Name())
		if err != nil {
			t.Fatalf("failed to read %s: %v", e.Name(), err)
		}
		var raw struct {
			Wallpaper string `yaml:"wallpaper"`
		}
		if err := yaml.Unmarshal(data, &raw); err != nil {
			t.Fatalf("failed to parse %s: %v", e.Name(), err)
		}
		if raw.Wallpaper == "" || strings.HasPrefix(raw.Wallpaper, "/") {
			continue // no wallpaper, or one on the user's own machine
		}
		checked++
		shipped := "configs/themes/" + raw.Wallpaper
		if _, err := fs.Stat(ConfigsFS, shipped); err != nil {
			t.Errorf(
				"%s declares wallpaper %q, which resolves to %q and is not shipped: %v",
				e.Name(), raw.Wallpaper, shipped, err,
			)
		}
	}

	if checked == 0 {
		t.Fatal(
			"no shipped theme declares a relative wallpaper — this test no longer " +
				"checks anything and should be removed or fixed",
		)
	}
}

// tiny1x1PNGBase64 is a real, valid 1x1 transparent PNG's bytes.
const tiny1x1PNGBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAAAAAA6fptVAAAACklEQVR4nGNgAAIAAAUAAen63NgAAAAASUVORK5CYII="

// TestCheckNoEmbeddedImageBytes_CatchesImageRenamedWithATextExtension
// proves the guard bites: a real PNG's bytes committed under a name with no
// raster extension at all must still be flagged. This is the exact case the
// extension-only net would miss, which is why the sniff runs first.
func TestCheckNoEmbeddedImageBytes_CatchesImageRenamedWithATextExtension(t *testing.T) {
	png, err := base64.StdEncoding.DecodeString(tiny1x1PNGBase64)
	if err != nil {
		t.Fatalf("failed to decode fixture PNG: %v", err)
	}
	fsys := fstest.MapFS{
		"configs/themes/notes.txt": &fstest.MapFile{Data: png},
	}

	offenders, err := checkNoEmbeddedImageBytes(fsys)
	if err != nil {
		t.Fatalf("checkNoEmbeddedImageBytes error: %v", err)
	}
	if len(offenders) != 1 || offenders[0] != "configs/themes/notes.txt" {
		t.Errorf("expected the renamed PNG to be flagged, got %v", offenders)
	}
}

// TestCheckNoEmbeddedImageBytes_TextFilesPass proves the guard is not
// vacuously true - ordinary text and JSON files must not be flagged.
func TestCheckNoEmbeddedImageBytes_TextFilesPass(t *testing.T) {
	fsys := fstest.MapFS{
		"configs/themes/default.yaml": &fstest.MapFile{
			Data: []byte("colors:\n  red: \"#fb4934\"\n"),
		},
		"configs/opencode/opencode.json.tmpl": &fstest.MapFile{
			Data: []byte(`{"theme": "{{ .Theme }}"}`),
		},
	}

	offenders, err := checkNoEmbeddedImageBytes(fsys)
	if err != nil {
		t.Fatalf("checkNoEmbeddedImageBytes error: %v", err)
	}
	if len(offenders) != 0 {
		t.Errorf("expected no offenders among plain text files, got %v", offenders)
	}
}

// darkColorFieldPattern matches a `"dark": "<value>"` entry in an OpenCode
// theme JSON template's `theme` block - the shape every raw-color role in
// that block takes (ADR-0043's "OpenCode's theme block has to be cleaned up
// first" section). "light" entries are the recorded, deliberate exception
// and are not matched by this pattern at all - exempted by field name, not
// by a pattern that could silently start matching them too.
var darkColorFieldPattern = regexp.MustCompile(`"dark":\s*"([^"]+)"`)

// TestEmbeddedOpenCodeThemeHasNoLiteralDarkColor asserts no `dark` entry in
// the shipped OpenCode theme's `theme` block is a literal color: every one
// must reference a `defs` key instead, so a new theme's palette actually
// reaches every rendered color (ADR-0043). A comment would not survive the
// next hand-edit - this makes the mistake structurally visible instead.
func TestEmbeddedOpenCodeThemeHasNoLiteralDarkColor(t *testing.T) {
	data, err := fs.ReadFile(ConfigsFS, "configs/opencode/themes/default.json.tmpl")
	if err != nil {
		t.Fatalf("failed to read embedded opencode theme template: %v", err)
	}

	matches := darkColorFieldPattern.FindAllStringSubmatch(string(data), -1)
	if len(matches) == 0 {
		t.Fatal(
			"no \"dark\": \"...\" entries found — the pattern no longer matches this file's shape",
		)
	}
	for _, m := range matches {
		if strings.HasPrefix(m[1], "#") {
			t.Errorf(
				"theme block has a literal dark color %q; every dark value must reference a defs key (ADR-0043)",
				m[1],
			)
		}
	}
}
