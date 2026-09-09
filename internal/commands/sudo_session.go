package commands

import (
	"context"
	"sync"
	"time"

	"github.com/cjairm/devgeta/pkg/logger"
	"github.com/cjairm/devgeta/pkg/utils"
)

// sudoRefreshInterval is how often a live SudoSession re-validates the cached
// credential.
//
// sudo's own default timestamp_timeout is 5 minutes, and a full `dg install`
// runs far longer than that, so without a refresh the credential lapses
// somewhere in the middle and every later root operation prompts again.
// Refreshing well inside the window leaves room for one slow step — a large
// cask, a source build — to sit between two ticks without the credential
// expiring underneath it.
const sudoRefreshInterval = time.Minute

// sudoNonInteractiveTimeout bounds the sudo calls that must never wait for a
// human. They are local, no-network operations that answer immediately; a
// bound only exists so a wedged sudo cannot stall the install or leave the
// keepalive goroutine parked forever.
const sudoNonInteractiveTimeout = 15 * time.Second

// SudoSession holds a single sudo authentication open for the length of a
// long-running install, so the user types their password once instead of once
// per root operation.
//
// devgeta does not run sudo itself on macOS — Homebrew does, for its own
// installer and for every cask — and on Debian/Ubuntu it runs it constantly
// (see debian.go and pkg/apt). Either way the prompts come from sudo's
// credential cache expiring mid-install, so the fix is the same on both
// platforms: authenticate once up front, keep the cache warm while the
// install runs, and put it back the way it was found.
//
// The last part is what makes this safe to compose with Homebrew's installer,
// which invalidates the credential on exit — but only when it did not find one
// already active:
//
//	if [[ -x /usr/bin/sudo ]] && ! /usr/bin/sudo -n -v 2>/dev/null
//	then
//	  trap '/usr/bin/sudo -k' EXIT
//	fi
//
// Priming before Homebrew runs therefore suppresses that trap, and the
// credential survives into the cask installs that follow. This session applies
// the same courtesy in reverse: it only releases a credential it created
// itself, so it never tears down a sudo session the user started.
//
// A zero SudoSession is not usable; construct one with NewSudoSession.
type SudoSession struct {
	exec BaseCommandExecutor

	// refreshInterval overrides sudoRefreshInterval. It exists so tests can
	// exercise the keepalive without sleeping for a minute.
	refreshInterval time.Duration

	// primed records that this session obtained the credential itself, which
	// is what makes releasing it on Stop ours to do rather than a user's
	// existing session to destroy.
	primed bool

	cancel   context.CancelFunc
	done     chan struct{}
	stopOnce sync.Once
}

func NewSudoSession(executor BaseCommandExecutor) *SudoSession {
	return &SudoSession{exec: executor}
}

// Start authenticates once, if needed, and begins keeping the credential
// alive. It returns no error on purpose: a sudo session is an optimization,
// not a prerequisite. A user who cannot sudo at all, or a run with no terminal
// to prompt on, must still get the rest of the install — the operations that
// genuinely need root will fail on their own terms, with their own message,
// rather than being pre-empted here by a failure to smooth over their prompts.
func (s *SudoSession) Start() {
	if _, err := LookPathFn("sudo"); err != nil {
		logger.L().Debugw("no sudo binary on PATH; skipping the sudo session", "error", err)
		return
	}

	if err := s.validate(); err == nil {
		logger.L().Debug("a sudo credential is already cached; no prompt needed")
	} else {
		utils.PrintInfo(
			"Administrator access is needed to install packages — you'll be asked for your password once.",
		)
		if err := s.authenticate(); err != nil {
			logger.L().Debugw("could not obtain a sudo credential", "error", err)
			utils.PrintWarning(
				"Could not get administrator access up front; individual steps may ask for your password.",
			)
			return
		}
		s.primed = true
	}

	s.startRefreshing()
}

// Stop halts the keepalive and releases the credential if this session created
// it. It is safe to call more than once, and on a session that never started —
// callers defer it next to Start.
func (s *SudoSession) Stop() {
	s.stopOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
			// Wait for the keepalive to actually exit: it shells out, and
			// returning while a `sudo -n -v` is still in flight would race the
			// `sudo -k` below and could leave the credential alive after Stop
			// claimed to have released it.
			<-s.done
		}
		if !s.primed {
			return
		}
		if err := s.release(); err != nil {
			logger.L().Debugw("failed to release the sudo credential", "error", err)
		}
	})
}

// startRefreshing runs the keepalive until Stop cancels it.
func (s *SudoSession) startRefreshing() {
	interval := s.refreshInterval
	if interval <= 0 {
		interval = sudoRefreshInterval
	}

	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := s.validate(); err != nil {
					// The credential was live moments ago, so a refusal now
					// means the policy is not caching it at all — a sudoers
					// timestamp_timeout of 0, typically pushed by MDM. Nothing
					// this session does can change that, and retrying every
					// interval would spin for the whole install, so say so
					// once and stop.
					logger.L().
						Debugw("sudo refused to refresh the credential; stopping the keepalive", "error", err)
					utils.PrintWarning(
						"This machine's sudo policy doesn't keep credentials cached; later steps may ask for your password again.",
					)
					return
				}
			}
		}
	}()
}

// validate reports whether a credential is cached, without ever prompting:
// `-n` makes sudo fail rather than ask. It is both the up-front probe and the
// keepalive's refresh, because in sudo those are the same operation — `-v`
// extends the timestamp when one exists.
func (s *SudoSession) validate() error {
	_, _, err := s.exec.ExecCommand(CommandParams{
		Command: "sudo",
		Args:    []string{"-n", "-v"},
		NoStdin: true,
		Timeout: sudoNonInteractiveTimeout,
	})
	return err
}

// authenticate is the one call that may prompt, and every field here is about
// making that prompt work:
//
//   - stdin is inherited (NoStdin stays false), which is what hands the
//     terminal to sudo so it can read the password;
//   - Stream is set, because ExecCommand otherwise captures the child's output
//     into the debug log — macos.go learned this from the Homebrew installer,
//     whose prompt disappeared and left the user watching a silent hang;
//   - there is no Timeout, because the only thing being waited on is a human
//     typing, and an expired deadline would kill the prompt mid-entry.
func (s *SudoSession) authenticate() error {
	_, _, err := s.exec.ExecCommand(CommandParams{
		Command: "sudo",
		Args:    []string{"-v"},
		Stream:  true,
	})
	return err
}

func (s *SudoSession) release() error {
	_, _, err := s.exec.ExecCommand(CommandParams{
		Command: "sudo",
		Args:    []string{"-k"},
		NoStdin: true,
		Timeout: sudoNonInteractiveTimeout,
	})
	return err
}
