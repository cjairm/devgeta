package archive

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestManifestRoundTripsPlainPaths(t *testing.T) {
	entries := []ManifestEntry{
		{Hash: strings.Repeat("a", 64), Path: "docs/readme.txt"},
		{Hash: strings.Repeat("b", 64), Path: "a b/c.txt"},
	}
	var buf bytes.Buffer
	if err := WriteManifest(&buf, entries); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}

	got, err := ReadManifest(&buf)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round trip = %+v, want %+v", got, entries)
	}
}

func TestManifestRoundTripsPathWithBackslash(t *testing.T) {
	entries := []ManifestEntry{{Hash: strings.Repeat("c", 64), Path: `weird\name.txt`}}
	var buf bytes.Buffer
	if err := WriteManifest(&buf, entries); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "\\") {
		t.Fatalf("expected escaped line to start with backslash, got %q", buf.String())
	}

	got, err := ReadManifest(&buf)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round trip = %+v, want %+v", got, entries)
	}
}

func TestManifestRoundTripsPathWithNewline(t *testing.T) {
	entries := []ManifestEntry{{Hash: strings.Repeat("d", 64), Path: "weird\nname.txt"}}
	var buf bytes.Buffer
	if err := WriteManifest(&buf, entries); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "\\") {
		t.Fatalf("expected escaped line to start with backslash, got %q", buf.String())
	}
	// The manifest must still be exactly one physical line despite the
	// literal newline in the path, or ReadManifest would split it in two.
	if strings.Count(buf.String(), "\n") != 1 {
		t.Fatalf("expected exactly one newline (the line terminator), got %q", buf.String())
	}

	got, err := ReadManifest(&buf)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round trip = %+v, want %+v", got, entries)
	}
}

func TestManifestRoundTripsMultipleEntries(t *testing.T) {
	entries := []ManifestEntry{
		{Hash: strings.Repeat("1", 64), Path: "one.txt"},
		{Hash: strings.Repeat("2", 64), Path: `two\slash.txt`},
		{Hash: strings.Repeat("3", 64), Path: "three.txt"},
	}
	var buf bytes.Buffer
	if err := WriteManifest(&buf, entries); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	got, err := ReadManifest(&buf)
	if err != nil {
		t.Fatalf("ReadManifest: %v", err)
	}
	if !reflect.DeepEqual(got, entries) {
		t.Errorf("round trip = %+v, want %+v", got, entries)
	}
}

func TestReadManifestRejectsMalformedLine(t *testing.T) {
	_, err := ReadManifest(strings.NewReader("not-a-valid-manifest-line\n"))
	if err == nil {
		t.Fatal("expected an error for a line with no hash/path separator")
	}
}
