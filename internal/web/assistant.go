package web

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"opencode-dashboard/internal/analyticsagent"
)

const maxAssistantRequestBytes = 64 << 10

const assistantStatusTimeout = 4 * time.Second

type AssistantService interface {
	Status(context.Context) analyticsagent.Status
	Chat(context.Context, analyticsagent.ChatInput) (analyticsagent.ChatResult, error)
}

func (h *Handlers) AssistantStatus(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	status := analyticsagent.BaseStatus()
	if h.assistant == nil {
		status.Reason = "MiniMax M3 is not configured"
	} else {
		ctx, cancel := context.WithTimeout(r.Context(), assistantStatusTimeout)
		defer cancel()
		status = h.assistant.Status(ctx)
	}
	writeJSONNoStore(w, http.StatusOK, status)
}

func (h *Handlers) AssistantChat(w http.ResponseWriter, r *http.Request) {
	if rejectNonlocalAssistantOrigin(w, r) {
		return
	}
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		writeAssistantError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if h.assistant == nil {
		writeAssistantError(w, http.StatusServiceUnavailable, "MiniMax M3 assistant is unavailable")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxAssistantRequestBytes)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	var input analyticsagent.ChatInput
	if err := decoder.Decode(&input); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeAssistantError(w, http.StatusRequestEntityTooLarge, "assistant request exceeds 64 KiB")
			return
		}
		writeAssistantError(w, http.StatusBadRequest, "assistant request must be valid JSON")
		return
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		writeAssistantError(w, http.StatusBadRequest, "assistant request must contain one JSON object")
		return
	}
	if err := analyticsagent.ValidateChatInput(input); err != nil {
		writeAssistantError(w, http.StatusBadRequest, "assistant messages are invalid")
		return
	}

	result, err := h.assistant.Chat(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, analyticsagent.ErrInvalidChat):
			writeAssistantError(w, http.StatusBadRequest, "assistant messages are invalid")
		case errors.Is(err, analyticsagent.ErrUnavailable):
			writeAssistantError(w, http.StatusServiceUnavailable, "MiniMax M3 assistant is unavailable")
		case errors.Is(err, analyticsagent.ErrBusy):
			writeAssistantError(w, http.StatusTooManyRequests, "assistant is busy; try again shortly")
		case errors.Is(err, context.DeadlineExceeded):
			writeAssistantError(w, http.StatusGatewayTimeout, "assistant request timed out")
		case errors.Is(err, analyticsagent.ErrProviderFailure), errors.Is(err, analyticsagent.ErrLoopLimit):
			writeAssistantError(w, http.StatusBadGateway, "MiniMax could not complete the analytics report")
		case errors.Is(err, context.Canceled):
			writeAssistantError(w, http.StatusRequestTimeout, "assistant request was canceled")
		default:
			writeAssistantError(w, http.StatusInternalServerError, "assistant request failed")
		}
		return
	}
	writeJSONNoStore(w, http.StatusOK, result)
}

func rejectNonlocalAssistantOrigin(w http.ResponseWriter, r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		// Modern browsers supply Sec-Fetch-Site even on requests (such as GET)
		// where Origin may be omitted. Command-line/local native clients omit both.
		if site := strings.TrimSpace(r.Header.Get("Sec-Fetch-Site")); site != "" && site != "same-origin" {
			writeAssistantError(w, http.StatusForbidden, "assistant requests are allowed only from the local dashboard")
			return true
		}
		return false
	}
	if !isAllowedAssistantOrigin(origin, r) {
		writeAssistantError(w, http.StatusForbidden, "assistant requests are allowed only from the local dashboard")
		return true
	}
	return false
}

func isAllowedAssistantOrigin(origin string, r *http.Request) bool {
	parsed, err := url.Parse(origin)
	if err != nil || !isLocalOrigin(origin) || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Path != "" && parsed.Path != "/") {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	if parsed.Scheme != scheme {
		return false
	}
	if strings.EqualFold(parsed.Host, r.Host) {
		return true
	}

	// The checked-in Vite development server is the one explicit cross-port
	// exception. Production assets are same-origin with the Go server.
	requestURL := &url.URL{Scheme: scheme, Host: r.Host}
	return parsed.Port() == "7451" && requestURL.Port() == strconv.Itoa(DefaultPort) &&
		isLoopbackHostname(parsed.Hostname()) && isLoopbackHostname(requestURL.Hostname())
}

func isLoopbackHostname(host string) bool {
	return strings.EqualFold(host, "localhost") || host == "127.0.0.1" || host == "::1"
}

func writeAssistantError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Cache-Control", "no-store")
	APIError{Error: http.StatusText(status), Message: message, Code: status}.Write(w)
}
