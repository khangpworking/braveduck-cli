//go:build unix

package cvfile

import (
	"runtime/debug"
	"syscall"
)

// limitMemory caps the memory this process can write to (RLIMIT_DATA), so a
// runaway allocation fails here instead of exhausting the machine. Not
// RLIMIT_AS: the Go runtime reserves address space it never uses, and a cap
// on that made ordinary reads fail now and then.
// (Only a race build, which can't take the cap, falls back on the Go
// runtime's soft limit; with the cap that limit just slows a bomb down.)
func limitMemory(n uint64) {
	if raceEnabled {
		debug.SetMemoryLimit(int64(n) / 2) // test builds only; see race_on.go
		return
	}
	_ = syscall.Setrlimit(syscall.RLIMIT_DATA, &syscall.Rlimit{Cur: n, Max: n})
}
