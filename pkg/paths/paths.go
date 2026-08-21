package paths

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"golang.org/x/sys/unix"
)

// This allows swapping it during tests
var FileAlreadyExist = files.FileAlreadyExist

// testSandboxPrefix names every sandbox directory. It doubles as the ownership
// boundary for the sweep below: an entry in the temp directory without this
// prefix is never a deletion candidate, so anything else sharing that
// directory is left alone.
const testSandboxPrefix = "devgeta-test-sandbox-"

// sandboxSweepMinAge is how old an abandoned sandbox must look before the sweep
// will even consider it. The flock in claimSandbox is what actually proves a
// sandbox has no live owner; this age filter covers only the microseconds
// between os.MkdirTemp returning a directory and its creator locking it, during
// which a concurrent sweeper would correctly see it unlocked. An hour is many
// orders of magnitude more than that window and still reclaims within an hour.
//
// A directory's mtime is bumped whenever an entry is added at its top level, so
// a busy sandbox only ever looks younger than it is — the direction that
// protects it.
const sandboxSweepMinAge = time.Hour

// sandboxSweepBudget caps how many abandoned sandboxes a single process
// deletes, so that a large backlog cannot turn package initialization into a
// long stall. Every test binary sweeps, so a backlog drains across runs instead
// of being paid for by whichever run finds it first.
const sandboxSweepBudget = 64

// testSandboxLock pins the open directory descriptor whose flock marks this
// process's sandbox as live, for as long as the process runs.
//
// It is a package variable rather than a local on purpose: the lock lives on
// the open file description, so once nothing references the *os.File the
// runtime's finalizer closes it, which releases the flock and invites another
// process's sweep to delete this test run's HOME out from under it. That is the
// same silent failure pkg/files.WithLock documents and designs around.
var testSandboxLock *os.File

// testSandbox is the throwaway root directory every derived path resolves
// under while running inside `go test`. It exists so a test that forgets (or
// incompletely applies) path isolation can never read or delete real user
// data. Outside `go test` it is empty and has no effect.
//
// HOME and the XDG base variables are redirected into the sandbox too, so
// env-reading fallbacks resolve inside it as well; tests that set their own
// XDG values (e.g. via t.Setenv) still take precedence because those reads
// stay dynamic. Every path helper resolves the home directory through
// userHome, which references this variable — that reference guarantees Go
// initializes the sandbox before any package-level path (e.g. Paths) is
// derived.
var testSandbox = func() string {
	if !testing.Testing() {
		return ""
	}
	dir, err := os.MkdirTemp("", testSandboxPrefix)
	if err != nil {
		panic("could not create test sandbox directory: " + err.Error())
	}
	for key, value := range map[string]string{
		"HOME":            dir,
		"XDG_CONFIG_HOME": filepath.Join(dir, ".config"),
		"XDG_DATA_HOME":   filepath.Join(dir, ".local", "share"),
		"XDG_STATE_HOME":  filepath.Join(dir, ".local", "state"),
		"XDG_CACHE_HOME":  filepath.Join(dir, ".cache"),
	} {
		if err := os.Setenv(key, value); err != nil {
			panic("could not redirect " + key + " to the test sandbox: " + err.Error())
		}
	}
	// Claiming and sweeping come last, after the redirect above: the isolation
	// this variable exists for must be in place regardless of anything below,
	// and reclaiming disk is strictly secondary to it.
	if lock, lockErr := claimSandbox(dir); lockErr == nil {
		testSandboxLock = lock
	}
	sweepAbandonedSandboxes(dir)
	return dir
}()

// claimSandbox takes a non-blocking exclusive flock on dir itself and returns
// the open descriptor holding it. The directory is locked directly rather than
// through a sidecar lock file, because deleting that directory is the exact
// operation the lock guards and a sidecar would be one more thing to leak.
//
// unix.Flock is used for the same reason pkg/files.WithLock uses it: the kernel
// releases the lock when the holding process exits — including when it is
// killed or calls os.Exit — so a sandbox whose owner died is immediately
// claimable, and there is no stale-lock age heuristic to get wrong. The lock
// belongs to the open file description rather than the process, so a second
// open of the same directory conflicts even within one process.
//
// pkg/files.WithLock itself cannot be reused here: it deliberately scopes the
// lock to a callback and releases it on return, whereas this lock has to be
// held for the entire life of the process.
func claimSandbox(dir string) (*os.File, error) {
	f, err := os.Open(dir)
	if err != nil {
		return nil, err
	}
	if err := unix.Flock(int(f.Fd()), unix.LOCK_EX|unix.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// sweepAbandonedSandboxes deletes sandboxes left behind by earlier test
// processes. It exists because the sandbox above is created during package
// initialization, where there is no defer, no TestMain and no t.Cleanup to hang
// teardown on. A TestMain in this package would only clean up when this
// package's own tests run, and adding one to every package that imports this
// one would still leak from whichever package forgot it — silently, which is
// the worst property a guard like this can have.
//
// A sandbox is deleted only when two independent signals agree that nothing
// owns it: it is older than sandboxSweepMinAge, and an exclusive flock on it
// succeeds. A live owner holds that flock, so the attempt fails with
// EWOULDBLOCK and the directory is skipped. own is skipped outright as well,
// even though it is both brand new and already locked by this process.
//
// Every error is ignored deliberately. Reclaiming disk must never be able to
// fail a test run, so an unreadable temp directory, a racing sweeper, or a
// sandbox that vanishes mid-sweep simply ends the attempt.
func sweepAbandonedSandboxes(own string) {
	// Only a process holding its own sandbox lock may sweep. That single rule
	// is what keeps the sweep safe on a filesystem where flock does not work:
	// there, every process fails to claim, so nobody sweeps, and the behavior
	// degrades to leaking directories — never to deleting a running test's
	// HOME. Enforcing it here rather than at the call site means no future
	// caller can reintroduce the mixed regime by sweeping without a claim.
	if testSandboxLock == nil {
		return
	}
	root := os.TempDir()
	entries, err := os.ReadDir(root)
	if err != nil {
		return
	}
	budget := sandboxSweepBudget
	for _, entry := range entries {
		if budget == 0 {
			return
		}
		if !entry.IsDir() || !strings.HasPrefix(entry.Name(), testSandboxPrefix) {
			continue
		}
		path := filepath.Join(root, entry.Name())
		if path == own {
			continue
		}
		info, infoErr := entry.Info()
		if infoErr != nil || time.Since(info.ModTime()) < sandboxSweepMinAge {
			continue
		}
		lock, lockErr := claimSandbox(path)
		if lockErr != nil {
			continue
		}
		if removeErr := os.RemoveAll(path); removeErr == nil {
			budget--
		}
		// The lock is released here rather than deferred: this loop may hold
		// hundreds of them in one pass, and each one's directory is already
		// gone by this point.
		_ = lock.Close()
	}
}

// userHome resolves the user's home directory, panicking when it cannot be
// determined. Under `go test` the read stays dynamic (tests may point HOME at
// their own temp dir via t.Setenv), but it can never resolve to the real home
// directory: the sandbox init already redirected HOME, and an empty HOME
// falls back to the sandbox root.
func userHome() string {
	if testSandbox != "" {
		if home := os.Getenv("HOME"); home != "" {
			return home
		}
		return testSandbox
	}
	home, err := os.UserHomeDir()
	if err != nil {
		panic("could not determine home directory")
	}
	return home
}

// Paths contains all directory path structures
var Paths = struct {
	App struct {
		Root    string
		Configs struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}
		Fonts struct {
			Alacritty string
		}
		Themes struct {
			Alacritty string
		}
	}
	Cache struct {
		Root string
	}
	Config struct {
		Root      string
		Aerospace string
		Alacritty string
		Claude    string
		Devgeta   string
		Fastfetch string
		Fonts     string
		Git       string
		I3        string
		Nvim      string
		OpenCode  string
		Themes    string
		Tmux      string
	}
	Data struct {
		Root string
	}
	Home struct {
		Root string
	}
	System struct {
		Applications string
		Fonts        string
	}
	User struct {
		Applications string
		Fonts        string
	}
}{
	App: struct {
		Root    string
		Configs struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}
		Fonts struct {
			Alacritty string
		}
		Themes struct {
			Alacritty string
		}
	}{
		Root: GetDataDir(constants.App.Name),
		Configs: struct {
			Aerospace string
			Alacritty string
			Claude    string
			Fastfetch string
			Fonts     string
			Git       string
			I3        string
			Neovim    string
			OpenCode  string
			Shared    string
			Templates string
			Themes    string
			Tmux      string
		}{
			Aerospace: GetAppDir(constants.App.Dir.Configs, constants.Aerospace),
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Alacritty),
			Claude:    GetAppDir(constants.App.Dir.Configs, constants.Claude),
			Fastfetch: GetAppDir(constants.App.Dir.Configs, constants.Fastfetch),
			Fonts:     GetAppDir(constants.App.Dir.Configs, constants.Fonts),
			Git:       GetAppDir(constants.App.Dir.Configs, constants.Git),
			I3:        GetAppDir(constants.App.Dir.Configs, constants.I3),
			Neovim:    GetAppDir(constants.App.Dir.Configs, constants.Neovim),
			OpenCode:  GetAppDir(constants.App.Dir.Configs, constants.OpenCode),
			Shared:    GetAppDir(constants.App.Dir.Configs, constants.Shared),
			Templates: GetAppDir(constants.App.Dir.Configs, constants.Templates),
			Themes:    GetAppDir(constants.App.Dir.Configs, constants.Themes),
			Tmux:      GetAppDir(constants.App.Dir.Configs, constants.Tmux),
		},
		Fonts: struct {
			Alacritty string
		}{
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Fonts, constants.Alacritty),
		},
		Themes: struct {
			Alacritty string
		}{
			Alacritty: GetAppDir(constants.App.Dir.Configs, constants.Themes, constants.Alacritty),
		},
	},
	Cache: struct {
		Root string
	}{
		Root: GetCacheDir(),
	},
	Config: struct {
		Root      string
		Aerospace string
		Alacritty string
		Claude    string
		Devgeta   string
		Fastfetch string
		Fonts     string
		Git       string
		I3        string
		Nvim      string
		OpenCode  string
		Themes    string
		Tmux      string
	}{
		Root:      GetConfigDir(),
		Aerospace: GetConfigDir(constants.Aerospace),
		Alacritty: GetConfigDir(constants.Alacritty),
		Claude:    GetHomeDir(".claude"),
		Devgeta:   GetConfigDir(constants.DevgetaApp),
		Fastfetch: GetConfigDir(constants.Fastfetch),
		Fonts:     GetConfigDir(constants.Fonts),
		Git:       GetConfigDir(constants.Git),
		I3:        GetConfigDir(constants.I3),
		Nvim:      GetConfigDir(constants.Nvim),
		OpenCode:  GetConfigDir(constants.OpenCode),
		Themes:    GetConfigDir(constants.Themes),
		Tmux:      GetConfigDir(constants.Tmux),
	},
	Data: struct {
		Root string
	}{
		Root: GetDataDir(),
	},
	Home: struct {
		Root string
	}{
		Root: GetHomeDir(),
	},
	System: struct {
		Applications string
		Fonts        string
	}{
		Applications: GetSystemApplicationsDir(runtime.GOOS == "darwin"),
		Fonts:        GetSystemFontsDir(runtime.GOOS == "darwin"),
	},
	User: struct {
		Applications string
		Fonts        string
	}{
		Applications: GetUserApplicationsDir(runtime.GOOS == "darwin"),
		Fonts:        GetUserFontsDir(runtime.GOOS == "darwin"),
	},
}

// Files contains all file path structures
var Files = struct {
	ShellConfig string
	ZshEnv      string
}{
	ShellConfig: GetShellConfigFile(),
	ZshEnv:      GetZshEnvFile(),
}

// Public API functions

// ExpandHome replaces a leading "~" with the user's home directory, so CLI
// flags can accept paths like ~/code/repo. Other paths pass through unchanged.
func ExpandHome(path string) string {
	if path == "~" {
		return userHome()
	}
	if after, ok := strings.CutPrefix(path, "~/"); ok {
		return filepath.Join(userHome(), after)
	}
	return path
}

// Returns XDG_CONFIG_HOME or fallback to ~/.config
// Reads environment variables dynamically to support testing
func GetConfigDir(subPath ...string) string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".config")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// Returns XDG_DATA_HOME or fallback to ~/.local/share
// Reads environment variables dynamically to support testing
func GetDataDir(subPath ...string) string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".local", "share")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

func GetAppDir(subPath ...string) string {
	appRoot := GetDataDir(constants.App.Name)
	return filepath.Join(append([]string{appRoot}, subPath...)...)
}

func GetHomeDir(subPath ...string) string {
	return filepath.Join(append([]string{userHome()}, subPath...)...)
}

// Returns XDG_STATE_HOME or fallback to ~/.local/state
// Reads environment variables dynamically to support testing
func GetStateDir(subPath ...string) string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".local", "state")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// Returns XDG_CACHE_HOME or fallback to ~/.cache
// Reads environment variables dynamically to support testing
func GetCacheDir(subPath ...string) string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		base = filepath.Join(userHome(), ".cache")
	}
	return filepath.Join(append([]string{base}, subPath...)...)
}

// ScratchAllocPrefix is the name prefix every scratch allocation carries
// (`devgeta task scratch` creates `<scratch root>/task-*` via os.MkdirTemp).
// It is the ownership boundary for everything that deletes inside the
// scratch root — both `--clean` and configure's stale-directory prune refuse
// to touch an entry without it, so anything a user parks under the root is
// left alone (ADR-0015 §3).
//
// It lives here rather than beside the allocator because both sides need it
// and internal/apps/baseapp cannot import internal/tooling/task —
// baseapp → tooling/task → apps/git → baseapp is an import cycle.
const ScratchAllocPrefix = "task-"

// EnsureScratchDir creates (if absent) and tightens devgeta's scratch
// directory — a disposable, per-user location under the cache root
// (ADR-0015) that shipped commands use instead of `/tmp` for working files.
// It is called both by scratch allocation itself (so a wiped `~/.cache`
// self-heals on the very next allocation, matching ADR-0015 §1's own
// argument for choosing the cache root — anything may empty it) and by
// configure's maintenance helper (which additionally prunes stale
// subdirectories, a job allocation has no reason to do).
//
// MkdirAll alone is not sufficient to guarantee 0700: it only applies its
// mode to a directory it actually creates, and even then the mode is masked
// by the process umask. An unconditional Chmod follows to fix a directory
// left more permissive by an older devgeta or by anything else — only the
// leaf needs it, since traversing into it already requires being its owner.
func EnsureScratchDir() (string, error) {
	dir := GetCacheDir("devgeta", "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

// Returns user-level applications dir
func GetUserApplicationsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := "/Applications"
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux (XDG-compliant user apps)
	return GetDataDir(append([]string{"applications"}, subPath...)...)
}

// Returns system-level applications dir
func GetSystemApplicationsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := "/Applications"
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux system-wide applications dirs
	// NOTE: /usr/share/applications is more common, but /usr/local/share/applications is also valid
	// You could return both or let the caller choose
	base := "/usr/share/applications"
	return filepath.Join(append([]string{base}, subPath...)...)
}

func GetShellConfigFile() string {
	home := userHome()
	configDir := os.Getenv("XDG_CONFIG_HOME")
	if configDir == "" {
		configDir = filepath.Join(home, ".config")
	}
	shellConfigFiles := []string{
		filepath.Join(home, ".zshrc"),
		filepath.Join(home, ".bashrc"),
		filepath.Join(home, ".bash_profile"),
		filepath.Join(home, ".profile"),
		filepath.Join(configDir, "fish", "config.fish"),
	}
	for _, filepath := range shellConfigFiles {
		if FileAlreadyExist(filepath) {
			return filepath
		}
	}
	// If none exist, default to .zshrc
	return filepath.Join(home, ".zshrc")
}

// GetZshEnvFile returns the user's ~/.zshenv path. Unlike GetShellConfigFile,
// this never falls back to searching alternate candidates: ~/.zshenv is the
// one place zsh sources before anything else (login or not), whether or not
// the file exists yet.
func GetZshEnvFile() string {
	return filepath.Join(userHome(), ".zshenv")
}

// Returns user-level fonts dir
func GetUserFontsDir(isMac bool, subPath ...string) string {
	if isMac {
		home := userHome()
		base := filepath.Join(home, "Library", "Fonts")
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux user fonts (XDG-compliant)
	return GetDataDir(append([]string{"fonts"}, subPath...)...)
}

// Returns system-level fonts dir
func GetSystemFontsDir(isMac bool, subPath ...string) string {
	if isMac {
		base := filepath.Join("/Library", "Fonts")
		return filepath.Join(append([]string{base}, subPath...)...)
	}
	// Linux system fonts (common default)
	base := "/usr/share/fonts"
	return filepath.Join(append([]string{base}, subPath...)...)
}
