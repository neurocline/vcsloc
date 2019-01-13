// vcs-torture/os_windows.go
// -- Windows-specific code for vcs-torture

// +build windows

package gsos

import (
	"golang.org/x/sys/windows"
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

// hiresTimestamp is a high-resolution time counter. On Windows 10, this has a resolution of
// 1 to 20 nanoseconds.
type HighresTimestamp uint64

// getHiresTimestamp returns the current time as a HighresTimestamp
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

// ------------------------

var (
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
)

var (
	// QueryPerformanceCounter returns the current value of the performance counter,
	// which could be TSC, or HPET, or the ACPI PMI timer
	// https://msdn.microsoft.com/en-us/library/windows/desktop/ms644904(v=vs.85).aspx
	procQueryPerformanceCounter   = kernel32.NewProc("QueryPerformanceCounter")

	// QueryPerformanceFrequency is the number of QPC clocks per second
	// https://msdn.microsoft.com/en-us/library/windows/desktop/ms644905(v=vs.85).aspx
	procQueryPerformanceFrequency = kernel32.NewProc("QueryPerformanceFrequency")

	// GetConsoleScreenBufferInfo retrieves information about the
	// specified console screen buffer.
	// http://msdn.microsoft.com/en-us/library/windows/desktop/ms683171(v=vs.85).aspx
	procGetConsoleScreenBufferInfo = kernel32.NewProc("GetConsoleScreenBufferInfo")
)

var qpcCounterFreq float64

func init() {
	var freq int64
	ret, _, err := procQueryPerformanceFrequency.Call(uintptr(unsafe.Pointer(&freq)))
	if ret == 0 {
		panic(err.Error())
	}

	qpcCounterFreq = float64(freq) / 1e9
}
