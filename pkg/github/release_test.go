package github

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The tests in this file never touch the network: every request is served by
// an httptest server bound to loopback.

func TestFetchLatestRelease_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name": "v1.2.3"}`))
	}))
	t.Cleanup(srv.Close)

	version, err := fetchLatestReleaseFromURL(srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version != "1.2.3" {
		t.Errorf("version = %q, want %q", version, "1.2.3")
	}
}

// A non-200 response must have its body drained so the underlying
// connection returns to the idle pool: proven here by reusing the same
// client/transport for a follow-up request and asserting the server only
// ever accepted one TCP connection (net/http will only offer an idle
// connection back for reuse if the previous response body was fully read).
func TestFetchLatestRelease_NonOKDrainsBodySoConnectionIsReused(t *testing.T) {
	var conns int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprintf("%d", 5000))
		w.WriteHeader(http.StatusForbidden)
		// A body far larger than a real rate-limit response needs, so an
		// undrained body would leave substantial unread bytes on the wire.
		_, _ = w.Write([]byte(strings.Repeat("x", 5000)))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			conns++
		}
	}
	t.Cleanup(srv.Close)

	client := &http.Client{Transport: &http.Transport{}}
	originalClient := httpClient
	httpClient = client
	t.Cleanup(func() { httpClient = originalClient })

	if _, err := fetchLatestReleaseFromURL(srv.URL); err == nil {
		t.Fatal("expected an error for a non-200 status, got nil")
	}
	if _, err := fetchLatestReleaseFromURL(srv.URL); err == nil {
		t.Fatal("expected an error for a non-200 status, got nil")
	}

	if conns != 1 {
		t.Errorf("server accepted %d TCP connections, want 1 (body wasn't drained/reused)", conns)
	}
}

func TestFetchLatestRelease_ResponseLargerThanCapIsRejected(t *testing.T) {
	// A single JSON field whose value alone exceeds the cap: valid JSON
	// syntax so a failure can only come from truncation by the size cap,
	// not from a body that was already malformed.
	oversized := `{"tag_name": "v1.0.0", "padding": "` +
		strings.Repeat("a", maxReleaseResponseSize+1024) + `"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(oversized))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchLatestReleaseFromURL(srv.URL); err == nil {
		t.Fatal("expected an error for an oversized response body, got nil")
	}
}

func TestFetchLatestRelease_MissingTagName(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(srv.Close)

	if _, err := fetchLatestReleaseFromURL(srv.URL); err == nil {
		t.Fatal("expected an error when tag_name is missing, got nil")
	}
}
