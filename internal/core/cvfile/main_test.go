package cvfile

import (
	"os"
	"testing"
)

// Tests that read a PDF start this test binary as the child (sandbox.go).
func TestMain(m *testing.M) {
	if IsChild() {
		os.Exit(RunChild())
	}
	os.Exit(m.Run())
}
