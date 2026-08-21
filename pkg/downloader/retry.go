package downloader

import (
	"context"
	"fmt"
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

// IsRetryableError determines if an error should trigger a retry
// Retryable: network timeouts, DNS failures, HTTP 429/502/503/504
// Non-retryable: HTTP 404/401/403, invalid URL, file system errors
func IsRetryableError(err error) bool {
	if err == nil {
		return false
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

// downloadFile performs a single file download attempt
func downloadFile(ctx context.Context, url, destPath string) error {
	// Create HTTP request with context
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	// Execute request
	client := &http.Client{
		Timeout: 30 * time.Second,
	}
	resp, err := client.Do(req)
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

	// Stream the body straight into destPath atomically: the bytes land in a
	// temporary file next to destPath and are renamed over it only after the
	// whole transfer succeeds. A failed copy, a cancelled context, or an
	// exhausted retry loop therefore never leaves a truncated file at destPath
	// for a caller that only checks whether the file exists. Closing the
	// destination handle is part of that check, so a late write error (ENOSPC,
	// EIO) surfaced only at Close is returned rather than discarded.
	if err := files.WriteFileAtomicFrom(destPath, resp.Body, files.FilePermission); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}
