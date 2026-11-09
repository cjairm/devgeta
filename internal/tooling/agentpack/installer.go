package agentpack

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/cjairm/devgeta/internal/config"
)

// Record is what one pack added to one coder, as the installer reports it and
// as it is kept in state.
type Record = config.AgentPackCoderState

// Tree is a built pack: the manifest it came from and the directory every
// coder is pointed at.
type Tree struct {
	Pack Manifest
	Dir  string
}

// Installer puts a built tree into one coder, through that coder's own plugin
// or package system. It is the only part of the engine that knows a coder.
type Installer interface {
	// Name is the coder's name (constants.Claude, OpenCode, Pi).
	Name() string
	// Available reports whether the coder is installed, so it can be skipped
	// with a reason when it is not.
	Available() bool
	// Conflicts lists, for a pack installed some other way, the exact command
	// that removes that copy. devgeta never runs it.
	Conflicts(pack Manifest) []string
	// Install registers the tree with the coder. On error it must leave
	// nothing behind: the engine does not undo a half-done install.
	Install(tree Tree) (Record, error)
	// Uninstall removes exactly what the record lists, and nothing else.
	Uninstall(r Record) error
	// Skipped names the parts of the pack this coder cannot load, and why.
	Skipped(tree Tree) []string
}

// Renderer is implemented by installers whose coder config file is rendered
// from agent_packs state (OpenCode). The engine hands it the state as it will
// be, before that state is saved, so the single state write can stay last and
// a failure can still leave state untouched (ADR-0070 §7).
type Renderer interface {
	Render(packs map[string]config.AgentPackState) error
}

// Finisher is implemented by installers with something shared to clean up once
// no pack is left for the coder (Claude's marketplace).
type Finisher interface{ Finish() error }

// RecordMerger is implemented by installers whose record holds a fact a repeat
// install cannot work out again. Claude's installer finds the @DEVGETA.md line
// already there on a second run and would record "not added", so uninstall
// would never remove a line devgeta did add; MergeRecord keeps that fact. It
// is called only when the coder already had a record for the pack.
type RecordMerger interface {
	MergeRecord(prior, next Record) Record
}

// Noter is implemented by installers that have something to tell the user. The
// engine collects the notes after each install or uninstall call and puts them
// in the coder's outcome; the command prints them.
type Noter interface {
	TakeNotes() []string
}

// Inspection is what an installer found when looking over a built tree.
type Inspection struct {
	// Lines are findings worth showing, passing or not.
	Lines []string
	// Err is a finding that fails the check.
	Err error
}

// Inspector is implemented by installers that can look over a built tree
// without installing it: Claude Code validates it, Pi reports its version.
type Inspector interface {
	Inspect(tree Tree) Inspection
}

// Verifier is implemented by installers whose coder config devgeta does not
// render from state, so what the coder holds can drift from the record. It
// returns a one-line problem, or "" when the record still holds.
type Verifier interface {
	Verify(pack string, r Record) string
}

// TreeSource builds the tree for a pack and deletes it again.
type TreeSource interface {
	Prepare(m Manifest, exclude []string) (Tree, BuildResult, error)
	// Check builds m into a temp tree, shows it to inspect, and deletes it. A
	// non-empty ref is tried instead of the pin; the commit it resolved to is
	// returned.
	Check(m Manifest, ref string, exclude []string, inspect func(dir string) error) (BuildResult, string, error)
	Remove(pack string) error
}

// StateStore reads agent_packs state and applies one change to it. Update runs
// fn against the freshly read state under the config's lock.
type StateStore interface {
	Load() (map[string]config.AgentPackState, error)
	Update(fn func(packs map[string]config.AgentPackState) error) error
}

// Engine orchestrates a pack across coders: build once, install into each,
// undo on failure, and write state last.
type Engine struct {
	Manifests  []Manifest
	Installers []Installer
	Trees      TreeSource
	State      StateStore
}

// InstallRequest asks for one pack in some coders.
type InstallRequest struct {
	Pack string
	// Coders limits the install; empty means every coder.
	Coders []string
	// Exclude replaces the saved exclusion list when non-nil, clears it when
	// non-nil and empty, and leaves it alone when nil.
	Exclude *[]string
}

// UninstallRequest asks for one pack to be removed from some coders.
type UninstallRequest struct {
	Pack string
	// Coders limits the removal; empty means every coder that has the pack.
	Coders []string
}

// CoderOutcome is what happened for one coder. Done is false for a coder that
// was skipped, with the reason in Reason.
type CoderOutcome struct {
	Coder  string
	Done   bool
	Reason string
	// Parts names what the coder cannot load (install only).
	Parts []string
	// Notes are things an installer wants the user to know, for example that
	// a line was left in a file because devgeta did not add it.
	Notes []string
}

// Report is the result of an install or uninstall.
type Report struct {
	Pack     string
	Version  string
	SHA      string
	Outcomes []CoderOutcome
}

// ConflictError means the pack is already installed another way. Nothing was
// built or installed.
type ConflictError struct {
	Pack     string
	Messages []string
}

func (e *ConflictError) Error() string {
	return fmt.Sprintf(
		"%s is already installed another way, and devgeta never removes a copy it did not install. "+
			"Remove the other copy, then run `dg agent-pack install %s` again:\n  %s",
		e.Pack, e.Pack, strings.Join(e.Messages, "\n  "),
	)
}

func (e *Engine) manifest(name string) (Manifest, error) {
	for _, m := range e.Manifests {
		if m.Name == name {
			return m, nil
		}
	}
	known := make([]string, 0, len(e.Manifests))
	for _, m := range e.Manifests {
		known = append(known, m.Name)
	}
	sort.Strings(known)
	return Manifest{}, fmt.Errorf("unknown pack %q (known packs: %s)", name, strings.Join(known, ", "))
}

func (e *Engine) installerNames() string {
	names := make([]string, 0, len(e.Installers))
	for _, i := range e.Installers {
		names = append(names, i.Name())
	}
	return strings.Join(names, ", ")
}

// pick returns the installers named in requested (all of them when it is
// empty), in the engine's own order, rejecting a name no installer has.
func (e *Engine) pick(requested []string) ([]Installer, error) {
	if len(requested) == 0 {
		return e.Installers, nil
	}
	want := map[string]bool{}
	for _, name := range requested {
		found := false
		for _, i := range e.Installers {
			if i.Name() == name {
				found = true
			}
		}
		if !found {
			return nil, fmt.Errorf("unknown coder %q (known coders: %s)", name, e.installerNames())
		}
		want[name] = true
	}
	var out []Installer
	for _, i := range e.Installers {
		if want[i.Name()] {
			out = append(out, i)
		}
	}
	return out, nil
}

func notInstalledReason(coder string) string {
	return fmt.Sprintf("%s is not installed", coder)
}

// Install installs a pack into the selected coders.
//
// Order matters: coders are chosen and conflicts checked before anything is
// fetched, the tree is built once, each coder is installed in turn, and state
// is written once at the very end. If a coder fails, the coders this run added
// are uninstalled in reverse and state is never touched. A coder that already
// had the pack is not undone: every coder points at the same tree path, so
// reinstalling refreshes it in place and rolling back would remove an install
// that was fine before this run.
func (e *Engine) Install(req InstallRequest) (report Report, err error) {
	m, err := e.manifest(req.Pack)
	if err != nil {
		return Report{}, err
	}
	chosen, err := e.pick(req.Coders)
	if err != nil {
		return Report{}, err
	}

	version := m.Tag
	if m.Embedded() {
		version = bundledVersion
	}
	report = Report{Pack: m.Name, Version: version, SHA: m.SHA}
	outcomes := map[string]*CoderOutcome{}
	var targets []Installer
	for _, ins := range chosen {
		o := &CoderOutcome{Coder: ins.Name()}
		outcomes[ins.Name()] = o
		if !ins.Available() {
			o.Reason = notInstalledReason(ins.Name())
			continue
		}
		targets = append(targets, ins)
	}
	defer func() {
		for _, ins := range chosen {
			report.Outcomes = append(report.Outcomes, *outcomes[ins.Name()])
		}
	}()
	if len(targets) == 0 {
		return report, nil
	}

	var conflicts []string
	for _, ins := range targets {
		conflicts = append(conflicts, ins.Conflicts(m)...)
	}
	if len(conflicts) > 0 {
		return report, &ConflictError{Pack: m.Name, Messages: conflicts}
	}

	current, err := e.State.Load()
	if err != nil {
		return report, fmt.Errorf("failed to read agent pack state: %w", err)
	}
	exclude := current[m.Name].Exclude
	if req.Exclude != nil {
		exclude = *req.Exclude
	}
	if len(exclude) == 0 {
		exclude = nil
	}

	tree, built, err := e.Trees.Prepare(m, exclude)
	if err != nil {
		return report, err
	}

	var added []Installer
	records := map[string]Record{}
	work := config.CloneAgentPacks(current)
	if work == nil {
		work = map[string]config.AgentPackState{}
	}
	rollback := func(cause error) (Report, error) {
		return report, e.rollback(cause, m.Name, added, records, current)
	}

	for _, ins := range targets {
		rec, err := ins.Install(tree)
		if err != nil {
			return rollback(fmt.Errorf("installing %s into %s: %w", m.Name, ins.Name(), err))
		}
		prior, had := current[m.Name].Coders[ins.Name()]
		if had {
			if merger, ok := ins.(RecordMerger); ok {
				rec = merger.MergeRecord(prior, rec)
			}
		} else {
			added = append(added, ins)
		}
		records[ins.Name()] = rec
		st := work[m.Name]
		coders := map[string]Record{}
		for k, v := range st.Coders {
			coders[k] = v
		}
		coders[ins.Name()] = rec
		st.Coders = coders
		work[m.Name] = st
		if r, ok := ins.(Renderer); ok {
			if err := r.Render(work); err != nil {
				return rollback(fmt.Errorf("configuring %s for %s: %w", ins.Name(), m.Name, err))
			}
		}
		o := outcomes[ins.Name()]
		o.Done = true
		o.Parts = ins.Skipped(tree)
		o.Notes = takeNotes(ins)
	}

	err = e.State.Update(func(packs map[string]config.AgentPackState) error {
		st := packs[m.Name]
		st.Version, st.SHA = version, m.SHA
		st.Exclude = exclude
		st.ReplacedHashes = built.ReplacedHashes
		coders := map[string]Record{}
		for k, v := range st.Coders {
			coders[k] = v
		}
		for k, v := range records {
			coders[k] = v
		}
		st.Coders = coders
		packs[m.Name] = st
		return nil
	})
	if err != nil {
		return rollback(fmt.Errorf("saving the state of %s: %w", m.Name, err))
	}
	return report, nil
}

// rollback undoes the coders a failed install added, newest first, and
// re-renders the coders that render from state so they stop listing the pack.
// Failures while undoing are added to the cause rather than hidden, and never
// stop the remaining steps.
func (e *Engine) rollback(
	cause error, pack string, added []Installer, records map[string]Record,
	original map[string]config.AgentPackState,
) error {
	errs := []error{cause}
	for i := len(added) - 1; i >= 0; i-- {
		ins := added[i]
		if err := ins.Uninstall(records[ins.Name()]); err != nil {
			errs = append(errs, fmt.Errorf("could not undo %s in %s: %w", pack, ins.Name(), err))
		}
	}
	restored := config.CloneAgentPacks(original)
	if restored == nil {
		restored = map[string]config.AgentPackState{}
	}
	for i := len(added) - 1; i >= 0; i-- {
		if r, ok := added[i].(Renderer); ok {
			if err := r.Render(restored); err != nil {
				errs = append(errs, fmt.Errorf("could not restore %s's config: %w", added[i].Name(), err))
			}
		}
	}
	return errors.Join(errs...)
}

// Uninstall removes a pack from the selected coders, deleting only what the
// state record lists. A coder that fails or is not installed keeps its record;
// the others are still cleaned. When no coder has the pack any more, its built
// tree and state entry go too.
func (e *Engine) Uninstall(req UninstallRequest) (Report, error) {
	m, manifestErr := e.manifest(req.Pack)
	current, err := e.State.Load()
	if err != nil {
		return Report{}, fmt.Errorf("failed to read agent pack state: %w", err)
	}
	st, installed := current[req.Pack]
	if !installed {
		if manifestErr != nil {
			return Report{}, manifestErr
		}
		return Report{}, fmt.Errorf(
			"%s is not installed; `dg agent-pack list` shows what is", m.Name,
		)
	}
	chosen, err := e.pick(req.Coders)
	if err != nil {
		return Report{}, err
	}

	report := Report{Pack: req.Pack, Version: st.Version, SHA: st.SHA}
	work := config.CloneAgentPacks(current)
	removed := map[string]bool{}
	var errs []error

	for _, ins := range chosen {
		name := ins.Name()
		rec, has := st.Coders[name]
		if !has {
			if len(req.Coders) > 0 {
				report.Outcomes = append(report.Outcomes, CoderOutcome{
					Coder: name, Reason: fmt.Sprintf("%s is not installed in %s", req.Pack, name),
				})
			}
			continue
		}
		o := CoderOutcome{Coder: name}
		if !ins.Available() {
			o.Reason = fmt.Sprintf("%s, so its record was kept", notInstalledReason(name))
			report.Outcomes = append(report.Outcomes, o)
			continue
		}
		if err := ins.Uninstall(rec); err != nil {
			o.Reason = err.Error()
			o.Notes = takeNotes(ins)
			report.Outcomes = append(report.Outcomes, o)
			errs = append(errs, fmt.Errorf("removing %s from %s: %w", req.Pack, name, err))
			continue
		}
		removed[name] = true
		o.Done = true
		o.Notes = takeNotes(ins)
		report.Outcomes = append(report.Outcomes, o)

		ws := work[req.Pack]
		coders := map[string]Record{}
		for k, v := range ws.Coders {
			if k != name {
				coders[k] = v
			}
		}
		ws.Coders = coders
		work[req.Pack] = ws
		if r, ok := ins.(Renderer); ok {
			if err := r.Render(work); err != nil {
				errs = append(errs, fmt.Errorf("configuring %s after removing %s: %w", name, req.Pack, err))
			}
		}
	}
	if len(removed) == 0 {
		return report, errors.Join(errs...)
	}

	packGone := len(work[req.Pack].Coders) == 0
	if packGone {
		delete(work, req.Pack)
	}
	err = e.State.Update(func(packs map[string]config.AgentPackState) error {
		ps := packs[req.Pack]
		coders := map[string]Record{}
		for k, v := range ps.Coders {
			if !removed[k] {
				coders[k] = v
			}
		}
		if len(coders) == 0 {
			delete(packs, req.Pack)
			return nil
		}
		ps.Coders = coders
		packs[req.Pack] = ps
		return nil
	})
	if err != nil {
		errs = append(errs, fmt.Errorf("saving the state after removing %s: %w", req.Pack, err))
		return report, errors.Join(errs...)
	}

	for _, ins := range chosen {
		if !removed[ins.Name()] || coderStillUsed(work, ins.Name()) {
			continue
		}
		if f, ok := ins.(Finisher); ok {
			if err := f.Finish(); err != nil {
				errs = append(errs, fmt.Errorf("cleaning up %s: %w", ins.Name(), err))
			}
		}
	}
	if packGone {
		if err := e.Trees.Remove(req.Pack); err != nil {
			errs = append(errs, fmt.Errorf("deleting the built tree of %s: %w", req.Pack, err))
		}
	}
	return report, errors.Join(errs...)
}

func takeNotes(ins Installer) []string {
	if n, ok := ins.(Noter); ok {
		return n.TakeNotes()
	}
	return nil
}

func coderStillUsed(packs map[string]config.AgentPackState, coder string) bool {
	for _, p := range packs {
		if _, ok := p.Coders[coder]; ok {
			return true
		}
	}
	return false
}

// PackStatus is what `dg agent-pack list` shows for one pack.
type PackStatus struct {
	Pack string
	// Pinned is the version this binary installs; "bundled" for a pack that
	// ships in the binary.
	Pinned string
	// Installed is the version recorded in state, empty when not installed.
	Installed string
	SHA       string
	Coders    []CoderStatus
}

// CoderStatus is one coder's state for a pack: "installed", "not installed", or
// "skipped" for a coder that is not installed. Detail carries the reason or a
// difference between the record and what the coder holds.
type CoderStatus struct {
	Coder  string
	State  string
	Detail string
}

// CheckRequest asks for a build and its checks, with nothing installed.
type CheckRequest struct {
	Pack string
	// Ref is a tag to try instead of the pin. Empty means the pin.
	Ref string
}

// CheckReport is the result of a check.
type CheckReport struct {
	Pack string
	Tag  string
	SHA  string
	// Pinned is true when the pin was checked, false for a --ref.
	Pinned bool
	// Changed lists fully replaced files whose upstream hash differs from the
	// one recorded at install: a person has to re-read each of them.
	Changed []string
	// Findings are what the coders' inspectors reported, in coder order.
	Findings []string
}

// bundledVersion is the version recorded for a pack that ships in the binary:
// it has no tag or commit, and it moves with the binary.
const bundledVersion = "bundled"

// Packs returns the packs this binary knows, by name.
func (e *Engine) Packs() []Manifest {
	out := append([]Manifest(nil), e.Manifests...)
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Status reports every known pack: its pin, what is installed, and per coder
// whether it has the pack. A coder with a record that is no longer installed
// says so, and one that can verify its record (Pi, whose settings devgeta does
// not render) reports any difference.
func (e *Engine) Status() ([]PackStatus, error) {
	current, err := e.State.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to read agent pack state: %w", err)
	}
	var out []PackStatus
	for _, m := range e.Packs() {
		st := current[m.Name]
		ps := PackStatus{Pack: m.Name, Pinned: m.Tag, Installed: st.Version, SHA: st.SHA}
		if m.Embedded() {
			ps.Pinned = bundledVersion
		}
		for _, ins := range e.Installers {
			cs := CoderStatus{Coder: ins.Name()}
			rec, has := st.Coders[ins.Name()]
			switch {
			case has && !ins.Available():
				cs.State = "installed"
				cs.Detail = fmt.Sprintf("recorded, but %s", notInstalledReason(ins.Name()))
			case has:
				cs.State = "installed"
				if v, ok := ins.(Verifier); ok {
					cs.Detail = v.Verify(m.Name, rec)
				}
			case !ins.Available():
				cs.State, cs.Detail = "skipped", notInstalledReason(ins.Name())
			default:
				cs.State = "not installed"
			}
			ps.Coders = append(ps.Coders, cs)
		}
		out = append(out, ps)
	}
	return out, nil
}

// Update moves an installed pack to the pin in this binary, rebuilding it so
// changed user overrides are picked up too, into the coders that have it. With
// no pack named it updates every installed one, in name order, and stops at the
// first failure.
func (e *Engine) Update(pack string) ([]Report, error) {
	current, err := e.State.Load()
	if err != nil {
		return nil, fmt.Errorf("failed to read agent pack state: %w", err)
	}
	var targets []string
	if pack != "" {
		if _, err := e.manifest(pack); err != nil {
			return nil, err
		}
		if _, ok := current[pack]; !ok {
			return nil, fmt.Errorf("%s is not installed; run: dg agent-pack install %s", pack, pack)
		}
		targets = []string{pack}
	} else {
		for _, m := range e.Packs() {
			if _, ok := current[m.Name]; ok {
				targets = append(targets, m.Name)
			}
		}
	}
	var reports []Report
	for _, name := range targets {
		var coders []string
		for _, ins := range e.Installers {
			if _, ok := current[name].Coders[ins.Name()]; ok {
				coders = append(coders, ins.Name())
			}
		}
		rep, err := e.Install(InstallRequest{Pack: name, Coders: coders})
		reports = append(reports, rep)
		if err != nil {
			return reports, fmt.Errorf("updating %s: %w", name, err)
		}
	}
	return reports, nil
}

// Check builds a pack into a temp tree, as an install would (with its saved
// exclusions and both override layers), runs every build check, lets each
// coder's inspector look at the tree, and lists replaced files whose upstream
// hash changed since they were recorded. It installs nothing and writes no
// state.
func (e *Engine) Check(req CheckRequest) (CheckReport, error) {
	m, err := e.manifest(req.Pack)
	if err != nil {
		return CheckReport{}, err
	}
	if m.Embedded() && req.Ref != "" {
		return CheckReport{}, fmt.Errorf(
			"%s ships with devgeta and has no tags to try; run `dg agent-pack check %s` without --ref", m.Name, m.Name,
		)
	}
	current, err := e.State.Load()
	if err != nil {
		return CheckReport{}, fmt.Errorf("failed to read agent pack state: %w", err)
	}
	saved := current[m.Name]

	report := CheckReport{Pack: m.Name, Tag: m.Tag, Pinned: req.Ref == ""}
	if req.Ref != "" {
		report.Tag = req.Ref
	}
	if m.Embedded() {
		report.Tag = bundledVersion
	}
	var failures []error
	inspect := func(dir string) error {
		tree := Tree{Pack: m, Dir: dir}
		for _, ins := range e.Installers {
			if insp, ok := ins.(Inspector); ok {
				got := insp.Inspect(tree)
				report.Findings = append(report.Findings, got.Lines...)
				if got.Err != nil {
					failures = append(failures, got.Err)
				}
			}
		}
		return errors.Join(failures...)
	}
	res, sha, err := e.Trees.Check(m, req.Ref, saved.Exclude, inspect)
	if err != nil {
		return report, err
	}
	report.SHA = sha
	report.Changed = ChangedReplacements(saved.ReplacedHashes, res.ReplacedHashes)
	return report, nil
}
