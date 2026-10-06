// Package memtrim hands memory back to Windows while the app is idle.
//
// A program that wakes once an hour does not need its pages resident in
// between. Trim empties the working set: the pages are not freed, Windows
// simply moves them to its standby list, where they are reclaimed if anything
// else needs the RAM and faulted back (a few microseconds each) if this
// program touches them again. The number Task Manager shows in its Memory
// column drops accordingly.
package memtrim

import (
	"runtime/debug"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32                = windows.NewLazySystemDLL("kernel32.dll")
	psapi                   = windows.NewLazySystemDLL("psapi.dll")
	procSetWorkingSetSize   = kernel32.NewProc("SetProcessWorkingSetSize")
	procGetProcessMemInfoEx = psapi.NewProc("GetProcessMemoryInfo")
)

// processMemoryCounters mirrors PROCESS_MEMORY_COUNTERS.
type processMemoryCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// WorkingSet returns the bytes of this process currently resident in RAM.
func WorkingSet() uint64 {
	var c processMemoryCounters
	c.cb = uint32(unsafe.Sizeof(c))
	r, _, _ := procGetProcessMemInfoEx.Call(uintptr(windows.CurrentProcess()), uintptr(unsafe.Pointer(&c)), uintptr(c.cb))
	if r == 0 {
		return 0
	}
	return uint64(c.WorkingSetSize)
}

// Trim returns unused Go heap to the operating system, then empties the
// working set. It reports the working set before and after, in bytes.
func Trim() (before, after uint64) {
	before = WorkingSet()
	debug.FreeOSMemory()
	// (SIZE_T)-1 for both limits is the documented way to empty the set.
	procSetWorkingSetSize.Call(uintptr(windows.CurrentProcess()), ^uintptr(0), ^uintptr(0))
	return before, WorkingSet()
}
