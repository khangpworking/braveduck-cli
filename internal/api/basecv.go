package api

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"

	"github.com/khangpworking/braveduck-cli/internal/core/cvfile"
	"github.com/khangpworking/braveduck-cli/internal/service"
)

// The base CV ("📄 My CV"): upload once, its text fills the profile.

type baseCVOut struct {
	CV      *service.BaseCV `json:"cv"` // null before the first upload
	Profile struct {
		Path   string `json:"path"`
		Filled bool   `json:"filled"`
	} `json:"profile"`
}

func (s *server) getBaseCV(w http.ResponseWriter, r *http.Request) {
	var out baseCVOut
	cv, ok, err := s.svc.BaseCV(r.Context())
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	if ok {
		out.CV = &cv
	}
	p, err := s.svc.Profile(r.Context())
	out.Profile.Path, out.Profile.Filled = p.Path, p.Filled
	reply(w, http.StatusOK, out, err)
}

// uploadBaseCV takes a multipart form: "file", and "replace"=true to
// overwrite an "About me" the user already wrote (else 409).
func (s *server) uploadBaseCV(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, cvfile.MaxBytes+1<<20)
	f, hdr, err := r.FormFile("file")
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			err = fmt.Errorf("the CV is over %d MB", cvfile.MaxBytes>>20)
		} else {
			err = errors.New(`send the CV as the multipart field "file"`)
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, cvfile.MaxBytes+1))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	cv, err := s.svc.UploadCV(r.Context(), hdr.Filename, data, r.FormValue("replace") == "true")
	if errors.Is(err, service.ErrAboutMeFilled) {
		writeErr(w, http.StatusConflict, err)
		return
	}
	reply(w, http.StatusCreated, cv, err)
}

// baseCVFile downloads the uploaded CV as it was uploaded.
func (s *server) baseCVFile(w http.ResponseWriter, r *http.Request) {
	cv, ok, err := s.svc.BaseCV(r.Context())
	if err != nil || !ok {
		if err == nil {
			err = fmt.Errorf("%w: no CV uploaded yet", service.ErrNotFound)
		}
		reply(w, 0, nil, err)
		return
	}
	data, err := os.ReadFile(cv.Path)
	if err != nil {
		reply(w, 0, nil, err)
		return
	}
	// Always a download, never rendered: an uploaded HTML CV must not run
	// as a page on the board's origin.
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": cv.Name}))
	_, _ = w.Write(data)
}
