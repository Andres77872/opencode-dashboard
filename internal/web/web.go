// Package web provides the HTTP server and API endpoints for the
// web dashboard interface.
//
// Server features:
// - Local-only binding (127.0.0.1)
// - REST API at /api/v1/*
// - Embedded static assets via go:embed
// - Browser auto-open utility
//
// API endpoints (Phase 3):
// - GET /api/v1/overview - aggregate metrics
// - GET /api/v1/daily?period=7d - time-series data
// - GET /api/v1/models - model comparison
// - GET /api/v1/tools - tool usage stats
// - GET /api/v1/projects - project aggregation
// - GET /api/v1/sessions - paginated session list
// - GET /api/v1/config - OpenCode configuration
//
// Implementation will be completed in Phase 3.
package web

// HasAssets reports whether production web assets were embedded into the binary.
// Without embedded assets the server still exposes the API and serves a placeholder page.
func HasAssets() bool {
	return hasAssets
}
