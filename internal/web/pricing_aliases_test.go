package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"opencode-dashboard/internal/pricingalias"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

type pricingAliasTestKey struct {
	sourceID   source.SourceID
	providerID string
	modelID    string
}

type fakePricingAliasStore struct {
	aliases    map[pricingAliasTestKey]pricingalias.Alias
	listErr    error
	upsertErr  error
	deleteErr  error
	upserts    []pricingalias.Alias
	deletes    []pricingAliasTestKey
	nextTimeMS int64
}

func newFakePricingAliasStore(aliases ...pricingalias.Alias) *fakePricingAliasStore {
	store := &fakePricingAliasStore{
		aliases:    make(map[pricingAliasTestKey]pricingalias.Alias, len(aliases)),
		nextTimeMS: 100,
	}
	for _, alias := range aliases {
		store.aliases[pricingAliasTestKey{alias.SourceID, alias.ProviderID, alias.ModelID}] = alias
	}
	return store
}

func (s *fakePricingAliasStore) List(_ context.Context, sourceID source.SourceID) ([]pricingalias.Alias, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	result := make([]pricingalias.Alias, 0, len(s.aliases))
	for key, alias := range s.aliases {
		if key.sourceID == sourceID {
			result = append(result, alias)
		}
	}
	return result, nil
}

func (s *fakePricingAliasStore) Upsert(_ context.Context, alias pricingalias.Alias) (pricingalias.Alias, error) {
	s.upserts = append(s.upserts, alias)
	if s.upsertErr != nil {
		return pricingalias.Alias{}, s.upsertErr
	}
	key := pricingAliasTestKey{alias.SourceID, alias.ProviderID, alias.ModelID}
	if previous, ok := s.aliases[key]; ok {
		alias.CreatedMS = previous.CreatedMS
	} else {
		alias.CreatedMS = s.nextTimeMS
	}
	s.nextTimeMS++
	alias.UpdatedMS = s.nextTimeMS
	s.aliases[key] = alias
	return alias, nil
}

func (s *fakePricingAliasStore) Delete(_ context.Context, sourceID source.SourceID, providerID, modelID string) error {
	key := pricingAliasTestKey{sourceID, providerID, modelID}
	s.deletes = append(s.deletes, key)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	delete(s.aliases, key)
	return nil
}

type pricingAliasFakeSource struct {
	*handlerFakeSource
	catalog       source.PricingCatalog
	resolutions   map[pricingAliasTestKey]source.PricingResolution
	defaultKind   source.PricingResolutionKind
	resolveCalls  []pricingAliasTestKey
	invalidations int
	lastModelsPQ  stats.PeriodQuery
	modelsErr     error
}

func newPricingAliasFakeSource(id source.SourceID) *pricingAliasFakeSource {
	return &pricingAliasFakeSource{
		handlerFakeSource: newHandlerFakeSource(id, true, 0),
		catalog: source.PricingCatalog{
			SourceID: id,
			Models:   []source.PricingCatalogModel{},
		},
		resolutions: make(map[pricingAliasTestKey]source.PricingResolution),
		defaultKind: source.PricingResolutionUnknown,
	}
}

func (s *pricingAliasFakeSource) Models(_ context.Context, pq stats.PeriodQuery) (stats.ModelStats, error) {
	s.modelsCalls++
	s.lastModelsPQ = pq
	if s.modelsErr != nil {
		return stats.ModelStats{}, s.modelsErr
	}
	return stats.ModelStats{Models: s.models}, nil
}

func (s *pricingAliasFakeSource) PricingCatalog(context.Context) source.PricingCatalog {
	return s.catalog
}

func (s *pricingAliasFakeSource) ResolvePricing(_ context.Context, providerID, modelID string) source.PricingResolution {
	key := pricingAliasTestKey{s.info.ID, providerID, modelID}
	s.resolveCalls = append(s.resolveCalls, key)
	if resolution, ok := s.resolutions[key]; ok {
		return resolution
	}
	return source.PricingResolution{
		SourceID:   s.info.ID,
		ProviderID: providerID,
		ModelID:    modelID,
		Kind:       s.defaultKind,
	}
}

func (s *pricingAliasFakeSource) Invalidate() {
	s.invalidations++
}

type fakePricingChangeCache struct {
	*fakeCacheManager
	status   PricingChangeStatus
	notified []source.SourceID
}

func newFakePricingChangeCache(status PricingChangeStatus) *fakePricingChangeCache {
	return &fakePricingChangeCache{fakeCacheManager: &fakeCacheManager{}, status: status}
}

func (c *fakePricingChangeCache) NotifyPricingChange(_ context.Context, sourceID source.SourceID) PricingChangeStatus {
	c.notified = append(c.notified, sourceID)
	return c.status
}

func pricingAliasTestServer(t *testing.T, src source.Source, store PricingAliasStore, cache CacheManager) *http.Server {
	t.Helper()
	id := src.Info(context.Background()).ID
	registry := source.NewRegistry(id)
	if err := registry.Register(src); err != nil {
		t.Fatalf("register source: %v", err)
	}
	return NewServer(ServerOptions{Registry: registry, Cache: cache, PricingAliases: store})
}

func performPricingAliasRequest(t *testing.T, server *http.Server, method, target, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	server.Handler.ServeHTTP(rec, req)
	return rec
}

func decodePricingAliasesResponse(t *testing.T, rec *httptest.ResponseRecorder) PricingAliasesResponse {
	t.Helper()
	var response PricingAliasesResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode pricing aliases response: %v; body: %s", err, rec.Body.String())
	}
	return response
}

func decodePricingAliasMutationResponse(t *testing.T, rec *httptest.ResponseRecorder) PricingAliasMutationResponse {
	t.Helper()
	var response PricingAliasMutationResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode pricing alias mutation response: %v; body: %s", err, rec.Body.String())
	}
	return response
}

func positivePricingRate() source.PricingRateSummary {
	return source.PricingRateSummary{InputPerMillion: 1, OutputPerMillion: 2}
}

func TestPricingAliasConstructorsAndRoutes(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	registry := source.NewRegistry(source.SourceCodex)
	if err := registry.Register(src); err != nil {
		t.Fatal(err)
	}
	store := newFakePricingAliasStore()

	// An omitted alias store leaves the handlers read-only rather than
	// substituting one, and a supplied store is used as given.
	if got := NewHandlers(ServerOptions{Registry: registry}); got == nil || got.pricingAliases != nil {
		t.Fatalf("NewHandlers() without a store = %#v, want no alias store", got)
	}
	if got := NewHandlers(ServerOptions{Registry: registry, PricingAliases: store}); got == nil || got.pricingAliases != store {
		t.Fatal("NewHandlers() did not retain the alias store it was given")
	}
	// Services left unset must not be invented, since each nil one is what makes
	// its endpoints report themselves unavailable.
	if got := NewHandlers(ServerOptions{Registry: registry}); got.cache != nil || got.quotas != nil || got.assistant != nil || got.chatlog != nil || got.pricingRates != nil {
		t.Fatalf("NewHandlers() populated services that were not configured: %#v", got)
	}
	// Registry and logger are the only fields with defaults.
	if got := NewHandlers(ServerOptions{}); got.registry == nil || got.logger == nil {
		t.Fatalf("NewHandlers() left required collaborators nil: %#v", got)
	}

	// The alias route answers whether or not a writable store is configured.
	for name, server := range map[string]*http.Server{
		"read-only": NewServer(ServerOptions{Registry: registry}),
		"writable":  NewServer(ServerOptions{Registry: registry, PricingAliases: store}),
	} {
		rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("%s GET route status = %d, want 200; body: %s", name, rec.Code, rec.Body.String())
		}
	}
}

func TestPricingAliasesUnsupportedSourcesReturnMetadataAndAliases(t *testing.T) {
	t.Run("source lacks pricing catalog capability", func(t *testing.T) {
		src := newHandlerFakeSource(source.SourceOpenCode, true, 0)
		alias := pricingalias.Alias{SourceID: source.SourceOpenCode, ProviderID: "provider", ModelID: "old", TargetModelID: "target"}
		server := pricingAliasTestServer(t, src, newFakePricingAliasStore(alias), nil)

		rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
		}
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
		response := decodePricingAliasesResponse(t, rec)
		if response.Supported {
			t.Error("supported = true, want false")
		}
		if response.SourceID != source.SourceOpenCode || response.Catalog.SourceID != source.SourceOpenCode {
			t.Errorf("source metadata = %q/%q, want opencode", response.SourceID, response.Catalog.SourceID)
		}
		if len(response.Aliases) != 1 || response.Aliases[0].ModelID != "old" {
			t.Errorf("aliases = %#v, want persisted alias", response.Aliases)
		}
		if response.ObservedModels == nil || len(response.ObservedModels) != 0 {
			t.Errorf("unresolved_models = %#v, want empty array", response.ObservedModels)
		}
		if src.modelsCalls != 0 {
			t.Errorf("Models calls = %d, want 0 for unsupported source", src.modelsCalls)
		}
	})

	t.Run("empty catalog retains catalog metadata", func(t *testing.T) {
		src := newPricingAliasFakeSource(source.SourceCodex)
		src.catalog.SnapshotID = "empty-snapshot"
		src.catalog.Currency = "USD"
		src.catalog.Note = "catalog not loaded"
		server := pricingAliasTestServer(t, src, newFakePricingAliasStore(), nil)

		rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
		response := decodePricingAliasesResponse(t, rec)
		if response.Supported {
			t.Error("supported = true, want false for empty catalog")
		}
		if response.Catalog.SnapshotID != "empty-snapshot" || response.Catalog.Currency != "USD" || response.Catalog.Note != "catalog not loaded" {
			t.Errorf("catalog metadata = %#v, want original metadata", response.Catalog)
		}
		if response.Catalog.Models == nil || len(response.Catalog.Models) != 0 {
			t.Errorf("catalog models = %#v, want empty array", response.Catalog.Models)
		}
		if src.modelsCalls != 0 {
			t.Errorf("Models calls = %d, want 0 for empty catalog", src.modelsCalls)
		}
	})
}

func TestPricingAliasesListsCatalogAliasesAndSortedObservedModels(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog = source.PricingCatalog{
		SourceID:   source.SourceCodex,
		SnapshotID: "snapshot-1",
		Currency:   "USD",
		Note:       "test catalog",
		Models: []source.PricingCatalogModel{
			{ModelID: "target", DisplayName: "Target", Rate: positivePricingRate()},
			{ModelID: "zero-input", Rate: source.PricingRateSummary{OutputPerMillion: 2}},
			{ModelID: "zero-output", Rate: source.PricingRateSummary{InputPerMillion: 1}},
		},
	}
	src.models = []stats.ModelEntry{
		{ProviderID: "p2", ModelID: "unknown-b", Sessions: 2, Messages: 20, Tokens: stats.TokenStats{Input: 200}},
		{ProviderID: "p2", ModelID: "unknown-a", Sessions: 1, Messages: 10},
		{ProviderID: "p1", ModelID: "unpriced", Sessions: 3, Messages: 10, Tokens: stats.TokenStats{Output: 50}},
		{ProviderID: "p9", ModelID: "unavailable", Sessions: 4, Messages: 30},
		{ProviderID: "p0", ModelID: "native", Sessions: 5, Messages: 100},
		{ProviderID: "p9", ModelID: "unavailable", Sessions: 99, Messages: 999},
		{ProviderID: "", ModelID: "missing-provider", Messages: 500},
		{ProviderID: "p3", ModelID: "", Messages: 500},
	}
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "p1", "unpriced"}] = source.PricingResolution{
		SourceID: source.SourceCodex, ProviderID: "p1", ModelID: "unpriced", Kind: source.PricingResolutionUnpriced, Note: "known without rates",
	}
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "p9", "unavailable"}] = source.PricingResolution{
		SourceID: source.SourceCodex, ProviderID: "p9", ModelID: "unavailable", Kind: source.PricingResolutionUnavailable,
	}
	rate := positivePricingRate()
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "p0", "native"}] = source.PricingResolution{
		SourceID: source.SourceCodex, ProviderID: "p0", ModelID: "native", TargetModelID: "target", Kind: source.PricingResolutionExact, Rate: &rate,
	}
	store := newFakePricingAliasStore(
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "z", ModelID: "b", TargetModelID: "target"},
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "a", ModelID: "c", TargetModelID: "target"},
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "a", ModelID: "a", TargetModelID: "target"},
	)
	server := pricingAliasTestServer(t, src, store, nil)

	rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	response := decodePricingAliasesResponse(t, rec)
	if !response.Supported {
		t.Fatal("supported = false, want true")
	}
	if src.lastModelsPQ != (stats.PeriodQuery{Period: "all"}) {
		t.Errorf("Models query = %#v, want period all", src.lastModelsPQ)
	}
	// Six distinct observed rows with non-empty model ids, plus one resolution per
	// saved alias that is not currently observed (needed to classify its state).
	if len(src.resolveCalls) != 9 {
		t.Errorf("ResolvePricing calls = %d, want 6 observed rows plus 3 undetected aliases", len(src.resolveCalls))
	}
	for _, alias := range response.Aliases {
		if alias.Detected || alias.State != PricingAliasStateNotDetected || alias.Active || !alias.Editable || !alias.TargetValid {
			t.Errorf("undetected alias state = %#v, want editable not_detected with a valid target", alias)
		}
	}
	if len(response.Catalog.Models) != 3 || !response.Catalog.Models[0].Targetable || response.Catalog.Models[1].Targetable || response.Catalog.Models[2].Targetable {
		t.Errorf("catalog targetable flags = %#v", response.Catalog.Models)
	}
	if got := []string{
		response.Aliases[0].ProviderID + "/" + response.Aliases[0].ModelID,
		response.Aliases[1].ProviderID + "/" + response.Aliases[1].ModelID,
		response.Aliases[2].ProviderID + "/" + response.Aliases[2].ModelID,
	}; strings.Join(got, ",") != "a/a,a/c,z/b" {
		t.Errorf("alias order = %v, want [a/a a/c z/b]", got)
	}

	// Every observed model is listed so any of them can be re-pointed, but the
	// ones still lacking pricing sort first because they are why this view exists.
	wantOrder := []string{"/missing-provider", "p9/unavailable", "p2/unknown-b", "p1/unpriced", "p2/unknown-a", "p0/native"}
	if len(response.ObservedModels) != len(wantOrder) {
		t.Fatalf("observed models = %#v, want %d rows", response.ObservedModels, len(wantOrder))
	}
	for index, want := range wantOrder {
		got := response.ObservedModels[index].ProviderID + "/" + response.ObservedModels[index].ModelID
		if got != want {
			t.Errorf("observed[%d] = %q, want %q", index, got, want)
		}
	}
	if response.ObservedModels[1].Aliasable {
		t.Error("unavailable model Aliasable = true, want false")
	}
	if !response.ObservedModels[0].Aliasable || !response.ObservedModels[2].Aliasable || !response.ObservedModels[3].Aliasable || !response.ObservedModels[4].Aliasable {
		t.Error("unknown and unpriced models must be aliasable")
	}
	// A natively priced model stays aliasable so a wrong name match can be fixed.
	if native := response.ObservedModels[5]; !native.Resolved || !native.Aliasable {
		t.Errorf("natively priced model = %#v, want a resolved but aliasable row", native)
	}
	for index, observed := range response.ObservedModels[:5] {
		if observed.Resolved {
			t.Errorf("observed[%d] = %#v, want resolved false", index, observed)
		}
	}
	if response.ObservedModels[3].ResolutionKind != source.PricingResolutionUnpriced || response.ObservedModels[3].ResolutionNote != "known without rates" || response.ObservedModels[3].ResolutionReason == "" {
		t.Errorf("unpriced resolution metadata = %#v", response.ObservedModels[3])
	}
	if response.ObservedModels[2].Tokens.Input != 200 {
		t.Errorf("unknown model tokens = %#v, want input 200", response.ObservedModels[2].Tokens)
	}

	// Every catalog-bearing source is offered as a target, not just this one.
	if len(response.Catalogs) != 1 || response.Catalogs[0].SourceID != source.SourceCodex || len(response.Catalogs[0].Models) != 3 {
		t.Errorf("catalogs = %#v, want the codex catalog", response.Catalogs)
	}
}

func TestPricingAliasesRetainsDetectionStatsForConfiguredAlias(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	src.models = []stats.ModelEntry{{ProviderID: "openai", ModelID: "custom", Sessions: 3, Messages: 42, Tokens: stats.TokenStats{Input: 1200}}}
	rate := positivePricingRate()
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "openai", "custom"}] = source.PricingResolution{
		SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "target", Kind: source.PricingResolutionUserAlias, Rate: &rate,
	}
	store := newFakePricingAliasStore(pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "target"})
	server := pricingAliasTestServer(t, src, store, nil)

	response := decodePricingAliasesResponse(t, performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", ""))
	if len(response.ObservedModels) != 1 || !response.ObservedModels[0].Resolved {
		t.Fatalf("observed models = %#v, want the aliased model reported as resolved", response.ObservedModels)
	}
	if len(response.Aliases) != 1 {
		t.Fatalf("aliases = %#v, want one", response.Aliases)
	}
	alias := response.Aliases[0]
	if !alias.Detected || alias.Sessions != 3 || alias.Messages != 42 || alias.Tokens.Input != 1200 {
		t.Errorf("alias detection stats = %#v", alias)
	}
}

func TestPricingAliasCreateUpdateAndDelete(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{
		{ModelID: "target-a", Rate: positivePricingRate()},
		{ModelID: "target-b", Rate: positivePricingRate()},
	}
	src.models = []stats.ModelEntry{{ProviderID: "openai", ModelID: "custom", Messages: 12}}
	store := newFakePricingAliasStore()
	cache := newFakePricingChangeCache(PricingChangeStatusStarted)
	server := pricingAliasTestServer(t, src, store, cache)

	create := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", `{
		"source_id":" codex ","provider_id":" openai ","model_id":" custom ","target_model_id":" target-a "
	}`)
	if create.Code != http.StatusOK {
		t.Fatalf("create status = %d, want 200; body: %s", create.Code, create.Body.String())
	}
	if create.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("create Cache-Control = %q, want no-store", create.Header().Get("Cache-Control"))
	}
	createResponse := decodePricingAliasMutationResponse(t, create)
	if createResponse.Reprice != PricingChangeStatusStarted {
		t.Errorf("create reprice = %q, want started", createResponse.Reprice)
	}
	if len(store.upserts) != 1 || store.upserts[0].SourceID != source.SourceCodex || store.upserts[0].ProviderID != "openai" || store.upserts[0].ModelID != "custom" || store.upserts[0].TargetModelID != "target-a" {
		t.Fatalf("create upsert = %#v, want trimmed exact alias", store.upserts)
	}
	if len(createResponse.Aliases) != 1 || createResponse.Aliases[0].TargetModelID != "target-a" || !createResponse.Aliases[0].Detected || createResponse.Aliases[0].Messages != 12 {
		t.Errorf("create aliases = %#v", createResponse.Aliases)
	}

	update := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", `{
		"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target-b"
	}`)
	if update.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body: %s", update.Code, update.Body.String())
	}
	updateResponse := decodePricingAliasMutationResponse(t, update)
	if len(updateResponse.Aliases) != 1 || updateResponse.Aliases[0].TargetModelID != "target-b" {
		t.Errorf("update aliases = %#v, want one target-b alias", updateResponse.Aliases)
	}
	if src.invalidations != 2 || len(cache.notified) != 2 {
		t.Errorf("after upserts invalidations/notifies = %d/%d, want 2/2", src.invalidations, len(cache.notified))
	}

	deleteResponseRecorder := performPricingAliasRequest(t, server, http.MethodDelete, "/api/v1/pricing/aliases?source=codex&provider=openai&model=custom", "")
	if deleteResponseRecorder.Code != http.StatusOK {
		t.Fatalf("delete status = %d, want 200; body: %s", deleteResponseRecorder.Code, deleteResponseRecorder.Body.String())
	}
	if deleteResponseRecorder.Header().Get("Cache-Control") != "no-store" {
		t.Errorf("delete Cache-Control = %q, want no-store", deleteResponseRecorder.Header().Get("Cache-Control"))
	}
	deleteResponse := decodePricingAliasMutationResponse(t, deleteResponseRecorder)
	if len(deleteResponse.Aliases) != 0 || deleteResponse.Aliases == nil {
		t.Errorf("delete aliases = %#v, want empty array", deleteResponse.Aliases)
	}
	if deleteResponse.Reprice != PricingChangeStatusStarted {
		t.Errorf("delete reprice = %q, want started", deleteResponse.Reprice)
	}

	idempotent := performPricingAliasRequest(t, server, http.MethodDelete, "/api/v1/pricing/aliases?source=codex&provider=openai&model=custom", "")
	if idempotent.Code != http.StatusOK {
		t.Fatalf("idempotent delete status = %d, want 200; body: %s", idempotent.Code, idempotent.Body.String())
	}
	if len(store.deletes) != 2 || src.invalidations != 4 || len(cache.notified) != 4 {
		t.Errorf("mutation counts deletes/invalidations/notifies = %d/%d/%d, want 2/4/4", len(store.deletes), src.invalidations, len(cache.notified))
	}
}

func TestPricingAliasPostValidationAndNativePrecedence(t *testing.T) {
	validBody := `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target"}`

	t.Run("strict and bounded JSON", func(t *testing.T) {
		src := newPricingAliasFakeSource(source.SourceCodex)
		src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
		store := newFakePricingAliasStore()
		server := pricingAliasTestServer(t, src, store, nil)

		tests := []struct {
			name        string
			body        string
			contentType string
			wantStatus  int
		}{
			{name: "unknown field", body: `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target","extra":true}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
			{name: "trailing object", body: validBody + `{}`, contentType: "application/json", wantStatus: http.StatusBadRequest},
			{name: "null", body: `null`, contentType: "application/json", wantStatus: http.StatusBadRequest},
			{name: "wrong media type", body: validBody, contentType: "text/plain", wantStatus: http.StatusUnsupportedMediaType},
			{name: "oversized", body: `{"source_id":"codex","provider_id":"openai","model_id":"` + strings.Repeat("x", maxPricingAliasRequestBytes) + `","target_model_id":"target"}`, contentType: "application/json", wantStatus: http.StatusRequestEntityTooLarge},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/pricing/aliases", strings.NewReader(test.body))
				req.Header.Set("Content-Type", test.contentType)
				rec := httptest.NewRecorder()
				server.Handler.ServeHTTP(rec, req)
				if rec.Code != test.wantStatus {
					t.Errorf("status = %d, want %d; body: %s", rec.Code, test.wantStatus, rec.Body.String())
				}
				if rec.Header().Get("Cache-Control") != "no-store" {
					t.Errorf("Cache-Control = %q, want no-store", rec.Header().Get("Cache-Control"))
				}
			})
		}
		if len(store.upserts) != 0 {
			t.Errorf("invalid requests wrote aliases: %#v", store.upserts)
		}
	})

	t.Run("required identifiers and source rules", func(t *testing.T) {
		tests := []struct {
			name       string
			src        source.Source
			body       string
			wantStatus int
		}{
			{name: "missing source", src: catalogSourceWithTarget(source.SourceCodex, source.SourceCodex), body: `{"provider_id":"openai","model_id":"custom","target_model_id":"target"}`, wantStatus: http.StatusBadRequest},
			{name: "missing model", src: catalogSourceWithTarget(source.SourceCodex, source.SourceCodex), body: `{"source_id":"codex","provider_id":"openai","target_model_id":"target"}`, wantStatus: http.StatusBadRequest},
			{name: "missing target", src: catalogSourceWithTarget(source.SourceCodex, source.SourceCodex), body: `{"source_id":"codex","provider_id":"openai","model_id":"custom"}`, wantStatus: http.StatusBadRequest},
			{name: "same observed and target", src: catalogSourceWithTarget(source.SourceCodex, source.SourceCodex), body: `{"source_id":"codex","provider_id":"openai","model_id":"target","target_model_id":"target"}`, wantStatus: http.StatusBadRequest},
			{name: "catalog source mismatch", src: catalogSourceWithTarget(source.SourceCodex, source.SourceClaudeCode), body: validBody, wantStatus: http.StatusBadRequest},
			{name: "unsupported source", src: newHandlerFakeSource(source.SourceCodex, true, 0), body: validBody, wantStatus: http.StatusBadRequest},
			{name: "invalid registry source", src: catalogSourceWithTarget(source.SourceClaudeCode, source.SourceClaudeCode), body: validBody, wantStatus: http.StatusBadRequest},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				store := newFakePricingAliasStore()
				server := pricingAliasTestServer(t, test.src, store, nil)
				rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", test.body)
				if rec.Code != test.wantStatus {
					t.Errorf("status = %d, want %d; body: %s", rec.Code, test.wantStatus, rec.Body.String())
				}
				if len(store.upserts) != 0 {
					t.Errorf("invalid request wrote aliases: %#v", store.upserts)
				}
			})
		}
	})

	// Name-based matching is a guess, so a model that already prices natively
	// can still be re-pointed: the user knows what a proxied model really is.
	t.Run("natively priced models stay aliasable", func(t *testing.T) {
		for _, kind := range []source.PricingResolutionKind{
			source.PricingResolutionExact,
			source.PricingResolutionNativeAlias,
			source.PricingResolutionFallback,
		} {
			t.Run(string(kind), func(t *testing.T) {
				src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
				rate := positivePricingRate()
				src.resolutions[pricingAliasTestKey{source.SourceCodex, "openai", "custom"}] = source.PricingResolution{
					SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "target", Kind: kind, Rate: &rate,
				}
				store := newFakePricingAliasStore()
				server := pricingAliasTestServer(t, src, store, nil)
				rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", validBody)
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
				}
				if len(store.upserts) != 1 || src.invalidations != 1 {
					t.Errorf("override wrote/invalidated = %d/%d, want 1/1", len(store.upserts), src.invalidations)
				}
			})
		}
	})

	t.Run("unknown and unpriced are aliasable", func(t *testing.T) {
		for _, kind := range []source.PricingResolutionKind{source.PricingResolutionUnknown, source.PricingResolutionUnpriced} {
			t.Run(string(kind), func(t *testing.T) {
				src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
				src.resolutions[pricingAliasTestKey{source.SourceCodex, "openai", "custom"}] = source.PricingResolution{Kind: kind}
				store := newFakePricingAliasStore()
				server := pricingAliasTestServer(t, src, store, nil)
				rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", validBody)
				if rec.Code != http.StatusOK {
					t.Errorf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
				}
				if len(store.upserts) != 1 {
					t.Errorf("upserts = %d, want 1", len(store.upserts))
				}
			})
		}
	})
}

func catalogSourceWithTarget(id, catalogID source.SourceID) *pricingAliasFakeSource {
	src := newPricingAliasFakeSource(id)
	src.catalog.SourceID = catalogID
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	return src
}

func TestPricingAliasTargetRulesAndProviderScoping(t *testing.T) {
	t.Run("target must be exact and usable", func(t *testing.T) {
		src := newPricingAliasFakeSource(source.SourceCodex)
		src.catalog.Models = []source.PricingCatalogModel{
			{ModelID: "target", Rate: positivePricingRate()},
			{ModelID: "zero-input", Rate: source.PricingRateSummary{OutputPerMillion: 2}},
			{ModelID: "zero-output", Rate: source.PricingRateSummary{InputPerMillion: 1}},
		}
		targets := []string{"missing", "zero-input", "zero-output"}
		for _, target := range targets {
			t.Run(target, func(t *testing.T) {
				store := newFakePricingAliasStore()
				server := pricingAliasTestServer(t, src, store, nil)
				body := `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"` + target + `"}`
				rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", body)
				if rec.Code != http.StatusBadRequest {
					t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
				}
				if len(store.upserts) != 0 {
					t.Errorf("invalid target wrote alias: %#v", store.upserts)
				}
			})
		}
	})

	t.Run("provider is part of the exact alias key", func(t *testing.T) {
		src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
		store := newFakePricingAliasStore()
		server := pricingAliasTestServer(t, src, store, nil)
		for _, provider := range []string{"", "openai", "azure"} {
			body := `{"source_id":"codex","provider_id":"` + provider + `","model_id":"custom","target_model_id":"target"}`
			rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", body)
			if rec.Code != http.StatusOK {
				t.Fatalf("provider %s status = %d, want 200; body: %s", provider, rec.Code, rec.Body.String())
			}
		}
		aliases, err := store.List(context.Background(), source.SourceCodex)
		if err != nil {
			t.Fatal(err)
		}
		if len(aliases) != 3 || len(store.upserts) != 3 {
			t.Fatalf("aliases = %#v, want three provider-scoped rows including empty provider", aliases)
		}
		providers := map[string]bool{}
		for _, alias := range aliases {
			providers[alias.ProviderID] = true
		}
		if !providers[""] || !providers["openai"] || !providers["azure"] {
			t.Errorf("provider-scoped upserts collapsed: %#v", store.upserts)
		}
	})
}

func TestPricingAliasDeleteValidation(t *testing.T) {
	src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
	store := newFakePricingAliasStore()
	server := pricingAliasTestServer(t, src, store, nil)

	paths := []string{
		"/api/v1/pricing/aliases?provider=openai&model=custom",
		"/api/v1/pricing/aliases?source=codex&model=custom",
		"/api/v1/pricing/aliases?source=codex&provider=openai",
		"/api/v1/pricing/aliases?source=missing&provider=openai&model=custom",
	}
	for _, path := range paths {
		rec := performPricingAliasRequest(t, server, http.MethodDelete, path, "")
		if rec.Code != http.StatusBadRequest {
			t.Errorf("DELETE %s status = %d, want 400; body: %s", path, rec.Code, rec.Body.String())
		}
	}
	if len(store.deletes) != 0 || src.invalidations != 0 {
		t.Errorf("invalid deletes reached store/source: %d/%d", len(store.deletes), src.invalidations)
	}

	emptyProvider := performPricingAliasRequest(t, server, http.MethodDelete, "/api/v1/pricing/aliases?source=codex&provider=&model=custom", "")
	if emptyProvider.Code != http.StatusOK {
		t.Fatalf("DELETE empty provider status = %d, want 200; body: %s", emptyProvider.Code, emptyProvider.Body.String())
	}
	if len(store.deletes) != 1 || store.deletes[0].providerID != "" || src.invalidations != 1 {
		t.Errorf("empty-provider delete = %#v, invalidations=%d", store.deletes, src.invalidations)
	}
}

func TestPricingAliasStoreAvailabilityAndErrors(t *testing.T) {
	src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
	validBody := `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target"}`

	t.Run("missing store only disables mutations", func(t *testing.T) {
		server := pricingAliasTestServer(t, src, nil, nil)
		get := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
		if get.Code != http.StatusOK {
			t.Fatalf("GET status = %d, want 200", get.Code)
		}
		response := decodePricingAliasesResponse(t, get)
		if response.Aliases == nil || len(response.Aliases) != 0 {
			t.Errorf("GET aliases = %#v, want empty array", response.Aliases)
		}
		post := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", validBody)
		if post.Code != http.StatusServiceUnavailable {
			t.Errorf("POST status = %d, want 503", post.Code)
		}
		deleteRec := performPricingAliasRequest(t, server, http.MethodDelete, "/api/v1/pricing/aliases?source=codex&provider=openai&model=custom", "")
		if deleteRec.Code != http.StatusServiceUnavailable {
			t.Errorf("DELETE status = %d, want 503", deleteRec.Code)
		}
	})

	t.Run("list failure is internal error", func(t *testing.T) {
		store := newFakePricingAliasStore()
		store.listErr = errors.New("list failed")
		server := pricingAliasTestServer(t, src, store, nil)
		rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("canceled list ends without an error response", func(t *testing.T) {
		store := newFakePricingAliasStore()
		store.listErr = context.Canceled
		server := pricingAliasTestServer(t, src, store, nil)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := httptest.NewRequest(http.MethodGet, "/api/v1/pricing/aliases?source=codex", nil).WithContext(ctx)
		rec := httptest.NewRecorder()

		server.Handler.ServeHTTP(rec, req)

		if rec.Body.Len() != 0 {
			t.Fatalf("canceled request body = %q, want no error response", rec.Body.String())
		}
	})

	t.Run("store validation error remains bad request", func(t *testing.T) {
		store := newFakePricingAliasStore()
		store.upsertErr = &pricingalias.ValidationError{Field: "model_id", Cause: pricingalias.ErrIdentifierRequired}
		server := pricingAliasTestServer(t, src, store, nil)
		rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", validBody)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
		}
		if src.invalidations != 0 {
			t.Errorf("invalidations = %d, want 0 after failed write", src.invalidations)
		}
	})

	t.Run("generic write error is internal error", func(t *testing.T) {
		store := newFakePricingAliasStore()
		store.deleteErr = errors.New("disk failed")
		server := pricingAliasTestServer(t, src, store, nil)
		rec := performPricingAliasRequest(t, server, http.MethodDelete, "/api/v1/pricing/aliases?source=codex&provider=openai&model=custom", "")
		if rec.Code != http.StatusInternalServerError {
			t.Errorf("status = %d, want 500; body: %s", rec.Code, rec.Body.String())
		}
	})
}

func TestPricingAliasNotifierStates(t *testing.T) {
	validBody := `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target"}`
	tests := []struct {
		name       string
		cache      CacheManager
		wantStatus PricingChangeStatus
		wantCalls  int
	}{
		{name: "started", cache: newFakePricingChangeCache(PricingChangeStatusStarted), wantStatus: PricingChangeStatusStarted, wantCalls: 1},
		{name: "queued", cache: newFakePricingChangeCache(PricingChangeStatusQueued), wantStatus: PricingChangeStatusQueued, wantCalls: 1},
		{name: "disabled", cache: newFakePricingChangeCache(PricingChangeStatusDisabled), wantStatus: PricingChangeStatusDisabled, wantCalls: 1},
		{name: "notifier reports unavailable", cache: newFakePricingChangeCache(PricingChangeStatusUnavailable), wantStatus: PricingChangeStatusUnavailable, wantCalls: 1},
		{name: "cache lacks notifier", cache: &fakeCacheManager{}, wantStatus: PricingChangeStatusUnavailable},
		{name: "cache absent", cache: nil, wantStatus: PricingChangeStatusUnavailable},
		{name: "invalid notifier status", cache: newFakePricingChangeCache(PricingChangeStatus("unexpected")), wantStatus: PricingChangeStatusUnavailable, wantCalls: 1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
			store := newFakePricingAliasStore()
			server := pricingAliasTestServer(t, src, store, test.cache)
			rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", validBody)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
			}
			response := decodePricingAliasMutationResponse(t, rec)
			if response.Reprice != test.wantStatus {
				t.Errorf("reprice = %q, want %q", response.Reprice, test.wantStatus)
			}
			if len(store.upserts) != 1 || src.invalidations != 1 {
				t.Errorf("successful mutation upserts/invalidations = %d/%d, want 1/1", len(store.upserts), src.invalidations)
			}
			if notifier, ok := test.cache.(*fakePricingChangeCache); ok {
				if len(notifier.notified) != test.wantCalls || (len(notifier.notified) == 1 && notifier.notified[0] != source.SourceCodex) {
					t.Errorf("notifier calls = %#v, want %d call for codex", notifier.notified, test.wantCalls)
				}
			}
		})
	}
}

func TestPricingAliasDeleteUsesExactEscapedKey(t *testing.T) {
	src := catalogSourceWithTarget(source.SourceCodex, source.SourceCodex)
	store := newFakePricingAliasStore(pricingalias.Alias{
		SourceID: source.SourceCodex, ProviderID: "provider/scope", ModelID: "model with space", TargetModelID: "target",
	})
	server := pricingAliasTestServer(t, src, store, nil)
	path := "/api/v1/pricing/aliases?source=codex&provider=" + url.QueryEscape("provider/scope") + "&model=" + url.QueryEscape("model with space")
	rec := performPricingAliasRequest(t, server, http.MethodDelete, path, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(store.deletes) != 1 || store.deletes[0].providerID != "provider/scope" || store.deletes[0].modelID != "model with space" {
		t.Errorf("delete keys = %#v, want exact decoded provider/model", store.deletes)
	}
}

// A user alias outranks native pricing, so an alias that displaced a usable
// native match stays active and editable and is flagged as an override. Only a
// target that no longer exists makes an alias stop applying.
func TestPricingAliasesReportOverrideAndInvalidTargets(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	src.models = []stats.ModelEntry{
		{ProviderID: "openai", ModelID: "overrides-native", Messages: 7},
		{ProviderID: "openai", ModelID: "still-aliased", Messages: 5},
	}
	rate := positivePricingRate()
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "openai", "overrides-native"}] = source.PricingResolution{
		SourceID: source.SourceCodex, TargetSourceID: source.SourceCodex, ProviderID: "openai", ModelID: "overrides-native",
		TargetModelID: "target", Kind: source.PricingResolutionUserAlias, OverridesNative: true, Rate: &rate,
	}
	src.resolutions[pricingAliasTestKey{source.SourceCodex, "openai", "still-aliased"}] = source.PricingResolution{
		SourceID: source.SourceCodex, TargetSourceID: source.SourceCodex, ProviderID: "openai", ModelID: "still-aliased",
		TargetModelID: "target", Kind: source.PricingResolutionUserAlias, Rate: &rate,
	}
	store := newFakePricingAliasStore(
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "overrides-native", TargetSourceID: source.SourceCodex, TargetModelID: "target"},
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "still-aliased", TargetSourceID: source.SourceCodex, TargetModelID: "target"},
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "gone", TargetSourceID: source.SourceCodex, TargetModelID: "retired-target"},
	)
	server := pricingAliasTestServer(t, src, store, nil)

	response := decodePricingAliasesResponse(t, performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", ""))
	if !response.Writable {
		t.Error("writable = false, want true when an alias store is configured")
	}
	byModel := make(map[string]PricingAliasEntry, len(response.Aliases))
	for _, alias := range response.Aliases {
		byModel[alias.ModelID] = alias
	}

	override := byModel["overrides-native"]
	if override.State != PricingAliasStateActive || !override.Active || !override.Editable || !override.OverridesNative {
		t.Errorf("overriding alias = %#v, want an editable active override", override)
	}
	if override.StateReason == "" {
		t.Error("overriding alias has no explanation")
	}

	active := byModel["still-aliased"]
	if active.State != PricingAliasStateActive || !active.Active || !active.Editable || !active.TargetValid || active.Messages != 5 {
		t.Errorf("active alias = %#v, want editable active state with activity", active)
	}
	if active.OverridesNative {
		t.Errorf("alias on an unpriced model reported as an override: %#v", active)
	}

	stale := byModel["gone"]
	if stale.State != PricingAliasStateTargetMissing || stale.TargetValid || !stale.Editable {
		t.Errorf("stale-target alias = %#v, want editable target_missing state", stale)
	}

	// Every observed model is listed now, but only the ones without usable
	// pricing are marked unresolved.
	byObserved := make(map[string]PricingAliasObservedModel, len(response.ObservedModels))
	for _, observed := range response.ObservedModels {
		byObserved[observed.ModelID] = observed
	}
	if got, ok := byObserved["overrides-native"]; !ok || !got.Resolved || !got.Aliasable {
		t.Errorf("priced observed model = %#v, want a resolved but still aliasable row", got)
	}
}

// A failed observation read must not hide alias management: the catalog and the
// saved aliases still render, with the degradation reported.
func TestPricingAliasesDegradeWhenObservedModelsFail(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	src.modelsErr = errors.New("observation timeout")
	store := newFakePricingAliasStore(
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "target"},
	)
	server := pricingAliasTestServer(t, src, store, nil)

	rec := performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	response := decodePricingAliasesResponse(t, rec)
	if !response.Supported || response.ObservationError == "" {
		t.Fatalf("degraded response = supported %v observation_error %q", response.Supported, response.ObservationError)
	}
	if len(response.Aliases) != 1 || response.Aliases[0].Detected {
		t.Errorf("aliases = %#v, want the saved alias without detection data", response.Aliases)
	}
	if len(response.ObservedModels) != 0 {
		t.Errorf("unresolved models = %#v, want none when observation failed", response.ObservedModels)
	}
}

// A committed mutation must never be reported as a failure just because the
// refreshed view could not be rebuilt; otherwise the UI invites a duplicate write.
func TestPricingAliasMutationSurvivesRefreshFailure(t *testing.T) {
	for _, test := range []struct {
		name   string
		method string
		target string
		body   string
	}{
		{name: "upsert", method: http.MethodPost, target: "/api/v1/pricing/aliases", body: `{"source_id":"codex","provider_id":"openai","model_id":"custom","target_model_id":"target"}`},
		{name: "delete", method: http.MethodDelete, target: "/api/v1/pricing/aliases?source=codex&provider=openai&model=custom", body: ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			src := newPricingAliasFakeSource(source.SourceCodex)
			src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
			store := newFakePricingAliasStore()
			cache := newFakePricingChangeCache(PricingChangeStatusStarted)
			server := pricingAliasTestServer(t, src, store, cache)

			store.listErr = errors.New("settings database is busy")
			rec := performPricingAliasRequest(t, server, test.method, test.target, test.body)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 for a committed mutation; body: %s", rec.Code, rec.Body.String())
			}
			response := decodePricingAliasMutationResponse(t, rec)
			if response.RefreshError == "" {
				t.Error("refresh_error is empty, want the refresh failure reported")
			}
			if response.Reprice != PricingChangeStatusStarted {
				t.Errorf("reprice = %q, want started", response.Reprice)
			}
			if response.SourceID != source.SourceCodex || response.Aliases == nil || response.ObservedModels == nil {
				t.Errorf("degraded mutation response = %#v, want source metadata with empty arrays", response.PricingAliasesResponse)
			}
			if len(cache.notified) != 1 {
				t.Errorf("repricing notifications = %d, want 1", len(cache.notified))
			}
		})
	}
}

// provider_id must be sent explicitly: an omitted field would silently create
// an unknown-provider alias the user never chose.
func TestPricingAliasUpsertRequiresExplicitProvider(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	store := newFakePricingAliasStore()
	server := pricingAliasTestServer(t, src, store, nil)

	omitted := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", `{"source_id":"codex","model_id":"custom","target_model_id":"target"}`)
	if omitted.Code != http.StatusBadRequest {
		t.Fatalf("omitted provider status = %d, want 400; body: %s", omitted.Code, omitted.Body.String())
	}
	if len(store.upserts) != 0 {
		t.Fatalf("omitted provider wrote aliases: %#v", store.upserts)
	}

	explicit := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", `{"source_id":"codex","provider_id":"","model_id":"custom","target_model_id":"target"}`)
	if explicit.Code != http.StatusOK {
		t.Fatalf("explicit empty provider status = %d, want 200; body: %s", explicit.Code, explicit.Body.String())
	}
	if len(store.upserts) != 1 || store.upserts[0].ProviderID != "" {
		t.Errorf("explicit empty provider upserts = %#v", store.upserts)
	}
}

// A source with a fixed provider identity (Claude Code prices everything as
// anthropic) must reject alias keys stored under any other provider: they would
// persist successfully and then never apply.
func TestPricingAliasUpsertRejectsUnmatchableProvider(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceClaudeCode)
	src.catalog = source.PricingCatalog{
		SourceID: source.SourceClaudeCode,
		Models:   []source.PricingCatalogModel{{ModelID: "claude-sonnet-4-5", Rate: positivePricingRate()}},
	}
	// The fake mirrors claudecode.ResolvePricing, which always answers as anthropic.
	for _, provider := range []string{"", "bedrock", "anthropic"} {
		src.resolutions[pricingAliasTestKey{source.SourceClaudeCode, provider, "custom"}] = source.PricingResolution{
			SourceID: source.SourceClaudeCode, ProviderID: "anthropic", ModelID: "custom", Kind: source.PricingResolutionUnknown,
		}
	}
	store := newFakePricingAliasStore()
	server := pricingAliasTestServer(t, src, store, nil)

	for _, provider := range []string{"bedrock", ""} {
		body := `{"source_id":"claude_code","provider_id":"` + provider + `","model_id":"custom","target_model_id":"claude-sonnet-4-5"}`
		rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", body)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("provider %q status = %d, want 400; body: %s", provider, rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "anthropic") {
			t.Errorf("provider %q rejection does not name the resolving provider: %s", provider, rec.Body.String())
		}
	}
	if len(store.upserts) != 0 {
		t.Fatalf("unmatchable providers wrote aliases: %#v", store.upserts)
	}

	matching := `{"source_id":"claude_code","provider_id":"anthropic","model_id":"custom","target_model_id":"claude-sonnet-4-5"}`
	rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", matching)
	if rec.Code != http.StatusOK {
		t.Fatalf("matching provider status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(store.upserts) != 1 || store.upserts[0].ProviderID != "anthropic" {
		t.Errorf("matching provider upserts = %#v", store.upserts)
	}
}

// pricingAliasMultiSourceServer registers several catalog sources so an alias
// can target a catalog other than the one it observes.
func pricingAliasMultiSourceServer(t *testing.T, store PricingAliasStore, sources ...source.Source) *http.Server {
	t.Helper()
	registry := source.NewRegistry(sources[0].Info(context.Background()).ID)
	for _, src := range sources {
		if err := registry.Register(src); err != nil {
			t.Fatalf("register source: %v", err)
		}
	}
	return NewServer(ServerOptions{Registry: registry, PricingAliases: store})
}

// The screenshot case end to end: Claude Code observes gpt-5.6-sol, which only
// the Codex catalog can price. The response must offer that catalog and the
// upsert must accept it.
func TestPricingAliasesOfferAndAcceptCrossSourceTargets(t *testing.T) {
	claude := newPricingAliasFakeSource(source.SourceClaudeCode)
	claude.catalog.Models = []source.PricingCatalogModel{{ModelID: "claude-opus-5", Rate: positivePricingRate()}}
	claude.models = []stats.ModelEntry{{ProviderID: "anthropic", ModelID: "gpt-5.6-sol", Sessions: 3, Messages: 1754}}

	codexSrc := newPricingAliasFakeSource(source.SourceCodex)
	codexSrc.catalog.Models = []source.PricingCatalogModel{
		{ModelID: "gpt-5.6-sol", Rate: source.PricingRateSummary{InputPerMillion: 5, CachedInputPerMillion: 0.5, CacheWritePerMillion: 6.25, OutputPerMillion: 30}},
		{ModelID: "unpriced", Rate: source.PricingRateSummary{}},
	}

	store := newFakePricingAliasStore()
	server := pricingAliasMultiSourceServer(t, store, claude, codexSrc)

	response := decodePricingAliasesResponse(t, performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=claude_code", ""))
	if len(response.Catalogs) != 2 {
		t.Fatalf("catalogs = %#v, want both sources", response.Catalogs)
	}
	byID := make(map[source.SourceID]PricingAliasCatalog, len(response.Catalogs))
	for _, catalog := range response.Catalogs {
		byID[catalog.SourceID] = catalog
	}
	foreign, ok := byID[source.SourceCodex]
	if !ok || len(foreign.Models) != 2 {
		t.Fatalf("codex catalog = %#v, want it offered to claude_code", foreign)
	}
	if !foreign.Models[0].Targetable || foreign.Models[1].Targetable {
		t.Errorf("codex targetable flags = %#v", foreign.Models)
	}
	if foreign.Models[0].Rate.CacheWritePerMillion != 6.25 {
		t.Errorf("cache write rate missing from the offered target: %#v", foreign.Models[0].Rate)
	}
	// The selected source's own catalog stays separately identified.
	if response.Catalog.SourceID != source.SourceClaudeCode {
		t.Errorf("catalog = %#v, want the selected source's own catalog", response.Catalog)
	}

	body := `{"source_id":"claude_code","provider_id":"anthropic","model_id":"gpt-5.6-sol","target_source_id":"codex","target_model_id":"gpt-5.6-sol"}`
	rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", body)
	if rec.Code != http.StatusOK {
		t.Fatalf("cross-source upsert status = %d, want 200; body: %s", rec.Code, rec.Body.String())
	}
	if len(store.upserts) != 1 || store.upserts[0].TargetSourceID != source.SourceCodex || store.upserts[0].TargetModelID != "gpt-5.6-sol" {
		t.Fatalf("stored alias = %#v, want a codex target", store.upserts)
	}
	// The observed and target model ids are identical here; only the self-alias
	// within one source is meaningless, so this must not be rejected.
	if store.upserts[0].SourceID != source.SourceClaudeCode {
		t.Errorf("stored alias source = %q", store.upserts[0].SourceID)
	}
}

func TestPricingAliasCrossSourceTargetValidation(t *testing.T) {
	claude := newPricingAliasFakeSource(source.SourceClaudeCode)
	claude.catalog.Models = []source.PricingCatalogModel{{ModelID: "claude-opus-5", Rate: positivePricingRate()}}

	codexSrc := newPricingAliasFakeSource(source.SourceCodex)
	codexSrc.catalog.Currency = "USD"
	codexSrc.catalog.Models = []source.PricingCatalogModel{
		{ModelID: "gpt-5.6", Rate: positivePricingRate()},
		{ModelID: "unpriced", Rate: source.PricingRateSummary{InputPerMillion: 1}},
	}

	kimi := newPricingAliasFakeSource(source.SourceKimiCode)
	kimi.catalog.Currency = "CNY"
	kimi.catalog.Models = []source.PricingCatalogModel{{ModelID: "kimi-k3", Rate: positivePricingRate()}}

	empty := newPricingAliasFakeSource(source.SourceQwenCode)

	for _, test := range []struct {
		name string
		body string
	}{
		{name: "target model not in the other catalog", body: `{"source_id":"claude_code","provider_id":"a","model_id":"m","target_source_id":"codex","target_model_id":"gpt-9"}`},
		{name: "target model is unpriced", body: `{"source_id":"claude_code","provider_id":"a","model_id":"m","target_source_id":"codex","target_model_id":"unpriced"}`},
		{name: "target source is unknown", body: `{"source_id":"claude_code","provider_id":"a","model_id":"m","target_source_id":"nope","target_model_id":"gpt-5.6"}`},
		{name: "target source has no catalog", body: `{"source_id":"claude_code","provider_id":"a","model_id":"m","target_source_id":"qwen_code","target_model_id":"gpt-5.6"}`},
		// Rates are per-million values in their own currency; mixing them would
		// silently produce a meaningless total.
		{name: "target prices in another currency", body: `{"source_id":"claude_code","provider_id":"a","model_id":"m","target_source_id":"kimi_code","target_model_id":"kimi-k3"}`},
		{name: "self alias within one source", body: `{"source_id":"claude_code","provider_id":"a","model_id":"claude-opus-5","target_source_id":"claude_code","target_model_id":"claude-opus-5"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := newFakePricingAliasStore()
			server := pricingAliasMultiSourceServer(t, store, claude, codexSrc, kimi, empty)
			rec := performPricingAliasRequest(t, server, http.MethodPost, "/api/v1/pricing/aliases", test.body)
			if rec.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body: %s", rec.Code, rec.Body.String())
			}
			if len(store.upserts) != 0 {
				t.Errorf("invalid cross-source request wrote aliases: %#v", store.upserts)
			}
		})
	}
}

// A saved cross-source alias must be validated against the catalog it actually
// targets, not against the observing source's own catalog.
func TestPricingAliasEntryValidatesAgainstTheTargetCatalog(t *testing.T) {
	claude := newPricingAliasFakeSource(source.SourceClaudeCode)
	claude.catalog.Models = []source.PricingCatalogModel{{ModelID: "claude-opus-5", Rate: positivePricingRate()}}
	claude.models = []stats.ModelEntry{{ProviderID: "anthropic", ModelID: "gpt-5.6-sol", Messages: 5}}
	rate := positivePricingRate()
	claude.resolutions[pricingAliasTestKey{source.SourceClaudeCode, "anthropic", "gpt-5.6-sol"}] = source.PricingResolution{
		SourceID: source.SourceClaudeCode, TargetSourceID: source.SourceCodex, ProviderID: "anthropic",
		ModelID: "gpt-5.6-sol", TargetModelID: "gpt-5.6-sol", Kind: source.PricingResolutionUserAlias, Rate: &rate,
	}

	codexSrc := newPricingAliasFakeSource(source.SourceCodex)
	codexSrc.catalog.Models = []source.PricingCatalogModel{{ModelID: "gpt-5.6-sol", Rate: positivePricingRate()}}

	store := newFakePricingAliasStore(
		pricingalias.Alias{SourceID: source.SourceClaudeCode, ProviderID: "anthropic", ModelID: "gpt-5.6-sol", TargetSourceID: source.SourceCodex, TargetModelID: "gpt-5.6-sol"},
		pricingalias.Alias{SourceID: source.SourceClaudeCode, ProviderID: "anthropic", ModelID: "stale", TargetSourceID: source.SourceCodex, TargetModelID: "retired"},
	)
	server := pricingAliasMultiSourceServer(t, store, claude, codexSrc)

	response := decodePricingAliasesResponse(t, performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=claude_code", ""))
	byModel := make(map[string]PricingAliasEntry, len(response.Aliases))
	for _, alias := range response.Aliases {
		byModel[alias.ModelID] = alias
	}

	// gpt-5.6-sol is absent from Claude's own catalog, so validating there would
	// wrongly report the alias as broken.
	active := byModel["gpt-5.6-sol"]
	if active.State != PricingAliasStateActive || !active.TargetValid || !active.Active {
		t.Errorf("cross-source alias = %#v, want an active, valid target", active)
	}
	if !strings.Contains(active.StateReason, string(source.SourceCodex)) {
		t.Errorf("cross-source state reason does not name the catalog: %q", active.StateReason)
	}

	stale := byModel["stale"]
	if stale.State != PricingAliasStateTargetMissing || stale.TargetValid {
		t.Errorf("stale cross-source alias = %#v, want target_missing", stale)
	}
	if !strings.Contains(stale.StateReason, string(source.SourceCodex)) {
		t.Errorf("target_missing reason does not name the target source: %q", stale.StateReason)
	}
}

// An alias written before cross-source targets existed carries no target source
// and must keep resolving against the source that observed it.
func TestPricingAliasEntryDefaultsBlankTargetSourceToItsOwnSource(t *testing.T) {
	src := newPricingAliasFakeSource(source.SourceCodex)
	src.catalog.Models = []source.PricingCatalogModel{{ModelID: "target", Rate: positivePricingRate()}}
	store := newFakePricingAliasStore(
		pricingalias.Alias{SourceID: source.SourceCodex, ProviderID: "openai", ModelID: "custom", TargetModelID: "target"},
	)
	server := pricingAliasTestServer(t, src, store, nil)

	response := decodePricingAliasesResponse(t, performPricingAliasRequest(t, server, http.MethodGet, "/api/v1/pricing/aliases?source=codex", ""))
	if len(response.Aliases) != 1 {
		t.Fatalf("aliases = %#v, want one", response.Aliases)
	}
	alias := response.Aliases[0]
	if alias.TargetSourceID != source.SourceCodex || !alias.TargetValid || alias.Foreign() {
		t.Fatalf("legacy alias = %#v, want a same-source target with a valid catalog match", alias)
	}
}
