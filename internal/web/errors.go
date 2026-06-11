package web

import (
	"encoding/json"
	"errors"
	"net/http"

	"opencode-dashboard/internal/source"
)

type APIError struct {
	Error   string `json:"error"`
	Message string `json:"message"`
	Code    int    `json:"code"`
}

func BadRequest(message string) APIError {
	return APIError{
		Error:   http.StatusText(http.StatusBadRequest),
		Message: message,
		Code:    http.StatusBadRequest,
	}
}

func NotFound(message string) APIError {
	return APIError{
		Error:   http.StatusText(http.StatusNotFound),
		Message: message,
		Code:    http.StatusNotFound,
	}
}

func InternalError(message string) APIError {
	return APIError{
		Error:   http.StatusText(http.StatusInternalServerError),
		Message: message,
		Code:    http.StatusInternalServerError,
	}
}

func ServiceUnavailable(message string) APIError {
	return APIError{
		Error:   http.StatusText(http.StatusServiceUnavailable),
		Message: message,
		Code:    http.StatusServiceUnavailable,
	}
}

func SourceError(err error) APIError {
	if errors.Is(err, source.ErrInvalidSource) || errors.Is(err, source.ErrUnsupportedSource) {
		return BadRequest(err.Error())
	}
	if errors.Is(err, source.ErrUnavailableSource) {
		return ServiceUnavailable(err.Error())
	}
	return InternalError("failed to resolve source")
}

func (e APIError) Write(w http.ResponseWriter) {
	writeJSON(w, e.Code, e)
}

// writeJSON respects a Cache-Control header the handler already set. Error
// responses are never cacheable: a transient failure must not be replayed by
// the browser for 30 seconds.
func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	if w.Header().Get("Cache-Control") == "" {
		if status >= 400 {
			w.Header().Set("Cache-Control", "no-store")
		} else {
			w.Header().Set("Cache-Control", "private, max-age=30")
		}
	}
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

// writeJSONNoStore is for live-status responses (e.g. sync progress) that the
// browser polls and must never serve from its HTTP cache.
func writeJSONNoStore(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, data)
}
