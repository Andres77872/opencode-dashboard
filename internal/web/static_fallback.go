//go:build !embedassets

package web

const hasAssets = false

func (s *Server) RegisterStaticRoutes() {
	s.mux.HandleFunc("/", placeholderHandler)
}
