package appstate

import (
	"strings"
	"testing"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/registry"
	"github.com/cjairm/devgeta/internal/testutil"
)

func init() { testutil.InitLogger() }

func TestDeniedPath(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		denied bool
	}{
		{"login data itself", "Login Data", true},
		{"login data journal", "Login Data-journal", true},
		{"login data for account", "Login Data For Account", true},
		{"cookies", "Cookies", true},
		{"cookies journal", "Cookies-journal", true},
		{"web data", "Web Data", true},
		{"secure preferences", "Secure Preferences", true},
		{"local state", "Local State", true},
		{"affiliation database", "Affiliation Database", true},
		{"the network directory itself", "Network", true},
		{"anything under the network directory", "Network/Cookies", true},

		// Case-insensitively, because macOS filesystems are: a member named
		// "login data" lands on the real Login Data.
		{"lowercased", "login data", true},
		{"mixed case", "SeCuRe PrEfErEnCeS", true},

		// A denied element anywhere in the path, not just the last one.
		{"denied element mid-path", "Extensions/abc/Cookies", true},
		{"denied directory mid-path", "Default/Network/Cookies", true},

		{"bookmarks", "Bookmarks", false},
		{"preferences is not secure preferences", "Preferences", false},
		{"sessions", "Sessions/Session_1", false},
		{"extension state", "Extension State/000003.log", false},
		{"history", "History", false},
		// Substring matches must not count: the rule is per path element.
		{"a file merely containing a denied name", "My Cookies Export", false},
		{"local state as a substring", "NotLocal State", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule, denied := DeniedPath(tt.path)
			if denied != tt.denied {
				t.Errorf(
					"DeniedPath(%q) = %q, %v; want denied=%v",
					tt.path,
					rule,
					denied,
					tt.denied,
				)
			}
			if denied && rule == "" {
				t.Errorf("DeniedPath(%q) reported denied with an empty rule", tt.path)
			}
			if !denied && rule != "" {
				t.Errorf("DeniedPath(%q) reported not-denied but named rule %q", tt.path, rule)
			}
		})
	}
}

func TestCheckAllowlistRejectsADeniedPath(t *testing.T) {
	groups := []apps.StateGroup{
		{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true},
		{Name: "passwords", Paths: []string{"Login Data"}, Default: false},
	}

	err := CheckAllowlist("brave", groups)
	if err == nil {
		t.Fatal("expected an error for a group naming a denied path, got nil")
	}
	for _, want := range []string{"brave", "passwords", "Login Data"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not name %q", err, want)
		}
	}
}

func TestCheckAllowlistAcceptsACleanAllowlist(t *testing.T) {
	groups := []apps.StateGroup{
		{Name: "bookmarks", Paths: []string{"Bookmarks"}, Default: true},
		{Name: "tabs", Paths: []string{"Sessions"}, Default: true},
	}

	if err := CheckAllowlist("brave", groups); err != nil {
		t.Fatalf("CheckAllowlist on a clean allowlist: %v", err)
	}
}

// TestNoRegisteredAdapterNamesADeniedPath is the guard ADR-0045 requires: a
// "no credentials" promise that depends on a reviewer noticing is not a
// promise. It iterates every registered app, so a future adapter is covered
// the moment it is registered, without anyone remembering to add it here.
func TestNoRegisteredAdapterNamesADeniedPath(t *testing.T) {
	for _, name := range registry.Names() {
		app, err := registry.GetApp(name)
		if err != nil {
			t.Fatalf("registry.GetApp(%q): %v", name, err)
		}
		porter, ok := app.(apps.StatePorter)
		if !ok {
			continue
		}
		if err := CheckAllowlist(name, porter.StateGroups()); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
