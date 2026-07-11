package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/quota"
)

// cmdClaudeStatusline is invoked by Claude Code as its statusline command: it
// receives the documented statusline JSON on stdin, persists the rate-limit
// snapshot for the dashboard, and prints the one-line status to display.
func cmdClaudeStatusline(args []string) error {
	fs := flag.NewFlagSet("claude-statusline", flag.ContinueOnError)
	file := fs.String("file", config.DefaultClaudeRateLimitsPath(), "rate-limit snapshot output path")
	install := fs.Bool("install", false, "write the statusLine entry into Claude Code's settings.json and exit")
	force := fs.Bool("force", false, "with --install, replace an existing statusLine configuration")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *install {
		return installClaudeStatusline(os.Stdout, *force)
	}
	return runClaudeStatusline(os.Stdin, os.Stdout, os.Stderr, *file)
}

// runClaudeStatusline never returns an error for bad input: a failing
// statusline command would degrade Claude Code's UI, so problems go to stderr
// and a plain fallback line is still printed.
func runClaudeStatusline(in io.Reader, out, errOut io.Writer, snapshotPath string) error {
	input, err := quota.ParseStatuslineInput(in)
	if err != nil {
		fmt.Fprintf(errOut, "claude-statusline: %v\n", err)
		fmt.Fprintln(out, "Claude Code")
		return nil
	}
	if snap, ok := input.Snapshot(time.Now()); ok {
		if err := quota.WriteClaudeSnapshot(snapshotPath, snap); err != nil {
			fmt.Fprintf(errOut, "claude-statusline: persist snapshot: %v\n", err)
		}
	}
	fmt.Fprintln(out, quota.StatuslineText(input))
	return nil
}

func installClaudeStatusline(out io.Writer, force bool) error {
	claudeHome := config.ResolveClaudeHome("")
	settingsPath := filepath.Join(claudeHome.Path, "settings.json")
	return installClaudeStatuslineAt(out, settingsPath, statuslineCommand(), force)
}

func installClaudeStatuslineAt(out io.Writer, settingsPath, command string, force bool) error {
	settings := map[string]any{}
	body, err := os.ReadFile(settingsPath)
	switch {
	case err == nil:
		if err := json.Unmarshal(body, &settings); err != nil {
			return fmt.Errorf("parse %s: %w", settingsPath, err)
		}
	case os.IsNotExist(err):
		// First-run: create the file with just the statusLine entry.
	default:
		return fmt.Errorf("read %s: %w", settingsPath, err)
	}

	entry := map[string]any{"type": "command", "command": command}
	if existing, ok := settings["statusLine"]; ok {
		if sameStatusline(existing, command) {
			fmt.Fprintf(out, "statusLine already configured in %s\n", settingsPath)
			return nil
		}
		if !force {
			return fmt.Errorf("%s already has a statusLine configured; re-run with --force to replace it", settingsPath)
		}
	}
	settings["statusLine"] = entry

	encoded, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(settingsPath), 0o755); err != nil {
		return fmt.Errorf("create settings dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(settingsPath), ".settings-*.tmp")
	if err != nil {
		return fmt.Errorf("create settings temp file: %w", err)
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(append(encoded, '\n')); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write settings: %w", err)
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("chmod settings: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close settings: %w", err)
	}
	if err := os.Rename(tmpPath, settingsPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("replace settings: %w", err)
	}

	fmt.Fprintf(out, "Configured statusLine in %s\n", settingsPath)
	fmt.Fprintf(out, "  command: %s\n", command)
	fmt.Fprintln(out, "Quota appears in the dashboard after the next Claude Code response (Pro/Max plans).")
	return nil
}

func sameStatusline(existing any, command string) bool {
	entry, ok := existing.(map[string]any)
	if !ok {
		return false
	}
	current, _ := entry["command"].(string)
	return current == command
}

// claudeStatuslineInstalled reports whether <claudeHome>/settings.json already
// routes the statusline through this binary (bare name or absolute path).
// Missing or malformed settings count as not installed.
func claudeStatuslineInstalled(claudeHome string) bool {
	body, err := os.ReadFile(filepath.Join(claudeHome, "settings.json"))
	if err != nil {
		return false
	}
	var settings struct {
		StatusLine struct {
			Command string `json:"command"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(body, &settings); err != nil {
		return false
	}
	return strings.Contains(settings.StatusLine.Command, "claude-statusline")
}

// statuslineCommand prefers the bare binary name when it resolves on PATH so
// settings.json survives reinstalls; otherwise it pins the absolute path.
func statuslineCommand() string {
	const bare = "opencode-dashboard claude-statusline"
	if _, err := exec.LookPath("opencode-dashboard"); err == nil {
		return bare
	}
	if exe, err := os.Executable(); err == nil {
		return exe + " claude-statusline"
	}
	return bare
}
