package main

import (
	"context"
	"strings"
	"testing"
)

// The single-user board only listens on this machine.
func TestServeRefusesPublicAddress(t *testing.T) {
	for _, addr := range []string{"0.0.0.0:8080", ":8080", "192.168.1.5:8080", "example.com:80"} {
		err := cmdServe(context.Background(), nil, nil, []string{"--addr", addr, "--no-open"})
		if err == nil || !strings.Contains(err.Error(), "--users") {
			t.Errorf("--addr %s: %v, want a refusal pointing at --users", addr, err)
		}
	}
	for _, h := range []string{"127.0.0.1", "localhost", "::1", "127.0.0.2"} {
		if !isLoopbackListen(h) {
			t.Errorf("%s isn't this machine?", h)
		}
	}
}
