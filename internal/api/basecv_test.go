package api

import (
	"bytes"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"testing"
)

func uploadCV(t *testing.T, url, name, content string, replace bool) *http.Response {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write([]byte(content))
	if replace {
		mw.WriteField("replace", "true")
	}
	mw.Close()
	req, _ := http.NewRequest(http.MethodPost, url+"/api/cv", &body)
	req.Header.Set("Authorization", "Bearer "+testToken)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestBaseCVUploadAndDownload(t *testing.T) {
	ts, _ := newTestServer(t)
	var got BaseCVReply
	json.NewDecoder(do(t, http.MethodGet, ts.URL+"/api/cv", "").Body).Decode(&got)
	if got.CV != nil || got.Profile.Filled {
		t.Fatalf("before upload = %+v", got)
	}

	cv := `<html><body><h1>Nguyễn Văn Khang</h1><script>alert(1)</script><p>Marketing AI, 3 năm kinh nghiệm với n8n và Make.</p></body></html>`
	if resp := uploadCV(t, ts.URL, "My CV.html", cv, false); resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("upload = %d %s", resp.StatusCode, b)
	}
	json.NewDecoder(do(t, http.MethodGet, ts.URL+"/api/cv", "").Body).Decode(&got)
	if got.CV == nil || got.CV.Name != "My CV.html" || !got.Profile.Filled {
		t.Fatalf("after upload = %+v", got)
	}

	// Uploading again asks first; replace=true goes ahead.
	if resp := uploadCV(t, ts.URL, "cv.md", "# Khang\n\nData analyst, five years of SQL and Python at Duck Co.", false); resp.StatusCode != http.StatusConflict {
		t.Fatalf("second upload = %d, want 409", resp.StatusCode)
	}
	if resp := uploadCV(t, ts.URL, "cv.md", "# Khang\n\nData analyst, five years of SQL and Python at Duck Co.", true); resp.StatusCode != http.StatusCreated {
		t.Fatalf("replace = %d", resp.StatusCode)
	}

	resp := do(t, http.MethodGet, ts.URL+"/api/cv/file", "")
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK || !strings.HasPrefix(string(b), "# Khang") ||
		resp.Header.Get("Content-Type") != "application/octet-stream" || !strings.HasPrefix(resp.Header.Get("Content-Disposition"), "attachment") {
		t.Fatalf("download = %d %q %v", resp.StatusCode, b, resp.Header)
	}
}

func TestBaseCVUploadRejects(t *testing.T) {
	ts, _ := newTestServer(t)
	if resp := uploadCV(t, ts.URL, "cv.exe", "MZ not a cv", false); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unsupported = %d, want 400", resp.StatusCode)
	}
	if resp := uploadCV(t, ts.URL, "cv.pdf", "%PDF-"+strings.Repeat("x", 7<<20), false); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("too big = %d, want 400", resp.StatusCode)
	}
	if resp := do(t, http.MethodPost, ts.URL+"/api/cv", `{"file":"x"}`); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("not multipart = %d, want 400", resp.StatusCode)
	}
	if resp := do(t, http.MethodGet, ts.URL+"/api/cv/file", ""); resp.StatusCode != http.StatusNotFound {
		t.Fatalf("download before upload = %d, want 404", resp.StatusCode)
	}
	// The token guards it like every other API call.
	resp, _ := http.Post(ts.URL+"/api/cv", "multipart/form-data", strings.NewReader(""))
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", resp.StatusCode)
	}
}
