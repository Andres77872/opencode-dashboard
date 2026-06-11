package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvDBPath          = "OPENCODE_DASHBOARD_DB"
	EnvCacheDBPath     = "OPENCODE_DASHBOARD_CACHE_DB"
	EnvClaudeConfigDir = "CLAUDE_CONFIG_DIR"
	EnvCodexHome       = "OPENCODE_DASHBOARD_CODEX_HOME"

	AppName          = "opencode"
	DashboardAppName = "opencode-dashboard"

	SourceOpenCode   = "opencode"
	SourceClaudeCode = "claude_code"
	SourceCodex      = "codex"

	DefaultDBName       = "opencode.db"
	LatestChannelDBName = "opencode-latest.db"
	BetaChannelDBName   = "opencode-beta.db"
	StableChannelDBName = "opencode.db"
)

type Config struct {
	dbPath     string
	channel    string
	source     string
	claudeHome string
	codexHome  string
}

type PathSelection struct {
	Path   string
	Source string
}

type Option func(*Config)

func WithDBPath(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.dbPath = path
		}
	}
}

func WithChannel(channel string) Option {
	return func(c *Config) {
		if channel != "" {
			c.channel = channel
		}
	}
}

func WithSource(source string) Option {
	return func(c *Config) {
		if source != "" {
			c.source = strings.TrimSpace(source)
		}
	}
}

func WithClaudeHome(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.claudeHome = path
		}
	}
}

func WithCodexHome(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.codexHome = path
		}
	}
}

func New(opts ...Option) *Config {
	c := &Config{}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Config) DBPath() string {
	selection, _ := ResolveOpenCodeDB(c.dbPath, c.channel)
	return selection.Path
}

func (c *Config) DBPathSource() string {
	selection, _ := ResolveOpenCodeDB(c.dbPath, c.channel)
	return selection.Source
}

func (c *Config) Source() string {
	if c.source != "" {
		return c.source
	}
	return SourceOpenCode
}

func (c *Config) ClaudeHome() string {
	return ResolveClaudeHome(c.claudeHome).Path
}

func (c *Config) ClaudeHomeSource() string {
	return ResolveClaudeHome(c.claudeHome).Source
}

func (c *Config) CodexHome() string {
	return ResolveCodexHome(c.codexHome).Path
}

func (c *Config) CodexHomeSource() string {
	return ResolveCodexHome(c.codexHome).Source
}

func (c *Config) ConfigPath() string {
	return filepath.Join(XDGConfigHome(), AppName, AppName+".json")
}

func (c *Config) DataDir() string {
	return filepath.Join(XDGDataHome(), AppName)
}

func (c *Config) StateDir() string {
	return filepath.Join(XDGStateHome(), AppName)
}

func DefaultDBPath() string {
	return filepath.Join(XDGDataHome(), AppName, DefaultDBName)
}

func DefaultCacheDBPath() string {
	return filepath.Join(XDGDataHome(), DashboardAppName, "usage-cache.sqlite")
}

func ResolveCacheDB(flagDB string) PathSelection {
	if flagDB != "" {
		return PathSelection{Path: flagDB, Source: "--cache-db"}
	}
	if envPath := os.Getenv(EnvCacheDBPath); envPath != "" {
		return PathSelection{Path: envPath, Source: EnvCacheDBPath + " environment override"}
	}
	return PathSelection{Path: DefaultCacheDBPath(), Source: "default dashboard cache"}
}

func DefaultClaudeHomePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".claude")
}

func DefaultCodexHomePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".codex")
}

func ResolveOpenCodeDB(flagDB string, channel string) (PathSelection, error) {
	if flagDB != "" && channel != "" {
		return PathSelection{}, fmt.Errorf("use either --db or --channel, not both")
	}

	if flagDB != "" {
		return PathSelection{Path: flagDB, Source: "--db flag"}, nil
	}

	if channel != "" {
		return PathSelection{Path: ChannelDBPath(channel), Source: "channel " + channel}, nil
	}

	if envPath := os.Getenv(EnvDBPath); envPath != "" {
		return PathSelection{Path: envPath, Source: EnvDBPath + " environment override"}, nil
	}

	return PathSelection{Path: DetectChannelDB(""), Source: "auto-detected local OpenCode database"}, nil
}

func ResolveClaudeHome(flagHome string) PathSelection {
	if flagHome != "" {
		return PathSelection{Path: flagHome, Source: "--claude-home"}
	}

	if envHome := os.Getenv(EnvClaudeConfigDir); envHome != "" {
		return PathSelection{Path: envHome, Source: EnvClaudeConfigDir}
	}

	return PathSelection{Path: DefaultClaudeHomePath(), Source: "$HOME/.claude"}
}

func ResolveCodexHome(flagHome string) PathSelection {
	if flagHome != "" {
		return PathSelection{Path: flagHome, Source: "--codex-home"}
	}

	if envHome := os.Getenv(EnvCodexHome); envHome != "" {
		return PathSelection{Path: envHome, Source: EnvCodexHome}
	}

	return PathSelection{Path: DefaultCodexHomePath(), Source: "$HOME/.codex"}
}

func ChannelDBPath(channel string) string {
	if channel == "" || channel == "stable" {
		return filepath.Join(XDGDataHome(), AppName, StableChannelDBName)
	}

	safe := sanitizeChannel(channel)
	return filepath.Join(XDGDataHome(), AppName, "opencode-"+safe+".db")
}

func DetectChannelDB(channel string) string {
	if channel != "" {
		return ChannelDBPath(channel)
	}

	stablePath := ChannelDBPath("stable")
	if _, err := os.Stat(stablePath); err == nil {
		return stablePath
	}

	latestPath := ChannelDBPath("latest")
	if _, err := os.Stat(latestPath); err == nil {
		return latestPath
	}

	betaPath := ChannelDBPath("beta")
	if _, err := os.Stat(betaPath); err == nil {
		return betaPath
	}

	return stablePath
}

func XDGDataHome() string {
	if xdgData := os.Getenv("XDG_DATA_HOME"); xdgData != "" {
		return xdgData
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "share")
}

func XDGConfigHome() string {
	if xdgConfig := os.Getenv("XDG_CONFIG_HOME"); xdgConfig != "" {
		return xdgConfig
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".config")
}

func XDGStateHome() string {
	if xdgState := os.Getenv("XDG_STATE_HOME"); xdgState != "" {
		return xdgState
	}

	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".local", "state")
}

func sanitizeChannel(channel string) string {
	var sb strings.Builder
	for _, r := range channel {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	return sb.String()
}

func DBExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func ValidateDBPath(path string) error {
	if path == "" {
		return nil
	}

	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("database file not found: %s", path)
		}
		return fmt.Errorf("failed to access database path: %w", err)
	}

	if info.IsDir() {
		return fmt.Errorf("database path is a directory, expected file: %s", path)
	}

	return nil
}
