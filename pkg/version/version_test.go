package version

import "testing"

func TestFind(t *testing.T) {
	cases := map[string]string{
		"opencode v2.2.6":          "v2.2.6",
		"2.1.319 (Claude Code)":    "2.1.319",
		"pi 1.5.0\n":               "1.5.0",
		"NVIM v0.11.1\nBuild type": "v0.11.1",
		"no version here":          "",
		"build 42":                 "", // a bare number is not a version
		"":                         "",
	}
	for output, want := range cases {
		got, ok := Find(output)
		if got != want || ok != (want != "") {
			t.Errorf("Find(%q) = %q, %v; want %q", output, got, ok, want)
		}
	}
}

func TestAtLeast(t *testing.T) {
	cases := []struct {
		current, required string
		want              bool
	}{
		{"2.2.6", "2.2.6", true},
		{"2.2.7", "2.2.6", true},
		{"2.10.0", "2.9.9", true},
		{"2.0.0", "1.9.0", true}, // the first differing part decides
		{"1.19.0", "2.2.6", false},
		{"2.2.5", "2.2.6", false},
		{"v2.2.6", "2.2.6", true},
		{"2.2", "2.2.0", true},
		{"2.2", "2.2.1", false},
		{"0.11.1", "0.11.1", true},
		{"2.2.6-beta.1", "2.2.6", true},
		{"", "2.2.6", false},
		{"garbage", "2.2.6", false},
		{"2.x.6", "2.2.6", false},
		{"2.2.6", "", false},
	}
	for _, tc := range cases {
		if got := AtLeast(tc.current, tc.required); got != tc.want {
			t.Errorf("AtLeast(%q, %q) = %v, want %v", tc.current, tc.required, got, tc.want)
		}
	}
}
