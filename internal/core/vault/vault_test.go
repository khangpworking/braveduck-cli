package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/zalando/go-keyring"
)

// fakeKeyring is an in-memory keychain; err != nil simulates "no keychain".
type fakeKeyring struct {
	m     map[string]string
	err   error
	block bool
}

func (f *fakeKeyring) Get(s, u string) (string, error) {
	if f.block {
		select {}
	}
	if f.err != nil {
		return "", f.err
	}
	v, ok := f.m[s+"/"+u]
	if !ok {
		return "", keyring.ErrNotFound
	}
	return v, nil
}

func (f *fakeKeyring) Set(s, u, p string) error {
	if f.err != nil {
		return f.err
	}
	f.m[s+"/"+u] = p
	return nil
}

func useKeyring(t *testing.T, k *fakeKeyring) {
	t.Helper()
	orig := keyringAPI
	keyringAPI = k
	t.Cleanup(func() { keyringAPI = orig })
}

func noKeychain(t *testing.T) {
	useKeyring(t, &fakeKeyring{err: errors.New("no secret service")})
}

func TestSealOpenRoundTrip(t *testing.T) {
	noKeychain(t)
	t.Setenv(PassphraseEnv, "")
	v, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	secret := []byte(`{"cookies":[{"name":"session","value":"s3cr3t"}]}`)
	blob, err := v.Seal("topcv", secret)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(blob, []byte("s3cr3t")) {
		t.Fatal("plaintext visible in the sealed blob")
	}
	got, err := v.Open("topcv", blob)
	if err != nil || !bytes.Equal(got, secret) {
		t.Fatalf("Open = %q, %v", got, err)
	}

	// Same plaintext twice must not produce the same blob (random nonce).
	blob2, _ := v.Seal("topcv", secret)
	if bytes.Equal(blob, blob2) {
		t.Fatal("nonce reused")
	}
}

func TestOpenRejectsTamperSwapAndWrongKey(t *testing.T) {
	noKeychain(t)
	t.Setenv(PassphraseEnv, "")
	v, _ := Open(t.TempDir())
	blob, _ := v.Seal("topcv", []byte("cookies"))

	flipped := append([]byte(nil), blob...)
	flipped[len(flipped)-1] ^= 1
	for name, tc := range map[string]struct {
		platform string
		blob     []byte
	}{
		"tampered":        {"topcv", flipped},
		"other platform":  {"vietnamworks", blob}, // AAD binds the platform
		"truncated":       {"topcv", blob[:5]},
		"unknown version": {"topcv", append([]byte{9}, blob[1:]...)},
	} {
		if _, err := v.Open(tc.platform, tc.blob); !errors.Is(err, ErrDecrypt) {
			t.Errorf("%s: err = %v, want ErrDecrypt", name, err)
		}
	}

	other, _ := Open(t.TempDir()) // a different install = a different key
	if _, err := other.Open("topcv", blob); !errors.Is(err, ErrDecrypt) {
		t.Errorf("wrong key: err = %v, want ErrDecrypt", err)
	}
}

func TestKeyFileFallbackIsPrivateAndSticky(t *testing.T) {
	noKeychain(t)
	t.Setenv(PassphraseEnv, "")
	dir := filepath.Join(t.TempDir(), "bd")
	v, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != SourceKeyFile {
		t.Fatalf("source = %q, want key file", v.Source)
	}
	fi, err := os.Stat(filepath.Join(dir, keyFileName))
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
	if di, _ := os.Stat(dir); di.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode = %v, want 0700", di.Mode().Perm())
	}
	blob, _ := v.Seal("topcv", []byte("x"))

	// A keychain showing up later must not orphan file-sealed sessions.
	useKeyring(t, &fakeKeyring{m: map[string]string{}})
	v2, err := Open(dir)
	if err != nil || v2.Source != SourceKeyFile {
		t.Fatalf("reopen source = %q (%v), want the existing key file", v2.Source, err)
	}
	if _, err := v2.Open("topcv", blob); err != nil {
		t.Fatalf("session unreadable after reopen: %v", err)
	}
}

func TestKeychainUsedWhenAvailable(t *testing.T) {
	t.Setenv(PassphraseEnv, "")
	k := &fakeKeyring{m: map[string]string{}}
	useKeyring(t, k)
	dir := t.TempDir()
	v, err := Open(dir)
	if err != nil || v.Source != SourceKeychain {
		t.Fatalf("source = %q (%v), want keychain", v.Source, err)
	}
	if _, err := os.Stat(filepath.Join(dir, keyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("key file written although the keychain holds the key")
	}
	blob, _ := v.Seal("topcv", []byte("x"))
	v2, _ := Open(dir)
	if _, err := v2.Open("topcv", blob); err != nil {
		t.Fatalf("keychain key not stable across opens: %v", err)
	}
}

func TestHangingKeychainFallsBackQuickly(t *testing.T) {
	t.Setenv(PassphraseEnv, "")
	useKeyring(t, &fakeKeyring{block: true})
	start := time.Now()
	v, err := Open(t.TempDir())
	if err != nil || v.Source != SourceKeyFile {
		t.Fatalf("source = %q (%v), want key file fallback", v.Source, err)
	}
	if d := time.Since(start); d > keyringTimeout+2*time.Second {
		t.Fatalf("fallback took %v", d)
	}
}

func TestPassphrase(t *testing.T) {
	noKeychain(t)
	dir := t.TempDir()
	t.Setenv(PassphraseEnv, "correct horse")
	v, err := Open(dir)
	if err != nil || v.Source != SourcePassphrase {
		t.Fatalf("source = %q (%v)", v.Source, err)
	}
	blob, _ := v.Seal("topcv", []byte("x"))
	if _, err := os.Stat(filepath.Join(dir, keyFileName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("passphrase mode wrote a key file")
	}

	t.Setenv(PassphraseEnv, "wrong horse")
	v2, _ := Open(dir)
	if _, err := v2.Open("topcv", blob); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("wrong passphrase: err = %v, want ErrDecrypt", err)
	}
	t.Setenv(PassphraseEnv, "correct horse")
	v3, _ := Open(dir)
	if _, err := v3.Open("topcv", blob); err != nil {
		t.Fatalf("right passphrase rejected: %v", err)
	}
}

func TestCorruptKeyFileIsAnError(t *testing.T) {
	noKeychain(t)
	t.Setenv(PassphraseEnv, "")
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, keyFileName), []byte("short"), 0o600)
	if _, err := Open(dir); err == nil {
		t.Fatal("corrupt key file accepted")
	}
}

// OpenFile ignores the per-machine key sources, so two users' data dirs on
// one server never share a key.
func TestOpenFileKeyIsPerDir(t *testing.T) {
	t.Setenv(PassphraseEnv, "shared by everyone")
	k := &fakeKeyring{m: map[string]string{}}
	useKeyring(t, k)
	alice, bob := t.TempDir(), t.TempDir()
	va, err := OpenFile(alice)
	if err != nil || va.Source != SourceKeyFile {
		t.Fatalf("source = %q (%v), want key file", va.Source, err)
	}
	vb, _ := OpenFile(bob)
	if len(k.m) != 0 {
		t.Fatal("OpenFile touched the OS keychain")
	}
	blob, _ := va.Seal("topcv", []byte("alice's cookies"))
	if _, err := vb.Open("topcv", blob); !errors.Is(err, ErrDecrypt) {
		t.Fatalf("bob opened alice's session: %v", err)
	}
	va2, _ := OpenFile(alice)
	if got, err := va2.Open("topcv", blob); err != nil || string(got) != "alice's cookies" {
		t.Fatalf("key not stable across opens: %q %v", got, err)
	}
	if fi, err := os.Stat(filepath.Join(alice, keyFileName)); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("key file mode = %v (%v), want 0600", fi.Mode().Perm(), err)
	}
}
