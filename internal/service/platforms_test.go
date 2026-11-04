package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-rod/rod/lib/proto"

	"github.com/khangpworking/braveduck-cli/internal/core/browser"
	"github.com/khangpworking/braveduck-cli/internal/core/platform"
	"github.com/khangpworking/braveduck-cli/internal/core/vault"
	"github.com/khangpworking/braveduck-cli/internal/store"
)

const secretCookie = "SUPER-SECRET-SESSION-VALUE"

type fakeBrowser struct {
	probeState   string
	probeAccount string // label the account page shows
	loginAccount string // label the login page shows ("" = none, like real TopCV)
	loginErr     error
	loginWait    chan struct{} // if set, login blocks until closed
	seeds        []string
	logins       atomic.Int32
}

func (f *fakeBrowser) login(_ context.Context, _ platform.Platform, seed string, o browser.LoginOptions) (browser.Session, error) {
	f.logins.Add(1)
	f.seeds = append(f.seeds, seed)
	if f.loginWait != nil {
		<-f.loginWait
	}
	if f.loginErr != nil {
		return browser.Session{}, f.loginErr
	}
	if o.OnDetected != nil {
		o.OnDetected()
	}
	return browser.Session{
		Cookies: []*proto.NetworkCookie{
			{Name: "topcv_session", Value: secretCookie, Domain: ".topcv.vn",
				Expires: proto.TimeSinceEpoch(time.Now().Add(90 * 24 * time.Hour).Unix())},
			{Name: "sso_token", Value: "x", Domain: ".topcv.vn", // short-lived helper: not the hint
				Expires: proto.TimeSinceEpoch(time.Now().Add(12 * time.Hour).Unix())},
		},
		Account: f.loginAccount,
		Storage: map[string]map[string]string{"https://www.topcv.vn": {"token": secretCookie}},
	}, nil
}

func (f *fakeBrowser) probe(context.Context, platform.Platform, browser.Session, string) (browser.ProbeResult, error) {
	r := browser.ProbeResult{State: f.probeState}
	if r.State == store.SessionConnected {
		r.Account = f.probeAccount
	}
	return r, nil
}

func newPlatformSvc(t *testing.T) (*Service, *fakeBrowser) {
	t.Helper()
	t.Setenv(vault.PassphraseEnv, "")
	s := newSvc(t)
	s.SetDataDir(t.TempDir())
	fb := &fakeBrowser{probeState: store.SessionConnected, probeAccount: "ng…@gmail.com"}
	s.plat.deps = platformDeps{login: fb.login, probe: fb.probe}
	return s, fb
}

func statusOf(t *testing.T, s *Service, id string) PlatformStatus {
	t.Helper()
	all, err := s.Platforms(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range all {
		if st.ID == id {
			return st
		}
	}
	t.Fatalf("no platform %s", id)
	return PlatformStatus{}
}

func TestConnectSavesSealedSessionAndNeverLeaksIt(t *testing.T) {
	s, _ := newPlatformSvc(t)
	ctx := context.Background()

	if st := statusOf(t, s, "topcv"); st.Status != "not_connected" || !st.Verified {
		t.Fatalf("before = %+v", st)
	}
	detected := false
	st, err := s.Connect(ctx, "TopCV", LoginHooks{OnDetected: func() { detected = true }})
	if err != nil {
		t.Fatal(err)
	}
	if st.Status != store.SessionConnected || st.Since == "" || !detected || st.Account != "ng…@gmail.com" {
		t.Fatalf("after connect = %+v (detected=%v)", st, detected)
	}
	if hint, _ := time.Parse(time.RFC3339, st.ExpiresHint); time.Until(hint) < 80*24*time.Hour {
		t.Fatalf("expiry hint %q follows a helper cookie, not topcv_session (~90 days)", st.ExpiresHint)
	}

	raw, _ := s.st.GetSession(ctx, "topcv")
	if strings.Contains(string(raw.Enc), secretCookie) {
		t.Fatal("session stored in plaintext")
	}
	all, _ := s.Platforms(ctx)
	if b, _ := json.Marshal(all); strings.Contains(string(b), secretCookie) {
		t.Fatal("session value visible in the platform list")
	}

	sess, seed, err := s.PlatformSession(ctx, "topcv")
	if err != nil || sess.Cookies[0].Value != secretCookie || seed == "" {
		t.Fatalf("decrypted session = %+v, seed %q, %v", sess, seed, err)
	}

	ev, _ := s.Activity(ctx, 1)
	if ev[0].Type != "connected" || strings.Contains(ev[0].Message, secretCookie) {
		t.Fatalf("event = %+v", ev[0])
	}
}

func TestReloginKeepsFingerprint(t *testing.T) {
	s, fb := newPlatformSvc(t)
	ctx := context.Background()
	s.Connect(ctx, "topcv", LoginHooks{})
	s.Connect(ctx, "topcv", LoginHooks{})
	if len(fb.seeds) != 2 || fb.seeds[0] != fb.seeds[1] {
		t.Fatalf("seeds = %v, want the same seed on re-login", fb.seeds)
	}
	s.Connect(ctx, "vietnamworks", LoginHooks{})
	if fb.seeds[2] == fb.seeds[0] {
		t.Fatal("two platforms share a fingerprint seed")
	}
}

func TestConnectRefusesStillLoggedOut(t *testing.T) {
	s, fb := newPlatformSvc(t)
	fb.probeState = store.SessionExpired
	if _, err := s.Connect(context.Background(), "topcv", LoginHooks{}); !errors.Is(err, ErrStillLoggedOut) {
		t.Fatalf("err = %v, want ErrStillLoggedOut", err)
	}
	if _, err := s.st.GetSession(context.Background(), "topcv"); !store.IsNotFound(err) {
		t.Fatal("a logged-out session was saved")
	}
}

func TestConnectErrors(t *testing.T) {
	s, fb := newPlatformSvc(t)
	ctx := context.Background()
	if _, err := s.Connect(ctx, "linkedin", LoginHooks{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("unknown platform = %v", err)
	}
	fb.loginErr = browser.ErrNotLoggedIn
	if _, err := s.Connect(ctx, "topcv", LoginHooks{}); !errors.Is(err, browser.ErrNotLoggedIn) {
		t.Fatalf("closed window = %v", err)
	}
	if st := statusOf(t, s, "topcv"); st.Status != "not_connected" {
		t.Fatalf("status after failed login = %+v", st)
	}
}

func TestStartConnectInBackground(t *testing.T) {
	s, fb := newPlatformSvc(t)
	ctx := context.Background()
	fb.loginWait = make(chan struct{})

	st, err := s.StartConnect("topcv")
	if err != nil || st.Status != "connecting" {
		t.Fatalf("StartConnect = %+v, %v", st, err)
	}
	if got := statusOf(t, s, "topcv"); got.Status != "connecting" {
		t.Fatalf("listed status = %q, want connecting", got.Status)
	}
	// A second click while the window is open must not open another one.
	if _, err := s.StartConnect("topcv"); !errors.Is(err, ErrLoginInProgress) {
		t.Fatalf("second StartConnect = %v", err)
	}
	if _, err := s.Connect(ctx, "topcv", LoginHooks{}); !errors.Is(err, ErrLoginInProgress) {
		t.Fatalf("Connect during StartConnect = %v", err)
	}
	close(fb.loginWait)

	deadline := time.Now().Add(3 * time.Second)
	for statusOf(t, s, "topcv").Status != store.SessionConnected {
		if time.Now().After(deadline) {
			t.Fatalf("background login never finished: %+v", statusOf(t, s, "topcv"))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n := fb.logins.Load(); n != 1 {
		t.Fatalf("%d login windows opened, want 1", n)
	}

	// A background failure is reported in the activity feed.
	fb.loginWait, fb.loginErr = nil, browser.ErrLoginTimeout
	s.StartConnect("vietnamworks")
	deadline = time.Now().Add(3 * time.Second)
	for {
		ev, _ := s.Activity(ctx, 1)
		if len(ev) == 1 && ev[0].Type == "error" && strings.Contains(ev[0].Message, "VietnamWorks") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("background failure not logged: %+v", ev)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestDisconnectAndCheck(t *testing.T) {
	s, fb := newPlatformSvc(t)
	ctx := context.Background()
	if _, err := s.CheckPlatform(ctx, "topcv"); !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "braveduck login topcv") {
		t.Fatalf("check unconnected = %v", err)
	}
	s.Connect(ctx, "topcv", LoginHooks{})

	fb.probeState = store.SessionExpired
	if st, _ := s.CheckPlatform(ctx, "topcv"); st.Status != store.SessionExpired {
		t.Fatalf("check after expiry = %+v", st)
	}

	// A blob the vault can't open (tampered / key changed) reads as expired.
	raw, _ := s.st.GetSession(ctx, "topcv")
	raw.Enc[len(raw.Enc)-1] ^= 1
	s.st.SaveSession(ctx, raw)
	fb.probeState = store.SessionConnected
	if st, _ := s.CheckPlatform(ctx, "topcv"); st.Status != store.SessionExpired {
		t.Fatalf("tampered session = %+v, want expired", st)
	}

	if del, err := s.Disconnect(ctx, "topcv"); !del || err != nil {
		t.Fatalf("disconnect = %v, %v", del, err)
	}
	if st := statusOf(t, s, "topcv"); st.Status != "not_connected" {
		t.Fatalf("after disconnect = %+v", st)
	}
	if del, _ := s.Disconnect(ctx, "topcv"); del {
		t.Fatal("second disconnect reported a deletion")
	}
}

// Real TopCV: the page a login finishes on doesn't carry the account, the
// account page the check opens does. The label must come from there, and a
// later check must refresh it without losing it when the page lacks it.
func TestAccountLabelFromAccountPage(t *testing.T) {
	s, fb := newPlatformSvc(t)
	ctx := context.Background()

	st, err := s.Connect(ctx, "topcv", LoginHooks{})
	if err != nil || st.Account != "ng…@gmail.com" {
		t.Fatalf("connect = %+v, %v; want the label from the account page", st, err)
	}

	fb.loginAccount = "lo…@gmail.com" // when the login page has it, it wins
	if st, _ := s.Connect(ctx, "topcv", LoginHooks{}); st.Account != "lo…@gmail.com" {
		t.Fatalf("re-login label = %q", st.Account)
	}

	fb.probeAccount = ""
	if st, _ := s.CheckPlatform(ctx, "topcv"); st.Account != "lo…@gmail.com" {
		t.Fatalf("check without a label wiped it: %q", st.Account)
	}
	fb.probeAccount = "kh…@gmail.com"
	if st, _ := s.CheckPlatform(ctx, "topcv"); st.Account != "kh…@gmail.com" {
		t.Fatalf("check didn't refresh the label: %q", st.Account)
	}
}
