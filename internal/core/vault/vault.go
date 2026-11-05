// Package vault encrypts platform sessions at rest (docs/specs/platform-sessions.md,
// "Security"). A session is sealed with AES-256-GCM under a 32-byte key; the
// platform id is bound in as associated data, so a blob saved for one site
// can't be swapped onto another.
//
// Threat model: casual copying of the data dir (backups, synced folders, a
// leaked .db). It does not defend against malware running as the user —
// nothing on a normal desktop does, the browser's own cookie store included.
package vault

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/zalando/go-keyring"
	"golang.org/x/crypto/argon2"
)

// Key source names, as `doctor` reports them.
const (
	SourcePassphrase = "passphrase (BRAVEDUCK_PASSPHRASE)"
	SourceKeychain   = "OS keychain"
	SourceKeyFile    = "key file"
)

// PassphraseEnv opts into a passphrase-derived key: sessions are then
// unreadable without it, at the cost of providing it whenever one is used.
const PassphraseEnv = "BRAVEDUCK_PASSPHRASE"

const (
	keyLen         = 32
	keyFileName    = "session.key"
	saltFileName   = "session.salt"
	keyringService = "braveduck"
	keyringUser    = "session-key"
	keyringTimeout = 3 * time.Second
	formatVersion  = 1
	aadPrefix      = "braveduck-session:v1:"
	argonTime      = 3
	argonMemoryKiB = 64 * 1024
	argonThreads   = 4
)

// ErrDecrypt means a blob couldn't be opened: wrong key (e.g. a different
// passphrase), wrong platform, or tampered data. Callers treat the session as
// gone and ask the user to log in again.
var ErrDecrypt = errors.New("session can't be decrypted — log in again")

// Vault seals and opens session blobs.
type Vault struct {
	aead   cipher.AEAD
	Source string // where the key came from (one of the Source* constants)
}

// keyringAPI is the keychain; swapped in tests.
var keyringAPI interface {
	Get(service, user string) (string, error)
	Set(service, user, password string) error
} = realKeyring{}

type realKeyring struct{}

func (realKeyring) Get(s, u string) (string, error) { return keyring.Get(s, u) }
func (realKeyring) Set(s, u, p string) error        { return keyring.Set(s, u, p) }

// Open loads (or creates, on first use) the key for the data dir dir and
// returns a Vault. Key sources, first match wins:
//  1. BRAVEDUCK_PASSPHRASE set → Argon2id(passphrase, per-install salt).
//  2. An existing key file → that file. Sticky: once sessions are sealed under
//     the file, a keychain appearing later doesn't orphan them.
//  3. The OS keychain, when the machine has one (existing entry, or a new one).
//  4. A new key file, 0600 in a 0700 dir — the realistic case on WSL/headless.
func Open(dir string) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, source, err := loadKey(dir)
	if err != nil {
		return nil, err
	}
	return newVault(key, source)
}

// OpenFile is Open with only the key file in dir: no passphrase and no OS
// keychain, both of which are one per machine. A shared server uses it so
// every user's sessions are sealed under that user's own key.
func OpenFile(dir string) (*Vault, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	key, err := readOrCreate(filepath.Join(dir, keyFileName), keyLen)
	if err != nil {
		return nil, err
	}
	return newVault(key, SourceKeyFile)
}

func newVault(key []byte, source string) (*Vault, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Vault{aead: aead, Source: source}, nil
}

func loadKey(dir string) ([]byte, string, error) {
	if pass := os.Getenv(PassphraseEnv); pass != "" {
		salt, err := readOrCreate(filepath.Join(dir, saltFileName), 16)
		if err != nil {
			return nil, "", err
		}
		return argon2.IDKey([]byte(pass), salt, argonTime, argonMemoryKiB, argonThreads, keyLen), SourcePassphrase, nil
	}

	keyPath := filepath.Join(dir, keyFileName)
	if b, err := os.ReadFile(keyPath); err == nil {
		if len(b) != keyLen {
			return nil, "", fmt.Errorf("%s is corrupt (want %d bytes, got %d)", keyPath, keyLen, len(b))
		}
		return b, SourceKeyFile, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, "", err
	}

	if key, ok := keychainKey(); ok {
		return key, SourceKeychain, nil
	}

	key, err := readOrCreate(keyPath, keyLen)
	return key, SourceKeyFile, err
}

// keychainKey returns the key from the OS keychain, creating it there if
// absent. ok is false when there's no usable keychain. Calls are bounded: a
// Linux session bus with no Secret Service can otherwise block for ~25s
// waiting on service activation.
func keychainKey() ([]byte, bool) {
	type result struct {
		key []byte
		ok  bool
	}
	ch := make(chan result, 1)
	// Read keyringAPI once, here: the goroutine may outlive this call (a hung
	// keychain is abandoned after the timeout) and must not race with whoever
	// sets keyringAPI next.
	kr := keyringAPI
	go func() {
		if s, err := kr.Get(keyringService, keyringUser); err == nil {
			if dec, err := hex.DecodeString(s); err == nil && len(dec) == keyLen {
				ch <- result{dec, true}
				return
			}
			ch <- result{nil, false} // unexpected value: don't touch it, use the file
			return
		} else if !errors.Is(err, keyring.ErrNotFound) {
			ch <- result{nil, false}
			return
		}
		key := make([]byte, keyLen)
		if _, err := rand.Read(key); err != nil {
			ch <- result{nil, false}
			return
		}
		if err := kr.Set(keyringService, keyringUser, hex.EncodeToString(key)); err != nil {
			ch <- result{nil, false}
			return
		}
		ch <- result{key, true}
	}()
	select {
	case r := <-ch:
		return r.key, r.ok
	case <-time.After(keyringTimeout):
		return nil, false
	}
}

// readOrCreate returns the n-byte secret at path, creating it (0600, random)
// if missing. O_EXCL keeps a concurrent creator's file instead of clobbering it.
func readOrCreate(path string, n int) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != n {
			return nil, fmt.Errorf("%s is corrupt (want %d bytes, got %d)", path, n, len(b))
		}
		return b, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		return readOrCreate(path, n)
	} else if err != nil {
		return nil, err
	}
	_, err = f.Write(b)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return b, err
}

// Seal encrypts plaintext for platform. Output: version byte, nonce, ciphertext.
func (v *Vault) Seal(platform string, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, v.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	out := append([]byte{formatVersion}, nonce...)
	return v.aead.Seal(out, nonce, plaintext, []byte(aadPrefix+platform)), nil
}

// Open decrypts a blob sealed for platform, or returns ErrDecrypt.
func (v *Vault) Open(platform string, blob []byte) ([]byte, error) {
	ns := v.aead.NonceSize()
	if len(blob) < 1+ns || blob[0] != formatVersion {
		return nil, ErrDecrypt
	}
	pt, err := v.aead.Open(nil, blob[1:1+ns], blob[1+ns:], []byte(aadPrefix+platform))
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}
