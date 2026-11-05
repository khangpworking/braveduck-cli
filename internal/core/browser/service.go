package browser

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/go-rod/rod/lib/launcher"
	"github.com/go-rod/rod/lib/launcher/flags"
)

// Service is the browser service a shared server runs in a container of its
// own (`braveduck browser-service`): go-rod's launcher.Manager, as in
// rod-manager, with two rules of its own. Every request must carry the key
// (?key=…), so a page open in one of its browsers can't start more of them;
// and the browser binary, profile and working directory are the service's,
// whatever the client asks for.
func Service(bin, key string) http.Handler {
	m := launcher.NewManager()
	m.Defaults = func(http.ResponseWriter, *http.Request) *launcher.Launcher { return launcher.New().Bin(bin) }
	m.BeforeLaunch = func(l *launcher.Launcher, _ http.ResponseWriter, _ *http.Request) {
		b := make([]byte, 8)
		_, _ = rand.Read(b)
		l.Bin(bin).Headless(true).
			UserDataDir(filepath.Join(os.TempDir(), "braveduck-browser", hex.EncodeToString(b))).
			Delete(flags.KeepUserDataDir).
			Delete(flags.WorkingDir)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("key")), []byte(key)) != 1 {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		m.ServeHTTP(w, r)
	})
}

// BrowserKeyFileEnv names the file holding the browser service's key.
const BrowserKeyFileEnv = "BRAVEDUCK_BROWSER_KEY_FILE"

// serviceURL is the browser service's address with its key added.
func serviceURL(svc string) (string, error) {
	path := strings.TrimSpace(os.Getenv(BrowserKeyFileEnv))
	if path == "" {
		return svc, nil
	}
	key, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(svc)
	if err != nil {
		return "", err
	}
	q := u.Query()
	q.Set("key", strings.TrimSpace(string(key)))
	u.RawQuery = q.Encode()
	return u.String(), nil
}
