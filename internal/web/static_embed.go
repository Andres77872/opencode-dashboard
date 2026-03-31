//go:build embedassets

package web

// Static asset embedding strategy:
//
// Build with: go build -tags embedassets ./cmd/dashboard
// Without tag: uses static_fallback.go (placeholder HTML, 503)
//
// IMPORTANT: The go:embed directive is relative to THIS FILE's directory.
// The embedded path "dist" expects assets at internal/web/dist/, NOT web/dist/.
// Build process (Phase 10) must either:
//   - Configure Vite to output to internal/web/dist/, OR
//   - Copy web/dist/ to internal/web/dist/ before Go build
import (
	"embed"
	"io/fs"
	"net/http"
	"path/filepath"
	"strings"
)

//go:embed all:dist
var embeddedFS embed.FS

const hasAssets = true

func (s *Server) RegisterStaticRoutes() {
	sub, err := fs.Sub(embeddedFS, "dist")
	if err != nil {
		s.mux.HandleFunc("/", placeholderHandler)
		return
	}

	fileServer := http.FileServer(http.FS(sub))
	s.mux.Handle("/", spaHandler{fileServer})
}

type spaHandler struct {
	fileServer http.Handler
}

func (h spaHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/") || r.URL.Path == "/health" {
		http.NotFound(w, r)
		return
	}

	ext := filepath.Ext(r.URL.Path)
	if ext != "" && ext != ".html" {
		h.fileServer.ServeHTTP(w, r)
		return
	}

	r.URL.Path = "/"
	h.fileServer.ServeHTTP(w, r)
}
