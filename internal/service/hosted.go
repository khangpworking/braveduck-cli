package service

import (
	"errors"
	"fmt"
)

// A shared server (`braveduck serve --users DIR`) runs one Service per user.
// There, two things that assume braveduck runs on the user's own computer
// are off: windows a person must see (logging in to a job site, assisted
// apply) would open on the server, and per-machine session keys (the OS
// keychain, BRAVEDUCK_PASSPHRASE) would be shared by every user.

// ErrHosted is returned by features that need a browser window on the
// user's own computer.
var ErrHosted = fmt.Errorf("%w: %w", ErrInvalid,
	errors.New("this opens a browser window on your own computer, so it's off on a shared server — run braveduck on your computer for it"))

// HostedInfo is what the board shows about a shared server: who is logged in
// and where to log out. Empty when braveduck runs on the user's own computer.
type HostedInfo struct {
	User      string `json:"user"`
	LogoutURL string `json:"logout_url,omitempty"`
	// Owner is the user whose computer the shared server runs on: windows
	// open on their own screen and paths are their own files, so what a
	// shared server turns off (logins to job sites, assisted apply, CV
	// files by path) stays on for them.
	Owner bool `json:"owner,omitempty"`
}

// SetHosted marks s as one user's service on a shared server. Call it after
// SetDataDir and before the service is used.
func (s *Service) SetHosted(info HostedInfo) { s.hosted = &info }

// OnOwnComputer reports whether braveduck runs on this user's own computer
// (alone, or as the owner of a shared server), so a window it opens is on
// their screen and a file path is theirs.
func (s *Service) OnOwnComputer() bool { return s.hosted == nil || s.hosted.Owner }

// Hosted is the shared-server info, nil on the user's own computer.
func (s *Service) Hosted() *HostedInfo { return s.hosted }
