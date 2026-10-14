package source

import (
	"context"
	"errors"
	"testing"
)

type testPreferences struct {
	values   map[SourceID]bool
	revision int64
}

func (p *testPreferences) SourceSettings(context.Context) (map[SourceID]bool, int64, error) {
	return p.values, p.revision, nil
}

func TestRegistryPreferencesGateQueriesAndAggregation(t *testing.T) {
	reg := NewRegistry(SourceOpenCode)
	a := newRegistryFakeSource(SourceOpenCode, true)
	b := newRegistryFakeSource(SourceAntigravity, true)
	reg.Register(a)
	reg.Register(b)
	p := &testPreferences{values: map[SourceID]bool{SourceAntigravity: false}, revision: 1}
	if err := reg.ConfigurePreferences(p, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := reg.Resolve("antigravity"); !errors.Is(err, ErrDisabledSource) {
		t.Fatal(err)
	}
	if len(reg.Available(context.Background())) != 1 {
		t.Fatal("disabled source reached aggregation")
	}
	for _, info := range reg.List(context.Background()) {
		if info.ID == SourceAntigravity && info.Enabled {
			t.Fatal("disabled source metadata")
		}
	}
	p.values[SourceAntigravity] = true
	p.revision++
	if err := reg.RefreshPreferences(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(reg.Available(context.Background())) != 2 {
		t.Fatal("reenabled source missing")
	}
}
