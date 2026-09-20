// Byte-progress plumbing shared by the write and verify phases (cycle doc
// 2026-09-13-dg-archive.md §5, "Progress by bytes against the scan total").
// Both phases already stream every byte through a reader, so counting costs
// one function call per buffer and no second pass over the data.
//
// This package reports progress as a plain callback rather than importing a
// renderer, so it stays free of terminal concerns and tests can pass a
// closure.
package archive

import "io"

// ProgressFunc receives the number of bytes processed since the last call.
// It is called once per read of a buffer, so implementations must be cheap.
type ProgressFunc func(n int64)

// countingReader reports the size of every chunk it reads to onRead.
type countingReader struct {
	r      io.Reader
	onRead ProgressFunc
}

// countReads wraps r so onRead sees the size of every chunk read through it.
// A nil onRead returns r untouched, so the no-progress path adds nothing.
func countReads(r io.Reader, onRead ProgressFunc) io.Reader {
	if onRead == nil {
		return r
	}
	return &countingReader{r: r, onRead: onRead}
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		c.onRead(int64(n))
	}
	return n, err
}
