package main

import (
	"context"

	usagecache "opencode-dashboard/internal/cache"
	"opencode-dashboard/internal/config"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/source/antigravity"
	"opencode-dashboard/internal/source/claudecode"
	"opencode-dashboard/internal/source/codex"
	"opencode-dashboard/internal/source/kimicode"
	opencodesource "opencode-dashboard/internal/source/opencode"
	"opencode-dashboard/internal/source/qwencode"
	"opencode-dashboard/internal/store"
)

func buildConfiguredRegistry(cache *cacheRuntime, st *store.Store, selection dbSelection, startup source.SourceID, claude, codexHome config.PathSelection, extra extraRegistrySelection) (*source.Registry, error) {
	reg := source.NewRegistry(source.SourceOpenCode)
	reg.SetStartupID(startup)
	if err := reg.ConfigurePreferences(extra.preferences, nil); err != nil {
		return nil, err
	}
	if cache != nil {
		cache.registry = reg
	}
	var rates source.PricingRateIndex
	if extra.pricingRates != nil {
		rates = extra.pricingRates
	}
	kimi, qwen, ag := config.ResolveKimiHome(""), config.ResolveQwenHome(""), config.ResolveAntigravityHome("")
	if extra.kimi != nil {
		kimi = extra.kimi.selection
	}
	if extra.qwen != nil {
		qwen = extra.qwen.selection
	}
	if extra.antigravity != nil {
		ag = extra.antigravity.selection
	}
	openSrc := opencodesource.NewLazy(selection.Path, selection.Source)
	if st != nil {
		openSrc = opencodesource.New(st, opencodesource.WithPath(selection.Path, selection.Source))
	}
	srcs := []source.Source{openSrc,
		claudecode.New(claudecode.Options{ClaudeHome: claude.Path, PathSource: claude.Source, PricingAliases: extra.pricingAliases, PricingRates: rates}),
		codex.New(codex.Options{CodexHome: codexHome.Path, PathSource: codexHome.Source, PricingAliases: extra.pricingAliases, PricingRates: rates}),
		kimicode.New(kimicode.Options{KimiHome: kimi.Path, PathSource: kimi.Source, PricingAliases: extra.pricingAliases, PricingRates: rates}),
		qwencode.New(qwencode.Options{QwenHome: qwen.Path, PathSource: qwen.Source, PricingAliases: extra.pricingAliases, PricingRates: rates}),
		antigravity.New(antigravity.Options{Home: ag.Path, PathSource: ag.Source, PricingAliases: extra.pricingAliases, PricingRates: rates})}
	names := []string{"OpenCode", "Claude Code", "Codex", "Kimi Code", "Qwen Code", "Antigravity"}
	paths := []config.PathSelection{{Path: selection.Path, Source: selection.Source}, claude, codexHome, kimi, qwen, ag}
	for i, id := range source.KnownIDs() {
		kind := "jsonl"
		if id == source.SourceOpenCode || id == source.SourceAntigravity {
			kind = "sqlite"
		}
		info := source.SourceInfo{ID: id, Label: names[i], Kind: kind, Path: paths[i].Path, PathSource: paths[i].Source, ReadOnly: true, LocalOnly: true, Capabilities: []string{"overview", "daily", "models", "tools", "projects", "sessions", "messages", "config"}, Diagnostics: source.SourceDiagnostics{Status: "unchecked", Reason: "source discovery is paused while disabled"}}
		if reg.Enabled(id) {
			info = srcs[i].Info(context.Background())
		}
		if err := reg.RegisterWithInfo(srcs[i], info); err != nil {
			return nil, err
		}
		if cache != nil {
			cache.rememberSourceAs(id, srcs[i])
		}
		if reg.Enabled(id) && info.Available {
			if err := registerCachedSource(context.Background(), reg, cache, srcs[i]); err != nil {
				return nil, err
			}
		}
	}
	if extra.pricingRates != nil {
		extra.pricingRates.Bind(reg)
	}
	if err := reg.ConfigurePreferences(extra.preferences, func(ids []source.SourceID) {
		if cache != nil {
			cache.sourcePreferencesChanged(ids)
		}
	}); err != nil {
		return nil, err
	}
	if cache != nil {
		cache.startPendingInitialSync()
		cache.startAutoSync()
	}
	return reg, nil
}

func (c *cacheRuntime) rememberSourceAs(id source.SourceID, src source.Source) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.live == nil {
		c.live = map[source.SourceID]source.Source{}
	}
	if c.cached == nil {
		c.cached = map[source.SourceID]bool{}
	}
	if _, ok := c.live[id]; !ok {
		c.order = append(c.order, id)
	}
	c.live[id] = src
}

func (c *cacheRuntime) sourcePreferencesChanged(ids []source.SourceID) {
	for _, id := range ids {
		enabled := c.registry.Enabled(id)
		c.mu.Lock()
		live := c.live[id]
		wrapped := c.wrappers[id]
		if !enabled {
			delete(c.pendingPricing, id)
			kept := c.pendingInitial[:0]
			for _, queued := range c.pendingInitial {
				if queued != id {
					kept = append(kept, queued)
				}
			}
			c.pendingInitial = kept
			if c.job.CurrentSourceID == id && c.sourceCancel != nil {
				c.sourceCancel()
			}
		}
		c.mu.Unlock()
		if wrapped != nil {
			wrapped.SetParticipation(enabled)
		}
		if enabled && live != nil {
			if invalidator, ok := live.(source.PricingInvalidator); ok {
				invalidator.Invalidate()
			}
			c.queueInitialSync(id)
		}
	}
	c.startPendingInitialSync()
}

func (c *cacheRuntime) sourceEnabled(id source.SourceID) bool {
	return c.registry == nil || c.registry.Enabled(id)
}

func (c *cacheRuntime) wrapSource(src source.Source, id source.SourceID) *usagecache.CachedSource {
	wrapped := usagecache.WrapSource(c.store, src)
	wrapped.SetEnabledCheck(func() bool { return c.sourceEnabled(id) })
	c.mu.Lock()
	if c.wrappers == nil {
		c.wrappers = map[source.SourceID]*usagecache.CachedSource{}
	}
	c.wrappers[id] = wrapped
	c.mu.Unlock()
	return wrapped
}

func (c *cacheRuntime) sourceIdentity(src source.Source) source.SourceID {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, live := range c.live {
		if live == src {
			return id
		}
	}
	return sourceIDOf(src)
}
