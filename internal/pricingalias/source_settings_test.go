package pricingalias

import (
	"context"
	"path/filepath"
	"testing"

	"opencode-dashboard/internal/source"
)

func TestSourceSettingsPersistenceRevisionAndDefaults(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "settings.sqlite")
	s, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	settings, revision, err := s.SourceSettings(ctx)
	if err != nil || len(settings) != 0 || revision != 0 {
		t.Fatalf("initial settings: %v %d %v", settings, revision, err)
	}
	if err := s.SetSourceEnabled(ctx, source.SourceAntigravity, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetSourceEnabled(ctx, source.SourceAntigravity, false); err != nil {
		t.Fatal(err)
	}
	settings, revision, err = s.SourceSettings(ctx)
	if err != nil || settings[source.SourceAntigravity] || revision != 1 {
		t.Fatalf("saved settings: %v %d %v", settings, revision, err)
	}
	s.Close()
	s, err = Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	settings, revision, err = s.SourceSettings(ctx)
	if err != nil || settings[source.SourceAntigravity] || revision != 1 {
		t.Fatalf("reopened settings: %v %d %v", settings, revision, err)
	}
	if err := s.SetSourceEnabled(ctx, source.SourceID("unknown"), false); err == nil {
		t.Fatal("unknown source accepted")
	}
	if err := s.SetSourceEnabled(ctx, source.SourceAntigravity, true); err != nil {
		t.Fatal(err)
	}
	settings, revision, _ = s.SourceSettings(ctx)
	if !settings[source.SourceAntigravity] || revision != 2 {
		t.Fatal("reenable did not persist")
	}
}
