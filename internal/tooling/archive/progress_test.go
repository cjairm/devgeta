package archive

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCountReadsWithoutCallbackReturnsTheReaderUntouched(t *testing.T) {
	src := strings.NewReader("payload")
	if got := countReads(src, nil); got != io.Reader(src) {
		t.Error("a nil callback should leave the reader unwrapped, adding no cost")
	}
}

func TestCountReadsSumsEveryByteRead(t *testing.T) {
	payload := bytes.Repeat([]byte("x"), 5000)
	var total int64
	var calls int

	r := countReads(bytes.NewReader(payload), func(n int64) {
		total += n
		calls++
	})
	read, err := io.Copy(io.Discard, r)
	if err != nil {
		t.Fatalf("Copy: %v", err)
	}

	if read != int64(len(payload)) {
		t.Fatalf("copied %d bytes, want %d", read, len(payload))
	}
	if total != int64(len(payload)) {
		t.Errorf("counted %d bytes, want %d", total, len(payload))
	}
	if calls == 0 {
		t.Error("the callback was never invoked")
	}
}

func TestCountReadsReportsNothingForAnEmptyReader(t *testing.T) {
	var calls int
	r := countReads(bytes.NewReader(nil), func(int64) { calls++ })
	if _, err := io.Copy(io.Discard, r); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	if calls != 0 {
		t.Errorf("callback ran %d times on an empty reader, want 0", calls)
	}
}

// failingReader returns some data and then an error, to prove a short read
// still counts the bytes that genuinely arrived.
type failingReader struct {
	data []byte
	done bool
}

func (f *failingReader) Read(p []byte) (int, error) {
	if f.done {
		return 0, io.ErrUnexpectedEOF
	}
	f.done = true
	n := copy(p, f.data)
	return n, nil
}

func TestCountReadsCountsBytesDeliveredBeforeAFailure(t *testing.T) {
	var total int64
	r := countReads(&failingReader{data: []byte("half")}, func(n int64) { total += n })

	if _, err := io.Copy(io.Discard, r); err == nil {
		t.Fatal("expected the underlying read error to surface")
	}
	if total != 4 {
		t.Errorf("counted %d bytes, want the 4 that were actually read", total)
	}
}
