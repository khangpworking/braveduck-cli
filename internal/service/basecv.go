package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/khangpworking/braveduck-cli/internal/core/cvfile"
	"github.com/khangpworking/braveduck-cli/internal/core/markdown"
)

// The base CV: the CV the user uploads once (PDF, .docx, Markdown or HTML).
// The file is kept in <data dir>/cv for applying when a job has no tailored
// CV, and its text goes into the profile's "About me" section, so rating and
// tailoring can start. braveduck reads it locally; nothing is sent anywhere
// by the upload itself.

// ErrAboutMeFilled means the profile's "About me" already has the user's
// writing; uploading again needs an explicit replace.
var ErrAboutMeFilled = errors.New(`your profile's "About me" already has text — replace it with this CV?`)

// maxCVTextChars caps the text copied into the profile (a CV is a few pages).
const maxCVTextChars = 50_000

// BaseCV describes the uploaded CV.
type BaseCV struct {
	Name          string `json:"name"` // the file's name as uploaded
	Kind          string `json:"kind"` // pdf, docx, md or html
	Size          int    `json:"size"`
	Path          string `json:"path"`
	UploadedAt    string `json:"uploaded_at"`
	ProfileFilled bool   `json:"profile_filled"` // rating can run
}

const baseCVKey = "base_cv"

// BaseCV returns the uploaded CV; ok is false when there is none.
func (s *Service) BaseCV(ctx context.Context) (cv BaseCV, ok bool, err error) {
	v, ok, err := s.st.Setting(ctx, baseCVKey)
	if err != nil || !ok {
		return BaseCV{}, false, err
	}
	if err := json.Unmarshal([]byte(v), &cv); err != nil {
		return BaseCV{}, false, err
	}
	if _, err := os.Stat(cv.Path); err != nil {
		return BaseCV{}, false, nil // the file was removed by hand
	}
	if p, err := s.Profile(ctx); err == nil {
		cv.ProfileFilled = p.Filled
	}
	return cv, true, nil
}

// UploadCV stores name/data as the base CV and writes its text into the
// profile's "About me" section. If that section already has the user's own
// text, it fails with ErrAboutMeFilled unless replace is set. Nothing is
// written when the file can't be read.
func (s *Service) UploadCV(ctx context.Context, name string, data []byte, replace bool) (BaseCV, error) {
	if s.profilePath == "" {
		return BaseCV{}, errors.New("no data dir configured")
	}
	if len(data) > cvfile.MaxBytes {
		return BaseCV{}, fmt.Errorf("%w: the CV is over %d MB", ErrInvalid, cvfile.MaxBytes>>20)
	}
	kind, err := cvfile.Detect(name, data)
	if err != nil {
		return BaseCV{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	text, err := cvfile.Text(ctx, kind, data)
	if err != nil {
		return BaseCV{}, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	if len([]rune(text)) > maxCVTextChars {
		return BaseCV{}, fmt.Errorf("%w: the CV has over %d characters of text", ErrInvalid, maxCVTextChars)
	}

	profileMu.Lock() // the profile's read-modify-write; SaveProfile takes it too
	defer profileMu.Unlock()
	p, err := s.Profile(ctx)
	if err != nil {
		return BaseCV{}, err
	}
	updated, hadText := withAboutMe(p.Text, text)
	if hadText && !replace {
		return BaseCV{}, ErrAboutMeFilled
	}

	dir := filepath.Join(filepath.Dir(s.profilePath), "cv")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return BaseCV{}, err
	}
	name = baseName(name)
	path := filepath.Join(dir, safeFileName(name, kind))
	prev, hadPrev, _ := s.BaseCV(ctx)
	if err := writePrivate(path, data); err != nil {
		return BaseCV{}, err
	}
	if err := writePrivate(s.profilePath, []byte(updated)); err != nil {
		return BaseCV{}, err
	}
	cv := BaseCV{Name: name, Kind: string(kind), Size: len(data), Path: path,
		UploadedAt: s.now().UTC().Format("2006-01-02T15:04:05Z")}
	b, _ := json.Marshal(cv)
	if err := s.st.SetSetting(ctx, baseCVKey, string(b)); err != nil {
		return BaseCV{}, err
	}
	// Only now that the new CV is recorded: the old file, and the PDF
	// printed from an old .md/.html CV.
	if hadPrev {
		for _, old := range []string{prev.Path, strings.TrimSuffix(prev.Path, filepath.Ext(prev.Path)) + ".pdf"} {
			if old != path {
				_ = os.Remove(old)
			}
		}
	}
	cv.ProfileFilled = profileFilled(updated)
	s.logEvent(ctx, "added", fmt.Sprintf("Uploaded your CV %q — its text is in your profile", cv.Name))
	return cv, nil
}

// baseCVFile is the base CV as a file job sites take (PDF or .docx): PDF and
// .docx as uploaded, Markdown and HTML printed to PDF. "" when there's none.
func (s *Service) baseCVFile(ctx context.Context) (string, error) {
	cv, ok, err := s.BaseCV(ctx)
	if err != nil || !ok {
		return "", err
	}
	if cv.Kind == string(cvfile.PDF) || cv.Kind == string(cvfile.DOCX) {
		return cv.Path, nil
	}
	raw, err := os.ReadFile(cv.Path)
	if err != nil {
		return "", err
	}
	page := string(raw)
	if cv.Kind == string(cvfile.Markdown) {
		page = markdown.Page(strings.TrimSuffix(cv.Name, filepath.Ext(cv.Name)), markdown.ToHTML(page))
	}
	pdf, err := renderCVPDF(ctx, page)
	if err != nil {
		return "", fmt.Errorf("couldn't make a PDF of your CV: %w", err)
	}
	out := strings.TrimSuffix(cv.Path, filepath.Ext(cv.Path)) + ".pdf"
	return out, writePrivate(out, pdf)
}

var (
	aboutMeRE  = regexp.MustCompile(`(?i)^#\s+about me\s*$`)
	h1RE       = regexp.MustCompile(`^#\s`)
	mdHeadRE   = regexp.MustCompile(`(?m)^(#{1,4})(\s)`)
	unsafeName = regexp.MustCompile(`[^\p{L}\p{N}._ ()\[\]-]+`)
)

// withAboutMe returns profile with its "About me" section's text replaced by
// cv (the section's guidance comments are kept), adding the section at the
// top if the user removed it. hadText reports whether the section held the
// user's own writing.
func withAboutMe(profile, cv string) (updated string, hadText bool) {
	// The profile's sections are "# " headings; the CV's own headings move
	// down two levels so they stay inside "About me".
	cv = mdHeadRE.ReplaceAllString(cv, "##$1$2")
	lines := strings.Split(profile, "\n")
	start := -1
	for i, l := range lines {
		if aboutMeRE.MatchString(strings.TrimSpace(l)) {
			start = i
			break
		}
	}
	if start < 0 {
		return "# About me\n\n" + cv + "\n\n" + profile, false
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if h1RE.MatchString(lines[i]) {
			end = i
			break
		}
	}
	body := strings.Join(lines[start+1:end], "\n")
	hadText = profileFilled(body)
	comments := strings.Join(htmlCommentRE.FindAllString(body, -1), "\n")
	section := []string{lines[start], ""}
	if comments != "" {
		section = append(section, comments, "")
	}
	section = append(section, cv, "", "")
	out := append(append(append([]string{}, lines[:start]...), section...), lines[end:]...)
	return strings.Join(out, "\n"), hadText
}

// baseName is the file name without the folders a browser or Windows may
// send along with it.
func baseName(name string) string {
	return filepath.Base(strings.ReplaceAll(name, `\`, "/"))
}

// safeFileName keeps the uploaded name readable (job sites show it to the
// employer) but strips odd characters, and gives it the kind's extension.
func safeFileName(base string, kind cvfile.Kind) string {
	stem := strings.TrimSpace(unsafeName.ReplaceAllString(strings.TrimSuffix(base, filepath.Ext(base)), "_"))
	stem = strings.Trim(stem, ". ")
	if stem == "" {
		stem = "cv"
	}
	if r := []rune(stem); len(r) > 80 {
		stem = string(r[:80])
	}
	return stem + "." + string(kind)
}

// writePrivate replaces path's content atomically, readable by the user only.
func writePrivate(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
