package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"sort"
	"strings"
	"time"

	"opencode-dashboard/internal/pricingalias"
	"opencode-dashboard/internal/source"
	"opencode-dashboard/internal/stats"
)

const (
	maxPricingAliasRequestBytes = 16 << 10
	pricingObservationTimeout   = 60 * time.Second
)

// PricingAliasStore is the writable subset of the durable pricing-alias store
// used by the HTTP API. Keeping this contract local lets handlers be tested
// without opening the settings database.
type PricingAliasStore interface {
	List(context.Context, source.SourceID) ([]pricingalias.Alias, error)
	Upsert(context.Context, pricingalias.Alias) (pricingalias.Alias, error)
	Delete(context.Context, source.SourceID, string, string) error
}

// PricingChangeStatus reports what happened to historical cache repricing after
// an alias mutation. Alias persistence is independent from this best-effort
// notification.
type PricingChangeStatus string

const (
	PricingChangeStatusStarted     PricingChangeStatus = "started"
	PricingChangeStatusQueued      PricingChangeStatus = "queued"
	PricingChangeStatusDisabled    PricingChangeStatus = "disabled"
	PricingChangeStatusUnavailable PricingChangeStatus = "unavailable"
)

// PricingChangeNotifier is an optional capability of the existing cache
// runtime. A cache implementation can use it to start or queue historical
// repricing after the source's pricing identity changes.
type PricingChangeNotifier interface {
	NotifyPricingChange(context.Context, source.SourceID) PricingChangeStatus
}

type PricingAliasCatalogModel struct {
	source.PricingCatalogModel
	Targetable bool `json:"targetable"`
}

type PricingAliasCatalog struct {
	SourceID source.SourceID `json:"source_id"`
	// SourceLabel is the human-readable source name, so the picker can group
	// targets without a second lookup against /api/v1/sources.
	SourceLabel string                     `json:"source_label,omitempty"`
	SnapshotID  string                     `json:"snapshot_id,omitempty"`
	Currency    string                     `json:"currency,omitempty"`
	Models      []PricingAliasCatalogModel `json:"models"`
	Note        string                     `json:"note,omitempty"`
}

// PricingAliasObservedModel is one provider/model pair the source has actually
// seen. Every observed model is listed, not just unpriced ones: a user alias
// outranks native pricing, so a model that resolves today can still be
// deliberately re-pointed at the catalog entry it really corresponds to.
type PricingAliasObservedModel struct {
	SourceID         source.SourceID              `json:"source_id"`
	ProviderID       string                       `json:"provider_id"`
	ModelID          string                       `json:"model_id"`
	Sessions         int64                        `json:"sessions"`
	Messages         int64                        `json:"messages"`
	Tokens           stats.TokenStats             `json:"tokens"`
	ResolutionKind   source.PricingResolutionKind `json:"resolution_kind"`
	ResolutionReason string                       `json:"resolution_reason"`
	ResolutionNote   string                       `json:"resolution_note,omitempty"`
	// Resolved reports that the model already has usable pricing without a user
	// alias, which the UI uses to separate "needs pricing" from "priced".
	Resolved  bool `json:"resolved"`
	Aliasable bool `json:"aliasable"`
}

type PricingAliasState string

const (
	PricingAliasStateActive        PricingAliasState = "active"
	PricingAliasStateNotDetected   PricingAliasState = "not_detected"
	PricingAliasStateTargetMissing PricingAliasState = "target_missing"
	PricingAliasStateIneffective   PricingAliasState = "ineffective"
)

type PricingAliasEntry struct {
	pricingalias.Alias
	Detected    bool              `json:"detected"`
	Sessions    int64             `json:"sessions"`
	Messages    int64             `json:"messages"`
	Tokens      stats.TokenStats  `json:"tokens"`
	State       PricingAliasState `json:"state"`
	StateReason string            `json:"state_reason"`
	Active      bool              `json:"active"`
	Editable    bool              `json:"editable"`
	TargetValid bool              `json:"target_valid"`
	// OverridesNative reports that this alias displaced usable native pricing.
	OverridesNative bool                         `json:"overrides_native"`
	ResolutionKind  source.PricingResolutionKind `json:"resolution_kind,omitempty"`
}

type PricingAliasesResponse struct {
	SourceID         source.SourceID `json:"source_id"`
	Supported        bool            `json:"supported"`
	Writable         bool            `json:"writable"`
	ObservationError string          `json:"observation_error,omitempty"`
	// Catalog is the selected source's own catalog, kept separate because its
	// snapshot id and note describe how this source prices its own models.
	Catalog PricingAliasCatalog `json:"catalog"`
	// Catalogs holds every source's bundled catalog, including the selected
	// one. An alias may target any of them: a CLI often reports models another
	// vendor prices, and only that vendor's catalog has the right rates.
	Catalogs       []PricingAliasCatalog       `json:"catalogs"`
	Aliases        []PricingAliasEntry         `json:"aliases"`
	ObservedModels []PricingAliasObservedModel `json:"observed_models"`
}

type PricingAliasMutationResponse struct {
	PricingAliasesResponse
	Reprice PricingChangeStatus `json:"reprice"`
	// RefreshError reports that the alias change was committed but the refreshed
	// view could not be rebuilt. The mutation itself still succeeded.
	RefreshError string `json:"refresh_error,omitempty"`
}

type pricingAliasInput struct {
	SourceID string `json:"source_id"`
	// ProviderID is a pointer so an omitted field is rejected while an explicit
	// empty string stays a deliberate unknown-provider key.
	ProviderID *string `json:"provider_id"`
	ModelID    string  `json:"model_id"`
	// TargetSourceID is optional: omitting it means "price from my own catalog".
	TargetSourceID *string `json:"target_source_id"`
	TargetModelID  string  `json:"target_model_id"`
}

func (h *Handlers) PricingAliases(w http.ResponseWriter, r *http.Request) {
	selected, ok := h.sourceForRequest(w, r)
	if !ok {
		return
	}
	response, err := h.buildPricingAliasesResponse(r.Context(), selected)
	if err != nil {
		h.logger.Error("failed to list pricing aliases", "source", selected.Info(r.Context()).ID, "error", err)
		InternalError("failed to list pricing aliases").Write(w)
		return
	}
	writeJSONNoStore(w, http.StatusOK, response)
}

func (h *Handlers) PricingAliasUpsert(w http.ResponseWriter, r *http.Request) {
	input, ok := decodePricingAliasInput(w, r)
	if !ok {
		return
	}
	if h.pricingAliases == nil {
		ServiceUnavailable("pricing alias persistence is unavailable").Write(w)
		return
	}

	if input.ProviderID == nil {
		BadRequest("provider_id is required (use an empty value for an unknown provider)").Write(w)
		return
	}
	alias := pricingalias.Alias{
		SourceID:      source.SourceID(strings.TrimSpace(input.SourceID)),
		ProviderID:    strings.TrimSpace(*input.ProviderID),
		ModelID:       strings.TrimSpace(input.ModelID),
		TargetModelID: strings.TrimSpace(input.TargetModelID),
	}
	if input.TargetSourceID != nil {
		alias.TargetSourceID = source.SourceID(strings.TrimSpace(*input.TargetSourceID))
	}
	if alias.SourceID == "" {
		BadRequest("source_id is required").Write(w)
		return
	}
	if alias.TargetSourceID == "" {
		alias.TargetSourceID = alias.SourceID
	}
	if alias.ModelID == "" {
		BadRequest("model_id is required").Write(w)
		return
	}
	if alias.TargetModelID == "" {
		BadRequest("target_model_id is required").Write(w)
		return
	}
	if alias.TargetSourceID == alias.SourceID && alias.ModelID == alias.TargetModelID {
		BadRequest("an alias cannot point a model at itself").Write(w)
		return
	}

	selected, err := h.registry.Resolve(string(alias.SourceID))
	if err != nil {
		SourceError(err).Write(w)
		return
	}
	selectedID := selected.Info(r.Context()).ID
	if selectedID != alias.SourceID {
		BadRequest("source_id does not match the resolved source").Write(w)
		return
	}
	catalogSource, supported := selected.(source.PricingCatalogSource)
	if !supported {
		BadRequest("pricing aliases are not supported for source " + string(alias.SourceID)).Write(w)
		return
	}
	catalog := catalogSource.PricingCatalog(r.Context())
	if catalog.SourceID != alias.SourceID {
		BadRequest("source_id does not match the pricing catalog source").Write(w)
		return
	}
	if len(catalog.Models) == 0 {
		BadRequest("pricing aliases are not supported for source " + string(alias.SourceID)).Write(w)
		return
	}

	resolution := catalogSource.ResolvePricing(r.Context(), alias.ProviderID, alias.ModelID)
	if resolution.ProviderID != "" && resolution.ProviderID != alias.ProviderID {
		// The source resolves pricing under a fixed provider identity, so an alias
		// stored under a different provider key could never be applied.
		BadRequest(fmt.Sprintf("provider_id %q cannot match source %s, which prices models as provider %q",
			alias.ProviderID, alias.SourceID, resolution.ProviderID)).Write(w)
		return
	}
	// A model that already prices natively is still aliasable: name-based
	// matching guesses, and the user is the authority on what a proxied model
	// actually is. Only a source whose own catalog failed to load is refused,
	// because that is a broken snapshot rather than a mapping problem.
	if resolution.Kind == source.PricingResolutionUnavailable {
		BadRequest("pricing resolution is unavailable for the observed model").Write(w)
		return
	}
	targetCatalog, apiErr := h.pricingAliasTargetCatalog(r.Context(), alias.TargetSourceID)
	if apiErr != nil {
		apiErr.Write(w)
		return
	}
	if !catalogHasTarget(targetCatalog, alias.TargetModelID) {
		BadRequest(fmt.Sprintf("target_model_id must be an exact %s catalog model with positive input and output rates",
			alias.TargetSourceID)).Write(w)
		return
	}
	// Every rate is a per-million value in its catalog's own currency, so
	// borrowing across currencies would silently mix units into one total.
	if left, right := catalogCurrency(catalog), catalogCurrency(targetCatalog); left != right {
		BadRequest(fmt.Sprintf("source %s prices in %s but target source %s prices in %s",
			alias.SourceID, left, alias.TargetSourceID, right)).Write(w)
		return
	}

	if _, err := h.pricingAliases.Upsert(r.Context(), alias); err != nil {
		h.writePricingAliasStoreError(w, "upsert", alias.SourceID, err)
		return
	}
	h.invalidatePricingSource(selected)
	reprice := h.notifyPricingChange(r.Context(), alias.SourceID)
	h.writePricingAliasMutation(w, r, "upsert", selected, alias.SourceID, reprice)
}

func (h *Handlers) PricingAliasDelete(w http.ResponseWriter, r *http.Request) {
	if h.pricingAliases == nil {
		ServiceUnavailable("pricing alias persistence is unavailable").Write(w)
		return
	}

	query := r.URL.Query()
	sourceID := source.SourceID(strings.TrimSpace(query.Get("source")))
	providerID := strings.TrimSpace(query.Get("provider"))
	modelID := strings.TrimSpace(query.Get("model"))
	if sourceID == "" {
		BadRequest("source is required").Write(w)
		return
	}
	if !query.Has("provider") {
		BadRequest("provider is required (use an empty value for an unknown provider)").Write(w)
		return
	}
	if modelID == "" {
		BadRequest("model is required").Write(w)
		return
	}

	selected, err := h.registry.Resolve(string(sourceID))
	if err != nil {
		SourceError(err).Write(w)
		return
	}
	selectedID := selected.Info(r.Context()).ID
	if selectedID != sourceID {
		BadRequest("source does not match the resolved source").Write(w)
		return
	}
	if err := h.pricingAliases.Delete(r.Context(), sourceID, providerID, modelID); err != nil {
		h.writePricingAliasStoreError(w, "delete", sourceID, err)
		return
	}
	h.invalidatePricingSource(selected)
	reprice := h.notifyPricingChange(r.Context(), sourceID)
	h.writePricingAliasMutation(w, r, "delete", selected, sourceID, reprice)
}

// writePricingAliasMutation answers a committed mutation. The refreshed view is
// best effort: the durable change already happened, so a failed rebuild is
// reported inside a success response instead of a 500 that reads as "not saved"
// and invites a duplicate retry.
func (h *Handlers) writePricingAliasMutation(w http.ResponseWriter, r *http.Request, operation string, selected source.Source, sourceID source.SourceID, reprice PricingChangeStatus) {
	result := PricingAliasMutationResponse{Reprice: reprice}
	response, err := h.buildPricingAliasesResponse(r.Context(), selected)
	if err != nil {
		h.logger.Error("failed to refresh pricing aliases after mutation", "operation", operation, "source", sourceID, "error", err)
		result.PricingAliasesResponse = PricingAliasesResponse{
			SourceID:       sourceID,
			Writable:       h.pricingAliases != nil,
			Catalog:        PricingAliasCatalog{SourceID: sourceID, Models: []PricingAliasCatalogModel{}},
			Catalogs:       []PricingAliasCatalog{},
			Aliases:        []PricingAliasEntry{},
			ObservedModels: []PricingAliasObservedModel{},
		}
		result.RefreshError = "the change was saved, but the pricing alias view could not be refreshed: " + err.Error()
		writeJSONNoStore(w, http.StatusOK, result)
		return
	}
	result.PricingAliasesResponse = response
	writeJSONNoStore(w, http.StatusOK, result)
}

func decodePricingAliasInput(w http.ResponseWriter, r *http.Request) (pricingAliasInput, bool) {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		APIError{
			Error:   http.StatusText(http.StatusUnsupportedMediaType),
			Message: "Content-Type must be application/json",
			Code:    http.StatusUnsupportedMediaType,
		}.Write(w)
		return pricingAliasInput{}, false
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxPricingAliasRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input *pricingAliasInput
	if err := decoder.Decode(&input); err != nil {
		writePricingAliasDecodeError(w, err)
		return pricingAliasInput{}, false
	}
	if input == nil {
		BadRequest("request body must contain one JSON object").Write(w)
		return pricingAliasInput{}, false
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			BadRequest("request body must contain exactly one JSON object").Write(w)
		} else {
			writePricingAliasDecodeError(w, err)
		}
		return pricingAliasInput{}, false
	}
	return *input, true
}

func writePricingAliasDecodeError(w http.ResponseWriter, err error) {
	var maxBytesErr *http.MaxBytesError
	if errors.As(err, &maxBytesErr) {
		APIError{
			Error:   http.StatusText(http.StatusRequestEntityTooLarge),
			Message: fmt.Sprintf("request body exceeds %d bytes", maxBytesErr.Limit),
			Code:    http.StatusRequestEntityTooLarge,
		}.Write(w)
		return
	}
	BadRequest("invalid pricing alias JSON: " + err.Error()).Write(w)
}

func (h *Handlers) buildPricingAliasesResponse(ctx context.Context, selected source.Source) (PricingAliasesResponse, error) {
	sourceID := selected.Info(ctx).ID
	response := PricingAliasesResponse{
		SourceID: sourceID,
		Writable: h.pricingAliases != nil,
		Catalog: PricingAliasCatalog{
			SourceID: sourceID,
			Models:   []PricingAliasCatalogModel{},
		},
		Catalogs:       h.pricingAliasCatalogs(ctx),
		Aliases:        []PricingAliasEntry{},
		ObservedModels: []PricingAliasObservedModel{},
	}

	if h.pricingAliases != nil {
		aliases, err := h.pricingAliases.List(ctx, sourceID)
		if err != nil {
			return PricingAliasesResponse{}, fmt.Errorf("list aliases: %w", err)
		}
		for _, alias := range aliases {
			// A blank target source means the alias predates cross-source
			// targets, so it still refers to the observing source's catalog.
			if alias.TargetSourceID == "" {
				alias.TargetSourceID = alias.SourceID
			}
			response.Aliases = append(response.Aliases, PricingAliasEntry{Alias: alias})
		}
		sort.SliceStable(response.Aliases, func(i, j int) bool {
			left, right := response.Aliases[i], response.Aliases[j]
			if left.ProviderID != right.ProviderID {
				return left.ProviderID < right.ProviderID
			}
			if left.ModelID != right.ModelID {
				return left.ModelID < right.ModelID
			}
			if left.TargetSourceID != right.TargetSourceID {
				return left.TargetSourceID < right.TargetSourceID
			}
			return left.TargetModelID < right.TargetModelID
		})
	}

	catalogSource, ok := selected.(source.PricingCatalogSource)
	if !ok {
		return response, nil
	}
	catalog := catalogSource.PricingCatalog(ctx)
	response.Catalog = pricingAliasCatalogResponse(sourceID, sourceLabel(ctx, selected), catalog)
	if len(catalog.Models) == 0 {
		// No catalog means this source computes no local per-model cost, so
		// there is nothing for an alias to change here.
		return response, nil
	}
	response.Supported = true
	targetCatalogs := catalogsByID(response.Catalogs)

	observationCtx, cancel := context.WithTimeout(ctx, pricingObservationTimeout)
	defer cancel()
	models, err := selected.Models(observationCtx, stats.PeriodQuery{Period: "all"})
	if err != nil {
		response.ObservationError = "observed models could not be refreshed: " + err.Error()
		decoratePricingAliasEntries(ctx, response.Aliases, catalogSource, targetCatalogs, nil)
		return response, nil
	}
	type observedModelKey struct {
		providerID string
		modelID    string
	}
	seen := make(map[observedModelKey]struct{}, len(models.Models))
	observedByKey := make(map[observedModelKey]stats.ModelEntry, len(models.Models))
	resolutionByKey := make(map[observedModelKey]source.PricingResolution, len(models.Models))
	for _, observed := range models.Models {
		providerID := strings.TrimSpace(observed.ProviderID)
		modelID := strings.TrimSpace(observed.ModelID)
		if modelID == "" {
			continue
		}
		key := observedModelKey{providerID: providerID, modelID: modelID}
		if _, duplicate := seen[key]; duplicate {
			continue
		}
		seen[key] = struct{}{}
		observedByKey[key] = observed

		resolution := catalogSource.ResolvePricing(ctx, providerID, modelID)
		resolutionByKey[key] = resolution
		resolved := !unresolvedPricingKind(resolution.Kind)
		response.ObservedModels = append(response.ObservedModels, PricingAliasObservedModel{
			SourceID:         sourceID,
			ProviderID:       providerID,
			ModelID:          modelID,
			Sessions:         observed.Sessions,
			Messages:         observed.Messages,
			Tokens:           observed.Tokens,
			ResolutionKind:   resolution.Kind,
			ResolutionReason: pricingResolutionReason(resolution.Kind),
			ResolutionNote:   resolution.Note,
			Resolved:         resolved,
			// A source whose own catalog is unreadable cannot be repaired by a
			// mapping, so only that state stays un-aliasable.
			Aliasable: resolution.Kind != source.PricingResolutionUnavailable,
		})
	}
	decoratePricingAliasEntries(ctx, response.Aliases, catalogSource, targetCatalogs, func(providerID, modelID string) (stats.ModelEntry, source.PricingResolution, bool) {
		key := observedModelKey{providerID: providerID, modelID: modelID}
		observed, ok := observedByKey[key]
		return observed, resolutionByKey[key], ok
	})
	sort.SliceStable(response.ObservedModels, func(i, j int) bool {
		left, right := response.ObservedModels[i], response.ObservedModels[j]
		// Models still needing pricing lead: they are the reason to open this view.
		if left.Resolved != right.Resolved {
			return !left.Resolved
		}
		if left.Messages != right.Messages {
			return left.Messages > right.Messages
		}
		if left.ProviderID != right.ProviderID {
			return left.ProviderID < right.ProviderID
		}
		return left.ModelID < right.ModelID
	})
	return response, nil
}

// pricingAliasCatalogs collects every registered source's bundled catalog so a
// single response can offer targets from all of them.
func (h *Handlers) pricingAliasCatalogs(ctx context.Context) []PricingAliasCatalog {
	catalogs := []PricingAliasCatalog{}
	if h.registry == nil {
		return catalogs
	}
	for _, candidate := range h.registry.Available(ctx) {
		catalogSource, ok := candidate.(source.PricingCatalogSource)
		if !ok {
			continue
		}
		info := candidate.Info(ctx)
		catalog := catalogSource.PricingCatalog(ctx)
		if len(catalog.Models) == 0 {
			continue
		}
		catalogs = append(catalogs, pricingAliasCatalogResponse(info.ID, info.Label, catalog))
	}
	sort.SliceStable(catalogs, func(i, j int) bool {
		return catalogs[i].SourceID < catalogs[j].SourceID
	})
	return catalogs
}

func catalogsByID(catalogs []PricingAliasCatalog) map[source.SourceID]PricingAliasCatalog {
	byID := make(map[source.SourceID]PricingAliasCatalog, len(catalogs))
	for _, catalog := range catalogs {
		byID[catalog.SourceID] = catalog
	}
	return byID
}

func badRequestPtr(message string) *APIError {
	err := BadRequest(message)
	return &err
}

// pricingAliasTargetCatalog resolves the catalog an alias wants to borrow from.
func (h *Handlers) pricingAliasTargetCatalog(ctx context.Context, targetSourceID source.SourceID) (source.PricingCatalog, *APIError) {
	target, err := h.registry.Resolve(string(targetSourceID))
	if err != nil {
		return source.PricingCatalog{}, badRequestPtr("target_source_id " + string(targetSourceID) + " is not an available source")
	}
	if target.Info(ctx).ID != targetSourceID {
		return source.PricingCatalog{}, badRequestPtr("target_source_id does not match the resolved target source")
	}
	catalogSource, ok := target.(source.PricingCatalogSource)
	if !ok {
		return source.PricingCatalog{}, badRequestPtr("source " + string(targetSourceID) + " has no bundled pricing catalog to target")
	}
	catalog := catalogSource.PricingCatalog(ctx)
	if len(catalog.Models) == 0 {
		return source.PricingCatalog{}, badRequestPtr("source " + string(targetSourceID) + " has no bundled pricing catalog to target")
	}
	return catalog, nil
}

func sourceLabel(ctx context.Context, selected source.Source) string {
	if selected == nil {
		return ""
	}
	return selected.Info(ctx).Label
}

func catalogCurrency(catalog source.PricingCatalog) string {
	if catalog.Currency != "" {
		return catalog.Currency
	}
	return "USD"
}

func pricingAliasCatalogResponse(sourceID source.SourceID, label string, catalog source.PricingCatalog) PricingAliasCatalog {
	response := PricingAliasCatalog{
		SourceID:    catalog.SourceID,
		SourceLabel: label,
		SnapshotID:  catalog.SnapshotID,
		Currency:    catalogCurrency(catalog),
		Models:      make([]PricingAliasCatalogModel, 0, len(catalog.Models)),
		Note:        catalog.Note,
	}
	if response.SourceID == "" {
		response.SourceID = sourceID
	}
	for _, model := range catalog.Models {
		response.Models = append(response.Models, PricingAliasCatalogModel{
			PricingCatalogModel: model,
			Targetable:          usablePricingRate(model.Rate),
		})
	}
	return response
}

type pricingAliasObservationLookup func(providerID, modelID string) (stats.ModelEntry, source.PricingResolution, bool)

func decoratePricingAliasEntries(ctx context.Context, aliases []PricingAliasEntry, catalogSource source.PricingCatalogSource, targetCatalogs map[source.SourceID]PricingAliasCatalog, observedLookup pricingAliasObservationLookup) {
	for index := range aliases {
		alias := &aliases[index]
		alias.TargetValid = responseCatalogHasTarget(targetCatalogs[alias.TargetSourceID], alias.TargetModelID)
		var resolution source.PricingResolution
		if observedLookup != nil {
			if observed, observedResolution, ok := observedLookup(alias.ProviderID, alias.ModelID); ok {
				alias.Detected = true
				alias.Sessions = observed.Sessions
				alias.Messages = observed.Messages
				alias.Tokens = observed.Tokens
				resolution = observedResolution
			}
		}
		if resolution.Kind == "" {
			resolution = catalogSource.ResolvePricing(ctx, alias.ProviderID, alias.ModelID)
		}
		alias.ResolutionKind = resolution.Kind
		aliasApplied := resolution.Kind == source.PricingResolutionUserAlias &&
			resolution.TargetModelID == alias.TargetModelID &&
			resolution.TargetSourceID == alias.TargetSourceID
		alias.OverridesNative = aliasApplied && resolution.OverridesNative
		// Every alias stays editable: a user alias now outranks native pricing,
		// so no resolution can render one permanently read-only.
		alias.Editable = true

		switch {
		case !alias.TargetValid:
			alias.State = PricingAliasStateTargetMissing
			alias.StateReason = "the saved target is no longer an exact priced catalog model in " + string(alias.TargetSourceID)
		case aliasApplied:
			alias.State = PricingAliasStateActive
			alias.Active = true
			if alias.Foreign() {
				alias.StateReason = "this alias prices the model from the " + string(alias.TargetSourceID) + " catalog"
			} else {
				alias.StateReason = "this alias supplies the active pricing target"
			}
		case !alias.Detected:
			alias.State = PricingAliasStateNotDetected
			alias.StateReason = "the alias is saved but this model is not currently detected"
		default:
			alias.State = PricingAliasStateIneffective
			alias.StateReason = "the saved alias does not currently resolve this provider and model"
		}
	}
}

func catalogHasTarget(catalog source.PricingCatalog, targetModelID string) bool {
	for _, model := range catalog.Models {
		if model.ModelID == targetModelID && usablePricingRate(model.Rate) {
			return true
		}
	}
	return false
}

func responseCatalogHasTarget(catalog PricingAliasCatalog, targetModelID string) bool {
	for _, model := range catalog.Models {
		if model.ModelID == targetModelID && model.Targetable {
			return true
		}
	}
	return false
}

func usablePricingRate(rate source.PricingRateSummary) bool {
	return source.UsablePricingRate(rate)
}

func unresolvedPricingKind(kind source.PricingResolutionKind) bool {
	switch kind {
	case source.PricingResolutionUnknown, source.PricingResolutionUnpriced, source.PricingResolutionUnavailable:
		return true
	default:
		return false
	}
}

func pricingResolutionReason(kind source.PricingResolutionKind) string {
	switch kind {
	case source.PricingResolutionUnknown:
		return "no catalog model or supported native mapping matched the observed model"
	case source.PricingResolutionUnpriced:
		return "the matched catalog model does not have positive input and output rates"
	case source.PricingResolutionUnavailable:
		return "pricing resolution is unavailable for the observed model"
	case source.PricingResolutionExact:
		return "the observed model matched a catalog model exactly"
	case source.PricingResolutionNativeAlias:
		return "the observed model matched a catalog model through a bundled alias"
	case source.PricingResolutionFallback:
		return "the observed model matched a catalog model approximately"
	case source.PricingResolutionUserAlias:
		return "a saved pricing alias supplies this model's rates"
	default:
		return "pricing resolution is not usable"
	}
}

func (h *Handlers) invalidatePricingSource(selected source.Source) {
	if invalidator, ok := selected.(source.PricingInvalidator); ok {
		invalidator.Invalidate()
	}
	// The cross-source index caches the same snapshots the source just dropped,
	// so it has to be rebuilt alongside them.
	if h.pricingRates != nil {
		h.pricingRates.Invalidate()
	}
}

func (h *Handlers) notifyPricingChange(ctx context.Context, sourceID source.SourceID) PricingChangeStatus {
	notifier, ok := h.cache.(PricingChangeNotifier)
	if !ok {
		return PricingChangeStatusUnavailable
	}
	status := notifier.NotifyPricingChange(ctx, sourceID)
	switch status {
	case PricingChangeStatusStarted, PricingChangeStatusQueued, PricingChangeStatusDisabled, PricingChangeStatusUnavailable:
		return status
	default:
		return PricingChangeStatusUnavailable
	}
}

func (h *Handlers) writePricingAliasStoreError(w http.ResponseWriter, operation string, sourceID source.SourceID, err error) {
	if errors.Is(err, pricingalias.ErrInvalidAlias) {
		BadRequest(err.Error()).Write(w)
		return
	}
	h.logger.Error("pricing alias store operation failed", "operation", operation, "source", sourceID, "error", err)
	InternalError("failed to " + operation + " pricing alias").Write(w)
}
