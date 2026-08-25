package config

// cloneGlobalConfig returns an independent deep copy of src: every reachable
// slice, map, and pointer gets its own backing storage rather than sharing
// src's. This is the other half of ADR-0030's cache contract (alongside
// writeGlobalConfigFile's refresh-on-write) - the cache must never hand out
// the cached *GlobalConfig itself, and never a shallow copy of it either.
//
// Load() has always filled a caller-owned struct, so every one of the ~95
// non-test call sites already assumes it owns the result, and several
// mutate it in place: RemoveFromInstalled (fromFile.go) filters through the
// shared backing array, AddToFailed writes into an existing element, and a
// Shortcuts write mutates a map that a shallow struct copy would share
// outright. Reproduced against a cache handing out shallow copies: with
// [git tmux neovim fzf bat] cached, a single
// RemoveFromInstalled("tmux", "terminal_tool") left the *cache* holding
// [git neovim fzf bat bat] - tmux lost, bat duplicated - and the next Load()
// served that corrupted list even though the file on disk was untouched.
//
// Every field cloneGlobalConfig does not explicitly deep-copy is a plain
// value type (string, bool, int) or a struct made entirely of those, so the
// `dst := *src` shallow copy below already gives it independent storage.
// TestCloneGlobalConfig_DeepCopiesEveryReachableField (clone_test.go) is the
// guard against a future field breaking that assumption: it populates every
// field, clones, then mutates every reachable slice element, map entry, and
// pointee in the source by reflection and asserts the clone is unaffected -
// so a newly added slice/map/pointer field that this file forgets to clone
// fails that test's build rather than rotting silently.
func cloneGlobalConfig(src *GlobalConfig) *GlobalConfig {
	if src == nil {
		return nil
	}

	dst := *src

	dst.AlreadyInstalled = cloneAlreadyInstalledConfig(src.AlreadyInstalled)
	dst.Installed = cloneInstalledConfig(src.Installed)
	dst.Shortcuts = cloneStringMap(src.Shortcuts)
	dst.FailedInstallations = cloneFailedInstallations(src.FailedInstallations)
	dst.Worktree = cloneWorktreeConfig(src.Worktree)
	dst.Review = cloneReviewConfig(src.Review)
	// AppPath, ConfigPath, CurrentFont, CurrentTheme, Shell, and Integrations
	// are strings/bools (or structs made only of those), so the `dst := *src`
	// copy above already gives them independent storage - nothing else to do.

	return &dst
}

func cloneStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

func cloneStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func cloneAlreadyInstalledConfig(c AlreadyInstalledConfig) AlreadyInstalledConfig {
	return AlreadyInstalledConfig{
		Packages:      cloneStringSlice(c.Packages),
		DesktopApps:   cloneStringSlice(c.DesktopApps),
		Fonts:         cloneStringSlice(c.Fonts),
		Themes:        cloneStringSlice(c.Themes),
		TerminalTools: cloneStringSlice(c.TerminalTools),
		DevLanguages:  cloneStringSlice(c.DevLanguages),
		Databases:     cloneStringSlice(c.Databases),
	}
}

func cloneInstalledConfig(c InstalledConfig) InstalledConfig {
	return InstalledConfig{
		Packages:      cloneStringSlice(c.Packages),
		DesktopApps:   cloneStringSlice(c.DesktopApps),
		Fonts:         cloneStringSlice(c.Fonts),
		Themes:        cloneStringSlice(c.Themes),
		TerminalTools: cloneStringSlice(c.TerminalTools),
		DevLanguages:  cloneStringSlice(c.DevLanguages),
		Databases:     cloneStringSlice(c.Databases),
	}
}

func cloneFailedInstallations(s []FailedInstallation) []FailedInstallation {
	if s == nil {
		return nil
	}
	out := make([]FailedInstallation, len(s))
	copy(out, s)
	return out
}

func cloneRecentRepos(s []RecentRepo) []RecentRepo {
	if s == nil {
		return nil
	}
	out := make([]RecentRepo, len(s))
	copy(out, s)
	return out
}

func cloneWorktreeConfig(wc WorktreeConfig) WorktreeConfig {
	out := wc
	out.RecentRepos = cloneRecentRepos(wc.RecentRepos)
	out.SearchPaths = cloneStringSlice(wc.SearchPaths)
	if wc.AttachAfterCreate != nil {
		v := *wc.AttachAfterCreate
		out.AttachAfterCreate = &v
	}
	return out
}

func cloneReviewConfig(rc ReviewConfig) ReviewConfig {
	out := rc
	out.Reviewers = cloneStringSlice(rc.Reviewers)
	return out
}
