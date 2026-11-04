package service

import (
	"os"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/jev"
)

// No test may call the real Jev API with the user's key: tests that need Jev
// install a fake with SetJev; everything else sees "no key".
func TestMain(m *testing.M) {
	loadJevKey = func() (string, string, error) { return "", "", jev.ErrNoKey }
	os.Exit(m.Run())
}
