package api

import (
	"os"
	"testing"

	"github.com/khangpworking/braveduck-cli/internal/core/cvfile"
)

// No test may call the real Jev API with the user's key: run with an empty
// HOME (so ~/.config/jev-route/typesafe.env isn't found) and no key in the env.
func TestMain(m *testing.M) {
	if cvfile.IsChild() { // a PDF read started by a test (cvfile/sandbox.go)
		os.Exit(cvfile.RunChild())
	}
	home, err := os.MkdirTemp("", "braveduck-test-home-")
	if err != nil {
		panic(err)
	}
	os.Setenv("HOME", home)
	os.Setenv("TYPESAFE_API_KEY", "")
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}
