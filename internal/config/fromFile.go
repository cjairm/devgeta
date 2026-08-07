package config

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/paths"
	"gopkg.in/yaml.v3"
)

// Used to store what this app installed
type InstalledConfig struct {
	Packages      []string `yaml:"packages"`
	DesktopApps   []string `yaml:"desktop_apps"`
	Fonts         []string `yaml:"fonts"`
	Themes        []string `yaml:"themes"`
	TerminalTools []string `yaml:"terminal_tools"`
	DevLanguages  []string `yaml:"dev_languages"`
	Databases     []string `yaml:"databases"`
}

// Used to store config that user already had installed before using this app
type AlreadyInstalledConfig struct {
	Packages      []string `yaml:"packages"`
	DesktopApps   []string `yaml:"desktop_apps"`
	Fonts         []string `yaml:"fonts"`
	Themes        []string `yaml:"themes"`
	TerminalTools []string `yaml:"terminal_tools"`
	DevLanguages  []string `yaml:"dev_languages"`
	Databases     []string `yaml:"databases"`
}

// ShellFeatures tracks which shell enhancements are enabled
type ShellFeatures struct {
	IsMac                 bool `yaml:"is_mac"`
	Mise                  bool `yaml:"mise"`
	Zoxide                bool `yaml:"zoxide"`
	ZshAutosuggestions    bool `yaml:"zsh_autosuggestions"`
	ZshSyntaxHighlighting bool `yaml:"zsh_syntax_highlighting"`
	Powerlevel10k         bool `yaml:"powerlevel10k"`
	ExtendedCapabilities  bool `yaml:"extended_capabilities"`
	LazyGit               bool `yaml:"lazy_git"`
	LazyDocker            bool `yaml:"lazy_docker"`
	Fzf                   bool `yaml:"fzf"`
	Neovim                bool `yaml:"neovim"`
	Tmux                  bool `yaml:"tmux"`
	Eza                   bool `yaml:"eza"`
	Bat                   bool `yaml:"bat"`
	Opencode              bool `yaml:"opencode"`
	Claude                bool `yaml:"claude"`
}

// IntegrationsConfig tracks explicit cross-app opt-ins that devgeta must
// preserve when re-rendering another app's config. RtkClaudeHook records that
// the user opted into rtk's command-rewriting hook (ADR-0004: the hook is
// never enabled automatically), so claude's settings.json template keeps the
// hook entry across every `dg configure claude --force`.
type IntegrationsConfig struct {
	RtkClaudeHook bool `yaml:"rtk_claude_hook,omitempty"`
}

// FailedInstallation tracks packages that failed to install
type FailedInstallation struct {
	PackageName  string    `yaml:"package_name"`
	Category     string    `yaml:"category"` // "package" | "dev_language" | "database"
	ErrorMessage string    `yaml:"error_message"`
	FailedAt     time.Time `yaml:"failed_at"`
	AttemptCount int       `yaml:"attempt_count"`
}

// RecentRepo tracks a repo root devgeta has created a worktree in, so the
// worktree TUI's repo picker can offer it again (most-recently-used first)
// even after every worktree under it has been removed.
type RecentRepo struct {
	Path     string    `yaml:"path"`
	LastUsed time.Time `yaml:"last_used"`
}

// maxRecentRepos caps the recent-repos store so it can't grow unbounded.
const maxRecentRepos = 20

// WorktreeLocationShared and WorktreeLocationInRepo are the two values
// WorktreeConfig.Location accepts. They are named constants - rather than
// bare string literals repeated across cmd/config_settings.go's Default,
// Set's validation, and their tests - so the two spellings can't drift out of
// sync with each other. Defined here rather than in internal/tooling/worktree
// because internal/config cannot import that package (it already imports
// internal/config in four files), so internal/config is the owner that adds
// no new dependency edge - internal/tooling/worktree does read these
// constants back through that existing edge (worktreePath, worktreeStateReal,
// and their callers).
const (
	WorktreeLocationShared = "shared"
	WorktreeLocationInRepo = "in-repo"
)

// WorktreeConfig stores worktree-specific settings
type WorktreeConfig struct {
	DefaultAI     string       `yaml:"default_ai,omitempty"`     // "opencode" | "claude"; empty = fallback to "opencode"
	RecentRepos   []RecentRepo `yaml:"recent_repos,omitempty"`   // MRU-ordered; new field, absent in old configs
	SearchPaths   []string     `yaml:"search_paths,omitempty"`   // dirs to scan for git repos for the worktree picker; empty = scanning disabled (the only off-switch)
	ScanDepth     int          `yaml:"scan_depth,omitempty"`     // max dir depth for the repo scan; 0 (or unset) = use the default of 4
	DefaultLayout string       `yaml:"default_layout,omitempty"` // default tmux window layout for `dg ws`'s create; empty = derive a single-pane layout from DefaultAI

	// Location selects where worktrees are created on disk, and where
	// remove/repair/state checks look for one: WorktreeLocationShared (the
	// default) or WorktreeLocationInRepo. Empty means WorktreeLocationShared,
	// so existing installs are unaffected - hence omitempty. Read by
	// worktree.go's worktreePath/worktreeStateIn (create) and, as a fallback
	// only (the git-verified currentWorktreePath is tried first), by
	// removeByRepo/Repair/RepairInRepo when no real worktree exists at
	// either location shape - see worktree.go's realWorktreePathOrConfigured.
	Location string `yaml:"location,omitempty"`

	// AttachAfterCreate controls whether `dg ws`'s n/N create attaches into the
	// new worktree's window (quitting the dashboard) or leaves you on the new
	// row to keep working in the dashboard.
	//
	// It is a *bool, unlike every other bool in this file, because it is the
	// only one whose default is true. The others are feature flags where the
	// zero value (false, "not enabled") is already the right default, so a
	// plain bool expresses "unset = default" for free. Here a plain bool
	// cannot: an absent key would unmarshal to false and silently flip
	// attach-on-create off for every existing config, which CLAUDE.md §10
	// forbids. nil (absent) therefore means "use the default (attach)" -
	// always read it through ShouldAttachAfterCreate rather than
	// dereferencing.
	AttachAfterCreate *bool `yaml:"attach_after_create,omitempty"`

	// NotifySound gates the audible ding ADR-0009 adds to the two agent-state
	// hooks (configs/claude/agent-state.sh, configs/opencode/plugin/notify.js)
	// for idle/blocked/error pane states while the window is unattended.
	//
	// Unlike AttachAfterCreate, a plain bool is right here: the default is
	// false ("off") - ADR-0009 is explicit that a tool which starts making
	// noise after an upgrade is a bug - so the zero value already IS the
	// correct default and needs no separate resolver to express "unset =
	// off" the way AttachAfterCreate's nil does for its true default.
	//
	// The hooks cannot read this field directly (no YAML parsing in bash or
	// the OpenCode plugin - ADR-0009), so it is not the runtime source of
	// truth: configs/tmux/tmux.conf.tmpl renders it into the deployed
	// ~/.tmux.conf as the tmux global option @dg_notify_sound, which the
	// hooks query with a plain `tmux show-option` call. This field is the
	// durable source that survives a killed tmux server; the tmux option is
	// the live one a fresh server is rendered with.
	NotifySound bool `yaml:"notify_sound,omitempty"`
}

// ReviewRoundsMin and ReviewRoundsMax are the valid inclusive range for
// ReviewConfig.Rounds. cmd/config_settings.go's review.rounds Setting rejects
// anything outside this range before it ever reaches GlobalConfig, so these
// constants - not a restated literal in the registry or its tests - are the
// one place the range is defined.
const (
	ReviewRoundsMin = 1
	ReviewRoundsMax = 5
)

// DefaultReviewRounds is the number of review rounds used when Rounds is
// unset (its zero value, 0). cmd/config_settings.go's review.rounds Default
// reports this constant rather than restating "3" as a literal, and it is the
// only place in Go that reads the round cap at all: the cap is enforced by
// the agent-side /review-loop command
// (configs/shared/commands/review-loop.md), which reads it back with
// `devgeta config get review.rounds` and falls back to this default when the
// key is unset. `dg task review-run` never reads Rounds — one invocation is
// exactly one round, by design (ADR-0017 §5).
const DefaultReviewRounds = 3

// ReviewConfig stores settings for `dg task review-run`, which runs
// configured AI reviewer agents headless and collects their verdicts. Both
// fields are omitempty: an empty/zero ReviewConfig is indistinguishable from
// one that was never touched, which is what lets an existing
// global_config.yaml load unchanged before this cycle's config plumbing ever
// ran.
type ReviewConfig struct {
	// Reviewers lists the AI reviewer agents review-run runs, each in
	// "provider/model" form (e.g. "anthropic/claude-opus-4-6"). Only the
	// shape - one "/" minimum, non-empty text on both sides - is validated
	// here; the model name itself is passed through without offline
	// verification (out of scope for this cycle - see the cycle doc's
	// Explicitly Out of Scope section). Empty is not "nothing to run": it
	// means one reviewer run on OpenCode's own default model, with no -m
	// flag at all (resolveReviewerRuns in
	// internal/tooling/task/reviewrun.go), so review-run works before this
	// key is ever set.
	Reviewers []string `yaml:"reviewers,omitempty"`

	// Rounds caps how many review rounds the agent-side /review-loop command
	// performs before it reports to the human. 0 (unset) means
	// DefaultReviewRounds; a configured value must be within
	// [ReviewRoundsMin, ReviewRoundsMax]. Nothing in Go reads this field to
	// decide anything: `dg task review-run` runs exactly one round per
	// invocation and never consults it, and /review-loop reads the value back
	// through `devgeta config get review.rounds`.
	Rounds int `yaml:"rounds,omitempty"`
}

// ShouldAttachAfterCreate reports whether a successful `dg ws` create should
// attach into the new window. It is the only place the "unset means attach"
// default lives, so no caller has to remember that a nil AttachAfterCreate is
// not the same as an explicit false.
//
// It hangs off *GlobalConfig, not *WorktreeConfig, and tolerates a nil
// receiver on purpose: its caller is the TUI's m.gc, which the rest of that
// code path already treats as possibly-nil (see worktree.ResolveLayout, which
// nil-guards the same pointer). Reading the setting as
// m.gc.Worktree.AttachAfterCreate would be the first hard dereference of that
// field and would panic where every neighbouring call survives. A nil config
// means nothing was configured, which is exactly the default case.
func (gc *GlobalConfig) ShouldAttachAfterCreate() bool {
	if gc == nil || gc.Worktree.AttachAfterCreate == nil {
		return true
	}
	return *gc.Worktree.AttachAfterCreate
}

// UpsertRecentRepo records path as the most-recently-used repo: if path is
// already present it is moved to the front with LastUsed bumped to now,
// otherwise it is prepended. The list is capped at maxRecentRepos entries,
// dropping the least-recently-used. path must already be canonicalized (see
// CanonicalRepoPath) so the same repo is never stored under two string forms.
func (wc *WorktreeConfig) UpsertRecentRepo(path string, now time.Time) {
	entries := make([]RecentRepo, 0, len(wc.RecentRepos)+1)
	entries = append(entries, RecentRepo{Path: path, LastUsed: now})
	for _, r := range wc.RecentRepos {
		if r.Path != path {
			entries = append(entries, r)
		}
	}
	if len(entries) > maxRecentRepos {
		entries = entries[:maxRecentRepos]
	}
	wc.RecentRepos = entries
}

// PrunedRecentRepos returns RecentRepos with any entry whose Path no longer
// exists on disk removed. It is a pure read-time filter: it does not mutate
// the receiver or persist anything, so callers decide separately whether and
// when to save the pruned result.
func (wc *WorktreeConfig) PrunedRecentRepos() []RecentRepo {
	pruned := make([]RecentRepo, 0, len(wc.RecentRepos))
	for _, r := range wc.RecentRepos {
		if _, err := os.Stat(r.Path); err == nil {
			pruned = append(pruned, r)
		}
	}
	return pruned
}

// CanonicalRepoPath normalizes a repo path to one canonical string form so
// the same repo is never tracked under multiple representations: expand a
// leading "~", make it absolute, clean it, then best-effort resolve
// symlinks (falling back to the cleaned absolute path when a path doesn't
// exist yet or symlink resolution otherwise fails). Every source that feeds
// the repo-candidates provider (recent-repos store, cursor repo, zoxide
// results) must canonicalize through this same function to dedupe correctly.
func CanonicalRepoPath(path string) string {
	expanded := paths.ExpandHome(path)
	abs, err := filepath.Abs(expanded)
	if err != nil {
		abs = expanded
	}
	cleaned := filepath.Clean(abs)
	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		return resolved
	}
	return cleaned
}

type GlobalConfig struct {
	AppPath             string                 `yaml:"app_path"`
	ConfigPath          string                 `yaml:"config_path"`
	AlreadyInstalled    AlreadyInstalledConfig `yaml:"already_installed"`
	CurrentFont         string                 `yaml:"current_font"`
	CurrentTheme        string                 `yaml:"current_theme"`
	Installed           InstalledConfig        `yaml:"installed"`
	Shortcuts           map[string]string      `yaml:"shortcuts"`
	Shell               ShellFeatures          `yaml:"shell"`
	FailedInstallations []FailedInstallation   `yaml:"failed_installations,omitempty"`
	Worktree            WorktreeConfig         `yaml:"worktree"`
	Integrations        IntegrationsConfig     `yaml:"integrations,omitempty"`
	Review              ReviewConfig           `yaml:"review,omitempty"`
}

func getGlobalConfigFilePath() string {
	return filepath.Join(
		paths.Paths.Config.Root,
		constants.App.Name,
		constants.App.File.GlobalConfig,
	)
}

func (gc *GlobalConfig) Load() error {
	globalConfigFile, err := os.ReadFile(getGlobalConfigFilePath())
	if err != nil {
		return err
	}
	return yaml.Unmarshal(globalConfigFile, gc)
}

func (gc *GlobalConfig) Save() error {
	data, err := yaml.Marshal(gc)
	if err != nil {
		return err
	}
	return files.WriteFileAtomic(getGlobalConfigFilePath(), data, files.FilePermission)
}

func (gc *GlobalConfig) Reset() error {
	logger.L().Debug("Resetting global config")
	*gc = GlobalConfig{}
	data, err := yaml.Marshal(gc)
	if err != nil {
		return err
	}
	return files.WriteFileAtomic(getGlobalConfigFilePath(), data, files.FilePermission)
}

func (gc *GlobalConfig) Create() error {
	globalConfigFilePath := getGlobalConfigFilePath()
	if paths.FileAlreadyExist(globalConfigFilePath) {
		return nil
	}
	appFolder := filepath.Join(
		paths.Paths.Config.Root,
		constants.App.Name,
	)
	if !files.DirAlreadyExist(appFolder) {
		if err := os.MkdirAll(appFolder, files.DirPermission); err != nil {
			return err
		}
	}
	// Initialize with empty config structure instead of copying template
	// This avoids dependency on extracted embedded files
	return gc.Reset()
}

func (gc *GlobalConfig) getSliceByType(configType, itemType string) *[]string {
	switch configType {
	case "installed":
		return gc.getInstalledSlice(itemType)
	case "already_installed":
		return gc.getAlreadyInstalledSlice(itemType)
	}
	return nil
}

func (gc *GlobalConfig) getInstalledSlice(itemType string) *[]string {
	switch itemType {
	case "package":
		return &gc.Installed.Packages
	case "desktop_app":
		return &gc.Installed.DesktopApps
	case "font":
		return &gc.Installed.Fonts
	case "theme":
		return &gc.Installed.Themes
	case "terminal_tool":
		return &gc.Installed.TerminalTools
	case "dev_language":
		return &gc.Installed.DevLanguages
	case "database":
		return &gc.Installed.Databases
	}
	return nil
}

func (gc *GlobalConfig) getAlreadyInstalledSlice(itemType string) *[]string {
	switch itemType {
	case "package":
		return &gc.AlreadyInstalled.Packages
	case "desktop_app":
		return &gc.AlreadyInstalled.DesktopApps
	case "font":
		return &gc.AlreadyInstalled.Fonts
	case "theme":
		return &gc.AlreadyInstalled.Themes
	case "terminal_tool":
		return &gc.AlreadyInstalled.TerminalTools
	case "dev_language":
		return &gc.AlreadyInstalled.DevLanguages
	case "database":
		return &gc.AlreadyInstalled.Databases
	}
	return nil
}

func (gc *GlobalConfig) IsTracked(itemName, itemType, configType string) bool {
	slice := gc.getSliceByType(configType, itemType)
	if slice == nil {
		return false
	}
	return slices.Contains(*slice, itemName)
}

func (gc *GlobalConfig) AddToConfig(itemName, itemType, configType string) {
	slice := gc.getSliceByType(configType, itemType)
	if slice == nil {
		return
	}
	if !slices.Contains(*slice, itemName) {
		*slice = append(*slice, itemName)
	}
}

// AddToInstalled adds an item to the installed config
func (gc *GlobalConfig) AddToInstalled(itemName, itemType string) {
	gc.AddToConfig(itemName, itemType, "installed")
}

// RemoveFromInstalled removes itemName from the installed tracking list for itemType.
func (gc *GlobalConfig) RemoveFromInstalled(itemName, itemType string) {
	slice := gc.getInstalledSlice(itemType)
	if slice == nil {
		return
	}
	result := (*slice)[:0]
	for _, v := range *slice {
		if v != itemName {
			result = append(result, v)
		}
	}
	*slice = result
}

func (gc *GlobalConfig) AddToAlreadyInstalled(itemName, itemType string) {
	gc.AddToConfig(itemName, itemType, "already_installed")
}

// AddToFailed adds a package to the failed installations list
// It stores the package name, category, error message, timestamp, and attempt count
func (gc *GlobalConfig) AddToFailed(packageName, category, errorMessage string, attemptCount int) {
	// Check if package already in failed list, update if exists
	for i := range gc.FailedInstallations {
		if gc.FailedInstallations[i].PackageName == packageName {
			gc.FailedInstallations[i].ErrorMessage = errorMessage
			gc.FailedInstallations[i].FailedAt = time.Now()
			gc.FailedInstallations[i].AttemptCount = attemptCount
			logger.L().Warnw(
				"Updated failed installation",
				"package", packageName,
				"category", category,
				"error", errorMessage,
			)
			return
		}
	}

	// Add new failed installation
	gc.FailedInstallations = append(gc.FailedInstallations, FailedInstallation{
		PackageName:  packageName,
		Category:     category,
		ErrorMessage: errorMessage,
		FailedAt:     time.Now(),
		AttemptCount: attemptCount,
	})
	logger.L().Warnw(
		"Added to failed installations",
		"package", packageName,
		"category", category,
		"error", errorMessage,
	)
}

func (gc *GlobalConfig) IsInstalledByDevgeta(itemName, itemType string) bool {
	return gc.IsTracked(itemName, itemType, "installed")
}

func (gc *GlobalConfig) IsAlreadyInstalled(itemName, itemType string) bool {
	return gc.IsTracked(itemName, itemType, "already_installed")
}

// EnableShellFeature enables a shell feature by name
func (gc *GlobalConfig) EnableShellFeature(featureName string) {
	switch featureName {
	case constants.Mise:
		gc.Shell.Mise = true
	case constants.Zoxide:
		gc.Shell.Zoxide = true
	case constants.ZshAutosuggestions:
		gc.Shell.ZshAutosuggestions = true
	case constants.Syntaxhighlighting:
		gc.Shell.ZshSyntaxHighlighting = true
	case constants.Powerlevel10k:
		gc.Shell.Powerlevel10k = true
	case "extended_capabilities":
		gc.Shell.ExtendedCapabilities = true
	case constants.LazyGit:
		gc.Shell.LazyGit = true
	case constants.LazyDocker:
		gc.Shell.LazyDocker = true
	case constants.Fzf:
		gc.Shell.Fzf = true
	case constants.Neovim:
		gc.Shell.Neovim = true
	case constants.Tmux:
		gc.Shell.Tmux = true
	case constants.Eza:
		gc.Shell.Eza = true
	case constants.Bat:
		gc.Shell.Bat = true
	case constants.OpenCode:
		gc.Shell.Opencode = true
	case constants.Claude:
		gc.Shell.Claude = true
	}
}

// DisableShellFeature disables a shell feature by name
func (gc *GlobalConfig) DisableShellFeature(featureName string) {
	switch featureName {
	case constants.Mise:
		gc.Shell.Mise = false
	case constants.Zoxide:
		gc.Shell.Zoxide = false
	case constants.ZshAutosuggestions:
		gc.Shell.ZshAutosuggestions = false
	case constants.Syntaxhighlighting:
		gc.Shell.ZshSyntaxHighlighting = false
	case constants.Powerlevel10k:
		gc.Shell.Powerlevel10k = false
	case "extended_capabilities":
		gc.Shell.ExtendedCapabilities = false
	case constants.LazyGit:
		gc.Shell.LazyGit = false
	case constants.LazyDocker:
		gc.Shell.LazyDocker = false
	case constants.Fzf:
		gc.Shell.Fzf = false
	case constants.Neovim:
		gc.Shell.Neovim = false
	case constants.Tmux:
		gc.Shell.Tmux = false
	case constants.Eza:
		gc.Shell.Eza = false
	case constants.Bat:
		gc.Shell.Bat = false
	case constants.OpenCode:
		gc.Shell.Opencode = false
	case constants.Claude:
		gc.Shell.Claude = false
	}
}

// IsShellFeatureEnabled checks if a shell feature is enabled
func (gc *GlobalConfig) IsShellFeatureEnabled(featureName string) bool {
	switch featureName {
	case constants.Mise:
		return gc.Shell.Mise
	case constants.Zoxide:
		return gc.Shell.Zoxide
	case constants.ZshAutosuggestions:
		return gc.Shell.ZshAutosuggestions
	case constants.Syntaxhighlighting:
		return gc.Shell.ZshSyntaxHighlighting
	case constants.Powerlevel10k:
		return gc.Shell.Powerlevel10k
	case "extended_capabilities":
		return gc.Shell.ExtendedCapabilities
	case constants.LazyGit:
		return gc.Shell.LazyGit
	case constants.LazyDocker:
		return gc.Shell.LazyDocker
	case constants.Fzf:
		return gc.Shell.Fzf
	case constants.Neovim:
		return gc.Shell.Neovim
	case constants.Tmux:
		return gc.Shell.Tmux
	case constants.Eza:
		return gc.Shell.Eza
	case constants.Bat:
		return gc.Shell.Bat
	case constants.OpenCode:
		return gc.Shell.Opencode
	case constants.Claude:
		return gc.Shell.Claude
	}
	return false
}

// ShellTemplateData is everything devgeta.zsh.tmpl is rendered from: which
// features are enabled, plus the values devgeta OWNS in Go and only writes into
// the shell config.
//
// ShellFeatures is EMBEDDED rather than held in a named field so every
// {{if .Mise}}-style guard the template already has keeps resolving unchanged -
// text/template promotes an embedded struct's fields exactly as Go does.
//
// The alias lines are here because a created tmux pane no longer resolves them:
// it execs the coder's binary through a non-interactive shell, which has no
// aliases at all (ADR-0020). devgeta.zsh's alias therefore stopped being the
// definition of how devgeta launches a coder and became a rendering of it -
// see pkg/constants.CoderLaunch, which is the one definition behind both.
type ShellTemplateData struct {
	ShellFeatures

	OpenCodeAlias string
	ClaudeAlias   string
}

// NewShellTemplateData pairs a ShellFeatures value with the launch recipes'
// rendered alias lines. It is the single wiring between the recipes and the
// template, which is what lets a test render the EMBEDDED template through the
// same path production uses instead of re-deriving the alias itself.
func NewShellTemplateData(shell ShellFeatures) ShellTemplateData {
	return ShellTemplateData{
		ShellFeatures: shell,
		OpenCodeAlias: constants.OpenCodeLaunch.AliasLine(),
		ClaudeAlias:   constants.ClaudeLaunch.AliasLine(),
	}
}

func (gc *GlobalConfig) RegenerateShellConfig() error {
	templatePath := filepath.Join(
		paths.Paths.App.Configs.Templates,
		constants.App.Template.ShellConfig,
	)
	outputPath := filepath.Join(paths.Paths.App.Root, fmt.Sprintf("%s.zsh", constants.App.Name))
	return files.GenerateFromTemplate(templatePath, outputPath, NewShellTemplateData(gc.Shell))
}
