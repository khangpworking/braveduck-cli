package hosted

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// loginUsers reads the login service's users file (Authelia's file
// backend), so an AI agent stops working the moment its user is disabled or
// removed there, as their logins do. /mcp skips the login service, which is
// why braveduck checks this itself. The file is read again when it changes;
// if it can't be read, no agent gets in.
type loginUsers struct {
	path string

	mu      sync.Mutex
	modTime time.Time
	size    int64
	active  map[string]bool // lower-cased name → may log in
}

// allowed reports whether name may use the board now.
func (u *loginUsers) allowed(name string) bool {
	fi, err := os.Stat(u.path)
	if err != nil {
		return false
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.active == nil || !fi.ModTime().Equal(u.modTime) || fi.Size() != u.size {
		active, err := readLoginUsers(u.path)
		if err != nil {
			u.active = nil
			return false
		}
		u.active, u.modTime, u.size = active, fi.ModTime(), fi.Size()
	}
	return u.active[name]
}

func readLoginUsers(path string) (map[string]bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var f struct {
		Users map[string]struct {
			Disabled bool `yaml:"disabled"`
		} `yaml:"users"`
	}
	if err := yaml.Unmarshal(b, &f); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	active := map[string]bool{}
	for name, u := range f.Users {
		if !u.Disabled {
			active[strings.ToLower(name)] = true
		}
	}
	return active, nil
}
