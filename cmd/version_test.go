/*
 * Copyright © 2025 Carlos Mendez <carlos@hadaelectronics.com> | https://cjairm.me/
 */
package cmd

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	"github.com/cjairm/devgeta/pkg/buildinfo"
)

func withVersionVars(t *testing.T, version, commit, buildDate string) {
	t.Helper()
	origV, origC, origB := buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate
	buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate = version, commit, buildDate
	t.Cleanup(func() {
		buildinfo.Version, buildinfo.Commit, buildinfo.BuildDate = origV, origC, origB
	})
}

func withBuildInfo(t *testing.T, info *debug.BuildInfo, ok bool) {
	t.Helper()
	orig := readBuildInfo
	readBuildInfo = func() (*debug.BuildInfo, bool) { return info, ok }
	t.Cleanup(func() { readBuildInfo = orig })
}

func TestResolveVersionInfo_UsesLdflagsWhenSet(t *testing.T) {
	withVersionVars(t, "v1.2.3", "abc1234", "2026-05-14T12:00:00Z")
	withBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "v9.9.9"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeefcafebabe"},
			{Key: "vcs.time", Value: "2099-01-01T00:00:00Z"},
		},
	}, true)

	v, c, b := resolveVersionInfo()
	if v != "v1.2.3" || c != "abc1234" || b != "2026-05-14T12:00:00Z" {
		t.Fatalf("ldflag values should win, got (%q, %q, %q)", v, c, b)
	}
}

func TestResolveVersionInfo_FallsBackToBuildInfo(t *testing.T) {
	withVersionVars(t, "dev", "unknown", "unknown")
	withBuildInfo(t, &debug.BuildInfo{
		Main: debug.Module{Version: "v0.11.0"},
		Settings: []debug.BuildSetting{
			{Key: "vcs.revision", Value: "deadbeefcafebabe"},
			{Key: "vcs.time", Value: "2026-05-14T09:45:22Z"},
		},
	}, true)

	v, c, b := resolveVersionInfo()
	if v != "v0.11.0" {
		t.Errorf("version: want %q, got %q", "v0.11.0", v)
	}
	if c != "deadbee" {
		t.Errorf("commit: want short hash %q, got %q", "deadbee", c)
	}
	if b != "2026-05-14T09:45:22Z" {
		t.Errorf("build date: want %q, got %q", "2026-05-14T09:45:22Z", b)
	}
}

func TestResolveVersionInfo_IgnoresDevelMainVersion(t *testing.T) {
	withVersionVars(t, "dev", "unknown", "unknown")
	withBuildInfo(t, &debug.BuildInfo{
		Main:     debug.Module{Version: "(devel)"},
		Settings: nil,
	}, true)

	v, _, _ := resolveVersionInfo()
	if v != "dev" {
		t.Errorf("(devel) should not override default, got %q", v)
	}
}

func TestResolveVersionInfo_NoBuildInfo(t *testing.T) {
	withVersionVars(t, "dev", "unknown", "unknown")
	withBuildInfo(t, nil, false)

	v, c, b := resolveVersionInfo()
	if v != "dev" || c != "unknown" || b != "unknown" {
		t.Fatalf("with no BuildInfo, defaults should remain, got (%q, %q, %q)", v, c, b)
	}
}

func TestResolveVersionInfo_ShortCommitForShortValue(t *testing.T) {
	withVersionVars(t, "dev", "unknown", "unknown")
	withBuildInfo(t, &debug.BuildInfo{
		Settings: []debug.BuildSetting{{Key: "vcs.revision", Value: "abc"}},
	}, true)

	_, c, _ := resolveVersionInfo()
	if c != "abc" {
		t.Errorf("short revisions should be passed through, got %q", c)
	}
}

func TestPrintVersion_Format(t *testing.T) {
	withVersionVars(t, "v0.10.3", "abc1234", "2026-05-14T12:00:00Z")
	withBuildInfo(t, nil, false)

	var buf bytes.Buffer
	printVersion(&buf)

	want := "devgeta v0.10.3 (commit: abc1234, built: 2026-05-14T12:00:00Z)\n"
	if buf.String() != want {
		t.Errorf("output mismatch:\n  want: %q\n  got:  %q", want, buf.String())
	}
}

func TestVersionCmd_WritesToCobraOutput(t *testing.T) {
	withVersionVars(t, "v0.10.3", "abc1234", "2026-05-14T12:00:00Z")
	withBuildInfo(t, nil, false)

	var buf bytes.Buffer
	versionCmd.SetOut(&buf)
	t.Cleanup(func() { versionCmd.SetOut(nil) })

	if err := versionCmd.RunE(versionCmd, nil); err != nil {
		t.Fatalf("RunE returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "devgeta v0.10.3") {
		t.Errorf("expected output to contain version, got %q", buf.String())
	}
}
