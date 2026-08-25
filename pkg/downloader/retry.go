package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/cjairm/devgeta/pkg/files"
	"github.com/cjairm/devgeta/pkg/logger"
)

// RetryConfig defines the configuration for retry behavior with exponential backoff
type RetryConfig struct {
	MaxRetries  int           // Maximum number of retry attempts (default: 3)
	InitialWait time.Duration // Initial backoff delay (default: 1s)
	MaxWait     time.Duration // Maximum backoff delay cap (default: 10s)
	Multiplier  float64       // Exponential backoff multiplier (default: 2.0)
	Jitter      float64       // Randomization factor 0.0-1.0 (default: 0.2 for ±20%)
}

// DefaultRetryConfig returns a retry configuration with sensible defaults
func DefaultRetryConfig() RetryConfig {
	return RetryConfig{
		MaxRetries:  3,
		InitialWait: 1 * time.Second,
		MaxWait:     10 * time.Second,
		Multiplier:  2.0,
		Jitter:      0.2,
	}
}

// CalculateBackoff calculates the wait duration for a given retry attempt
// Uses exponential backoff with jitter: initialWait * (multiplier ^ attempt) ± jitter
func (rc *RetryConfig) CalculateBackoff(attempt int) time.Duration {
	// Calculate exponential backoff
	wait := float64(rc.InitialWait) * math.Pow(rc.Multiplier, float64(attempt))

	// Cap at MaxWait
	if time.Duration(wait) > rc.MaxWait {
		wait = float64(rc.MaxWait)
	}

	// Add jitter (±Jitter%)
	jitterAmount := wait * rc.Jitter
	jitter := (rand.Float64() * 2 * jitterAmount) - jitterAmount

	return time.Duration(wait + jitter)
}

// ErrDownloadStalled is the sentinel returned (wrapped) when a download's
// body stops producing bytes for longer than the idle window. It exists so
// IsRetryableError can recognize a stall directly via errors.Is instead of
// matching a message shaped to hit a substring: a context cancelled by our
// own inactivity timer would otherwise surface as the generic
// "context canceled" error, which matches none of IsRetryableError's
// existing substrings and would wrongly classify a stalled transfer as a
// permanent failure on the very first attempt.
var ErrDownloadStalled = errors.New("download stalled: no data received within idle window")

// IsRetryableError determines if an error should trigger a retry
// Retryable: network timeouts, DNS failures, HTTP 429/502/503/504, stalled bodies
// Non-retryable: HTTP 404/401/403, invalid URL, file system errors
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDownloadStalled) {
		return true
	}
	errStr := err.Error()
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary failure") ||
		strings.Contains(errStr, "connection refused") ||
		strings.Contains(errStr, "no such host") ||
		strings.Contains(errStr, "(retryable)") // HTTP 429/502/503/504
}

// DownloadFileWithRetry downloads a file with retry logic and exponential backoff
// Returns error if all retry attempts fail or a non-retryable error is encountered
func DownloadFileWithRetry(ctx context.Context, url, destPath string, config RetryConfig) error {
	var lastErr error

	for attempt := 0; attempt <= config.MaxRetries; attempt++ {
		// Wait before retry (skip on first attempt)
		if attempt > 0 {
			backoff := config.CalculateBackoff(attempt - 1)
			logger.L().Infow(
				"Retrying download",
				"attempt", attempt+1,
				"max_attempts", config.MaxRetries+1,
				"backoff", backoff,
				"url", url,
			)
			time.Sleep(backoff)
		}

		// Attempt download
		err := downloadFile(ctx, url, destPath)
		if err == nil {
			logger.L().Infow(
				"Download successful",
				"url", url,
				"destination", destPath,
				"attempts", attempt+1,
			)
			return nil
		}

		lastErr = err

		// Check if error is retryable
		if !IsRetryableError(err) {
			logger.L().Errorw(
				"Non-retryable error encountered",
				"url", url,
				"error", err,
				"attempt", attempt+1,
			)
			return fmt.Errorf("download failed (non-retryable): %w", err)
		}

		logger.L().Warnw(
			"Download attempt failed",
			"url", url,
			"error", err,
			"attempt", attempt+1,
			"max_attempts", config.MaxRetries+1,
		)
	}

	return fmt.Errorf("download failed after %d attempts: %w", config.MaxRetries+1, lastErr)
}

const (
	// responseHeaderTimeout bounds how long we wait for a server to start
	// responding (DNS + connect + TLS + request write + first response
	// byte). It does not cover the body read — see idleWindow below, which
	// is why this alone would leave a stalled body with no deadline at all.
	responseHeaderTimeout = 30 * time.Second

	// tlsHandshakeTimeout matches net/http's own DefaultTransport default;
	// there is no reason for a font-archive download to need a slower TLS
	// handshake than any other HTTP client in the process.
	tlsHandshakeTimeout = 10 * time.Second

	// productionIdleWindow bounds body inactivity: if a download goes this
	// long without a single byte arriving, the transfer is presumed stalled
	// and cancelled so the retry loop can try again rather than hanging
	// forever. 30s is a deliberately generous multiple of ordinary network
	// jitter (TCP retransmission timeouts and congestion-window pauses are
	// typically sub-second to a few seconds even on a poor connection) while
	// still catching a genuinely dead connection — a crashed server or a
	// silently dropped socket — within a bounded, user-visible time. It is a
	// carryover of the old whole-request 30s timeout's magnitude, just
	// applied to inactivity instead of total transfer duration.
	productionIdleWindow = 30 * time.Second
)

// idleWindow is the body-inactivity bound applied to every download. It
// defaults to productionIdleWindow; tests lower it (see retry_test.go) so a
// stall doesn't cost tens of seconds of wall-clock time per test run.
var idleWindow = productionIdleWindow

// sharedTransport must be built once and shared across every downloadFile
// call. A plain &http.Client{Timeout: ...} with Transport left nil falls
// through to the process-wide http.DefaultTransport, so per-call
// allocation of *that* was harmless — but ResponseHeaderTimeout and
// TLSHandshakeTimeout live on the Transport, not the Client, so a real
// *http.Transport has to be constructed, and constructing a fresh one per
// call would give every retry attempt (and every other download) its own
// idle-connection pool instead of sharing one.
var sharedTransport = &http.Transport{
	ResponseHeaderTimeout: responseHeaderTimeout,
	TLSHandshakeTimeout:   tlsHandshakeTimeout,
}

// httpClient has no whole-request Timeout: that field would bound the body
// read too, which is exactly the defect being fixed (a slow-but-progressing
// multi-megabyte download must not be killed at an arbitrary time limit).
// The stall bound instead comes from stallDetectingReader, applied per
// download in downloadFile.
var httpClient = &http.Client{
	Transport: sharedTransport,
}

// stallDetectingReader wraps an io.Reader and resets an idle timer on every
// Read that returns n > 0. If the timer fires before the next such Read, it
// cancels the associated context with ErrDownloadStalled. This is the
// intentional replacement for a per-Read net.Conn.SetReadDeadline via a
// custom DialContext: that alternative was measured (see task-9 report) to
// bound a stall equally well but poisons the pooled connection on every
// idle gap, breaking connection reuse. Wrapping the body instead leaves the
// connection pool untouched.
type stallDetectingReader struct {
	r     io.Reader
	timer *time.Timer
	idle  time.Duration
}

func (sr *stallDetectingReader) Read(p []byte) (int, error) {
	n, err := sr.r.Read(p)
	if n > 0 {
		sr.timer.Reset(sr.idle)
	}
	return n, err
}

// newStallDetectingReader starts the idle timer immediately (a body that
// never sends a first byte is exactly as stalled as one that stops midway)
// and returns a stop function the caller must invoke once done reading, so
// the timer is released instead of firing after the transfer already
// finished successfully.
func newStallDetectingReader(
	r io.Reader,
	idle time.Duration,
	cancel context.CancelCauseFunc,
) (io.Reader, func()) {
	timer := time.AfterFunc(idle, func() { cancel(ErrDownloadStalled) })
	sr := &stallDetectingReader{r: r, timer: timer, idle: idle}
	return sr, func() { timer.Stop() }
}

// downloadFile performs a single file download attempt
func downloadFile(ctx context.Context, url, destPath string) error {
	// A cause-carrying context lets the idle timer report exactly why the
	// request was cancelled (ErrDownloadStalled) rather than the generic
	// "context canceled" that a plain cancel() produces — the retry loop
	// needs that distinction to classify a stall as retryable.
	stallCtx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)

	// Create HTTP request with the cause-carrying context
	req, err := http.NewRequestWithContext(stallCtx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	resp, err := httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("HTTP request failed: %w", err)
	}
	// The response body is a read-only handle, so its Close error carries no
	// data-loss signal and there is nothing actionable to do with it.
	defer func() { _ = resp.Body.Close() }()

	// Check HTTP status
	if resp.StatusCode != http.StatusOK {
		// Determine if status code is retryable
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusBadGateway ||
			resp.StatusCode == http.StatusServiceUnavailable ||
			resp.StatusCode == http.StatusGatewayTimeout {
			return fmt.Errorf("HTTP %d (retryable): %s", resp.StatusCode, resp.Status)
		}
		return fmt.Errorf("HTTP %d (non-retryable): %s", resp.StatusCode, resp.Status)
	}

	body, stopStallTimer := newStallDetectingReader(resp.Body, idleWindow, cancel)
	defer stopStallTimer()

	// Stream the body straight into destPath atomically: the bytes land in a
	// temporary file next to destPath and are renamed over it only after the
	// whole transfer succeeds. A failed copy, a cancelled context, or an
	// exhausted retry loop therefore never leaves a truncated file at destPath
	// for a caller that only checks whether the file exists. Closing the
	// destination handle is part of that check, so a late write error (ENOSPC,
	// EIO) surfaced only at Close is returned rather than discarded.
	if err := files.WriteFileAtomicFrom(destPath, body, files.FilePermission); err != nil {
		// If our own idle timer caused this failure, report the sentinel
		// (wrapped, so errors.Is still finds it) instead of whatever generic
		// "context canceled" shape the copy surfaced it as.
		if cause := context.Cause(stallCtx); errors.Is(cause, ErrDownloadStalled) {
			return fmt.Errorf("failed to write file: %w", cause)
		}
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
