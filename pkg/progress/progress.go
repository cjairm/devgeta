// Package progress renders a single-line byte meter for long streaming work:
// how much is done, how much is left, and how long the rest is likely to take.
//
// The hot path is deliberately trivial. Add does one atomic add per chunk of
// bytes and nothing else; a single background goroutine does all of the
// arithmetic, formatting, and terminal writing on a fixed tick. Measuring
// therefore costs no extra reads of the data being measured and no per-chunk
// formatting, so a meter does not slow down the work it reports on.
//
// Output adapts to where it is going. An attached terminal gets one line
// redrawn in place several times a second; a redirected or piped stream gets a
// plain line every 30 seconds, so logs stay readable instead of filling with
// carriage returns.
package progress

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/sys/unix"
)

const (
	// ttyInterval is how often an attached terminal redraws. Fast enough to
	// look live, slow enough that the render goroutine is idle ~99% of the time.
	ttyInterval = 250 * time.Millisecond
	// plainInterval is how often a non-terminal writer gets a new line. Long,
	// because every one of them is kept forever in somebody's log.
	plainInterval = 30 * time.Second
	// barCells is the width of the drawn bar, excluding its brackets.
	barCells = 24
	// rateAlpha weights the newest throughput sample in the smoothed rate.
	// Low enough to ride out a slow directory, high enough that unplugging a
	// fast drive for a slow one shows up within a couple of seconds.
	rateAlpha = 0.3
)

// Reporter is a byte-progress meter. Create one with New, call Start before
// the work begins, Add as bytes are processed, and Stop when it ends. A
// Reporter is not reusable: create one per phase of work.
type Reporter struct {
	label string
	total int64
	done  atomic.Int64

	out      io.Writer
	tty      bool
	interval time.Duration
	now      func() time.Time
	// width reports the usable columns, or 0 when unknown (not a terminal).
	width func() int

	running  atomic.Bool
	stopOnce sync.Once
	quit     chan struct{}
	finished chan struct{}

	// Fields below are touched only by the render goroutine, and by Stop
	// after it has exited.
	started  time.Time
	lastAt   time.Time
	lastDone int64
	rate     float64
}

// New returns a meter that draws to out. A total of zero or less means the
// size of the work is unknown, and the meter reports only what it has seen so
// far — no percentage, bar, or estimate. Pass os.Stderr for normal use, so the
// meter never contaminates piped stdout.
func New(out io.Writer, label string, total int64) *Reporter {
	r := &Reporter{
		label:    label,
		total:    total,
		out:      out,
		interval: plainInterval,
		now:      time.Now,
		width:    func() int { return 0 },
		quit:     make(chan struct{}),
		finished: make(chan struct{}),
	}
	if f, ok := out.(*os.File); ok && isTerminal(f) {
		r.tty = true
		r.interval = ttyInterval
		r.width = func() int { return terminalWidth(f) }
	}
	return r
}

// Add records that n more bytes have been processed. It is safe to call from
// any goroutine and does one atomic add, so it is cheap enough to call once
// per read of a buffer.
func (r *Reporter) Add(n int64) {
	r.done.Add(n)
}

// Start begins drawing. Every Start must be paired with exactly one Stop.
func (r *Reporter) Start() {
	if !r.running.CompareAndSwap(false, true) {
		return
	}
	now := r.now()
	r.started, r.lastAt = now, now
	go r.loop()
}

// Stop halts drawing and leaves a single final line behind summarizing the
// whole run. It is safe to call without a preceding Start, and safe to call
// more than once.
func (r *Reporter) Stop() {
	r.stopOnce.Do(func() {
		if r.running.Load() {
			close(r.quit)
			<-r.finished
		}
		r.renderFinal(r.done.Load())
	})
}

func (r *Reporter) loop() {
	defer close(r.finished)
	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()
	for {
		select {
		case <-r.quit:
			return
		case <-ticker.C:
			r.renderTick(r.done.Load())
		}
	}
}

func (r *Reporter) renderTick(done int64) {
	now := r.now()
	r.updateRate(done, now)
	r.write(r.progressLine(done), false)
}

func (r *Reporter) renderFinal(done int64) {
	elapsed := r.now().Sub(r.started)
	r.write(r.finalLine(done, elapsed), true)
}

// updateRate folds the newest throughput sample into the smoothed rate. An
// exponential moving average is used rather than the average since start so
// that the estimate tracks the drive actually in front of the user — a slow
// USB enclosure should not keep quoting the speed of the first cached second.
func (r *Reporter) updateRate(done int64, now time.Time) {
	seconds := now.Sub(r.lastAt).Seconds()
	if seconds <= 0 {
		return
	}
	sample := float64(done-r.lastDone) / seconds
	if r.rate == 0 {
		r.rate = sample
	} else {
		r.rate = rateAlpha*sample + (1-rateAlpha)*r.rate
	}
	r.lastAt, r.lastDone = now, done
}

// progressLine renders the in-flight line, dropping the bar and then trimming
// if the terminal is too narrow to hold it.
func (r *Reporter) progressLine(done int64) string {
	line := r.composeLine(done, true)
	width := r.width()
	if width <= 0 || utf8.RuneCountInString(line) <= width {
		return line
	}
	line = r.composeLine(done, false)
	if utf8.RuneCountInString(line) <= width {
		return line
	}
	return string([]rune(line)[:width])
}

func (r *Reporter) composeLine(done int64, withBar bool) string {
	parts := []string{r.label}
	if r.total > 0 {
		percent := float64(done) / float64(r.total) * 100
		if percent > 100 {
			percent = 100
		}
		if withBar {
			parts = append(parts, bar(percent))
		}
		parts = append(
			parts,
			fmt.Sprintf("%3.0f%%", percent),
			fmt.Sprintf("%s / %s", FormatBytes(done), FormatBytes(r.total)),
		)
	} else {
		parts = append(parts, FormatBytes(done))
	}
	if r.rate > 0 {
		parts = append(parts, FormatBytes(int64(r.rate))+"/s")
		if r.total > done {
			remaining := time.Duration(float64(r.total-done) / r.rate * float64(time.Second))
			parts = append(parts, FormatDuration(remaining)+" left")
		}
	}
	return strings.Join(parts, "  ")
}

// finalLine reports the whole run rather than the last quarter-second of it:
// the average rate over the full duration is what the user wants to remember.
func (r *Reporter) finalLine(done int64, elapsed time.Duration) string {
	line := fmt.Sprintf("%s  %s in %s", r.label, FormatBytes(done), FormatDuration(elapsed))
	if seconds := elapsed.Seconds(); seconds > 0 {
		line += fmt.Sprintf("  (%s/s)", FormatBytes(int64(float64(done)/seconds)))
	}
	return line
}

func (r *Reporter) write(line string, final bool) {
	if r.tty {
		// Clear the whole line before redrawing: a shorter line must not
		// leave the tail of a longer one behind it.
		fmt.Fprintf(r.out, "\r\x1b[2K%s", line)
		if final {
			fmt.Fprintln(r.out)
		}
		return
	}
	fmt.Fprintln(r.out, line)
}

func terminalWidth(f *os.File) int {
	ws, err := unix.IoctlGetWinsize(int(f.Fd()), unix.TIOCGWINSZ)
	if err != nil || ws.Col == 0 {
		return 0
	}
	return int(ws.Col)
}

func bar(percent float64) string {
	filled := int(percent / 100 * barCells)
	if filled < 0 {
		filled = 0
	}
	if filled > barCells {
		filled = barCells
	}
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", barCells-filled) + "]"
}

// FormatBytes renders n as a human-readable binary size.
func FormatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	units := [...]string{"KiB", "MiB", "GiB", "TiB", "PiB"}
	div, exp := int64(unit), 0
	for value := n / unit; value >= unit && exp < len(units)-1; value /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %s", float64(n)/float64(div), units[exp])
}

// FormatDuration renders d compactly, largest two units only.
func FormatDuration(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	d = d.Round(time.Second)
	hours := int(d / time.Hour)
	minutes := int(d/time.Minute) % 60
	seconds := int(d/time.Second) % 60
	switch {
	case hours > 0:
		return fmt.Sprintf("%dh%02dm", hours, minutes)
	case minutes > 0:
		return fmt.Sprintf("%dm%02ds", minutes, seconds)
	default:
		return fmt.Sprintf("%ds", seconds)
	}
}

func isTerminal(f *os.File) bool {
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
