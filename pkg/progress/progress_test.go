package progress

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

// fixedClock returns a now func that advances by step on every call, so rate
// and ETA arithmetic is exercised without sleeping.
func fixedClock(start time.Time, step time.Duration) func() time.Time {
	var mu sync.Mutex
	current := start
	return func() time.Time {
		mu.Lock()
		defer mu.Unlock()
		now := current
		current = current.Add(step)
		return now
	}
}

func TestFormatBytes(t *testing.T) {
	cases := []struct {
		in   int64
		want string
	}{
		{0, "0 B"},
		{512, "512 B"},
		{1023, "1023 B"},
		{1024, "1.0 KiB"},
		{1536, "1.5 KiB"},
		{1024 * 1024, "1.0 MiB"},
		{5 * 1024 * 1024 * 1024, "5.0 GiB"},
		{3 * 1024 * 1024 * 1024 * 1024, "3.0 TiB"},
		{2 * 1024 * 1024 * 1024 * 1024 * 1024, "2.0 PiB"},
	}
	for _, c := range cases {
		if got := FormatBytes(c.in); got != c.want {
			t.Errorf("FormatBytes(%d) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFormatDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{-5 * time.Second, "0s"},
		{0, "0s"},
		{9 * time.Second, "9s"},
		{90 * time.Second, "1m30s"},
		{59 * time.Minute, "59m00s"},
		{time.Hour + 4*time.Minute, "1h04m"},
		{25 * time.Hour, "25h00m"},
	}
	for _, c := range cases {
		if got := FormatDuration(c.in); got != c.want {
			t.Errorf("FormatDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBarFillsProportionally(t *testing.T) {
	if got := utf8.RuneCountInString(bar(50)); got != barCells+2 {
		t.Errorf("bar is %d runes, want %d including brackets", got, barCells+2)
	}
	if strings.Count(bar(0), "█") != 0 {
		t.Error("bar(0) should have no filled cells")
	}
	if strings.Count(bar(100), "░") != 0 {
		t.Error("bar(100) should have no empty cells")
	}
	// Out-of-range input must clamp rather than panic or overrun.
	if strings.Count(bar(250), "█") != barCells {
		t.Error("bar(250) should clamp to full")
	}
	if strings.Count(bar(-10), "█") != 0 {
		t.Error("bar(-10) should clamp to empty")
	}
}

func TestProgressLineReportsDoneAndRemaining(t *testing.T) {
	r := New(&bytes.Buffer{}, "Writing", 1000*1024*1024)
	r.Add(500 * 1024 * 1024)
	r.rate = 100 * 1024 * 1024 // 100 MiB/s → 5s of work left

	line := r.progressLine(r.done.Load())

	for _, want := range []string{"Writing", "50%", "500.0 MiB / 1000.0 MiB", "100.0 MiB/s", "5s left"} {
		if !strings.Contains(line, want) {
			t.Errorf("progress line %q is missing %q", line, want)
		}
	}
}

func TestProgressLineClampsPastTotal(t *testing.T) {
	// A source that grew between scan and write can exceed the total; the
	// meter must not report 143% or a negative time remaining.
	r := New(&bytes.Buffer{}, "Writing", 700)
	r.Add(1000)
	r.rate = 100

	line := r.progressLine(r.done.Load())

	if !strings.Contains(line, "100%") {
		t.Errorf("line %q should clamp to 100%%", line)
	}
	if strings.Contains(line, "left") {
		t.Errorf("line %q should not estimate time remaining past the total", line)
	}
}

func TestProgressLineDropsBarThenTrimsWhenNarrow(t *testing.T) {
	r := New(&bytes.Buffer{}, "Writing", 1000*1024*1024)
	r.Add(500 * 1024 * 1024)
	r.rate = 100 * 1024 * 1024

	r.width = func() int { return 200 }
	if !strings.Contains(r.progressLine(r.done.Load()), "█") {
		t.Error("a wide terminal should keep the bar")
	}

	r.width = func() int { return 60 }
	narrow := r.progressLine(r.done.Load())
	if strings.Contains(narrow, "█") || strings.Contains(narrow, "░") {
		t.Errorf("a 60-column terminal should drop the bar, got %q", narrow)
	}
	if utf8.RuneCountInString(narrow) > 60 {
		t.Errorf("line %q exceeds 60 columns", narrow)
	}

	r.width = func() int { return 12 }
	tiny := r.progressLine(r.done.Load())
	if utf8.RuneCountInString(tiny) != 12 {
		t.Errorf("line %q should be trimmed to exactly 12 columns", tiny)
	}
}

func TestUnknownTotalOmitsPercentAndEstimate(t *testing.T) {
	r := New(&bytes.Buffer{}, "Reading", 0)
	r.Add(2048)
	r.rate = 1024

	line := r.progressLine(r.done.Load())

	if strings.Contains(line, "%") {
		t.Errorf("line %q should have no percentage when the total is unknown", line)
	}
	if strings.Contains(line, "left") {
		t.Errorf("line %q should have no estimate when the total is unknown", line)
	}
	if !strings.Contains(line, "2.0 KiB") {
		t.Errorf("line %q should still report bytes seen", line)
	}
}

func TestNonTerminalWritesPlainLinesWithoutControlCodes(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, "Writing", 1024)
	r.now = fixedClock(time.Unix(0, 0), time.Second)
	r.interval = time.Millisecond

	r.Start()
	r.Add(1024)
	// Give the render goroutine room for at least one tick.
	time.Sleep(50 * time.Millisecond)
	r.Stop()

	out := buf.String()
	if strings.Contains(out, "\r") || strings.Contains(out, "\x1b") {
		t.Errorf("a non-terminal writer must get no carriage returns or escapes, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("plain output should end in a newline")
	}
	if !strings.Contains(out, "1.0 KiB") {
		t.Errorf("final line should report the total moved, got %q", out)
	}
}

func TestTerminalRedrawsInPlaceAndClearsTheLine(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, "Writing", 1024)
	// New only turns on TTY mode for a real terminal, so drive the flag
	// directly to exercise the drawing path against a buffer.
	r.tty = true
	r.now = fixedClock(time.Unix(0, 0), time.Second)

	r.Add(512)
	r.renderTick(r.done.Load())
	r.Add(512)
	r.Stop()

	out := buf.String()
	if !strings.Contains(out, "\r\x1b[2K") {
		t.Errorf("terminal output should clear the line before redrawing, got %q", out)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Error("the final terminal line should be terminated so later output starts fresh")
	}
}

func TestStopWithoutStartStillReportsAFinalLine(t *testing.T) {
	var buf bytes.Buffer
	r := New(&buf, "Writing", 1024)
	r.now = fixedClock(time.Unix(0, 0), time.Second)
	r.Add(1024)

	r.Stop()
	r.Stop() // must be idempotent, not a double close

	if !strings.Contains(buf.String(), "1.0 KiB") {
		t.Errorf("Stop without Start should still summarize, got %q", buf.String())
	}
}

func TestAddIsCumulativeAndConcurrencySafe(t *testing.T) {
	r := New(&bytes.Buffer{}, "Writing", 0)

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				r.Add(7)
			}
		}()
	}
	wg.Wait()

	if got := r.done.Load(); got != 50*100*7 {
		t.Errorf("done = %d, want %d", got, 50*100*7)
	}
}

func TestUpdateRateTracksTheRecentRateNotTheAverage(t *testing.T) {
	r := New(&bytes.Buffer{}, "Writing", 0)
	start := time.Unix(0, 0)
	r.started, r.lastAt = start, start

	// One fast second, then several slow ones: the smoothed rate must fall
	// well below the average so the estimate follows the drive in use.
	r.updateRate(1000, start.Add(time.Second))
	fast := r.rate
	for i := 2; i <= 6; i++ {
		r.updateRate(int64(1000+100*(i-1)), start.Add(time.Duration(i)*time.Second))
	}

	if r.rate >= fast {
		t.Errorf("rate %v should have fallen from the initial %v", r.rate, fast)
	}
	if r.rate <= 100 {
		t.Errorf("rate %v should still be above the slow sample, not snap straight to it", r.rate)
	}
}
