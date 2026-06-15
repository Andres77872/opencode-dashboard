// Package update provides version lookup, comparison, and installer execution
// for the `opencode-dashboard update` command. Network and process side effects
// are isolated here so the cmd layer only prints and orchestrates.
package update

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"opencode-dashboard/internal/version"
)

const (
	// Repo is the GitHub "owner/name" slug for releases and the installer.
	Repo = "Andres77872/opencode-dashboard"

	binaryName       = "opencode-dashboard"
	installScriptURL = "https://raw.githubusercontent.com/" + Repo + "/master/scripts/install.sh"
	latestReleaseURL = "https://api.github.com/repos/" + Repo + "/releases/latest"
)

type releaseResponse struct {
	TagName string `json:"tag_name"`
}

// LatestRelease returns the tag_name of the newest GitHub release (e.g. "v0.1.20").
func LatestRelease(ctx context.Context, client *http.Client) (string, error) {
	return latestReleaseFrom(ctx, client, latestReleaseURL)
}

func latestReleaseFrom(ctx context.Context, client *http.Client, url string) (string, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent())
	req.Header.Set("Accept", "application/vnd.github+json")
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("contact github: %w", err)
	}
	defer resp.Body.Close()

	switch resp.StatusCode {
	case http.StatusOK:
	case http.StatusForbidden, http.StatusTooManyRequests:
		if resp.Header.Get("X-RateLimit-Remaining") == "0" {
			return "", fmt.Errorf("github api rate limit exceeded; set GITHUB_TOKEN or retry later")
		}
		return "", fmt.Errorf("github api refused the request (status %d); set GITHUB_TOKEN or retry later", resp.StatusCode)
	case http.StatusNotFound:
		return "", fmt.Errorf("no releases found for %s", Repo)
	default:
		return "", fmt.Errorf("github api returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}
	var rel releaseResponse
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", fmt.Errorf("parse release json: %w", err)
	}
	if strings.TrimSpace(rel.TagName) == "" {
		return "", fmt.Errorf("release response had no tag_name")
	}
	return rel.TagName, nil
}

// NormalizeTag ensures a version is in "vX.Y.Z" form (mirrors install.sh).
func NormalizeTag(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return v
	}
	return "v" + strings.TrimLeft(v, "vV")
}

// CompareVersions returns (sign, ok): sign is -1/0/1 for a<b / a==b / a>b.
// ok is false when either side is non-numeric (e.g. "dev"), signaling the
// caller to treat the versions as incomparable. Missing trailing fields are
// zero, so "v0.1" == "v0.1.0" and "v0.1.0" < "v0.1.0.1".
func CompareVersions(a, b string) (int, bool) {
	av, aok := parseVersion(a)
	bv, bok := parseVersion(b)
	if !aok || !bok {
		return 0, false
	}
	n := len(av)
	if len(bv) > n {
		n = len(bv)
	}
	for i := 0; i < n; i++ {
		var x, y int
		if i < len(av) {
			x = av[i]
		}
		if i < len(bv) {
			y = bv[i]
		}
		if x != y {
			if x < y {
				return -1, true
			}
			return 1, true
		}
	}
	return 0, true
}

func parseVersion(v string) ([]int, bool) {
	v = strings.TrimSpace(v)
	v = strings.TrimLeft(v, "vV")
	if v == "" {
		return nil, false
	}
	fields := strings.Split(v, ".")
	out := make([]int, 0, len(fields))
	for _, f := range fields {
		n, err := strconv.Atoi(f)
		if err != nil {
			return nil, false
		}
		out = append(out, n)
	}
	return out, true
}

// DefaultInstallPath is where the installer writes: ~/.local/bin/opencode-dashboard.
func DefaultInstallPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home: %w", err)
	}
	return filepath.Join(home, ".local", "bin", binaryName), nil
}

// ResolveExecutable resolves symlinks, falling back to the cleaned path.
func ResolveExecutable(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(resolved)
	}
	return filepath.Clean(path)
}

// RunOptions configures RunInstaller.
type RunOptions struct {
	Version    string // release tag to install; empty => installer resolves latest
	NoChecksum bool   // sets NO_CHECKSUM=1
	Stdout     io.Writer
	Stderr     io.Writer
}

// RunInstaller fetches the official install script over HTTPS, validates it
// looks like a shell script, and pipes it to bash with config passed via env.
// Output streams live to opts.Stdout/Stderr.
//
// Security: this runs a remote script. Acceptable because it is this project's
// own documented installer fetched over HTTPS from the canonical repo. We
// fetch-then-validate so transport/non-script errors surface as Go errors
// instead of being fed to bash blind.
func RunInstaller(ctx context.Context, client *http.Client, opts RunOptions) error {
	if opts.Stdout == nil || opts.Stderr == nil {
		return fmt.Errorf("stdout and stderr writers are required")
	}
	if client == nil {
		client = http.DefaultClient
	}
	bashPath, err := exec.LookPath("bash")
	if err != nil {
		return fmt.Errorf("bash is required to run the installer but was not found in PATH")
	}
	script, err := fetchScript(ctx, client, installScriptURL)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bashPath, "-s", "--")
	cmd.Stdin = bytes.NewReader(script)
	cmd.Stdout = opts.Stdout
	cmd.Stderr = opts.Stderr
	env := append(os.Environ(), "REPO="+Repo)
	if opts.Version != "" {
		env = append(env, "VERSION="+opts.Version)
	}
	if opts.NoChecksum {
		env = append(env, "NO_CHECKSUM=1")
	}
	cmd.Env = env
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("installer exited: %w", err)
	}
	return nil
}

func fetchScript(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build script request: %w", err)
	}
	req.Header.Set("User-Agent", version.UserAgent())
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download install script: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("download install script: status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read install script: %w", err)
	}
	if !bytes.HasPrefix(bytes.TrimSpace(body), []byte("#!")) {
		return nil, fmt.Errorf("downloaded install script does not look like a shell script")
	}
	return body, nil
}
