//go:build unix

package cvfile

import (
	"runtime/debug"
	"syscall"
)

// limitMemory caps this process's address space, so a runaway allocation
// fails here instead of exhausting the machine.
func limitMemory(n uint64) {
	debug.SetMemoryLimit(int64(n) / 2)
	if raceEnabled {
		return // test builds only; see race_on.go
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_AS, &syscall.Rlimit{Cur: n, Max: n})
}
