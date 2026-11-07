package api

import (
	"embed"
	"io/fs"
	"net/http"
)

// The new board: a React app whose source is in web/ at the repo root. Its
// build (npm run build there) is committed here, so building braveduck needs
// no Node. CI rebuilds it and fails if the committed copy is stale.
//
//go:embed app
var appFS embed.FS

// AppPath is where the new board is served while it replaces the old one.
const AppPath = "/app/"

// mountApp serves the new board under AppPath, with the token filled into its
// page the same way as the old board's.
func mountApp(mux *http.ServeMux, token string) {
	sub, _ := fs.Sub(appFS, "app")
	index := renderIndex(sub, token)
	mux.HandleFunc("GET "+AppPath+"{$}", index)
	mux.HandleFunc("GET "+AppPath+"index.html", index)
	mux.Handle("GET "+AppPath, http.StripPrefix(AppPath, http.FileServer(http.FS(sub))))
}
