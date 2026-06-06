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

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=30")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
