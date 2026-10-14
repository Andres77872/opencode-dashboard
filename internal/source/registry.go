package source

import (
	"context"
	"errors"
	"strings"
	"sync"
)

type closeableSource interface {
	Close() error
}

type registryEntry struct {
	source Source
	info   SourceInfo
}

type Registry struct {
	mu          sync.RWMutex
	defaultID   SourceID
	startupID   SourceID
	entries     map[SourceID]registryEntry
	order       []SourceID
	disabled    map[SourceID]bool
	revision    int64
	preferences Preferences
	onChange    func([]SourceID)
}

func (r *Registry) ConfigurePreferences(p Preferences, onChange func([]SourceID)) error {
	r.mu.Lock()
	r.preferences = p
	r.onChange = onChange
	r.mu.Unlock()
	return r.RefreshPreferences(context.Background())
}

func (r *Registry) RefreshPreferences(ctx context.Context) error {
	r.mu.RLock()
	p := r.preferences
	r.mu.RUnlock()
	if p == nil {
		return nil
	}
	settings, revision, err := p.SourceSettings(ctx)
	if err != nil {
		return err
	}
	r.mu.Lock()
	if revision < r.revision {
		r.mu.Unlock()
		return nil
	}
	changed := make([]SourceID, 0)
	for _, id := range KnownIDs() {
		enabled, exists := settings[id]
		disabled := exists && !enabled
		if r.disabled[id] != disabled {
			changed = append(changed, id)
		}
	}
	r.disabled = make(map[SourceID]bool)
	for id, enabled := range settings {
		r.disabled[id] = !enabled
	}
	r.revision = revision
	callback := r.onChange
	r.mu.Unlock()
	if callback != nil && len(changed) != 0 {
		callback(changed)
	}
	return nil
}

func (r *Registry) Enabled(id SourceID) bool {
	if r == nil {
		return true
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return !r.disabled[id]
}

func (r *Registry) Revision() int64 { r.mu.RLock(); defer r.mu.RUnlock(); return r.revision }

// RegisterWithInfo avoids discovery reads when a source starts disabled.
func (r *Registry) RegisterWithInfo(src Source, info SourceInfo) error {
	if src == nil || info.ID == "" {
		return InvalidSourceError{ID: string(info.ID)}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upsert(info.ID, registryEntry{source: src, info: info})
	return nil
}

func NewRegistry(defaultID SourceID) *Registry {
	if defaultID == "" {
		defaultID = SourceOpenCode
	}
	return &Registry{
		defaultID: defaultID,
		startupID: defaultID,
		entries:   make(map[SourceID]registryEntry),
	}
}

func (r *Registry) DefaultID() SourceID {
	if r == nil {
		return SourceOpenCode
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultIDLocked()
}

// defaultIDLocked reads defaultID with r.mu already held. Callers that already
// hold the lock must use this: sync.RWMutex forbids recursive read locking, so
// re-entering RLock while a writer is pending deadlocks the whole registry.
func (r *Registry) defaultIDLocked() SourceID {
	if r.defaultID == "" {
		return SourceOpenCode
	}
	return r.defaultID
}

func (r *Registry) StartupID() SourceID {
	if r == nil {
		return SourceOpenCode
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.startupIDLocked()
}

// startupIDLocked reads startupID with r.mu already held. See defaultIDLocked.
func (r *Registry) startupIDLocked() SourceID {
	if r.startupID != "" {
		return r.startupID
	}
	if r.defaultID != "" {
		return r.defaultID
	}
	return SourceOpenCode
}

func (r *Registry) SetStartupID(id SourceID) {
	if r == nil || id == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.startupID = id
}

func (r *Registry) Register(src Source) error {
	if src == nil {
		return InvalidSourceError{ID: "<nil>"}
	}
	info := src.Info(context.Background())
	if info.ID == "" {
		return InvalidSourceError{ID: "<empty>"}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upsert(info.ID, registryEntry{source: src, info: info})
	return nil
}

func (r *Registry) RegisterUnavailable(info SourceInfo) error {
	if info.ID == "" {
		return InvalidSourceError{ID: "<empty>"}
	}
	info.Available = false
	r.mu.Lock()
	defer r.mu.Unlock()
	r.upsert(info.ID, registryEntry{info: info})
	return nil
}

func (r *Registry) Resolve(selectedID string) (Source, error) {
	if r == nil {
		return nil, UnavailableSourceError{ID: SourceOpenCode, Reason: "source registry is not configured"}
	}
	if err := r.RefreshPreferences(context.Background()); err != nil {
		return nil, UnavailableSourceError{Reason: "source preferences could not be loaded"}
	}
	r.mu.RLock()
	id := strings.TrimSpace(selectedID)
	if id == "" {
		// Omitted/empty source parameters intentionally resolve to the API
		// compatibility default, not the startup-selected source. The web client
		// sends an explicit source param when startup fallback and default differ.
		id = string(r.defaultIDLocked())
	}
	if id == "both" {
		r.mu.RUnlock()
		return nil, UnsupportedSourceError{ID: id, Reason: "v1 supports one selected source at a time"}
	}

	sourceID := SourceID(id)
	if r.disabled[sourceID] {
		r.mu.RUnlock()
		return nil, DisabledSourceError{ID: sourceID}
	}
	entry, ok := r.entries[sourceID]
	r.mu.RUnlock()
	if !ok {
		return nil, InvalidSourceError{ID: id}
	}
	info := entry.info
	if entry.source != nil {
		info = entry.source.Info(context.Background())
	}
	if !r.Enabled(sourceID) {
		return nil, DisabledSourceError{ID: sourceID}
	}
	if !info.Available || entry.source == nil {
		return nil, UnavailableSourceError{ID: sourceID, Reason: info.Diagnostics.Reason}
	}
	return entry.source, nil
}

func (r *Registry) List(ctx context.Context) []SourceInfo {
	infos, _ := r.ListWithRevision(ctx)
	return infos
}

// ListWithRevision captures preferences and their revision together. Source
// discovery runs without the registry lock because pricing can consult other
// registered catalogs and source discovery can take time.
func (r *Registry) ListWithRevision(ctx context.Context) ([]SourceInfo, int64) {
	if r == nil {
		return []SourceInfo{}, 0
	}
	r.mu.RLock()
	entries := make([]registryEntry, 0, len(r.order))
	for _, id := range r.order {
		entry := r.entries[id]
		entry.info.Enabled = !r.disabled[id]
		entry.info.Default = id == r.defaultIDLocked()
		entry.info.Selected = id == r.startupIDLocked()
		entries = append(entries, entry)
	}
	revision := r.revision
	r.mu.RUnlock()
	infos := make([]SourceInfo, 0, len(entries))
	for _, entry := range entries {
		info := entry.info
		if entry.source != nil && info.Enabled {
			info = entry.source.Info(ctx)
			info.Enabled, info.Default, info.Selected = entry.info.Enabled, entry.info.Default, entry.info.Selected
		}
		infos = append(infos, info)
	}
	return infos, revision
}

// Available returns the live, available Source instances in registration order.
// Entries that are placeholders (nil source) or report Available == false are skipped.
// It is used by the cross-source aggregator (see AggregateOverview).
func (r *Registry) Available(ctx context.Context) []Source {
	if r == nil {
		return nil
	}
	if err := r.RefreshPreferences(ctx); err != nil {
		return []Source{}
	}
	r.mu.RLock()
	entries := make([]registryEntry, 0, len(r.order))
	for _, id := range r.order {
		if entry := r.entries[id]; entry.source != nil && !r.disabled[id] {
			entries = append(entries, entry)
		}
	}
	r.mu.RUnlock()
	out := make([]Source, 0, len(entries))
	for _, entry := range entries {
		if entry.source.Info(ctx).Available && r.Enabled(entry.info.ID) {
			out = append(out, entry.source)
		}
	}
	return out
}

// registered returns the live Source instances in registration order without
// consulting any of them.
//
// Available filters on Info(), which for transcript sources loads pricing and
// therefore captures alias state. A caller that Info() itself depends on — the
// cross-source catalog index — must use this instead, or it recurses back into
// itself. It also releases the lock before returning, so callers never hold the
// registry read lock while doing work of their own.
func (r *Registry) registered() []Source {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Source, 0, len(r.order))
	for _, id := range r.order {
		if entry := r.entries[id]; entry.source != nil {
			out = append(out, entry.source)
		}
	}
	return out
}

func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, src := range r.registered() {
		if closer, ok := src.(closeableSource); ok {
			if err := closer.Close(); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

func (r *Registry) upsert(id SourceID, entry registryEntry) {
	if r.entries == nil {
		r.entries = make(map[SourceID]registryEntry)
	}
	if _, exists := r.entries[id]; !exists {
		r.order = append(r.order, id)
	}
	r.entries[id] = entry
}
