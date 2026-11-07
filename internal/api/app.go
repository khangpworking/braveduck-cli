package api

import (
	"embed"
	"io/fs"
	"net/http"
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
// page. /app/, where it was tried out next to the old board, now leads back
// here.
func mountApp(mux *http.ServeMux, token string) {
	sub, _ := fs.Sub(appFS, "app")
	index := renderIndex(sub, token)
	mux.HandleFunc("GET /{$}", index)
	mux.HandleFunc("GET /index.html", index)
	mux.Handle("GET /", http.FileServer(http.FS(sub)))
	mux.HandleFunc("GET /app/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, BoardPath, http.StatusMovedPermanently)
	})
}
