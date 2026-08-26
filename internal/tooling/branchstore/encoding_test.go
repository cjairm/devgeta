package branchstore

import "testing"

func TestEncodeNameRoundTripsThroughDecode(t *testing.T) {
	cases := []string{
		"feat",
		"feat/login",
		"feat/a/b",
		"weird%name",
		"has spaces",
		"../../escape",
	}
	for _, name := range cases {
		encoded := EncodeName(name)
		decoded, err := DecodeName(encoded)
		if err != nil {
			t.Errorf("DecodeName(%q): %v", encoded, err)
			continue
		}
		if decoded != name {
			t.Errorf("round trip %q -> %q -> %q", name, encoded, decoded)
		}
	}
}

func TestEncodeNameNeverEmitsAPathSeparator(t *testing.T) {
	if got := EncodeName("../../etc/passwd"); got == ".." || len(got) == 0 {
		t.Fatalf("EncodeName produced a suspicious result: %q", got)
	}
	for _, r := range EncodeName("a/b\\c") {
		if r == '/' || r == '\\' {
			t.Fatalf("EncodeName leaked a path separator: %q", EncodeName("a/b\\c"))
		}
	}
}

func TestDecodeNameRejectsUnencodedByte(t *testing.T) {
	if _, err := DecodeName("has space"); err == nil {
		t.Fatal("expected an error decoding an unencoded byte")
	}
}

func TestDecodeNameRejectsTruncatedEscape(t *testing.T) {
	if _, err := DecodeName("trunc%2"); err == nil {
		t.Fatal("expected an error decoding a truncated escape")
	}
}
