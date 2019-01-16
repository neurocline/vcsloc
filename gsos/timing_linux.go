// vcs-torture/gsos/os_linux.go
// -- Linux-specific high-resolution timer

// +build linux

package gsos

import (
	"time"
)

//go:noescape
//go:linkname nanotime runtime.nanotime
func nanotime() int64

// ----------------------------------------------------------------------------------------------
// Linux timing code (adapted from https://github.com/ScaleFT/monotime)
// Linux Go has a nanosecond timer, so use it.

// hiresTimestamp is a high-resolution time counter. On Windows 10, this has a resolution of
// 1 to 20 nanoseconds.
type HighresTimestamp uint64

// getHiresTimestamp returns the current time as a HighresTimestamp
func HighresTime() HighresTimestamp {
	return HighresTimestamp(nanotime())
}

// HighresTimestamp.Duration() converts a HighresTimestamp into a time.Duration value
// (this looks horrible, but matches the Mach library code)
func (t HighresTimestamp) Duration() time.Duration {
	return time.Duration(uint64(t) * uint64(tbinfo.numer) / uint64(tbinfo.denom)))
}
