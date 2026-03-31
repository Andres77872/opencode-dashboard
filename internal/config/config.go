package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	EnvDBPath = "OPENCODE_DASHBOARD_DB"

	AppName = "opencode"

	DefaultDBName       = "opencode.db"
	LatestChannelDBName = "opencode-latest.db"
	BetaChannelDBName   = "opencode-beta.db"
	StableChannelDBName = "opencode.db"
)

type Config struct {
	dbPath string
}

type Option func(*Config)

func WithDBPath(path string) Option {
	return func(c *Config) {
		if path != "" {
			c.dbPath = path
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
	if c.dbPath != "" {
		return c.dbPath
	}

	if path, ok := os.LookupEnv(EnvDBPath); ok {
		return path
	}

	return DefaultDBPath()
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
