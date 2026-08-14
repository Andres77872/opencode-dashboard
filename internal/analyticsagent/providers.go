package analyticsagent

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"opencode-dashboard/internal/pricingalias"
)

const (
	ProviderKimi               = "kimi"
	ProviderCustom             = "custom"
	defaultBuiltInContextLimit = 262_144
	minimumUsableContextLimit  = 8_192
	modelDiscoveryTimeout      = 20 * time.Second
)

var (
	ErrStaleSelection = errors.New("assistant provider selection changed")
	ErrNoSelection    = errors.New("no assistant provider/model is selected")
)

type ProviderModel struct {
	ID           string `json:"id"`
	ContextLimit int    `json:"context_limit"`
	Verified     bool   `json:"verified"`
	Selectable   bool   `json:"selectable"`
}

// ProviderInfo is safe to serialize: credentials never enter this type.
type ProviderInfo struct {
	ID                   string                    `json:"id"`
	Kind                 string                    `json:"kind"`
	Name                 string                    `json:"name"`
	BaseURL              string                    `json:"base_url,omitempty"`
	DestinationLabel     string                    `json:"destination_label"`
	BuiltIn              bool                      `json:"built_in"`
	Available            bool                      `json:"available"`
	Reason               string                    `json:"reason,omitempty"`
	APIKeyConfigured     bool                      `json:"api_key_configured"`
	InsecureTransportAck bool                      `json:"insecure_transport_ack,omitempty"`
	Models               []ProviderModel           `json:"models"`
	Catalog              pricingalias.CatalogState `json:"catalog"`
	CreatedMS            int64                     `json:"created_ms,omitempty"`
	UpdatedMS            int64                     `json:"updated_ms,omitempty"`
}

type ProvidersResponse struct {
	Providers []ProviderInfo                  `json:"providers"`
	Selection pricingalias.AssistantSelection `json:"selection"`
}

type CredentialResolver func(context.Context) (baseURL, apiKey string, err error)

type builtInProvider struct {
	id               string
	name             string
	defaultBaseURL   string
	currentBaseURL   string
	resolve          CredentialResolver
	models           []ProviderModel
	available        bool
	reason           string
	catalog          pricingalias.CatalogState
	apiKeyConfigured bool
	apiKey           string
}

// ProviderSnapshot is captured exactly once at turn admission and shared by
// the lead and every specialist even if global settings change mid-stream.
type ProviderSnapshot struct {
	Client            Client
	Provider          string
	Model             string
	ContextLimit      int
	SelectionRevision int64
	Verified          bool
	DestinationLabel  string
	ConsentToken      string
}

type ProviderResolver interface {
	Status(context.Context) (ProviderSnapshot, error)
	Capture(context.Context, int64) (ProviderSnapshot, error)
}

type AssistantProviderRegistry struct {
	settings   *pricingalias.Store
	httpClient *http.Client
	mu         sync.RWMutex
	builtins   map[string]*builtInProvider
}

func NewAssistantProviderRegistry(settings *pricingalias.Store, miniMax, kimi CredentialResolver, httpClient *http.Client) *AssistantProviderRegistry {
	registry := &AssistantProviderRegistry{settings: settings, httpClient: httpClient, builtins: make(map[string]*builtInProvider)}
	registry.builtins[ProviderMiniMax] = &builtInProvider{id: ProviderMiniMax, name: "MiniMax", defaultBaseURL: DefaultMiniMaxBaseURL, resolve: miniMax, catalog: pricingalias.CatalogState{Status: "never"}}
	registry.builtins[ProviderKimi] = &builtInProvider{id: ProviderKimi, name: "Kimi", defaultBaseURL: DefaultKimiBaseURL, resolve: kimi, catalog: pricingalias.CatalogState{Status: "never"}}
	return registry
}

func (r *AssistantProviderRegistry) List(ctx context.Context) (ProvidersResponse, error) {
	if r == nil || r.settings == nil {
		return ProvidersResponse{}, errors.New("assistant provider settings are unavailable")
	}
	selection, err := r.settings.AssistantSelection(ctx)
	if err != nil {
		return ProvidersResponse{}, err
	}
	providers := make([]ProviderInfo, 0, 4)
	r.mu.RLock()
	for _, id := range []string{ProviderKimi, ProviderMiniMax} {
		builtin := r.builtins[id]
		baseURL := builtin.currentBaseURL
		if baseURL == "" {
			baseURL = builtin.defaultBaseURL
		}
		info := ProviderInfo{ID: id, Kind: id, Name: builtin.name, BaseURL: baseURL,
			DestinationLabel: destinationLabel(baseURL), BuiltIn: true, Available: builtin.available,
			Reason: builtin.reason, APIKeyConfigured: builtin.apiKeyConfigured,
			Models: cloneProviderModels(builtin.models), Catalog: builtin.catalog}
		providers = append(providers, info)
	}
	r.mu.RUnlock()
	custom, err := r.settings.ListAssistantProviders(ctx)
	if err != nil {
		return ProvidersResponse{}, err
	}
	for _, provider := range custom {
		models := make([]ProviderModel, 0, len(provider.Models))
		for _, model := range provider.Models {
			models = append(models, ProviderModel{ID: model.ID, ContextLimit: model.ContextLimit, Verified: model.Verified, Selectable: model.ContextLimit >= minimumUsableContextLimit})
		}
		providers = append(providers, ProviderInfo{ID: provider.ID, Kind: ProviderCustom, Name: provider.Name,
			BaseURL: provider.BaseURL, DestinationLabel: destinationLabel(provider.BaseURL), BuiltIn: false,
			Available: hasSelectableModel(models), APIKeyConfigured: provider.APIKeyConfigured,
			InsecureTransportAck: provider.InsecureTransportAck, Models: models, Catalog: provider.Catalog,
			CreatedMS: provider.CreatedMS, UpdatedMS: provider.UpdatedMS})
	}
	return ProvidersResponse{Providers: providers, Selection: selection}, nil
}

func hasSelectableModel(models []ProviderModel) bool {
	for _, model := range models {
		if model.Selectable {
			return true
		}
	}
	return false
}

// cloneProviderModels preserves the API contract that models is always a JSON
// array. append to a nil slice produces nil for an empty catalog, which the
// encoder serializes as null and makes array consumers needlessly fragile.
func cloneProviderModels(models []ProviderModel) []ProviderModel {
	return append(make([]ProviderModel, 0, len(models)), models...)
}

func (r *AssistantProviderRegistry) Refresh(ctx context.Context, providerID string) (ProviderInfo, error) {
	discoveryCtx, cancel := context.WithTimeout(ctx, modelDiscoveryTimeout)
	defer cancel()
	ctx = discoveryCtx
	providerID = strings.TrimSpace(providerID)
	if providerID == ProviderMiniMax || providerID == ProviderKimi {
		return r.refreshBuiltIn(ctx, providerID)
	}
	provider, err := r.settings.GetAssistantProvider(ctx, providerID)
	if err != nil {
		return ProviderInfo{}, err
	}
	models, err := DiscoverOpenAIModels(ctx, provider.BaseURL, provider.APIKey, provider.InsecureTransportAck, r.httpClient)
	if err != nil {
		_ = r.settings.RecordAssistantCatalogFailure(context.WithoutCancel(ctx), providerID, publicDiscoveryReason(err))
		updated, _ := r.providerInfo(ctx, providerID)
		return updated, err
	}
	stored := make([]pricingalias.AssistantModel, 0, len(models))
	for _, model := range models {
		stored = append(stored, pricingalias.AssistantModel{ID: model.ID, ContextLimit: model.ContextLimit, Verified: true, Discovered: true})
	}
	if err := r.settings.ReplaceAssistantCatalog(ctx, providerID, stored); err != nil {
		return ProviderInfo{}, err
	}
	return r.providerInfo(ctx, providerID)
}

func (r *AssistantProviderRegistry) Create(ctx context.Context, input pricingalias.CreateAssistantProviderInput) (ProviderInfo, error) {
	if _, _, err := NormalizeProviderBaseURL(input.BaseURL, input.InsecureTransportAck); err != nil {
		return ProviderInfo{}, err
	}
	provider, err := r.settings.CreateAssistantProvider(ctx, input)
	if err != nil {
		return ProviderInfo{}, err
	}
	info, refreshErr := r.Refresh(ctx, provider.ID)
	if refreshErr != nil {
		// Discovery failure is safe catalog state, not a failed create. The caller
		// receives the provider and may configure a manual model immediately.
		if current, getErr := r.providerInfo(ctx, provider.ID); getErr == nil {
			return current, nil
		} else {
			return ProviderInfo{}, getErr
		}
	}
	return info, nil
}

func (r *AssistantProviderRegistry) Update(ctx context.Context, id string, input pricingalias.UpdateAssistantProviderInput) (ProviderInfo, error) {
	current, err := r.settings.GetAssistantProvider(ctx, id)
	if err != nil {
		return ProviderInfo{}, err
	}
	baseURL, insecure := current.BaseURL, current.InsecureTransportAck
	if input.BaseURL != nil {
		baseURL = *input.BaseURL
	}
	if input.InsecureTransportAck != nil {
		insecure = *input.InsecureTransportAck
	}
	if _, _, err := NormalizeProviderBaseURL(baseURL, insecure); err != nil {
		return ProviderInfo{}, err
	}
	endpointChanged := baseURL != current.BaseURL || (input.APIKey != nil && strings.TrimSpace(*input.APIKey) != "") || input.ClearAPIKey
	if _, err := r.settings.UpdateAssistantProvider(ctx, id, input); err != nil {
		return ProviderInfo{}, err
	}
	if endpointChanged {
		info, refreshErr := r.Refresh(ctx, id)
		if refreshErr != nil {
			if current, getErr := r.providerInfo(ctx, id); getErr == nil {
				return current, nil
			} else {
				return ProviderInfo{}, getErr
			}
		}
		return info, nil
	}
	return r.providerInfo(ctx, id)
}

func (r *AssistantProviderRegistry) Delete(ctx context.Context, id string) error {
	if id == ProviderMiniMax || id == ProviderKimi {
		return errors.New("built-in assistant providers cannot be deleted")
	}
	return r.settings.DeleteAssistantProvider(ctx, id)
}

func (r *AssistantProviderRegistry) PutModel(ctx context.Context, providerID string, model pricingalias.AssistantModel) (ProviderInfo, error) {
	if providerID == ProviderMiniMax || providerID == ProviderKimi {
		return ProviderInfo{}, errors.New("built-in model metadata is read-only")
	}
	if _, err := r.settings.PutAssistantModel(ctx, providerID, model); err != nil {
		return ProviderInfo{}, err
	}
	return r.providerInfo(ctx, providerID)
}

func (r *AssistantProviderRegistry) refreshBuiltIn(ctx context.Context, id string) (ProviderInfo, error) {
	r.mu.RLock()
	builtin := r.builtins[id]
	r.mu.RUnlock()
	if builtin == nil {
		return ProviderInfo{}, pricingalias.ErrProviderNotFound
	}
	now := time.Now().UnixMilli()
	baseURL, key, err := builtin.resolveCredential(ctx)
	if err == nil && strings.TrimSpace(key) == "" {
		err = &AuthenticationError{Message: "no credential is configured"}
	}
	var discovered []DiscoveredModel
	if err == nil {
		discovered, err = DiscoverOpenAIModels(ctx, baseURL, key, true, r.httpClient)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	builtin.catalog.LastAttemptMS = now
	builtin.currentBaseURL = baseURL
	builtin.apiKeyConfigured = strings.TrimSpace(key) != ""
	builtin.apiKey = key
	if err != nil {
		builtin.available = false
		builtin.reason = publicDiscoveryReason(err)
		builtin.catalog.Status = "error"
		builtin.catalog.Error = builtin.reason
		return r.builtinInfoLocked(builtin, baseURL), err
	}
	models := make([]ProviderModel, 0, len(discovered))
	for _, model := range discovered {
		if id == ProviderMiniMax && model.ID != MiniMaxM3Model {
			continue
		}
		limit := model.ContextLimit
		if limit == 0 {
			limit = defaultBuiltInContextLimit
		}
		models = append(models, ProviderModel{ID: model.ID, ContextLimit: limit, Verified: true, Selectable: true})
	}
	sort.Slice(models, func(i, j int) bool { return models[i].ID < models[j].ID })
	builtin.models = models
	builtin.available = len(models) > 0
	builtin.reason = ""
	if !builtin.available {
		builtin.reason = "authenticated model discovery returned no supported models"
	}
	builtin.catalog = pricingalias.CatalogState{Status: "ready", LastAttemptMS: now, LastSuccessMS: now}
	return r.builtinInfoLocked(builtin, baseURL), nil
}

func (b *builtInProvider) resolveCredential(ctx context.Context) (string, string, error) {
	if b.resolve == nil {
		return b.defaultBaseURL, "", nil
	}
	baseURL, key, err := b.resolve(ctx)
	if strings.TrimSpace(baseURL) == "" {
		baseURL = b.defaultBaseURL
	}
	return baseURL, key, err
}

func (r *AssistantProviderRegistry) builtinInfoLocked(b *builtInProvider, baseURL string) ProviderInfo {
	if baseURL == "" {
		baseURL = b.defaultBaseURL
	}
	return ProviderInfo{ID: b.id, Kind: b.id, Name: b.name, BaseURL: baseURL, DestinationLabel: destinationLabel(baseURL),
		BuiltIn: true, Available: b.available, Reason: b.reason, APIKeyConfigured: b.apiKeyConfigured,
		Models: cloneProviderModels(b.models), Catalog: b.catalog}
}

func (r *AssistantProviderRegistry) providerInfo(ctx context.Context, id string) (ProviderInfo, error) {
	response, err := r.List(ctx)
	if err != nil {
		return ProviderInfo{}, err
	}
	for _, provider := range response.Providers {
		if provider.ID == id {
			return provider, nil
		}
	}
	return ProviderInfo{}, pricingalias.ErrProviderNotFound
}

func (r *AssistantProviderRegistry) Select(ctx context.Context, providerID, modelID string) (pricingalias.AssistantSelection, error) {
	if strings.TrimSpace(providerID) == "" && strings.TrimSpace(modelID) == "" {
		return r.settings.SetAssistantSelection(ctx, "", "")
	}
	response, err := r.List(ctx)
	if err != nil {
		return pricingalias.AssistantSelection{}, err
	}
	for _, provider := range response.Providers {
		if provider.ID != providerID {
			continue
		}
		for _, model := range provider.Models {
			if model.ID == modelID && model.Selectable {
				return r.settings.SetAssistantSelection(ctx, providerID, modelID)
			}
		}
		return pricingalias.AssistantSelection{}, ErrModelNotSelectable
	}
	return pricingalias.AssistantSelection{}, pricingalias.ErrProviderNotFound
}

var ErrModelNotSelectable = errors.New("assistant model is not selectable until its context limit is configured")

func (r *AssistantProviderRegistry) Status(ctx context.Context) (ProviderSnapshot, error) {
	selection, err := r.settings.AssistantSelection(ctx)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	return r.captureSelection(ctx, selection)
}

func (r *AssistantProviderRegistry) Capture(ctx context.Context, expectedRevision int64) (ProviderSnapshot, error) {
	selection, err := r.settings.AssistantSelection(ctx)
	if err != nil {
		return ProviderSnapshot{}, err
	}
	if expectedRevision != selection.Revision {
		return ProviderSnapshot{}, ErrStaleSelection
	}
	return r.captureSelection(ctx, selection)
}

func (r *AssistantProviderRegistry) captureSelection(ctx context.Context, selection pricingalias.AssistantSelection) (ProviderSnapshot, error) {
	if selection.ProviderID == "" || selection.ModelID == "" {
		return ProviderSnapshot{SelectionRevision: selection.Revision}, ErrNoSelection
	}
	if selection.ProviderID == ProviderMiniMax || selection.ProviderID == ProviderKimi {
		r.mu.RLock()
		builtin := r.builtins[selection.ProviderID]
		var selected ProviderModel
		for _, model := range builtin.models {
			if model.ID == selection.ModelID {
				selected = model
				break
			}
		}
		baseURL, key := builtin.currentBaseURL, builtin.apiKey
		available := builtin.available
		if baseURL == "" {
			baseURL = builtin.defaultBaseURL
		}
		r.mu.RUnlock()
		if !available || !selected.Selectable {
			return ProviderSnapshot{SelectionRevision: selection.Revision}, ErrModelNotSelectable
		}
		if strings.TrimSpace(key) == "" {
			return ProviderSnapshot{SelectionRevision: selection.Revision}, ErrUnavailable
		}
		var client Client
		if selection.ProviderID == ProviderMiniMax && selection.ModelID == MiniMaxM3Model {
			mini, err := NewMiniMaxClient(MiniMaxClientConfig{APIKey: key, BaseURL: baseURL, HTTPClient: r.httpClient})
			if err != nil {
				return ProviderSnapshot{}, err
			}
			client = noProbeClient{Client: mini}
		} else {
			generic, err := NewOpenAIClient(OpenAIClientConfig{APIKey: key, BaseURL: baseURL, Model: selection.ModelID, MaxCompletionTokens: reservedOutputFor(selected.ContextLimit), HTTPClient: r.httpClient})
			if err != nil {
				return ProviderSnapshot{}, err
			}
			client = generic
		}
		destination := destinationLabel(baseURL)
		return ProviderSnapshot{Client: client, Provider: selection.ProviderID, Model: selection.ModelID,
			ContextLimit: selected.ContextLimit, SelectionRevision: selection.Revision, Verified: true,
			DestinationLabel: destination, ConsentToken: consentToken(selection.ProviderID, destination)}, nil
	}
	provider, err := r.settings.GetAssistantProvider(ctx, selection.ProviderID)
	if err != nil {
		return ProviderSnapshot{SelectionRevision: selection.Revision}, err
	}
	var selected pricingalias.AssistantModel
	for _, model := range provider.Models {
		if model.ID == selection.ModelID {
			selected = model
			break
		}
	}
	if selected.ContextLimit < minimumUsableContextLimit {
		return ProviderSnapshot{SelectionRevision: selection.Revision}, ErrModelNotSelectable
	}
	client, err := NewOpenAIClient(OpenAIClientConfig{APIKey: provider.APIKey, BaseURL: provider.BaseURL, Model: selected.ID,
		MaxCompletionTokens: reservedOutputFor(selected.ContextLimit), InsecureTransportAck: provider.InsecureTransportAck, HTTPClient: r.httpClient})
	if err != nil {
		return ProviderSnapshot{}, err
	}
	destination := destinationLabel(provider.BaseURL)
	return ProviderSnapshot{Client: client, Provider: provider.ID, Model: selected.ID, ContextLimit: selected.ContextLimit,
		SelectionRevision: selection.Revision, Verified: selected.Verified, DestinationLabel: destination,
		ConsentToken: consentToken(selection.ProviderID, destination)}, nil
}

type noProbeClient struct{ Client }

func (noProbeClient) EnsureAvailable(context.Context) error { return nil }
func (c noProbeClient) ChatStream(ctx context.Context, request ChatRequest, emit func(string) error) (*ChatResponse, error) {
	if streaming, ok := c.Client.(StreamingClient); ok {
		return streaming.ChatStream(ctx, request, emit)
	}
	response, err := c.Client.Chat(ctx, request)
	if err == nil && response != nil && emit != nil {
		err = emit(response.Content)
	}
	return response, err
}

func destinationLabel(baseURL string) string {
	_, destination, err := NormalizeProviderBaseURL(baseURL, true)
	if err == nil {
		return destination
	}
	return "configured provider endpoint"
}

func consentToken(providerID, destination string) string {
	digest := sha256.Sum256([]byte(PrivacyConsentVersion + "\x00" + providerID + "\x00" + strings.ToLower(strings.TrimSpace(destination))))
	return base64.RawURLEncoding.EncodeToString(digest[:18])
}

func publicDiscoveryReason(err error) string {
	switch {
	case errors.Is(err, ErrAuthentication):
		return "authentication was rejected or is unavailable"
	case errors.Is(err, ErrRateLimited):
		return "model discovery is temporarily rate limited"
	default:
		return "model discovery failed; the last successful catalog was kept"
	}
}

func reservedOutputFor(contextLimit int) int {
	value := contextLimit / 8
	if value > maxCompletionTokens {
		value = maxCompletionTokens
	}
	if value < 1024 {
		value = 1024
	}
	return value
}

func (r *AssistantProviderRegistry) RefreshAll(ctx context.Context) {
	for _, id := range []string{ProviderKimi, ProviderMiniMax} {
		_, _ = r.Refresh(ctx, id)
	}
}

func (r *AssistantProviderRegistry) DestinationConsentToken(providerID, destination string) string {
	return consentToken(providerID, destination)
}
