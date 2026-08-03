package commands

import (
	"errors"
	"testing"
)

func TestMockCommand_IsPackageInstalled_PerNameMap(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true, "tmux": false}

	ok, err := m.IsPackageInstalled("git")
	if err != nil || !ok {
		t.Errorf("git: got (%v, %v), want (true, nil)", ok, err)
	}

	ok, err = m.IsPackageInstalled("tmux")
	if err != nil || ok {
		t.Errorf("tmux: got (%v, %v), want (false, nil)", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_FallsBackToGlobalBool(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalled = true // legacy global flag, no map entry for "unmapped"

	ok, err := m.IsPackageInstalled("unmapped")
	if err != nil || !ok {
		t.Errorf("got (%v, %v), want (true, nil) via fallback", ok, err)
	}
}

func TestMockCommand_IsPackageInstalled_PerNameError(t *testing.T) {
	m := NewMockCommand()
	wantErr := errors.New("brew: command not found")
	m.PackageInstalledErrors = map[string]error{"broken": wantErr}

	ok, err := m.IsPackageInstalled("broken")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_IsDesktopAppInstalled_PerNameMapAndError(t *testing.T) {
	m := NewMockCommand()
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	wantErr := errors.New("dpkg: not found")
	m.DesktopAppInstalledErrors = map[string]error{"broken-app": wantErr}

	if ok, err := m.IsDesktopAppInstalled("docker"); err != nil || !ok {
		t.Errorf("docker: got (%v, %v), want (true, nil)", ok, err)
	}
	if ok, err := m.IsDesktopAppInstalled("broken-app"); ok || err != wantErr {
		t.Errorf("broken-app: got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

func TestMockCommand_Reset_ClearsPerNameMaps(t *testing.T) {
	m := NewMockCommand()
	m.PackageInstalledMap = map[string]bool{"git": true}
	m.DesktopAppInstalledMap = map[string]bool{"docker": true}
	m.PackageInstalledErrors = map[string]error{"x": errors.New("boom")}
	m.DesktopAppInstalledErrors = map[string]error{"y": errors.New("boom")}

	m.Reset()

	if len(m.PackageInstalledMap) != 0 || len(m.DesktopAppInstalledMap) != 0 ||
		len(m.PackageInstalledErrors) != 0 || len(m.DesktopAppInstalledErrors) != 0 {
		t.Error("Reset should clear all per-name maps")
	}
}

func TestMockBaseCommand_IsFontPresent_Error(t *testing.T) {
	m := NewMockBaseCommand()
	wantErr := errors.New("fc-list: not found")
	m.IsFontPresentResult = false
	m.IsFontPresentError = wantErr

	ok, err := m.IsFontPresent("JetBrainsMono")
	if ok || err != wantErr {
		t.Errorf("got (%v, %v), want (false, %v)", ok, err, wantErr)
	}
}

// TestResetExecCommandClearsExecCommandFn pins ResetExecCommand's contract:
// after a reset the mock must answer from whatever the NEXT test configures,
// never from the previous one.
//
// ExecCommandFn takes precedence over execResults and the fixed
// stdout/stderr/error fields, so a reset that clears those but leaves the
// callback in place is worse than no reset at all — the next subtest's setup
// is accepted silently and then ignored, and its assertions fail for reasons
// that have nothing to do with the code under test.
func TestResetExecCommandClearsExecCommandFn(t *testing.T) {
	mock := NewMockBaseCommand()
	mock.ExecCommandFn = func(CommandParams) (string, string, error) {
		return "from the stale callback", "", nil
	}

	mock.ResetExecCommand()

	if mock.ExecCommandFn != nil {
		t.Fatal("ResetExecCommand left ExecCommandFn set; a reused mock would keep " +
			"answering from the previous subtest's callback")
	}

	// The reset mock must now honor a freshly configured result.
	mock.SetExecCommandResult("fresh", "", nil)
	stdout, _, err := mock.ExecCommand(CommandParams{Command: "git"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stdout != "fresh" {
		t.Errorf("ExecCommand returned %q; the stale callback is still winning", stdout)
	}
}
