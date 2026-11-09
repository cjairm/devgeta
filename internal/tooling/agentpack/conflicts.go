package agentpack

import (
	"net/url"
	"regexp"
	"strings"
)

// sourceNamesPack reports whether a coder's plugin or package spec is another
// copy of the pack's upstream. It is how an installer notices that the user
// already has the pack some other way, so devgeta can stop instead of loading
// it twice (ADR-0070 §5).
//
// A spec names the pack when it mentions the pack's repository (owner/name,
// any case, ending at a boundary so "superpowers-fork" is not "superpowers"),
// or when, stripped of its scheme, version and a trailing .opencode/plugins,
// its last path segment is the pack's name. That covers "superpowers",
// "npm:superpowers@7.0.2", "npm:@obra/superpowers", "git:github.com/obra/
// superpowers@v7" and a checkout at /code/superpowers. It cannot tell two
// owners' packages of the same name apart; reporting one as a conflict costs a
// moment, missing one costs two copies of the pack.
func sourceNamesPack(source string, pack Manifest) bool {
	s := strings.ToLower(strings.TrimSpace(source))
	if s == "" {
		return false
	}
	if slug := repoSlug(pack.Repo); slug != "" && slugPattern(slug).MatchString(s) {
		return true
	}
	s = strings.TrimPrefix(s, "npm:")
	s, _, _ = strings.Cut(s, "@git+") // "name@git+https://..." keeps just the name
	s = strings.TrimRight(s, "/")
	s = strings.TrimSuffix(s, "/.opencode/plugins")
	// A trailing @version, but not the @ of a scope ("@obra/superpowers").
	if i := strings.LastIndex(s, "@"); i > 0 && !strings.Contains(s[i:], "/") {
		s = s[:i]
	}
	return s[strings.LastIndex(s, "/")+1:] == strings.ToLower(pack.Name)
}

// slugPattern matches owner/name where it stands alone: not inside a longer
// owner or repository name.
func slugPattern(slug string) *regexp.Regexp {
	const edge = `[^a-z0-9_.-]`
	return regexp.MustCompile(`(?:^|` + edge + `)` + regexp.QuoteMeta(slug) + `(?:\.git)?(?:$|` + edge + `)`)
}

// repoSlug turns https://github.com/obra/superpowers.git into
// "obra/superpowers", lower-cased, or "" when the URL has no owner and name.
func repoSlug(repo string) string {
	u, err := url.Parse(repo)
	if err != nil {
		return ""
	}
	slug := strings.Trim(strings.TrimSuffix(u.Path, ".git"), "/")
	if strings.Count(slug, "/") != 1 {
		return ""
	}
	return strings.ToLower(slug)
}
