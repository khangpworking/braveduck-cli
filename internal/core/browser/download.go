// Package browser gives braveduck a built-in stealth browser so "Extract & Add"
// can render Cloudflare/JS-gated listings (TopCV, …) with zero external setup —
// no Python, no pip, no manual browser install. It self-provisions CloakHQ's
// patched Chromium (the same binary the CloakBrowser project ships, with its
// fingerprint patches compiled in) on first use, caches it under ~/.cloakbrowser,
// and drives it over CDP via go-rod. The download is pinned to a known version
// and checksum-verified; any failure surfaces to the caller, which degrades to a
// bare-URL add rather than blocking.
package browser

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// pinnedVersions maps a platform tag to the CloakBrowser Chromium build braveduck
// pulls. Pinned (not "latest") so a checksum stays valid and upstream can't shift
// under us. Bump deliberately alongside the SHA verification.
var pinnedVersions = map[string]string{
	"linux-x64":    "146.0.7680.177.5",
	"linux-arm64":  "146.0.7680.177.3",
	"darwin-arm64": "145.0.7632.109.2",
	"darwin-x64":   "145.0.7632.109.2",
	"windows-x64":  "146.0.7680.177.5",
}

// platformTag resolves the running OS/arch to CloakBrowser's release tag.
func platformTag() (string, error) {
	switch runtime.GOOS {
	case "linux":
		switch runtime.GOARCH {
		case "amd64":
			return "linux-x64", nil
		case "arm64":
			return "linux-arm64", nil
		}
	case "darwin":
		switch runtime.GOARCH {
		case "arm64":
			return "darwin-arm64", nil
		case "amd64":
			return "darwin-x64", nil
		}
	case "windows":
		if runtime.GOARCH == "amd64" {
			return "windows-x64", nil
		}
	}
	return "", fmt.Errorf("cloakbrowser: unsupported platform %s/%s", runtime.GOOS, runtime.GOARCH)
}

// cacheRoot is where provisioned Chromium builds live. Matches CloakBrowser's
// default (~/.cloakbrowser) so an existing install is reused, and honors the same
// CLOAKBROWSER_CACHE_DIR override.
func cacheRoot() (string, error) {
	if d := strings.TrimSpace(os.Getenv("CLOAKBROWSER_CACHE_DIR")); d != "" {
		return d, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".cloakbrowser"), nil
}

// binaryRelPath is the executable's location inside chromium-{version}/, which
// differs per platform (flat on Linux/Windows, inside the .app on macOS).
func binaryRelPath(version string) string {
	dir := "chromium-" + version
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(dir, "chrome.exe")
	case "darwin":
		return filepath.Join(dir, "Chromium.app", "Contents", "MacOS", "Chromium")
	default:
		return filepath.Join(dir, "chrome")
	}
}

// EnsureBinary returns the path to a ready-to-launch CloakBrowser Chromium,
// downloading and extracting it on first use. CLOAKBROWSER_BINARY_PATH short-
// circuits everything (bring-your-own binary); an already-cached build is reused.
func EnsureBinary(ctx context.Context) (string, error) {
	if p := strings.TrimSpace(os.Getenv("CLOAKBROWSER_BINARY_PATH")); p != "" {
		return p, nil
	}
	tag, err := platformTag()
	if err != nil {
		return "", err
	}
	version, ok := pinnedVersions[tag]
	if !ok {
		return "", fmt.Errorf("cloakbrowser: no pinned version for %s", tag)
	}
	root, err := cacheRoot()
	if err != nil {
		return "", err
	}
	binPath := filepath.Join(root, binaryRelPath(version))
	if fi, err := os.Stat(binPath); err == nil && !fi.IsDir() {
		return binPath, nil
	}
	if err := download(ctx, root, tag, version); err != nil {
		return "", err
	}
	if _, err := os.Stat(binPath); err != nil {
		return "", fmt.Errorf("cloakbrowser: binary missing after extract: %w", err)
	}
	return binPath, nil
}

// download fetches, checksum-verifies, and extracts the Chromium archive into
// root/chromium-{version}. It tries CloakBrowser's CDN first, then the GitHub
// release mirror, for both the archive and the SHA256SUMS manifest.
func download(ctx context.Context, root, tag, version string) error {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	archiveName := "cloakbrowser-" + tag + ext

	bases := []string{
		"https://cloakbrowser.dev/chromium-v" + version,
		"https://github.com/CloakHQ/cloakbrowser/releases/download/chromium-v" + version,
	}
	var checksumURLs, archiveURLs []string
	for _, b := range bases {
		checksumURLs = append(checksumURLs, b+"/SHA256SUMS")
		archiveURLs = append(archiveURLs, b+"/"+archiveName)
	}

	want, err := expectedSHA(ctx, checksumURLs, archiveName)
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp(root, "cloak-dl-*"+ext)
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	tmp.Close()
	defer os.Remove(tmpPath)

	var dlErr error
	got := false
	for _, u := range archiveURLs {
		if err := downloadTo(ctx, u, tmpPath, want); err != nil {
			dlErr = err
			continue
		}
		got = true
		break
	}
	if !got {
		return fmt.Errorf("cloakbrowser: download failed: %w", dlErr)
	}

	staging, err := os.MkdirTemp(root, "cloak-stage-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(staging)

	if ext == ".zip" {
		if err := extractZip(tmpPath, staging); err != nil {
			return err
		}
	} else {
		if err := extractTarGz(tmpPath, staging); err != nil {
			return err
		}
	}

	// The archive wraps everything in a single top-level dir; flatten it so the
	// binary lands at chromium-{version}/chrome (matching binaryRelPath).
	top := staging
	if entries, err := os.ReadDir(staging); err == nil && len(entries) == 1 && entries[0].IsDir() {
		top = filepath.Join(staging, entries[0].Name())
	}
	final := filepath.Join(root, "chromium-"+version)
	os.RemoveAll(final)
	if err := os.Rename(top, final); err != nil {
		return err
	}
	return os.Chmod(filepath.Join(root, binaryRelPath(version)), 0o755)
}

// expectedSHA fetches the first reachable SHA256SUMS manifest and returns the
// hash line matching archiveName. A hard requirement — we won't run an unverified
// downloaded executable.
func expectedSHA(ctx context.Context, urls []string, archiveName string) (string, error) {
	var lastErr error
	for _, u := range urls {
		body, err := httpGet(ctx, u)
		if err != nil {
			lastErr = err
			continue
		}
		sc := bufio.NewScanner(strings.NewReader(string(body)))
		for sc.Scan() {
			fields := strings.Fields(sc.Text())
			if len(fields) != 2 {
				continue
			}
			// SHA256SUMS binary mode prefixes the name with '*'.
			if strings.TrimPrefix(fields[1], "*") == archiveName {
				return strings.ToLower(fields[0]), nil
			}
		}
		lastErr = fmt.Errorf("cloakbrowser: %s not listed in %s", archiveName, u)
	}
	return "", fmt.Errorf("cloakbrowser: no checksum for %s: %w", archiveName, lastErr)
}

// downloadTo streams url to dst, verifying its SHA-256 against want before the
// file is trusted. A mismatch or non-200 is an error and leaves dst overwritten
// (the caller retries the next mirror or aborts).
func downloadTo(ctx context.Context, url, dst, want string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cloakbrowser: GET %s: %s", url, resp.Status)
	}

	f, err := os.Create(dst)
	if err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(io.MultiWriter(f, h), resp.Body); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != want {
		return fmt.Errorf("cloakbrowser: checksum mismatch for %s (got %s, want %s)", url, got, want)
	}
	return nil
}

// httpGet is a small GET-into-memory helper for the checksum manifest (tiny).
func httpGet(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

// extractTarGz unpacks a .tar.gz into dest, rejecting path-traversal entries and
// preserving directories, regular files, symlinks, and hard links.
func extractTarGz(archivePath, dest string) error {
	f, err := os.Open(archivePath)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		target, err := safeJoin(dest, hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				out.Close()
				return err
			}
			out.Close()
		case tar.TypeSymlink:
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			if err := os.Symlink(hdr.Linkname, target); err != nil {
				return err
			}
		case tar.TypeLink:
			linkTarget, err := safeJoin(dest, hdr.Linkname)
			if err != nil {
				return err
			}
			os.MkdirAll(filepath.Dir(target), 0o755)
			os.Remove(target)
			if err := os.Link(linkTarget, target); err != nil {
				return err
			}
		}
	}
}

// extractZip unpacks a .zip into dest (Windows builds), rejecting traversal.
func extractZip(archivePath, dest string) error {
	zr, err := zip.OpenReader(archivePath)
	if err != nil {
		return err
	}
	defer zr.Close()
	for _, zf := range zr.File {
		target, err := safeJoin(dest, zf.Name)
		if err != nil {
			return err
		}
		if zf.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := zf.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, zf.Mode())
		if err != nil {
			rc.Close()
			return err
		}
		_, cErr := io.Copy(out, rc)
		out.Close()
		rc.Close()
		if cErr != nil {
			return cErr
		}
	}
	return nil
}

// safeJoin joins name under base, refusing entries that would escape it.
func safeJoin(base, name string) (string, error) {
	target := filepath.Join(base, name)
	if target != base && !strings.HasPrefix(target, base+string(os.PathSeparator)) {
		return "", fmt.Errorf("cloakbrowser: unsafe path in archive: %q", name)
	}
	return target, nil
}
