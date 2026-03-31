// Package version provides build version information.
//
// Version is injected via ldflags during build:
//
//	go build -ldflags "-X 'opencode-dashboard/internal/version.Version=v1.0.0' -X 'opencode-dashboard/internal/version.GitCommit=abc123'"
//
// This package provides a centralized location for version-related
// utilities that can be imported by other packages.
package version

// Version holds the semantic version string.
// Set via ldflags: -X 'opencode-dashboard/internal/version.Version=v1.0.0'
var Version = "dev"

// GitCommit holds the git commit hash.
// Set via ldflags: -X 'opencode-dashboard/internal/version.GitCommit=abc123'
var GitCommit = "unknown"

// BuildInfo returns formatted version information with commit.
// Format: "v1.0.0 (abc123)" or "dev (abc123)" for development builds.
func BuildInfo() string {
	return Version + " (" + ShortCommit() + ")"
}

// ShortCommit returns the first 7 characters of the git commit hash.
// Returns "unknown" if the commit is not set.
func ShortCommit() string {
	if GitCommit == "unknown" || len(GitCommit) < 7 {
		return GitCommit
	}
	return GitCommit[:7]
}

// UserAgent returns a user agent string suitable for HTTP requests.
// Format: "opencode-dashboard/v1.0.0"
func UserAgent() string {
	return "opencode-dashboard/" + Version
}
