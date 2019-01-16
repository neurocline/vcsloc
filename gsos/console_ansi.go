// vcs-torture/gsos/console_ansi.go
// Unix console information/control (darwin, linux, *bsd)

// +build !windows

package gsos

import (
	"os"

	"golang.org/x/sys/unix"
)

// TerminalWidth returns the width of the terminal (uses 0 as error return value).
func TerminalWidth() int {

	// Try in this order: stderr, stdin, stdout. Hopefully at least one
	// of them hasn't been redirected
	fhs := []*os.File{os.Stdin, os.Stderr, os.Stdout}
	for _, fh := range fhs {
		ws, err := unix.IoctlGetWinsize(int(fh.Fd()), unix.TIOCGWINSZ)
		if err == nil {
			return int(ws.Col)
		}
	}

	// There's no known handle pointing to a terminal, so we can't get
	// any information about it
	return 0
}

// TBD supposedly SIGWINCH is sent when the terminal is resized. We could
// catch that and then do something. Bash and other shells do this.
// For now, I just assume that it's implausible that the terminal size changes.
// TBD put the code for Isatty here, it could come in handy.
