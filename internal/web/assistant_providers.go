package web

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"

	"opencode-dashboard/internal/analyticsagent"
	"opencode-dashboard/internal/pricingalias"
)

const maxAssistantProviderRequestBytes = 32 << 10

type assistantProviderMutation struct {
	Name                 *string `json:"name,omitempty"`
	BaseURL              *string `json:"base_url,omitempty"`
	APIKey               *string `json:"api_key,omitempty"`
	ClearAPIKey          bool    `json:"clear_api_key,omitempty"`
	InsecureTransportAck *bool   `json:"insecure_transport_ack,omitempty"`
}

func (h *Handlers) AssistantProviders(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	response, err := h.assistantProviders.List(r.Context())
	if err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusOK, response)
}

func (h *Handlers) AssistantProviderCreate(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	var body assistantProviderMutation
	if !decodeAssistantProviderJSON(w, r, &body) {
		return
	}
	if body.Name == nil || body.BaseURL == nil {
		writeAssistantError(w, http.StatusBadRequest, "name and base_url are required")
		return
	}
	provider, err := h.assistantProviders.Create(r.Context(), pricingalias.CreateAssistantProviderInput{
		Name: *body.Name, BaseURL: *body.BaseURL, APIKey: valueOrEmpty(body.APIKey), InsecureTransportAck: valueOrFalse(body.InsecureTransportAck),
	})
	if err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusCreated, provider)
}

func (h *Handlers) AssistantProviderUpdate(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	var body assistantProviderMutation
	if !decodeAssistantProviderJSON(w, r, &body) {
		return
	}
	provider, err := h.assistantProviders.Update(r.Context(), r.PathValue("id"), pricingalias.UpdateAssistantProviderInput{
		Name: body.Name, BaseURL: body.BaseURL, APIKey: body.APIKey, ClearAPIKey: body.ClearAPIKey, InsecureTransportAck: body.InsecureTransportAck,
	})
	if err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusOK, provider)
}

func (h *Handlers) AssistantProviderDelete(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	if err := h.assistantProviders.Delete(r.Context(), r.PathValue("id")); err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
}

func (h *Handlers) AssistantProviderModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	var body struct{}
	if !decodeAssistantProviderJSON(w, r, &body) {
		return
	}
	provider, err := h.assistantProviders.Refresh(r.Context(), r.PathValue("id"))
	if err != nil {
		// A safe provider record with retained last-good models is more useful
		// than replacing it with a provider-specific transport error.
		if current, listErr := h.assistantProviders.List(r.Context()); listErr == nil {
			for _, item := range current.Providers {
				if item.ID == r.PathValue("id") {
					writeJSONNoStore(w, http.StatusOK, item)
					return
				}
			}
		}
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusOK, provider)
}

func (h *Handlers) AssistantProviderModelPut(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	var body struct {
		ModelID      string `json:"model_id"`
		ContextLimit int    `json:"context_limit"`
	}
	if !decodeAssistantProviderJSON(w, r, &body) {
		return
	}
	provider, err := h.assistantProviders.PutModel(r.Context(), r.PathValue("id"), pricingalias.AssistantModel{ID: body.ModelID, ContextLimit: body.ContextLimit})
	if err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusOK, provider)
}

func (h *Handlers) AssistantSelectionPut(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	if h.assistantProviders == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "assistant provider settings are unavailable")
		return
	}
	var body struct {
		ProviderID string `json:"provider_id"`
		ModelID    string `json:"model_id"`
	}
	if !decodeAssistantProviderJSON(w, r, &body) {
		return
	}
	selection, err := h.assistantProviders.Select(r.Context(), body.ProviderID, body.ModelID)
	if err != nil {
		h.providerSettingsFailure(w, err)
		return
	}
	writeJSONNoStore(w, http.StatusOK, selection)
}

func decodeAssistantProviderJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAssistantError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxAssistantProviderRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAssistantError(w, http.StatusRequestEntityTooLarge, "assistant provider request exceeds 32 KiB")
		} else {
			writeAssistantError(w, http.StatusBadRequest, "assistant provider request must be valid JSON")
		}
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAssistantError(w, http.StatusBadRequest, "assistant provider request must contain one JSON object")
		return false
	}
	return true
}

func (h *Handlers) providerSettingsFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pricingalias.ErrProviderNotFound):
		writeAssistantError(w, http.StatusNotFound, "assistant provider not found")
	case errors.Is(err, analyticsagent.ErrModelNotSelectable), errors.Is(err, pricingalias.ErrInvalidSelection):
		writeAssistantError(w, http.StatusConflict, "assistant model is not selectable")
	case errors.Is(err, pricingalias.ErrInvalidProvider), strings.Contains(err.Error(), "base_url"), strings.Contains(err.Error(), "HTTP endpoint"):
		writeAssistantError(w, http.StatusBadRequest, "assistant provider configuration is invalid")
	case strings.Contains(err.Error(), "built-in"):
		writeAssistantError(w, http.StatusBadRequest, "built-in assistant provider configuration is read-only")
	default:
		h.logger.Error("assistant provider settings operation failed", "error", safeProviderSettingsError(err))
		writeAssistantError(w, http.StatusInternalServerError, "assistant provider settings operation failed")
	}
}

func safeProviderSettingsError(err error) string {
	if errors.Is(err, pricingalias.ErrProviderNotFound) {
		return pricingalias.ErrProviderNotFound.Error()
	}
	return "provider settings failure"
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func valueOrFalse(value *bool) bool { return value != nil && *value }
