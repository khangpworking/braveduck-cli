package service

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestAgentKeys(t *testing.T) {
	s := newSvc(t)
	ctx := context.Background()
	if _, err := s.NewAgentKey(ctx); !errors.Is(err, ErrNotHosted) {
		t.Fatalf("on your own computer: %v, want ErrNotHosted", err)
	}
	s.SetHosted(HostedInfo{User: "alice"})
	if ok, _ := s.HasAgentKey(ctx); ok {
		t.Fatal("a key before any was made")
	}
	k1, err := s.NewAgentKey(ctx)
	if err != nil || !strings.HasPrefix(k1, "alice:") || len(k1) < 6+64 {
		t.Fatalf("key = %q, %v", k1, err)
	}
	if !s.CheckAgentKey(ctx, k1) || s.CheckAgentKey(ctx, k1+"x") || s.CheckAgentKey(ctx, "") {
		t.Fatal("key check wrong")
	}
	if v, _, _ := s.st.Setting(ctx, agentKeySetting); strings.Contains(v, k1[6:]) {
		t.Fatal("the key itself was stored")
	}
	k2, _ := s.NewAgentKey(ctx)
	if s.CheckAgentKey(ctx, k1) || !s.CheckAgentKey(ctx, k2) {
		t.Fatal("a new key didn't replace the old one")
	}
	if err := s.RevokeAgentKey(ctx); err != nil || s.CheckAgentKey(ctx, k2) {
		t.Fatalf("revoked key still works (%v)", err)
	}
	if ok, _ := s.HasAgentKey(ctx); ok {
		t.Fatal("HasAgentKey after revoking")
	}
}
