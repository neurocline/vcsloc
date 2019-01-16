// vcs-torture/os_windows.go
// -- Windows-specific code for vcs-torture

// +build windows

package gsos

import (
	"golang.org/x/sys/windows"
)

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
