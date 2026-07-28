// -------------------------
// TODO: Write documentation how to use this
// - Tmux documentation: https://github.com/tmux/tmux
// - Personal configuration: https://github.com/cjairm/devenv/tree/main/tmux
// - Releases: https://github.com/tmux/tmux/releases
// - Installing instructions: https://github.com/tmux/tmux/wiki/Installing
// -------------------------

package tmux

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cjairm/devgeta/internal/apps"
	"github.com/cjairm/devgeta/internal/apps/baseapp"
	cmd "github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/config"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/paths"
)

var _ apps.App = (*Tmux)(nil)

const configFileName = ".tmux.conf"

type Tmux struct {
	Cmd  cmd.Command
	Base cmd.BaseCommandExecutor
}

func (t *Tmux) Name() string       { return constants.Tmux }
func (t *Tmux) Kind() apps.AppKind { return apps.KindTerminal }

func New() *Tmux {
	osCmd := cmd.NewCommand()
	baseCmd := cmd.NewBaseCommand()
	return &Tmux{Cmd: osCmd, Base: baseCmd}
}

func (t *Tmux) Install() error {
	return t.Cmd.InstallPackage(constants.Tmux)
}

func (t *Tmux) ForceInstall() error {
	return baseapp.Reinstall(t.Install, t.Uninstall)
}

func (t *Tmux) SoftInstall() error {
	return t.Cmd.MaybeInstallPackage(constants.Tmux)
}

func (t *Tmux) ForceConfigure() error {
	gc := &config.GlobalConfig{}
	if err := gc.Create(); err != nil {
		return fmt.Errorf("failed to create global config: %w", err)
	}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	gc.ReconcileShellFeatures()
	gc.AddToInstalled(constants.Tmux, "package")
	if err := enableFeature(gc); err != nil {
		return fmt.Errorf("failed to enable tmux feature: %w", err)
	}
	configDest := filepath.Join(paths.Paths.Home.Root, configFileName)
	if err := files.CopyFile(
		filepath.Join(paths.Paths.App.Configs.Tmux, "tmux.conf"),
		configDest,
	); err != nil {
		return err
	}
	// Reload the running tmux server if we're inside a session (best-effort).
	if os.Getenv("TMUX") != "" {
		_ = t.ExecuteCommand("source-file", configDest)
	}
	return nil
}

func (t *Tmux) SoftConfigure() error {
	configFile := filepath.Join(paths.Paths.Home.Root, configFileName)
	isFilePresent := files.FileAlreadyExist(configFile)
	if isFilePresent {
		gc := &config.GlobalConfig{}
		if err := gc.Create(); err != nil {
			return fmt.Errorf("failed to create global config: %w", err)
		}
		if err := gc.Load(); err != nil {
			return fmt.Errorf("failed to load global config: %w", err)
		}
		if !gc.IsShellFeatureEnabled(constants.Tmux) {
			if err := enableFeature(gc); err != nil {
				return fmt.Errorf("failed to enable tmux feature: %w", err)
			}
		}
		return nil
	}
	return t.ForceConfigure()
}

func (t *Tmux) Uninstall() error {
	gc := &config.GlobalConfig{}
	if err := gc.Load(); err != nil {
		return fmt.Errorf("failed to load global config: %w", err)
	}
	if err := t.Cmd.UninstallPackage(constants.Tmux); err != nil {
		return fmt.Errorf("failed to uninstall tmux: %w", err)
	}
	_ = os.Remove(filepath.Join(paths.Paths.Home.Root, configFileName))
	gc.DisableShellFeature(constants.Tmux)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to regenerate shell config: %w", err)
	}
	gc.RemoveFromInstalled(constants.Tmux, "package")
	return gc.Save()
}

func (t *Tmux) ExecuteCommand(args ...string) error {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    args,
	}
	if _, _, err := t.Base.ExecCommand(execCommand); err != nil {
		return fmt.Errorf("failed to execute tmux command: %w", err)
	}
	return nil
}

func (t *Tmux) Update() error {
	return fmt.Errorf("%w for tmux", apps.ErrUpdateNotSupported)
}

// CreateSession creates a new detached tmux session in the given directory
func (t *Tmux) CreateSession(name, workdir string) error {
	return t.ExecuteCommand("new-session", "-d", "-s", name, "-c", workdir)
}

// CreateSessionWithWindow creates a new detached session whose first (and only)
// window is named windowName, rooted at workdir. Used when a worktree's session
// does not yet exist, so the session starts with the worktree window directly
// instead of a stray default window.
func (t *Tmux) CreateSessionWithWindow(session, windowName, workdir string) error {
	return t.ExecuteCommand("new-session", "-d", "-s", session, "-n", windowName, "-c", workdir)
}

// CreateWindowInSession creates a window inside a specific session.
func (t *Tmux) CreateWindowInSession(session, name, workdir string) error {
	return t.ExecuteCommand("new-window", "-t", session+":", "-n", name, "-c", workdir)
}

// SessionInfo describes one tmux session for listing purposes.
type SessionInfo struct {
	Name     string
	Attached bool
}

// ListSessions returns every session on the tmux server. Unlike WindowSession,
// which swallows errors to a bool because callers only care whether a specific
// window was found, this distinguishes "genuinely no sessions" from "the query
// failed": callers populate a UI region from this and need to tell the two
// apart. tmux has two distinct ways of reporting "no server reachable", both
// meaning zero sessions legitimately exist: a stale socket left behind by a
// dead server ("no server running on <path>"), and no socket at all because
// tmux has never been started for this user ("error connecting to <path>
// (No such file or directory)" - the common case on a fresh machine). Both
// return (nil, nil). "error connecting to" alone is not sufficient: tmux uses
// that same prefix for permission-denied and non-socket-file errors on the
// socket path, which are real, actionable failures and must not be swallowed
// - so the ENOENT case is matched specifically by requiring "No such file or
// directory" too. Any other non-zero exit returns the wrapped error.
func (t *Tmux) ListSessions() ([]SessionInfo, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"list-sessions", "-F", "#{session_name}\t#{session_attached}"},
	}
	stdout, stderr, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		noServerRunning := strings.Contains(stderr, "no server running")
		noSocketFile := strings.Contains(stderr, "error connecting to") &&
			strings.Contains(stderr, "No such file or directory")
		if noServerRunning || noSocketFile {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to list tmux sessions: %w", err)
	}
	var sessions []SessionInfo
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		parts := strings.SplitN(strings.TrimSpace(scanner.Text()), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		sessions = append(sessions, SessionInfo{
			Name: parts[0],
			// session_attached is a count of attached clients, not a 0/1 flag
			// (man tmux) - any non-zero count means the session is attached.
			Attached: parts[1] != "0",
		})
	}
	return sessions, nil
}

// WindowSession returns the session that contains a window with the given name,
// searching across all sessions on the tmux server (not just the attached one).
// Returns ("", false) when no such window exists or no server is reachable.
func (t *Tmux) WindowSession(name string) (string, bool) {
	for _, sw := range t.SessionWindows() {
		if sw.Window == name {
			return sw.Session, true
		}
	}
	return "", false
}

// FindWindowsBySuffix returns the names of all windows whose name ends with the
// given suffix, searching across all sessions on the tmux server. It is used to
// locate an orphaned worktree window when the owning repo can no longer be
// determined (worktree windows are named "wt-<repo>-<flat-name>", so a suffix like
// "-<flat-name>" still finds them). Returns nil when none match or no server is
// reachable. Callers must handle multiple matches — the same worktree name can
// exist across repos — to avoid killing the wrong window.
func (t *Tmux) FindWindowsBySuffix(suffix string) []string {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"list-windows", "-a", "-F", "#{window_name}"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return nil
	}
	var matches []string
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		name := strings.TrimSpace(scanner.Text())
		if strings.HasSuffix(name, suffix) {
			matches = append(matches, name)
		}
	}
	return matches
}

// SessionWindow pairs one tmux window with the session that owns it.
type SessionWindow struct {
	Session string
	Window  string
}

// PaneState holds the current state of a single pane.
type PaneState struct {
	Session string
	Window  string
	PaneID  string
	State   string // "@dg_agent_state" value; "" means no agent has written to this pane
}

// SessionWindows returns every (session, window) pair on the tmux server from
// a single list-windows -a scan. It is the shared shape callers reach for when
// they need to correlate windows to their sessions in bulk - e.g.
// WorktreeManager.ListSessions uses it to find every session hosting a
// wt-prefixed window without one exec call per session. Returns nil when no
// server is reachable or the query fails, matching WindowSession and
// FindWindowsBySuffix's existing tolerance for this same command.
func (t *Tmux) SessionWindows() []SessionWindow {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"list-windows", "-a", "-F", "#{session_name}\t#{window_name}"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return nil
	}
	var pairs []SessionWindow
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		parts := strings.SplitN(strings.TrimSpace(scanner.Text()), "\t", 2)
		if len(parts) != 2 {
			continue
		}
		pairs = append(pairs, SessionWindow{Session: parts[0], Window: parts[1]})
	}
	return pairs
}

// PaneStates returns every pane on the tmux server with its session, window, pane ID, and
// agent state from a single list-panes -a scan. The agent state field may be an empty string
// when the @dg_agent_state pane option has not yet been set by an agent. Returns nil when no
// server is reachable or the query fails, matching SessionWindows's existing tolerance for
// this same command.
func (t *Tmux) PaneStates() []PaneState {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args: []string{
			"list-panes",
			"-a",
			"-F",
			"#{session_name}\t#{window_name}\t#{pane_id}\t#{@dg_agent_state}",
		},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return nil
	}
	var states []PaneState
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		parts := strings.SplitN(scanner.Text(), "\t", 4)
		if len(parts) != 4 {
			continue
		}
		states = append(
			states,
			PaneState{
				Session: strings.TrimSpace(parts[0]),
				Window:  strings.TrimSpace(parts[1]),
				PaneID:  strings.TrimSpace(parts[2]),
				State:   strings.TrimSpace(parts[3]),
			},
		)
	}
	return states
}

// ClearAgentStateForWindow unsets @dg_agent_state on every pane belonging to
// the named window, across all sessions (mirroring WindowSession's "search
// every session" semantics - a window name is unique in practice, but this
// doesn't assume it). Called when the user attaches to a window: attaching is
// the user acknowledging whatever state was showing, and the next real turn
// writes busy/idle/etc. itself - clearing (not writing a value) restores the
// pane to "no agent state to report", matching what an agent that has never
// run in that pane looks like (ADR-0005). Also clears the window-level mirror
// (@dg_window_agent_state) used by the status bar to flag unattended windows.
// A window with no matching panes (e.g. one that doesn't exist yet) is a silent
// no-op - safe to call unconditionally. Best-effort per pane: continues past a
// failure on one pane (e.g. a pane that closed between the scan and the clear)
// rather than aborting the rest; returns the first error encountered, if any,
// purely for logging - callers are not expected to treat this as fatal.
func (t *Tmux) ClearAgentStateForWindow(window string) error {
	var firstErr error
	var matchedPaneID string
	for _, ps := range t.PaneStates() {
		if ps.Window != window {
			continue
		}
		matchedPaneID = ps.PaneID
		if err := t.ExecuteCommand(
			"set-option", "-p", "-u", "-t", ps.PaneID, "@dg_agent_state",
		); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if matchedPaneID != "" {
		// Also clear the window-level status-bar mirror (Step 8,
		// @dg_window_agent_state) - otherwise a stale "wants you" flag can
		// reappear on tmux's own status bar once the user leaves this window,
		// even though the dashboard (which reads the pane-level values just
		// cleared above) correctly shows nothing pending. -w -t <any matching
		// pane> targets the window that pane belongs to; a window-level
		// option only needs clearing once, not once per pane.
		if err := t.ExecuteCommand(
			"set-option", "-w", "-u", "-t", matchedPaneID, "@dg_window_agent_state",
		); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// SwitchToWindow moves the attached client to the given session and selects the
// window, so it works no matter which session the client is currently on.
func (t *Tmux) SwitchToWindow(session, name string) error {
	if err := t.SwitchToSession(session); err != nil {
		return err
	}
	return t.ExecuteCommand("select-window", "-t", session+":"+name)
}

// CurrentSession returns the name of the session the attached client is on.
// Returns ("", false) when not running inside tmux or when the query fails.
func (t *Tmux) CurrentSession() (string, bool) {
	if os.Getenv("TMUX") == "" {
		return "", false
	}
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"display-message", "-p", "#{session_name}"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return "", false
	}
	name := strings.TrimSpace(stdout)
	if name == "" {
		return "", false
	}
	return name, true
}

// SwitchToSession moves the attached client to the given session.
func (t *Tmux) SwitchToSession(name string) error {
	return t.ExecuteCommand("switch-client", "-t", name)
}

// SendKeysToWindowInSession sends keystrokes to a window in a specific session.
func (t *Tmux) SendKeysToWindowInSession(session, window, keys string) error {
	return t.ExecuteCommand("send-keys", "-t", session+":"+window, keys, "Enter")
}

// KillSession terminates a tmux session
func (t *Tmux) KillSession(name string) error {
	return t.ExecuteCommand("kill-session", "-t", name)
}

// HasSession checks if a session exists
func (t *Tmux) HasSession(name string) bool {
	err := t.ExecuteCommand("has-session", "-t", name)
	return err == nil
}

// SendKeys sends keystrokes to a session
func (t *Tmux) SendKeys(session, keys string) error {
	return t.ExecuteCommand("send-keys", "-t", session, keys, "Enter")
}

// CreateWindow creates a new window in the current session
func (t *Tmux) CreateWindow(name, workdir string) error {
	return t.ExecuteCommand("new-window", "-n", name, "-c", workdir)
}

// SplitWindow splits an existing window, rooting the new pane at workdir.
// direction describes the resulting pane arrangement (this cycle's Layout
// model's language), not tmux's own flag letters: "vertical" means panes
// side by side (tmux's -h), "horizontal" means panes stacked (tmux's -v).
func (t *Tmux) SplitWindow(window, workdir, direction string) error {
	switch direction {
	case "vertical":
		return t.ExecuteCommand("split-window", "-h", "-t", window, "-c", workdir)
	case "horizontal":
		return t.ExecuteCommand("split-window", "-v", "-t", window, "-c", workdir)
	default:
		return fmt.Errorf(
			"unknown split direction %q (want \"vertical\" or \"horizontal\")",
			direction,
		)
	}
}

// ActivePaneID returns the tmux pane_id (e.g. "%12") of window's currently
// active pane. window may be a bare window name (resolved in the current/
// attached session, matching SendKeysToWindow's target form) or a qualified
// "session:window" target (matching SendKeysToWindowInSession's form) -
// whichever form the caller already has.
//
// Pane IDs, not pane indexes, are the only reliable way to re-target a
// specific pane once other panes may have been created around it: tmux
// numbers a window's panes starting from the "pane-base-index" option, which
// devgeta's own shipped tmux.conf sets to 1 (configs/tmux/tmux.conf) - so on
// a devgeta-configured server, pane index 0 does not exist, and even a
// correctly-computed base-index is only a snapshot that stops matching once
// panes are added/removed/reordered. Pane IDs are assigned by tmux in
// creation order and are unique server-wide regardless of that option, so
// capturing the id right after the window is created (while its sole pane is
// unambiguously "the active one") and re-selecting by that id later is
// robust to all of the above.
func (t *Tmux) ActivePaneID(window string) (string, error) {
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"display-message", "-t", window, "-p", "#{pane_id}"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return "", fmt.Errorf("failed to query active pane for %s: %w", window, err)
	}
	id := strings.TrimSpace(stdout)
	if id == "" {
		return "", fmt.Errorf("tmux returned no pane id for %s", window)
	}
	return id, nil
}

// SelectPane makes the pane identified by paneID (a tmux pane_id like "%12",
// as returned by ActivePaneID) the active pane in its window. Pane IDs are
// unique server-wide, so no window or session qualification is needed.
func (t *Tmux) SelectPane(paneID string) error {
	return t.ExecuteCommand("select-pane", "-t", paneID)
}

// HasWindow checks if a window exists in the current session
func (t *Tmux) HasWindow(name string) bool {
	if os.Getenv("TMUX") == "" {
		return false
	}
	execCommand := cmd.CommandParams{
		Command: constants.Tmux,
		Args:    []string{"list-windows", "-F", "#{window_name}"},
	}
	stdout, _, err := t.Base.ExecCommand(execCommand)
	if err != nil {
		return false
	}
	scanner := bufio.NewScanner(strings.NewReader(stdout))
	for scanner.Scan() {
		if strings.TrimSpace(scanner.Text()) == name {
			return true
		}
	}
	return false
}

// KillWindow closes a specific window by name. It resolves the window's session
// first so windows living in a session other than the attached one are still
// killed; falls back to a bare name target if the lookup finds nothing.
func (t *Tmux) KillWindow(name string) error {
	if session, ok := t.WindowSession(name); ok {
		return t.ExecuteCommand("kill-window", "-t", session+":"+name)
	}
	return t.ExecuteCommand("kill-window", "-t", name)
}

// SendKeysToWindow sends keystrokes to a specific window
func (t *Tmux) SendKeysToWindow(window, keys string) error {
	return t.ExecuteCommand("send-keys", "-t", window, keys, "Enter")
}

// SelectWindow switches focus to a specific window by name
func (t *Tmux) SelectWindow(name string) error {
	return t.ExecuteCommand("select-window", "-t", name)
}

func enableFeature(gc *config.GlobalConfig) error {
	gc.EnableShellFeature(constants.Tmux)
	if err := gc.RegenerateShellConfig(); err != nil {
		return fmt.Errorf("failed to generate shell config: %w", err)
	}
	if err := gc.Save(); err != nil {
		return fmt.Errorf("failed to save global config: %w", err)
	}
	return nil
}
