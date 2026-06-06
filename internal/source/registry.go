package source

import (
	"context"
	"errors"
	"strings"
)

type closeableSource interface {
	Close() error
}

type registryEntry struct {
	source Source
	info   SourceInfo
}

type Registry struct {
	defaultID SourceID
	startupID SourceID
	entries   map[SourceID]registryEntry
	order     []SourceID
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
	if r == nil || r.defaultID == "" {
		return SourceOpenCode
	}
	return r.defaultID
}

func (r *Registry) StartupID() SourceID {
	if r == nil || r.startupID == "" {
		return r.DefaultID()
	}
	return r.startupID
}

func (r *Registry) SetStartupID(id SourceID) {
	if r == nil || id == "" {
		return
	}
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
	r.upsert(info.ID, registryEntry{source: src, info: info})
	return nil
}

func (r *Registry) RegisterUnavailable(info SourceInfo) error {
	if info.ID == "" {
		return InvalidSourceError{ID: "<empty>"}
	}
	info.Available = false
	r.upsert(info.ID, registryEntry{info: info})
	return nil
}

func (r *Registry) Resolve(selectedID string) (Source, error) {
	if r == nil {
		return nil, UnavailableSourceError{ID: SourceOpenCode, Reason: "source registry is not configured"}
	}
	id := strings.TrimSpace(selectedID)
	if id == "" {
		// Omitted/empty source parameters intentionally resolve to the API
		// compatibility default, not the startup-selected source. The web client
		// sends an explicit source param when startup fallback and default differ.
		id = string(r.DefaultID())
	}
	if id == "both" {
		return nil, UnsupportedSourceError{ID: id, Reason: "v1 supports one selected source at a time"}
	}

	sourceID := SourceID(id)
	entry, ok := r.entries[sourceID]
	if !ok {
		return nil, InvalidSourceError{ID: id}
	}
	info := entry.info
	if entry.source != nil {
		info = entry.source.Info(context.Background())
	}
	if !info.Available || entry.source == nil {
		return nil, UnavailableSourceError{ID: sourceID, Reason: info.Diagnostics.Reason}
	}
	return entry.source, nil
}

func (r *Registry) List(ctx context.Context) []SourceInfo {
	if r == nil {
		return []SourceInfo{}
	}
	infos := make([]SourceInfo, 0, len(r.order))
	for _, id := range r.order {
		entry := r.entries[id]
		info := entry.info
		if entry.source != nil {
			info = entry.source.Info(ctx)
		}
		info.Default = id == r.DefaultID()
		info.Selected = id == r.StartupID()
		infos = append(infos, info)
	}
	return infos
}

func (r *Registry) Close() error {
	if r == nil {
		return nil
	}
	var errs []error
	for _, id := range r.order {
		entry := r.entries[id]
		if closer, ok := entry.source.(closeableSource); ok {
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
