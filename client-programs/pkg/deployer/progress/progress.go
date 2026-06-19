// Package progress renders deploy/delete step status as compact
// per-step lines instead of free-form prints.
//
// The default reporter writes 'plain text with [N/M] counters' —
// no spinner library dep. When
// stdout is a TTY, in-progress lines are over-written via \r so the
// final state replaces the polling chatter. When stdout is not a TTY
// (CI, pipe, file), every state change appends a new line so the log
// is grep-able.
package progress

import (
	"fmt"
	"io"
	"os"
	"sync"
)

// Reporter is the surface deploy.go / delete.go talk to. Each call to
// Start opens a Step the caller closes via Done or Fail. Update can be
// called any number of times in between to surface intermediate phase
// changes (the wait poller drives this).
//
// Reporters are safe for use from a single goroutine. The CLI today
// runs deploy strictly sequentially so a mutex-protected concurrency
// story isn't needed.
type Reporter interface {
	Start(label string) Step
	// Note prints a one-off informational line outside the step counter
	// (used for things like 'syncing cached local secrets'). It does
	// not advance the step counter.
	Note(msg string)
}

// Step represents one numbered deploy operation (apply ECC, wait
// SessionManager Ready, etc.). Update surfaces an intermediate state
// while the step is pending; Done / Fail close the step.
type Step interface {
	Update(phase string)
	Done(summary string)
	Fail(err error)
}

// New builds the default reporter. total is the expected step count
// (drives the [N/M] counter); 0 means counters are hidden ("delete"
// has a variable step count depending on what's actually present, so
// it passes 0 and just gets prefix-less lines).
//
// w is the destination; isTTY determines whether \r-based overwrite
// is used. Callers in cmd/ wire isTTY from os.Stdout.
func New(w io.Writer, total int, isTTY bool) Reporter {
	if w == nil {
		w = io.Discard
	}
	return &reporter{w: w, total: total, isTTY: isTTY}
}

// NewForStdout wires the default reporter against os.Stdout and
// auto-detects TTY. Used by cmd/ to avoid threading isatty through
// every option struct.
func NewForStdout(total int) Reporter {
	return New(os.Stdout, total, isTerminal(os.Stdout))
}

// isTerminal returns true when w is a TTY-like file descriptor.
// Stdlib-only check: os.File.Stat() yields ModeCharDevice for terminals.
func isTerminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}

type reporter struct {
	w       io.Writer
	total   int
	isTTY   bool
	mu      sync.Mutex
	current int
}

func (r *reporter) Start(label string) Step {
	r.mu.Lock()
	r.current++
	n := r.current
	r.mu.Unlock()
	s := &step{r: r, n: n, label: label}
	s.render("")
	return s
}

func (r *reporter) Note(msg string) {
	fmt.Fprintln(r.w, "·", msg)
}

type step struct {
	r        *reporter
	n        int
	label    string
	lastLine string // tracks last rendered text for TTY overwrite
}

func (s *step) Update(phase string) { s.render(phase) }

func (s *step) Done(summary string) {
	s.finalize("✓", summary)
}

func (s *step) Fail(err error) {
	s.finalize("✗", err.Error())
}

// render writes the in-progress line, optionally overwriting the
// previous render on a TTY.
func (s *step) render(phase string) {
	line := s.format("→", phase)
	if s.r.isTTY && s.lastLine != "" {
		// Carriage return + spaces to clear previous content.
		fmt.Fprint(s.r.w, "\r"+pad(line, len(s.lastLine)))
	} else {
		fmt.Fprintln(s.r.w, line)
	}
	s.lastLine = line
}

// finalize writes the closing line (✓ or ✗). On a TTY this overwrites
// the in-progress line; on a non-TTY it appends.
func (s *step) finalize(symbol, msg string) {
	final := s.format(symbol, msg)
	if s.r.isTTY {
		fmt.Fprintln(s.r.w, "\r"+pad(final, len(s.lastLine)))
	} else {
		fmt.Fprintln(s.r.w, final)
	}
	s.lastLine = ""
}

// format builds '[3/6] symbol Label: detail'. Empty total hides the
// counter; empty detail hides the colon-detail tail.
func (s *step) format(symbol, detail string) string {
	prefix := ""
	if s.r.total > 0 {
		prefix = fmt.Sprintf("[%d/%d] ", s.n, s.r.total)
	}
	out := fmt.Sprintf("%s%s %s", prefix, symbol, s.label)
	if detail != "" {
		out += ": " + detail
	}
	return out
}

// pad right-pads s with spaces to at least width characters so a TTY
// overwrite hides longer previous content.
func pad(s string, width int) string {
	if len(s) >= width {
		return s
	}
	return s + spaces(width-len(s))
}

func spaces(n int) string {
	if n <= 0 {
		return ""
	}
	b := make([]byte, n)
	for i := range b {
		b[i] = ' '
	}
	return string(b)
}
