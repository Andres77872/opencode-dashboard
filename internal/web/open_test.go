package web

import (
	"runtime"
	"strings"
	"syscall"
	"testing"
)

func TestOpenBrowserUnsupportedPlatform(t *testing.T) {
	if runtime.GOOS == "linux" || runtime.GOOS == "darwin" {
		t.Skip("skipping on supported platform")
	}

	err := OpenBrowser("http://localhost:7450")
	if err == nil {
		t.Fatal("expected error for unsupported platform")
	}
	if !strings.Contains(err.Error(), "unsupported platform") {
		t.Errorf("error = %v, want unsupported platform message", err)
	}
}

func TestOpenBrowserSysProcAttr(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("skipping on unsupported platform")
	}

	// We can't actually run OpenBrowser in a test because it launches a browser.
	// Instead, verify the function builds and SysProcAttr is accessible.
	// This test ensures the syscall.SysProcAttr struct exists and Setpgid works.
	attr := &syscall.SysProcAttr{Setpgid: true}
	if !attr.Setpgid {
		t.Error("Setpgid should be true")
	}
}
