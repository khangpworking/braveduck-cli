package api

import (
	"bytes"
	"embed"
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// The board: a React app whose source is in web/ at the repo root. Its build
// (npm run build there) is committed here, so building braveduck needs no
// Node. CI rebuilds it and fails if the committed copy is stale.
//
//go:embed app
var appFS embed.FS

// BoardPath is where the board is served.
const BoardPath = "/"

// mountApp serves the board at BoardPath, with the token filled into its
// page, the landing page at /welcome, and a 404 page for any other address.
// /app/, where the board was tried out next to the old one, leads back here.
func mountApp(mux *http.ServeMux, token string) {
	sub, _ := fs.Sub(appFS, "app")
	index := renderIndex(sub, token)
	welcome := renderPage(sub, "welcome.html", http.StatusOK)
	notFound := renderPage(sub, "404.html", http.StatusNotFound)
	files := http.FileServer(http.FS(sub))
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET /index.html", index)
	mux.HandleFunc("GET /welcome", welcome)
	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			writeErr(w, http.StatusNotFound, errors.New("no such API endpoint"))
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if st, err := fs.Stat(sub, name); err != nil || st.IsDir() {
			notFound(w, r)
			return
		}
		// Built files carry a hash of their content in their name, so a
		// browser (and Cloudflare) can keep them for good: a new build
		// has new names.
		if strings.HasPrefix(name, "assets/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /app/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, BoardPath, http.StatusMovedPermanently)
	})
}

// renderPage serves one of the build's pages that needs no token (the
// landing page, the 404 page) in the board's language, with status code.
func renderPage(web fs.FS, name string, code int) http.HandlerFunc {
	raw, err := fs.ReadFile(web, name)
	if err != nil {
		panic("api: the board's build has no " + name)
	}
	page := bytes.Replace(raw, []byte(`<html lang="vi">`), []byte(`<html lang="`+BoardLang()+`">`), 1)
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(code)
		_, _ = w.Write(page)
	}
}
