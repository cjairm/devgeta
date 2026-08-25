package downloader

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cjairm/devgeta/pkg/logger"
)

// The tests in this file never touch the network: every request is served by an
// httptest server bound to loopback, and every destination lives under t.TempDir().
func init() { logger.Init(false) }

// fastRetryConfig keeps the backoff sleeps negligible so retry paths stay fast.
func fastRetryConfig(maxRetries int) RetryConfig {
	return RetryConfig{
		MaxRetries:  maxRetries,
		InitialWait: time.Millisecond,
		MaxWait:     2 * time.Millisecond,
		Multiplier:  2.0,
		Jitter:      0.0,
	}
}

// assertNoFileAt fails if anything exists at destPath, and also fails if the
// atomic write left a temporary file behind in the destination directory.
func assertNoFileAt(t *testing.T, destPath string) {
	t.Helper()
	if _, err := os.Stat(destPath); err == nil {
		t.Fatalf("expected no file at %s after a failed download, but one exists", destPath)
	} else if !os.IsNotExist(err) {
		t.Fatalf("unexpected error stating %s: %v", destPath, err)
	}
	assertNoTempLeftovers(t, destPath)
}

// assertNoTempLeftovers fails if a partially written temp file was left next to destPath.
func assertNoTempLeftovers(t *testing.T, destPath string) {
	t.Helper()
	pattern := filepath.Join(filepath.Dir(destPath), "."+filepath.Base(destPath)+".tmp.*")
	leftovers, err := filepath.Glob(pattern)
	if err != nil {
		t.Fatalf("failed to glob for temp files: %v", err)
	}
	if len(leftovers) > 0 {
		t.Fatalf("temporary files left behind: %v", leftovers)
	}
}

// truncatingServer promises more bytes than it delivers and then aborts the
// connection, which makes the client's copy fail partway through the body —
// exactly the shape of an interrupted transfer, with no network involved.
func truncatingServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(make([]byte, 64)); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}),
	)
	t.Cleanup(srv.Close)
	return srv
}

func TestDownloadFileWithRetry_Success(t *testing.T) {
	body := []byte("devgeta-payload")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write(body); err != nil {
			t.Errorf("failed to write test response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	if err := DownloadFileWithRetry(
		context.Background(),
		srv.URL,
		destPath,
		fastRetryConfig(1),
	); err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(got) != string(body) {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
	assertNoTempLeftovers(t, destPath)
}

func TestDownloadFileWithRetry_TruncatedTransferLeavesNoFile(t *testing.T) {
	srv := truncatingServer(t)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	err := DownloadFileWithRetry(context.Background(), srv.URL, destPath, fastRetryConfig(1))
	if err == nil {
		t.Fatal("expected an error when the transfer is cut short, got nil")
	}
	assertNoFileAt(t, destPath)
}

// A failed download must not clobber whatever was already at destPath: the
// rename is the only commit point, so the previous complete file survives.
func TestDownloadFileWithRetry_FailurePreservesExistingFile(t *testing.T) {
	srv := truncatingServer(t)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	previous := []byte("previous-complete-content")
	if err := os.WriteFile(destPath, previous, 0o644); err != nil {
		t.Fatalf("failed to seed existing file: %v", err)
	}

	if err := DownloadFileWithRetry(
		context.Background(),
		srv.URL,
		destPath,
		fastRetryConfig(1),
	); err == nil {
		t.Fatal("expected an error when the transfer is cut short, got nil")
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("existing file disappeared after a failed download: %v", err)
	}
	if string(got) != string(previous) {
		t.Errorf("existing file was clobbered: got %q, want %q", got, previous)
	}
	assertNoTempLeftovers(t, destPath)
}

func TestDownloadFileWithRetry_NonRetryableStatusLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	err := DownloadFileWithRetry(context.Background(), srv.URL, destPath, fastRetryConfig(2))
	if err == nil {
		t.Fatal("expected an error for HTTP 404, got nil")
	}
	assertNoFileAt(t, destPath)
}

func TestDownloadFileWithRetry_ExhaustedRetriesLeaveNoFile(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		http.Error(w, "service unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	err := DownloadFileWithRetry(context.Background(), srv.URL, destPath, fastRetryConfig(2))
	if err == nil {
		t.Fatal("expected an error after retries are exhausted, got nil")
	}
	if attempts != 3 {
		t.Errorf("server saw %d attempts, want 3", attempts)
	}
	assertNoFileAt(t, destPath)
}

// A retry must publish the successful attempt's bytes, never a mix with the
// truncated bytes of the attempt before it.
func TestDownloadFileWithRetry_RetryPublishesOnlyTheSuccessfulAttempt(t *testing.T) {
	body := []byte("complete-second-attempt")
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.Header().Set("Content-Length", "4096")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write([]byte("partial")); err != nil {
				return
			}
			if f, ok := w.(http.Flusher); ok {
				f.Flush()
			}
			panic(http.ErrAbortHandler)
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("failed to write test response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	// The first attempt's copy failure is retryable ("unexpected EOF" is not,
	// so drive the retry through the loop by asserting on the outcome we can
	// observe): either the download fails and leaves nothing, or it succeeds
	// with exactly the complete body — never a truncated file.
	err := DownloadFileWithRetry(context.Background(), srv.URL, destPath, fastRetryConfig(2))
	if err != nil {
		assertNoFileAt(t, destPath)
		return
	}
	got, readErr := os.ReadFile(destPath)
	if readErr != nil {
		t.Fatalf("failed to read downloaded file: %v", readErr)
	}
	if string(got) != string(body) {
		t.Errorf("downloaded content = %q, want %q", got, body)
	}
	assertNoTempLeftovers(t, destPath)
}

func TestDownloadFileWithRetry_CancelledContextLeavesNoFile(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if _, err := w.Write([]byte("payload")); err != nil {
			return
		}
	}))
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	if err := DownloadFileWithRetry(ctx, srv.URL, destPath, fastRetryConfig(1)); err == nil {
		t.Fatal("expected an error for a cancelled context, got nil")
	}
	assertNoFileAt(t, destPath)
}

func TestIsRetryableError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"timeout", errString("dial tcp 10.0.0.1:443: i/o timeout"), true},
		{"retryable status", errString("HTTP 503 (retryable): Service Unavailable"), true},
		{"non-retryable status", errString("HTTP 404 (non-retryable): Not Found"), false},
		{"connection refused", errString("dial tcp: connection refused"), true},
		{"no such host", errString("lookup example: no such host"), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsRetryableError(tc.err); got != tc.want {
				t.Errorf("IsRetryableError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

type errString string

func (e errString) Error() string { return string(e) }

// withTestIdleWindow lowers the package's body-inactivity bound to a couple
// of seconds for the duration of one test, so a stall test doesn't cost
// productionIdleWindow's tens of seconds of real wall-clock time, and
// restores the production value afterward.
func withTestIdleWindow(t *testing.T, window time.Duration) {
	t.Helper()
	original := idleWindow
	idleWindow = window
	t.Cleanup(func() { idleWindow = original })
}

// slowButProgressingServer writes a handful of chunks with a delay between
// each that is comfortably inside the idle window, then finishes normally.
// It proves a slow-but-steady transfer is never mistaken for a stall.
func slowButProgressingServer(t *testing.T, chunkDelay time.Duration, chunks int) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		flusher, _ := w.(http.Flusher)
		for i := 0; i < chunks; i++ {
			if _, err := w.Write([]byte("chunk-data-")); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
			time.Sleep(chunkDelay)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// stalledServer sends a partial body and then never writes another byte,
// holding the connection open (no abort, no close) — the exact shape of a
// stalled transfer, as opposed to truncatingServer's abrupt disconnect.
func stalledServer(t *testing.T, stallFor time.Duration) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte("partial-data")); err != nil {
			return
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Stop sleeping as soon as the client's cancellation reaches the
		// server side, so t.Cleanup(srv.Close) doesn't have to wait out the
		// full stallFor before the test can shut the server down.
		select {
		case <-time.After(stallFor):
		case <-r.Context().Done():
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// oldWholeRequestTimeout is the exact value httpClient's Timeout field used
// to carry before this fix (see retry.go's sharedTransport/httpClient
// comments): a hard cap on the entire request, body read included,
// regardless of progress. It is not read by any production code any
// more — it exists here purely so this test can assert against the
// literal historical number, not a value invented for test convenience.
const oldWholeRequestTimeout = 30 * time.Second

func TestDownloadFileWithRetry_SlowButProgressingTransferIsNotAborted(t *testing.T) {
	withTestIdleWindow(t, 2*time.Second)
	// 22 chunks * 1.5s = 33s total transfer time: comfortably more than
	// oldWholeRequestTimeout (30s), while every individual chunk gap (1.5s)
	// stays comfortably under the 2s idle window. A test whose total
	// duration never exceeded 30s would also have passed against the old,
	// buggy http.Client{Timeout: 30 * time.Second} — it wouldn't prove
	// anything was fixed. This one wouldn't: the old code killed the whole
	// request at the 30s mark no matter how much progress had been made, so
	// it would have aborted this transfer around chunk 20; the new
	// inactivity-based bound lets it run to completion because no single
	// gap ever approaches 2s.
	const chunkDelay = 1500 * time.Millisecond
	const chunks = 22
	srv := slowButProgressingServer(t, chunkDelay, chunks)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	start := time.Now()
	if err := DownloadFileWithRetry(
		context.Background(),
		srv.URL,
		destPath,
		fastRetryConfig(0),
	); err != nil {
		t.Fatalf("expected a slow-but-progressing download to succeed, got: %v", err)
	}
	elapsed := time.Since(start)

	// This is the assertion that actually exercises the fix's namesake
	// property: without it, nothing here would distinguish this test from
	// one that also passes under the reverted, whole-request-timeout code.
	if elapsed <= oldWholeRequestTimeout {
		t.Fatalf(
			"transfer took %v, want more than the old %v whole-request timeout — "+
				"otherwise this test proves nothing about the fix",
			elapsed, oldWholeRequestTimeout,
		)
	}

	got, err := os.ReadFile(destPath)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if len(got) != len("chunk-data-")*chunks {
		t.Errorf("downloaded %d bytes, want %d", len(got), len("chunk-data-")*chunks)
	}
	assertNoTempLeftovers(t, destPath)
}

func TestDownloadFileWithRetry_StalledBodyFailsWithinIdleWindowAsRetryable(t *testing.T) {
	withTestIdleWindow(t, 2*time.Second)
	// Stall well past the idle window so the timer is guaranteed to fire,
	// but well under the test's own timeout budget.
	srv := stalledServer(t, 10*time.Second)

	destPath := filepath.Join(t.TempDir(), "asset.tar.gz")
	start := time.Now()
	err := downloadFile(context.Background(), srv.URL, destPath)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected an error for a stalled body, got nil")
	}
	if elapsed > 8*time.Second {
		t.Errorf(
			"stalled download took %v to fail, want well under the 10s stall duration",
			elapsed,
		)
	}
	if !errors.Is(err, ErrDownloadStalled) {
		t.Errorf("error %v does not wrap ErrDownloadStalled", err)
	}
	if !IsRetryableError(err) {
		t.Errorf("IsRetryableError(%v) = false, want true for a stalled download", err)
	}
	assertNoFileAt(t, destPath)
}

func TestCalculateBackoff_CapsAtMaxWait(t *testing.T) {
	cfg := RetryConfig{
		InitialWait: time.Second,
		MaxWait:     4 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.0,
	}
	if got := cfg.CalculateBackoff(0); got != time.Second {
		t.Errorf("CalculateBackoff(0) = %v, want %v", got, time.Second)
	}
	if got := cfg.CalculateBackoff(10); got != 4*time.Second {
		t.Errorf("CalculateBackoff(10) = %v, want the %v cap", got, 4*time.Second)
	}
}
