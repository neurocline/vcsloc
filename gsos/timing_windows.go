// vcsloc/gsos/timing_windows.go
// -- Windows-specific high-resolution timer

// +build windows

package gsos

import (
	"time"
	"unsafe"
)

// ----------------------------------------------------------------------------------------------
// Windows timing code (adapted from https://github.com/ScaleFT/monotime)
// We need this because the Go library uses timeGetTime for "high-resolution" timing, and
// that's anything but high-resolution (1ms intervals at best). So we use QueryPerformanceCounter,
// which, as of Windows 10, has had all the quirks worked out of it.
// Currently, we make no attempt to implement time.Time-compatible values.
//
// Get rid of this code once the Go library supports nanosecond-level timing for all
// relevant operating systems.

var qpcCounterFreq float64

func init() {
	var freq int64
	ret, _, err := procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&freq)))
	if ret == 0 {
		panic(err.Error())
	}

	qpcCounterFreq = float64(freq) / 1e9
}

// HighresTimestamp is a high-resolution time counter. On Windows 10, this has a resolution of
// 1 to 20 nanoseconds.
type HighresTimestamp uint64

// HighresTime returns the current time as a HighresTimestamp
func HighresTime() HighresTimestamp {
	var hiresTime HighresTimestamp

	ret, _, err := procQueryPerformanceCounter.Call(uintptr(unsafe.Pointer(&hiresTime)))
	if ret == 0 {
		panic(err.Error())
	}

	return hiresTime
}

// HighresTimestamp.Duration() converts a HighresTimestamp into a time.Duration value
func (t HighresTimestamp) Duration() time.Duration {
	return time.Duration(float64(t) / qpcCounterFreq)
}
