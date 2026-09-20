// The denylist is the half of ADR-0045's guarantee that does not depend on
// anyone being careful. The allowlist is what actually decides that only
// named paths move; this list is what makes "and a credential can never be
// among them" a property of the code — CheckAllowlist runs against every
// registered adapter in denylist_test.go, so a future adapter that names a
// password store fails the build rather than shipping.
//
// The same list runs on the way in. An allowlist constrains what devgeta
// packed, not what an arbitrary tar in front of us contains, so an import
// checks every member against these rules too (import.go). Without that, a
// perfectly relative member named "Default/Login Data" lands in the live
// profile and the guarantee only ever held for export.
package appstate

import (
	"fmt"
	"path"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
)

// denyRule is one pattern, matched against a single path element, and the
// reason it exists. The reason is not decoration: it is what a refusal
// prints, and the only thing that tells a user why a bundle was rejected.
type denyRule struct {
	// Pattern is a path.Match glob, written lowercase and matched against a
	// lowercased path element.
	Pattern string
	Why     string
}

// denyRules is the whole list. Every entry traces to ADR-0045's Context: the
// files that are either credentials or bound to the machine that wrote them.
//
// The trailing "*" on the store names covers the sidecars SQLite and
// Chromium write beside them — "Login Data-journal", "Cookies-journal",
// "Login Data For Account" — which hold the same bytes and would otherwise
// walk straight through a rule that named only the base file.
var denyRules = []denyRule{
	{"login data*", "a credential store, encrypted against the local keychain"},
	{"cookies*", "session cookies, encrypted against the local keychain"},
	{"web data*", "autofill and payment data"},
	{"secure preferences", "carries HMACs bound to the machine that wrote it"},
	{"local state", "holds that machine's encryption key"},
	{"affiliation database*", "password-manager affiliation data"},
	{"network", "the network stack's cookie and credential stores"},
}

// DeniedPath reports the rule denying relPath, or ("", false) when nothing
// does. relPath is forward-slash separated and relative to a profile root.
//
// Every element is checked, not just the last: "Network/Cookies" must be
// denied by the "Network" rule, and a denied file reached through an
// allowlisted directory ("Extensions/abc/Cookies") is still a denied file.
//
// Matching is case-insensitive because the filesystems this runs on are.
// macOS is case-insensitive by default, so a bundle member named
// "Default/login data" opens the real "Login Data" — a case-sensitive rule
// would be a rule that can be spelled around.
func DeniedPath(relPath string) (string, bool) {
	for _, element := range strings.Split(path.Clean(relPath), "/") {
		lowered := strings.ToLower(element)
		for _, rule := range denyRules {
			if matched, err := path.Match(rule.Pattern, lowered); err == nil && matched {
				return rule.Why, true
			}
		}
	}
	return "", false
}

// CheckAllowlist returns an error naming the first denied path an adapter's
// groups declare. It is what denylist_test.go runs over every registered app,
// and what keeps ADR-0045's "credentials can never be in a group" from being
// a comment someone has to remember.
func CheckAllowlist(app string, groups []apps.StateGroup) error {
	for _, group := range groups {
		for _, p := range group.Paths {
			if why, denied := DeniedPath(p); denied {
				return fmt.Errorf(
					"%s's %q group names %q, which is on the denylist: %s",
					app,
					group.Name,
					p,
					why,
				)
			}
		}
	}
	return nil
}
