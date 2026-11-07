package service

import (
	"context"
	"errors"
	"testing"
)

// On a shared server, windows and file paths are off, except for the user
// whose computer the server runs on.
func TestHostedOwnerKeepsWindows(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		info  *HostedInfo
		local bool
	}{
		{nil, true},
		{&HostedInfo{User: "friend@example.com"}, false},
		{&HostedInfo{User: "me@example.com", Owner: true}, true},
	} {
		s := newSvc(t)
		if c.info != nil {
			s.SetHosted(*c.info)
		}
		if got := s.OnOwnComputer(); got != c.local {
			t.Errorf("%+v: OnOwnComputer = %v", c.info, got)
		}
		_, err := s.PrepareApply(ctx, 999)
		if hosted := errors.Is(err, ErrHosted); hosted == c.local {
			t.Errorf("%+v: assisted apply refused as hosted = %v", c.info, hosted)
		}
	}
}
