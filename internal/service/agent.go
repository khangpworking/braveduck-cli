package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"fmt"
)

// On a shared server a user's AI agent reaches their MCP server with a key
// made on the board — never their password. The key is "<user>:<random>",
// so the server knows whose board to check it against; only its SHA-256
// is kept, and making a new key replaces the old one.

const agentKeySetting = "agent_key_sha256"

// ErrNotHosted is returned by features that only exist on a shared server.
var ErrNotHosted = fmt.Errorf("%w: %w", ErrInvalid, errors.New("only on a shared server — on your own computer, connect your agent with `braveduck mcp`"))

// NewAgentKey makes a key for the user's AI agent, replacing any earlier
// one, and returns it. It can't be shown again.
func (s *Service) NewAgentKey(ctx context.Context) (string, error) {
	if s.hosted == nil {
		return "", ErrNotHosted
	}
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	key := s.hosted.User + ":" + hex.EncodeToString(b)
	sum := sha256.Sum256([]byte(key))
	if err := s.st.SetSetting(ctx, agentKeySetting, hex.EncodeToString(sum[:])); err != nil {
		return "", err
	}
	s.logEvent(ctx, "info", "Made a new key for your AI agent (the old one no longer works)")
	return key, nil
}

// RevokeAgentKey stops the current agent key working.
func (s *Service) RevokeAgentKey(ctx context.Context) error {
	if s.hosted == nil {
		return ErrNotHosted
	}
	if err := s.st.SetSetting(ctx, agentKeySetting, ""); err != nil {
		return err
	}
	s.logEvent(ctx, "info", "Removed your AI agent's key")
	return nil
}

// HasAgentKey reports whether an agent key exists.
func (s *Service) HasAgentKey(ctx context.Context) (bool, error) {
	v, _, err := s.st.Setting(ctx, agentKeySetting)
	return v != "", err
}

// CheckAgentKey reports whether key is this board's agent key.
func (s *Service) CheckAgentKey(ctx context.Context, key string) bool {
	want, _, err := s.st.Setting(ctx, agentKeySetting)
	if err != nil || want == "" || key == "" {
		return false
	}
	sum := sha256.Sum256([]byte(key))
	return subtle.ConstantTimeCompare([]byte(hex.EncodeToString(sum[:])), []byte(want)) == 1
}
