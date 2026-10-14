package web

import (
	"context"
	"encoding/json"
	"io"
	"net/http"

	"opencode-dashboard/internal/source"
)

type SourceSettingsStore interface {
	SetSourceEnabled(context.Context, source.SourceID, bool) error
}

func (h *Handlers) SourceUpdate(w http.ResponseWriter, r *http.Request) {
	if h.sourceSettings == nil {
		ServiceUnavailable("source settings are unavailable").Write(w)
		return
	}
	id := source.SourceID(r.PathValue("id"))
	if !source.IsKnown(id) {
		BadRequest("unknown source").Write(w)
		return
	}
	var input struct {
		Enabled *bool `json:"enabled"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&input); err != nil || input.Enabled == nil {
		BadRequest("enabled must be a boolean").Write(w)
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		BadRequest("request must contain one JSON object").Write(w)
		return
	}
	if err := h.sourceSettings.SetSourceEnabled(r.Context(), id, *input.Enabled); err != nil {
		InternalError("failed to save source preference").Write(w)
		return
	}
	h.Sources(w, r)
}
