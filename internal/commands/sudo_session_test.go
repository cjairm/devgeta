package commands

import (
	"errors"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/logger"
)

func init() { logger.Init(false) }

// sudoRecorder records the sudo invocations a SudoSession makes.
//
// The keepalive runs on its own goroutine, so a test cannot read
// MockBaseCommand.ExecCommandCalls directly without racing it. ExecCommandFn is
// called outside the mock's own lock, which makes it the one safe seam for
// observing a session that is still running.
type sudoRecorder struct {
	mu    sync.Mutex
	calls []CommandParams
}

func (r *sudoRecorder) record(cmd CommandParams) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, cmd)
}

func (r *sudoRecorder) snapshot() []CommandParams {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CommandParams, len(r.calls))
	copy(out, r.calls)
	return out
}

func (r *sudoRecorder) countOf(args string) int {
	n := 0
	for _, c := range r.snapshot() {
		if sudoArgs(c) == args {
			n++
		}
	}
	return n
}

func sudoArgs(cmd CommandParams) string {
	return strings.Join(cmd.Args, " ")
}

// stubSudoOnPath makes the package's LookPathFn seam report a sudo binary, so
// a test exercises the session's own logic rather than whatever the machine
// running the test happens to have installed.
func stubSudoOnPath(t *testing.T) {
	t.Helper()
	original := LookPathFn
	t.Cleanup(func() { LookPathFn = original })
	LookPathFn = func(file string) (string, error) {
		if file != "sudo" {
			return original(file)
		}
		return "/usr/bin/sudo", nil
	}
}

// newTestSudoSession wires a SudoSession to a mock executor that records every
// invocation and answers it with answer. No real command ever runs.
func newTestSudoSession(
	t *testing.T,
	answer func(cmd CommandParams) error,
) (*SudoSession, *sudoRecorder) {
	t.Helper()
	stubSudoOnPath(t)

	rec := &sudoRecorder{}
	mock := NewMockBaseCommand()
	mock.ExecCommandFn = func(cmd CommandParams) (string, string, error) {
		rec.record(cmd)
		return "", "", answer(cmd)
	}
	return NewSudoSession(mock), rec
}

// sudoAnswers answers each invocation by its joined arguments. A missing key
// means the command succeeded.
func sudoAnswers(answers map[string]error) func(CommandParams) error {
	return func(cmd CommandParams) error { return answers[sudoArgs(cmd)] }
}

func waitFor(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestSudoSessionSkipsThePromptWhenACredentialIsAlreadyCached(t *testing.T) {
	// Everything succeeds, so the `sudo -n -v` probe finds a live credential.
	s, rec := newTestSudoSession(t, sudoAnswers(nil))
	s.Start()
	s.Stop()

	if got := rec.countOf("-n -v"); got == 0 {
		t.Error("expected the session to probe for a cached credential with `sudo -n -v`")
	}
	if got := rec.countOf("-v"); got != 0 {
		t.Errorf("expected no interactive `sudo -v` when one was already cached, got %d", got)
	}
}

func TestSudoSessionLeavesAPreexistingCredentialAlone(t *testing.T) {
	s, rec := newTestSudoSession(t, sudoAnswers(nil))
	s.Start()
	s.Stop()

	if got := rec.countOf("-k"); got != 0 {
		t.Errorf(
			"expected no `sudo -k` for a credential the session did not create, got %d",
			got,
		)
	}
}

func TestSudoSessionPrimesTheCredentialWhenNoneIsCached(t *testing.T) {
	s, rec := newTestSudoSession(
		t,
		sudoAnswers(map[string]error{"-n -v": errors.New("no cached credential")}),
	)
	s.Start()
	defer s.Stop()

	var prime *CommandParams
	calls := rec.snapshot()
	for i := range calls {
		if sudoArgs(calls[i]) == "-v" {
			prime = &calls[i]
		}
	}
	if prime == nil {
		t.Fatalf("expected an interactive `sudo -v` when nothing was cached, got %v", calls)
	}
	if prime.Command != "sudo" {
		t.Errorf("expected the prime to run sudo, got %q", prime.Command)
	}
	if prime.NoStdin {
		t.Error("the prime must inherit stdin — sudo reads the password from the terminal")
	}
}

func TestSudoSessionPromptReachesTheTerminal(t *testing.T) {
	// ExecCommand only tees a command's output to the terminal when Stream is
	// set; otherwise it goes to the debug log. macos.go hit exactly this with
	// the Homebrew installer — the password prompt vanished and the user
	// watched a silent hang.
	s, rec := newTestSudoSession(
		t,
		sudoAnswers(map[string]error{"-n -v": errors.New("no cached credential")}),
	)
	s.Start()
	defer s.Stop()

	for _, c := range rec.snapshot() {
		if sudoArgs(c) == "-v" && !c.Stream {
			t.Fatal("the prime must stream, or its password prompt never reaches the user")
		}
	}
}

func TestSudoSessionReleasesACredentialItPrimed(t *testing.T) {
	s, rec := newTestSudoSession(
		t,
		sudoAnswers(map[string]error{"-n -v": errors.New("no cached credential")}),
	)
	s.Start()
	s.Stop()

	if got := rec.countOf("-k"); got != 1 {
		t.Errorf("expected exactly one `sudo -k` for the credential it primed, got %d", got)
	}
}

func TestSudoSessionGivesUpWhenTheUserCannotSudo(t *testing.T) {
	s, rec := newTestSudoSession(t, sudoAnswers(map[string]error{
		"-n -v": errors.New("no cached credential"),
		"-v":    errors.New("user may not run sudo on this machine"),
	}))
	s.Start()
	s.Stop()

	if got := rec.countOf("-k"); got != 0 {
		t.Errorf("expected no `sudo -k` when the credential was never obtained, got %d", got)
	}
	if calls := rec.snapshot(); len(calls) != 2 {
		t.Errorf("expected the session to stop after the failed prime, got %d calls: %v",
			len(calls), calls)
	}
}

func TestSudoSessionRefreshesTheCredentialUntilStopped(t *testing.T) {
	s, rec := newTestSudoSession(t, sudoAnswers(nil))
	s.refreshInterval = time.Millisecond
	s.Start()

	waitFor(t, func() bool { return rec.countOf("-n -v") >= 3 },
		"the session to refresh the credential")

	for _, c := range rec.snapshot() {
		if sudoArgs(c) == "-n -v" && !c.NoStdin {
			t.Fatal("a refresh must not inherit stdin — it must never block on a human")
		}
	}

	s.Stop()
	settled := rec.countOf("-n -v")
	time.Sleep(20 * time.Millisecond)
	if got := rec.countOf("-n -v"); got != settled {
		t.Errorf("expected refreshes to stop with the session, went from %d to %d", settled, got)
	}
}

func TestSudoSessionStopsRefreshingWhenARefreshFails(t *testing.T) {
	var probes int32
	s, rec := newTestSudoSession(t, func(cmd CommandParams) error {
		if sudoArgs(cmd) != "-n -v" {
			return nil
		}
		// The first `-n -v` is the probe, which finds a cached credential.
		// Every refresh after it fails — what a sudo policy that caches
		// nothing (timestamp_timeout=0) looks like from here.
		if atomic.AddInt32(&probes, 1) == 1 {
			return nil
		}
		return errors.New("a password is required")
	})
	s.refreshInterval = time.Millisecond
	s.Start()
	defer s.Stop()

	waitFor(t, func() bool { return rec.countOf("-n -v") >= 2 }, "the first refresh")
	time.Sleep(20 * time.Millisecond)

	if got := rec.countOf("-n -v"); got != 2 {
		t.Errorf(
			"expected the session to stop refreshing after one failed refresh, got %d",
			got,
		)
	}
}

func TestSudoSessionDoesNothingWithoutASudoBinary(t *testing.T) {
	original := LookPathFn
	t.Cleanup(func() { LookPathFn = original })
	LookPathFn = func(string) (string, error) { return "", exec.ErrNotFound }

	rec := &sudoRecorder{}
	mock := NewMockBaseCommand()
	mock.ExecCommandFn = func(cmd CommandParams) (string, string, error) {
		rec.record(cmd)
		return "", "", nil
	}
	s := NewSudoSession(mock)
	s.Start()
	s.Stop()

	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("expected no commands without a sudo binary, got %d: %v", len(calls), calls)
	}
}

func TestSudoSessionStopIsIdempotent(t *testing.T) {
	s, rec := newTestSudoSession(
		t,
		sudoAnswers(map[string]error{"-n -v": errors.New("no cached credential")}),
	)
	s.Start()
	s.Stop()
	s.Stop()

	if got := rec.countOf("-k"); got != 1 {
		t.Errorf("expected exactly one `sudo -k` across repeated Stops, got %d", got)
	}
}

func TestSudoSessionStopWithoutStartIsSafe(t *testing.T) {
	// cmd/install.go defers Stop, so it runs even on the paths where Start
	// never did.
	s, rec := newTestSudoSession(t, sudoAnswers(nil))
	s.Stop()

	if calls := rec.snapshot(); len(calls) != 0 {
		t.Errorf("expected no commands from a session that never started, got %v", calls)
	}
}
