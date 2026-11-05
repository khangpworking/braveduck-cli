//go:build !unix

package cvfile

import "runtime/debug"

// limitMemory asks the Go runtime to stay under n; there's no OS cap here,
// but the parent's time limit still ends a runaway read.
func limitMemory(n uint64) { debug.SetMemoryLimit(int64(n) / 2) }
