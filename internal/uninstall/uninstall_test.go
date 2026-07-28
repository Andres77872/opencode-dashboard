package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// This package deletes user data with os.RemoveAll, so every test here points
// HOME and the XDG roots at a temporary directory. Nothing outside t.TempDir()
// is ever a candidate for removal.
func sandbox(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	t.Setenv("HOME", root)
	t.Setenv("XDG_DATA_HOME", filepath.Join(root, "data"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(root, "state"))
	return root
}

func mkdir(t *testing.T, path string) string {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", path, err)
	}
	return path
}

func writeFile(t *testing.T, path, content string) string {
	t.Helper()
	mkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
	return path
}

func targetByKind(t *testing.T, plan *RemovalPlan, kind string) Target {
	t.Helper()
	for _, target := range plan.Targets {
		if target.Kind == kind {
			return target
		}
	}
	t.Fatalf("plan has no %q target: %#v", kind, plan.Targets)
	return Target{}
}

func TestPlanMarksExistingDirectoriesForRemoval(t *testing.T) {
	root := sandbox(t)
	data := mkdir(t, filepath.Join(root, "data", appName))
	config := mkdir(t, filepath.Join(root, "config", appName))
	state := mkdir(t, filepath.Join(root, "state", appName))

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if !plan.HasRemovals() {
		t.Fatal("HasRemovals() = false, want true when app directories exist")
	}
	for kind, want := range map[string]string{
		"data directory":   data,
		"config directory": config,
		"state directory":  state,
	} {
		target := targetByKind(t, plan, kind)
		if target.Path != want {
			t.Errorf("%s path = %q, want %q", kind, target.Path, want)
		}
		if !target.Exists || !target.Remove {
			t.Errorf("%s = %#v, want exists and removable", kind, target)
		}
	}
}

func TestPlanSkipsMissingPathsAndReportsWhy(t *testing.T) {
	sandbox(t)

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if plan.HasRemovals() {
		t.Fatalf("HasRemovals() = true with nothing installed: %#v", plan.Targets)
	}
	for _, target := range plan.Targets {
		if target.Exists || target.Remove {
			t.Errorf("%s = %#v, want neither existing nor removable", target.Kind, target)
		}
		if target.Reason == "" {
			t.Errorf("%s gives no reason for being skipped", target.Kind)
		}
	}
}

// The running binary must survive an uninstall: deleting it mid-process is what
// the plan's own note promises not to do.
func TestPlanNeverRemovesTheRunningBinary(t *testing.T) {
	root := sandbox(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	binary := filepath.Join(root, ".local", "bin", binaryName)
	mkdir(t, filepath.Dir(binary))
	if err := os.Symlink(self, binary); err != nil {
		t.Skipf("symlink unsupported here: %v", err)
	}

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	target := targetByKind(t, plan, "binary")
	if !target.Exists {
		t.Fatalf("binary target = %#v, want it to be seen", target)
	}
	if target.Remove {
		t.Fatal("plan would delete the binary the process is running from")
	}
	if !strings.Contains(target.Reason, "currently running") {
		t.Errorf("reason = %q, want it to name the running binary", target.Reason)
	}

	if _, err := Execute(plan); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if _, err := os.Lstat(binary); err != nil {
		t.Fatalf("running binary was removed: %v", err)
	}
}

func TestPlanRefusesPathsOfTheWrongType(t *testing.T) {
	root := sandbox(t)
	// A directory where the binary is expected, and a file where a directory is.
	mkdir(t, filepath.Join(root, ".local", "bin", binaryName))
	writeFile(t, filepath.Join(root, "data", appName), "not a directory")

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	binary := targetByKind(t, plan, "binary")
	if binary.Remove || !strings.Contains(binary.Reason, "found a directory") {
		t.Errorf("binary target = %#v, want refusal naming the directory", binary)
	}
	data := targetByKind(t, plan, "data directory")
	if data.Remove || !strings.Contains(data.Reason, "expected a directory") {
		t.Errorf("data target = %#v, want refusal naming the type mismatch", data)
	}

	if _, err := Execute(plan); err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	for _, path := range []string{
		filepath.Join(root, ".local", "bin", binaryName),
		filepath.Join(root, "data", appName),
	} {
		if _, err := os.Lstat(path); err != nil {
			t.Errorf("Execute removed a refused target %s: %v", path, err)
		}
	}
}

func TestExecuteRemovesOnlyApprovedTargets(t *testing.T) {
	root := sandbox(t)
	installed := writeFile(t, filepath.Join(root, ".local", "bin", binaryName), "#!/bin/sh\n")
	data := mkdir(t, filepath.Join(root, "data", appName))
	writeFile(t, filepath.Join(data, "usage-cache.sqlite"), "cache")
	// Neighbouring OpenCode-owned state the plan must never target.
	keep := writeFile(t, filepath.Join(root, "data", "opencode", "opencode.db"), "opencode")

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	result, err := Execute(plan)
	if err != nil {
		t.Fatalf("Execute(): %v", err)
	}
	if len(result.Removed) != 2 {
		t.Fatalf("removed %d targets, want the binary and the data directory: %#v", len(result.Removed), result.Removed)
	}
	for _, path := range []string{installed, data} {
		if _, err := os.Lstat(path); !os.IsNotExist(err) {
			t.Errorf("%s still present after Execute: %v", path, err)
		}
	}
	if _, err := os.Lstat(keep); err != nil {
		t.Errorf("Execute removed OpenCode-owned data at %s: %v", keep, err)
	}
	// Config and state were never created, so they are reported as skipped
	// rather than silently dropped from the result.
	if len(result.Skipped) != 2 {
		t.Errorf("skipped %d targets, want the two absent directories: %#v", len(result.Skipped), result.Skipped)
	}
}

// A target that cannot be removed must be reported as skipped with the reason,
// never counted as removed, and must not stop the remaining targets.
func TestExecuteReportsFailuresWithoutAbandoningOtherTargets(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	if runtime.GOOS == "windows" {
		t.Skip("POSIX permission semantics required")
	}
	root := sandbox(t)
	data := mkdir(t, filepath.Join(root, "data", appName))
	writeFile(t, filepath.Join(data, "locked", "file"), "x")
	config := mkdir(t, filepath.Join(root, "config", appName))

	// Make the data directory's child undeletable by clearing write on its parent.
	if err := os.Chmod(data, 0o555); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(data, 0o755) })

	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	result, err := Execute(plan)
	if err == nil {
		t.Fatal("Execute() error = nil, want the removal failure reported")
	}
	if !strings.Contains(err.Error(), "data directory") {
		t.Errorf("error = %v, want it to name the failing target", err)
	}
	for _, target := range result.Removed {
		if target.Kind == "data directory" {
			t.Error("a target that failed to be removed is reported as removed")
		}
	}
	var sawFailure bool
	for _, target := range result.Skipped {
		if target.Kind == "data directory" && target.Reason != "" {
			sawFailure = true
		}
	}
	if !sawFailure {
		t.Errorf("skipped targets do not explain the failure: %#v", result.Skipped)
	}
	// The config directory came after the failure and must still be gone.
	if _, err := os.Lstat(config); !os.IsNotExist(err) {
		t.Errorf("later target %s was abandoned after an earlier failure: %v", config, err)
	}
}

func TestExecuteRejectsNilPlanAndHasRemovalsIsNilSafe(t *testing.T) {
	if _, err := Execute(nil); err == nil {
		t.Error("Execute(nil) error = nil, want a rejection")
	}
	var plan *RemovalPlan
	if plan.HasRemovals() {
		t.Error("(*RemovalPlan)(nil).HasRemovals() = true, want false")
	}
	if (&RemovalPlan{}).HasRemovals() {
		t.Error("empty plan reports removals")
	}
}

func TestPlanDocumentsWhatItLeavesAlone(t *testing.T) {
	sandbox(t)
	plan, err := Plan()
	if err != nil {
		t.Fatalf("Plan(): %v", err)
	}
	if len(plan.Notes) == 0 {
		t.Fatal("plan carries no notes explaining what is preserved")
	}
	joined := strings.Join(plan.Notes, "\n")
	for _, want := range []string{"opencode", "running binary"} {
		if !strings.Contains(joined, want) {
			t.Errorf("notes do not mention %q: %q", want, joined)
		}
	}
}
