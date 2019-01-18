// vcsloc/gsos/terminal.go

package gsos

import (
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

type Terminal interface {
	// Output that can be throttled and won't advance the line
	Progressf(format string, a ...interface{}) (n int, err error)

	// Non-status output that is line-position savvy
	Printf(format string, a ...interface{}) (n int, err error)

	// Fatal output that is line-position savvy
	// (currently calls log.Fatalf)
	Fatalf(format string, a ...interface{})

	// True if Progressf will result in output
	Ready() bool

	// Will force the next Progressf to result in output
	Force() Terminal

	// The current line length of the terminal
	Len() int
}

// StringFillToExact is a helper function that produces a line of exactly lineLen
// characters, truncating or padding with trailing spaces as needed.
func StringFillToExact(out string, lineLen int) string {
	if len(out) < lineLen {
		out = out + strings.Repeat(" ", lineLen - len(out))
	} else if len(out) > lineLen {
		out = out[:lineLen]
	}
	return out
}

// ----------------------------------------------------------------------------------------------

// ThrottleTerminal is a simple kind of Terminal, one that throttles the output rate
// to a user-specified value.
type ThrottleTerminal struct {
	unterminatedLine bool
	lastStatus time.Time
	period time.Duration

	startTime time.Time
	lineMax int
}

// NewThrottleTerminal creates a new ThrottleTerminal that
// throttles at the rate of msg/period.
func NewThrottleTerminal(period time.Duration) *ThrottleTerminal {
	t := &ThrottleTerminal{
		lastStatus: time.Now(),
		period: period,
		startTime: time.Now(),
	}
	t.Len()
	return t
}

// Progressf shows a progress message which will not advance past the
// current terminal line; output rate is throttled by Ready().
func (t *ThrottleTerminal) Progressf(format string, a ...interface{}) (n int, err error) {
	if !t.Ready() {
		return 0, nil
	}

	// Create a line of exactly the terminal length - this is so that progress messages
	// don't leave garbage at their right-hand edge.
	out := fmt.Sprintf(format, a...)
	out = fmt.Sprintf("T+%.2f: %s", time.Since(t.startTime).Seconds(), out)
	out = StringFillToExact(out, t.lineMax)

	// Reset Ready() timer and do unterminated line output (we rely on the user
	// not terminating it themself).
	t.unterminatedLine = true
	t.lastStatus = time.Now()
	return fmt.Fprintf(os.Stderr, "\r%s", out)
}

// Printf unconditionally prints to the terminal, handling potential unterminated
// lines by previous Progressf messages.
func (t *ThrottleTerminal) Printf(format string, a ...interface{}) (n int, err error) {
	if t.unterminatedLine {
		fmt.Fprintf(os.Stderr, "\n")
		t.unterminatedLine = false
	}
	t.lastStatus = time.Now()
	return fmt.Fprintf(os.Stderr, format, a...)
}

// Printf unconditionally prints to the terminal, handling potential unterminated
// lines by previous Progressf messages, and then exits the program
func (t *ThrottleTerminal) Fatalf(format string, a ...interface{}) {
	if t.unterminatedLine {
		fmt.Fprintf(os.Stderr, "\n")
		t.unterminatedLine = false
	}
	log.Fatalf(format, a...)
}

// Ready returns true if Progressf will result in terminal output; this is controlled
// by a duration set up at ThrottleTerminal creation.
func (t *ThrottleTerminal) Ready() bool {
	return time.Since(t.lastStatus) >= t.period
}

// Force resets the duration so the next Progressf does output. It returns
// the Terminal so that it can be used in a chain fashion, e.g.
// "t.Force().Statusf(...)"
func (t *ThrottleTerminal) Force() Terminal {
	t.lastStatus = time.Now().Add(-t.period)
	return t
}

// Len returns the allowed width of Progressf output.
func (t *ThrottleTerminal) Len() int {
	if t.lineMax == 0 {
		lineLen := TerminalWidth()
		if lineLen < 2 {
			lineLen = 80 // we don't expect this to fail, but let's not create a nightmare
		}
		t.lineMax = lineLen - 1
	}
	return t.lineMax + 1
}
