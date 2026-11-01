package platform

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuiltinsAreValid(t *testing.T) {
	r, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(r.IDs(), ","); got != "careerlink,joboko,topcv,vietnamworks" {
		t.Fatalf("ids = %s", got)
	}
	for _, p := range r.All() {
		if err := p.validate(); err != nil {
			t.Errorf("builtin %s invalid: %v", p.ID, err)
		}
	}
	tc, _ := r.Get("TopCV ")
	if !tc.Verified || tc.ProbeJS == "" {
		t.Fatal("TopCV's verified isAuth probe missing")
	}
}

func TestOwns(t *testing.T) {
	p := Platform{Domains: []string{"topcv.vn"}}
	for host, want := range map[string]bool{
		"topcv.vn": true, "www.topcv.vn": true, ".topcv.vn": true, "WWW.TOPCV.VN": true,
		"evil-topcv.vn": false, "topcv.vn.evil.com": false, "google.com": false,
	} {
		if p.Owns(host) != want {
			t.Errorf("Owns(%q) = %v, want %v", host, !want, want)
		}
	}
}

func TestUserPlatforms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "platforms.json")
	os.WriteFile(path, []byte(`[
	  {"id":"stepstone","name":"StepStone","login_url":"https://www.stepstone.de/login",
	   "domains":["stepstone.de"],"probe_url":"https://www.stepstone.de/account","login_pattern":"/login"},
	  {"id":"topcv","name":"TopCV (fixed)","login_url":"https://www.topcv.vn/dang-nhap",
	   "domains":["topcv.vn"],"probe_url":"https://www.topcv.vn/viec-lam-da-luu","login_pattern":"dang-nhap"}
	]`), 0o600)
	r, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.Get("stepstone"); err != nil {
		t.Fatal("user platform not loaded")
	}
	if tc, _ := r.Get("topcv"); tc.Name != "TopCV (fixed)" || tc.Builtin {
		t.Fatalf("user override of a builtin not applied: %+v", tc)
	}
	if _, err := r.Get("nope"); !errors.Is(err, ErrUnknown) || !strings.Contains(err.Error(), "stepstone") {
		t.Fatalf("unknown id error = %v", err)
	}
}

func TestUserPlatformValidation(t *testing.T) {
	for name, entry := range map[string]string{
		"bad id":              `{"id":"Step Stone","name":"x","login_url":"https://a.de/l","domains":["a.de"],"probe_url":"https://a.de/p","login_pattern":"l"}`,
		"http url":            `{"id":"a","name":"x","login_url":"http://a.de/l","domains":["a.de"],"probe_url":"https://a.de/p","login_pattern":"l"}`,
		"http on a lookalike": `{"id":"a","name":"x","login_url":"http://localhost.evil.com/l","domains":["localhost.evil.com"],"probe_url":"http://localhost.evil.com/p","login_pattern":"l"}`,
		"no domains":          `{"id":"a","name":"x","login_url":"https://a.de/l","domains":[],"probe_url":"https://a.de/p","login_pattern":"l"}`,
		"url off-domain":      `{"id":"a","name":"x","login_url":"https://evil.com/l","domains":["a.de"],"probe_url":"https://a.de/p","login_pattern":"l"}`,
		"bad regexp":          `{"id":"a","name":"x","login_url":"https://a.de/l","domains":["a.de"],"probe_url":"https://a.de/p","login_pattern":"("}`,
		"no login pattern":    `{"id":"a","name":"x","login_url":"https://a.de/l","domains":["a.de"],"probe_url":"https://a.de/p"}`,
	} {
		path := filepath.Join(t.TempDir(), "platforms.json")
		os.WriteFile(path, []byte("["+entry+"]"), 0o600)
		if _, err := Load(path); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
