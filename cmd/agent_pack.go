package cmd

import (
	"fmt"
	"io"
	"strings"

	"github.com/cjairm/devgeta/internal/commands"
	"github.com/cjairm/devgeta/internal/tooling/agentpack"
	"github.com/cjairm/devgeta/pkg/constants"
	"github.com/spf13/cobra"
)

// agentPackEngine is what the command needs from the engine in
// internal/tooling/agentpack. It is an interface so the command's tests can
// answer with canned reports; the engine has its own tests.
type agentPackEngine interface {
	Install(agentpack.InstallRequest) (agentpack.Report, error)
	Uninstall(agentpack.UninstallRequest) (agentpack.Report, error)
	Update(pack string) ([]agentpack.Report, error)
	Check(agentpack.CheckRequest) (agentpack.CheckReport, error)
	Status() ([]agentpack.PackStatus, error)
	Packs() []agentpack.Manifest
}

// newAgentPackEngine builds the real engine; overridden in tests.
var newAgentPackEngine = func() (agentPackEngine, error) {
	engine, err := agentpack.NewEngine()
	if err != nil {
		return nil, err
	}
	return engine, nil
}

func init() {
	rootCmd.AddCommand(newAgentPackCmd())
}

// newAgentPackCmd builds the `dg agent-pack` tree. Each subcommand keeps its
// flags in its own closure, so a fresh tree (as the tests build) starts clean.
func newAgentPackCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "agent-pack",
		Short: "Install agent packs (skills, hooks and rules) into Claude Code, OpenCode and Pi",
		Long: `Install, update and remove agent packs.

An agent pack is a pinned upstream plus devgeta's own layer, built into one
folder and installed into each coder through that coder's own plugin or package
system:

  superpowers  skills and a session bootstrap (https://github.com/obra/superpowers)
  ponytail     a lazy-senior-dev ruleset (https://github.com/DietrichGebert/ponytail)
  devgeta      devgeta's response and writing rules (DEVGETA.md)

Every pack is fetched at a pinned tag and commit, so what you get is what
devgeta was checked against. Anything a coder can't load is named in the output.

To change a pack, put files laid out like the pack under
~/.config/devgeta/agent-packs/<pack>/ (for example skills/<name>/SKILL.md) and
run dg agent-pack update <pack>. See docs/apps/agent-packs.md.

Examples:
  dg agent-pack list
  dg agent-pack install superpowers
  dg agent-pack install ponytail --coder claude,pi
  dg agent-pack update
  dg agent-pack check superpowers --ref v7.0.3`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(
		newAgentPackListCmd(),
		newAgentPackInstallCmd(),
		newAgentPackUninstallCmd(),
		newAgentPackUpdateCmd(),
		newAgentPackCheckCmd(),
	)
	return root
}

// withAgentPackEngine refreshes the pack definitions embedded in the binary,
// which an old extraction would leave at an old pin, then runs fn with an
// engine.
func withAgentPackEngine(fn func(agentPackEngine) error) error {
	if err := refreshEmbeddedConfigs(); err != nil {
		return err
	}
	engine, err := newAgentPackEngine()
	if err != nil {
		return err
	}
	return fn(engine)
}

func newAgentPackListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "Show each pack, its pinned and installed version, and each coder's state",
		Long: `Show each pack with the version this devgeta installs (pinned), the version
installed, and for every coder whether the pack is installed there.

A coder that isn't installed is shown as skipped, with the reason. A pack Pi
should hold but its settings no longer list is flagged with the command that
puts it back.

Example:
  dg agent-pack list`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return withAgentPackEngine(func(e agentPackEngine) error {
				status, err := e.Status()
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), formatAgentPackList(status))
				return nil
			})
		},
	}
}

func newAgentPackInstallCmd() *cobra.Command {
	var coders, exclude []string
	c := &cobra.Command{
		Use:   "install <pack>",
		Short: "Install a pack into your coders",
		Long: `Fetch a pack at its pinned version, build it, and install it into your coders.

Without --coder it goes into every coder that is installed. A coder that isn't
installed is skipped, and the output says so. If any coder fails, the ones
already done in this run are undone.

--exclude drops skills from the built pack. It is saved, so later installs and
updates keep it. A new --exclude replaces the saved list, --exclude= clears
it, and leaving the flag out keeps it. Excluding a skill that a kept skill
names is refused, with both skills named.

If the same upstream is already installed another way (for example Superpowers
from claude-plugins-official), devgeta stops and prints the command that removes
that copy. It never removes a copy it didn't install.

Examples:
  dg agent-pack install superpowers
  dg agent-pack install ponytail --coder claude,pi
  dg agent-pack install superpowers --exclude dispatching-parallel-agents
  dg agent-pack install superpowers --exclude=`,
		Args: cobra.ExactArgs(1),
	}
	c.Flags().StringSliceVar(&coders, "coder", nil,
		"Install only into these coders (comma-separated: claude, opencode, pi)")
	c.Flags().StringSliceVar(&exclude, "exclude", nil,
		"Skills to leave out (comma-separated); replaces the saved list, and --exclude= clears it")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		req := agentpack.InstallRequest{Pack: args[0], Coders: coders}
		if cmd.Flags().Changed("exclude") {
			list := append([]string{}, exclude...)
			req.Exclude = &list
		}
		return withAgentPackEngine(func(e agentPackEngine) error {
			report, err := e.Install(req)
			out := cmd.OutOrStdout()
			fmt.Fprint(out, formatInstallReport("Installed", report))
			if err != nil {
				return err
			}
			fmt.Fprint(out, packFollowUp(e, report))
			return nil
		})
	}
	return c
}

func newAgentPackUninstallCmd() *cobra.Command {
	var coders []string
	c := &cobra.Command{
		Use:   "uninstall <pack>",
		Short: "Remove a pack from your coders",
		Long: `Remove a pack from the coders that have it.

Only what devgeta installed is removed, as recorded in global_config.yaml.
Other plugins, packages and edits in your coders' config are left alone. When no
coder has the pack any more its built folder is deleted too; the downloaded
copy is kept so a reinstall needs no network.

Examples:
  dg agent-pack uninstall superpowers
  dg agent-pack uninstall devgeta --coder claude`,
		Args: cobra.ExactArgs(1),
	}
	c.Flags().StringSliceVar(&coders, "coder", nil,
		"Remove only from these coders (comma-separated: claude, opencode, pi)")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return withAgentPackEngine(func(e agentPackEngine) error {
			report, err := e.Uninstall(agentpack.UninstallRequest{Pack: args[0], Coders: coders})
			fmt.Fprint(cmd.OutOrStdout(), formatInstallReport("Removed", report))
			return err
		})
	}
	return c
}

func newAgentPackUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update [pack]",
		Short: "Move installed packs to the version this devgeta pins, and rebuild them",
		Long: `Move an installed pack to the version pinned by this devgeta and rebuild it,
into the coders that already have it. With no pack it updates every installed
pack.

It always rebuilds, so overrides you changed under
~/.config/devgeta/agent-packs/<pack>/ are picked up too. Saved --exclude lists
are kept.

Examples:
  dg agent-pack update
  dg agent-pack update superpowers`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			pack := ""
			if len(args) == 1 {
				pack = args[0]
			}
			return withAgentPackEngine(func(e agentPackEngine) error {
				reports, err := e.Update(pack)
				out := cmd.OutOrStdout()
				if err == nil && len(reports) == 0 {
					fmt.Fprintln(out, "No agent packs are installed. Install one with: dg agent-pack install <pack>")
					return nil
				}
				for _, report := range reports {
					fmt.Fprint(out, formatInstallReport("Updated", report))
				}
				return err
			})
		},
	}
}

func newAgentPackCheckCmd() *cobra.Command {
	var ref string
	c := &cobra.Command{
		Use:   "check <pack>",
		Short: "Build a pack and run its checks without installing anything",
		Long: `Build a pack into a temporary folder, run every build check, and report. It
installs nothing and changes no state, so it is how to try a newer upstream tag
before devgeta ships it.

It also runs claude plugin validate on the build when Claude Code is installed,
prints the Pi version it ran against when Pi is installed, and lists the files
you replace in full whose upstream copy changed since you replaced them: those
need a person to re-read the upstream change.

Examples:
  dg agent-pack check superpowers
  dg agent-pack check superpowers --ref v7.0.3`,
		Args: cobra.ExactArgs(1),
	}
	c.Flags().StringVar(&ref, "ref", "", "Try this upstream tag instead of the pinned one")
	c.RunE = func(cmd *cobra.Command, args []string) error {
		return withAgentPackEngine(func(e agentPackEngine) error {
			report, err := e.Check(agentpack.CheckRequest{Pack: args[0], Ref: ref})
			fmt.Fprint(cmd.OutOrStdout(), formatCheckReport(report, err))
			return err
		})
	}
	return c
}

// shortSHA is the 7-character form of a commit, or "" for none.
func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}

// packLabel is "superpowers v7.0.2 (22f0b46)"; an empty commit (a pack that
// ships in the binary) leaves the parentheses out.
func packLabel(pack, version, sha string) string {
	label := strings.TrimSpace(pack + " " + version)
	if s := shortSHA(sha); s != "" {
		label += " (" + s + ")"
	}
	return label
}

// formatInstallReport prints one pack's per-coder outcomes after an install,
// update or uninstall. done is the past-tense verb for a coder that was done.
func formatInstallReport(done string, report agentpack.Report) string {
	if report.Pack == "" {
		return ""
	}
	var b strings.Builder
	anyDone := false
	for _, o := range report.Outcomes {
		anyDone = anyDone || o.Done
	}
	if anyDone {
		fmt.Fprintf(&b, "%s %s\n", done, packLabel(report.Pack, report.Version, report.SHA))
	} else {
		fmt.Fprintf(&b, "Nothing was %s for %s\n", strings.ToLower(done), report.Pack)
	}
	width := 0
	for _, o := range report.Outcomes {
		if len(o.Coder) > width {
			width = len(o.Coder)
		}
	}
	for _, o := range report.Outcomes {
		state := strings.ToLower(done)
		if !o.Done {
			state = "skipped"
			if o.Reason != "" {
				state += ": " + o.Reason
			}
		}
		fmt.Fprintf(&b, "  %-*s  %s\n", width, o.Coder, state)
		for _, p := range o.Parts {
			fmt.Fprintf(&b, "    not loaded: %s\n", p)
		}
		for _, n := range o.Notes {
			fmt.Fprintf(&b, "    %s\n", n)
		}
	}
	// A coder that wasn't installed can be filled in once it is.
	if strings.EqualFold(done, "Installed") || strings.EqualFold(done, "Updated") {
		for _, o := range report.Outcomes {
			if !o.Done && strings.Contains(o.Reason, "is not installed") {
				fmt.Fprintf(&b, "  To finish %s later: dg agent-pack install %s --coder %s\n",
					o.Coder, report.Pack, o.Coder)
			}
		}
	}
	return b.String()
}

// packFollowUp prints, after an install that put the pack somewhere, the
// commands the pack needs on PATH that are missing (a warning, not a failure)
// and the pack's own notices for the coders it went into.
func packFollowUp(e agentPackEngine, report agentpack.Report) string {
	installed := map[string]bool{}
	for _, o := range report.Outcomes {
		if o.Done {
			installed[o.Coder] = true
		}
	}
	if len(installed) == 0 {
		return ""
	}
	var manifest *agentpack.Manifest
	for _, m := range e.Packs() {
		if m.Name == report.Pack {
			m := m
			manifest = &m
		}
	}
	if manifest == nil {
		return ""
	}
	var b strings.Builder
	for _, r := range manifest.MissingRuntimes(commands.LookPathFn) {
		fmt.Fprintf(&b, "Warning: %s is not on PATH. %s needs it for %s.\n", r.Command, manifest.Name, r.Reason)
	}
	for _, n := range manifest.Notices {
		if n.Coder == "" || installed[n.Coder] {
			fmt.Fprintf(&b, "%s\n", strings.TrimSpace(n.Text))
		}
	}
	return b.String()
}

func formatCheckReport(report agentpack.CheckReport, err error) string {
	var b strings.Builder
	label := packLabel(report.Pack, report.Tag, report.SHA)
	if err == nil {
		fmt.Fprintf(&b, "%s builds and passes every check.\n", label)
	} else if label != "" {
		fmt.Fprintf(&b, "%s did not pass its checks.\n", label)
	}
	for _, f := range report.Findings {
		fmt.Fprintf(&b, "  %s\n", f)
	}
	if len(report.Changed) > 0 {
		fmt.Fprintf(&b, "Upstream changed these files you replace in full; re-read each one:\n")
		for _, f := range report.Changed {
			fmt.Fprintf(&b, "  %s\n", f)
		}
	}
	if err == nil {
		fmt.Fprintln(&b, "Nothing was installed.")
	}
	return b.String()
}

func formatAgentPackList(status []agentpack.PackStatus) string {
	var b strings.Builder
	width := 0
	for _, p := range status {
		for _, c := range p.Coders {
			if len(c.Coder) > width {
				width = len(c.Coder)
			}
		}
	}
	for _, p := range status {
		fmt.Fprintf(&b, "%s\n", packListHeader(p))
		if p.Installed != "" && p.Installed != p.Pinned && p.Pinned != "bundled" {
			fmt.Fprintf(&b, "  behind the pin: dg agent-pack update %s\n", p.Pack)
		}
		for _, c := range p.Coders {
			line := c.State
			if c.Detail != "" {
				if c.State == "skipped" {
					line += ": " + c.Detail
				} else {
					line += " (" + c.Detail + ")"
				}
			}
			fmt.Fprintf(&b, "  %-*s  %s\n", width, c.Coder, line)
		}
	}
	return b.String()
}

func packListHeader(p agentpack.PackStatus) string {
	pin := "pinned " + p.Pinned
	if p.Pinned == "bundled" {
		pin = "bundled"
	}
	installed := "not installed"
	if p.Installed != "" {
		installed = "installed " + p.Installed
		if s := shortSHA(p.SHA); s != "" {
			installed += " (" + s + ")"
		}
	}
	return fmt.Sprintf("%s  %s  %s", p.Pack, pin, installed)
}

// agentPackPlan says where `dg install` puts the default packs.
type agentPackPlan struct {
	// explicit is `--only agent-packs`: every configured coder.
	explicit bool
	// fresh are the coders this run installed, the only ones a default
	// install touches.
	fresh []string
	// already are the coders this run found installed. Their agent content is
	// the user's, so they are left alone and told how to opt in.
	already []string
}

// onlyCoders keeps the names that are AI coders, in order.
func onlyCoders(names []string) []string {
	var out []string
	for _, n := range names {
		switch n {
		case constants.Claude, constants.OpenCode, constants.Pi:
			out = append(out, n)
		}
	}
	return out
}

// installDefaultAgentPacks installs the default packs into the coders the plan
// names and prints what happened. Re-running `dg install` never changes an
// existing coder's agent content, so a coder that was already there only gets
// the command to add the packs. A pack that fails is reported and the next one
// is still tried.
func installDefaultAgentPacks(out io.Writer, e agentPackEngine, plan agentPackPlan) {
	// The plan comes from the terminal summary, which lists every app it could
	// tell about; only coders take packs, and `--coder git` would be an error.
	plan.fresh, plan.already = onlyCoders(plan.fresh), onlyCoders(plan.already)
	var coders []string
	switch {
	case plan.explicit:
		fmt.Fprintln(out, "Installing agent packs into every configured coder...")
	case len(plan.fresh) > 0:
		coders = plan.fresh
		fmt.Fprintf(out, "Installing agent packs into %s...\n", strings.Join(coders, ", "))
	default:
		fmt.Fprintln(out, "No coder was installed in this run, so no agent packs were installed.")
		printAgentPackHints(out, plan.already)
		if len(plan.already) == 0 {
			fmt.Fprintln(out, "Install packs later with: dg agent-pack install <pack>")
		}
		return
	}
	for _, pack := range agentpack.DefaultPacks {
		report, err := e.Install(agentpack.InstallRequest{Pack: pack, Coders: coders})
		fmt.Fprint(out, formatInstallReport("Installed", report))
		if err != nil {
			fmt.Fprintf(out, "Could not install %s: %v\n  Retry with: dg agent-pack install %s\n", pack, err, pack)
			continue
		}
		fmt.Fprint(out, packFollowUp(e, report))
	}
	if !plan.explicit {
		printAgentPackHints(out, plan.already)
	}
}

// printAgentPackHints tells the user how to put the default packs into coders
// that were installed before this run.
func printAgentPackHints(out io.Writer, already []string) {
	for _, coder := range already {
		fmt.Fprintf(out, "%s was already installed, so its agent content was left alone. To add the default packs:\n", coder)
		for _, pack := range agentpack.DefaultPacks {
			fmt.Fprintf(out, "  dg agent-pack install %s --coder %s\n", pack, coder)
		}
	}
}
