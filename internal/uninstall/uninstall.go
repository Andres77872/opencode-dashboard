// Package uninstall provides safe self-removal planning for opencode-dashboard.
package uninstall

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"opencode-dashboard/internal/config"
)

const (
	appName    = "opencode-dashboard"
	binaryName = "opencode-dashboard"
)

type Target struct {
	Kind   string
	Path   string
	Exists bool
	Remove bool
	Reason string
}

type RemovalPlan struct {
	Targets []Target
	Notes   []string
}

type Result struct {
	Removed []Target
	Skipped []Target
}

func Plan() (*RemovalPlan, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("resolve user home: %w", err)
	}

	binaryPath := filepath.Join(homeDir, ".local", "bin", binaryName)
	currentExec, _ := currentExecutable()

	plan := &RemovalPlan{
		Targets: []Target{
			planBinaryTarget(binaryPath, currentExec),
			planDirTarget("data directory", filepath.Join(config.XDGDataHome(), appName)),
			planDirTarget("config directory", filepath.Join(config.XDGConfigHome(), appName)),
			planDirTarget("state directory", filepath.Join(config.XDGStateHome(), appName)),
		},
		Notes: []string{
			"OpenCode-owned files under ~/.local/share/opencode/, ~/.config/opencode/, and related channel databases are never removed.",
			"The currently running binary is not deleted in-process; remove it manually after this command exits if needed.",
		},
	}

	return plan, nil
}

func Execute(plan *RemovalPlan) (Result, error) {
	if plan == nil {
		return Result{}, fmt.Errorf("removal plan is required")
	}

	var (
		result Result
		errs   []error
	)

	for _, target := range plan.Targets {
		if !target.Exists || !target.Remove {
			result.Skipped = append(result.Skipped, target)
			continue
		}

		var err error
		switch target.Kind {
		case "binary":
			err = os.Remove(target.Path)
		default:
			err = os.RemoveAll(target.Path)
		}

		if err != nil {
			errs = append(errs, fmt.Errorf("remove %s %q: %w", target.Kind, target.Path, err))
			result.Skipped = append(result.Skipped, Target{
				Kind:   target.Kind,
				Path:   target.Path,
				Exists: target.Exists,
				Remove: false,
				Reason: err.Error(),
			})
			continue
		}

		result.Removed = append(result.Removed, target)
	}

	if len(errs) > 0 {
		return result, errors.Join(errs...)
	}

	return result, nil
}

func (p *RemovalPlan) HasRemovals() bool {
	if p == nil {
		return false
	}

	for _, target := range p.Targets {
		if target.Exists && target.Remove {
			return true
		}
	}

	return false
}

func currentExecutable() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return filepath.Clean(resolved), nil
	}

	return filepath.Clean(path), nil
}

func planBinaryTarget(path string, currentExec string) Target {
	target := Target{
		Kind: "binary",
		Path: filepath.Clean(path),
	}

	info, err := os.Lstat(target.Path)
	if err != nil {
		if os.IsNotExist(err) {
			target.Reason = "not installed at the default local path"
			return target
		}

		target.Reason = err.Error()
		return target
	}

	target.Exists = true
	if info.IsDir() {
		target.Reason = "expected a file, found a directory"
		return target
	}

	resolvedTarget := target.Path
	if eval, err := filepath.EvalSymlinks(target.Path); err == nil {
		resolvedTarget = filepath.Clean(eval)
	}

	if samePath(target.Path, currentExec) || samePath(resolvedTarget, currentExec) {
		target.Reason = "currently running binary"
		return target
	}

	target.Remove = true
	return target
}

func planDirTarget(kind string, path string) Target {
	target := Target{
		Kind: kind,
		Path: filepath.Clean(path),
	}

	info, err := os.Stat(target.Path)
	if err != nil {
		if os.IsNotExist(err) {
			target.Reason = "path does not exist"
			return target
		}

		target.Reason = err.Error()
		return target
	}

	target.Exists = true
	if !info.IsDir() {
		target.Reason = "expected a directory"
		return target
	}

	target.Remove = true
	return target
}

func samePath(a string, b string) bool {
	if a == "" || b == "" {
		return false
	}

	return filepath.Clean(a) == filepath.Clean(b)
}
