//go:build race

package cvfile

// raceEnabled: the race detector reserves terabytes of address space for its
// shadow memory, so an address-space cap would kill every child.
const raceEnabled = true
