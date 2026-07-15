package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"opencode-dashboard/internal/source"
)

const (
	DefaultHost       = "127.0.0.1"
	DefaultPort       = 7450
	defaultAddr       = "127.0.0.1:7450"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 130 * time.Second
	apiV1Prefix       = "/api/v1"
)

type Server struct {
	Addr     string
	Registry *source.Registry
	handlers *Handlers
	mux      *http.ServeMux
}

func NewServer(addr string, registry *source.Registry, logger *slog.Logger) *http.Server {
	return NewServerWithCache(addr, registry, logger, nil)
}

func NewServerWithCache(addr string, registry *source.Registry, logger *slog.Logger, cache CacheManager) *http.Server {
	return NewServerWithServices(addr, registry, logger, cache, nil)
}

func NewServerWithServices(addr string, registry *source.Registry, logger *slog.Logger, cache CacheManager, quotas QuotaService) *http.Server {
	return NewServerWithAssistant(addr, registry, logger, cache, quotas, nil)
}

// NewServerWithAssistant is the complete web service constructor. Older
// constructors remain source-compatible for TUI/tests and simply omit the
// optional web-only analytics assistant.
func NewServerWithAssistant(addr string, registry *source.Registry, logger *slog.Logger, cache CacheManager, quotas QuotaService, assistant AssistantService) *http.Server {
	if addr == "" {
		addr = defaultAddr
	}
	if logger == nil {
		logger = slog.Default()
	}
	if registry == nil {
		registry = source.NewRegistry(source.SourceOpenCode)
	}

	srv := &Server{
		Addr:     addr,
		Registry: registry,
		handlers: NewHandlersWithAssistant(registry, cache, quotas, assistant, logger),
		mux:      http.NewServeMux(),
	}

	srv.registerRoutes()
	srv.RegisterStaticRoutes()

	handler := Chain(srv.mux,
		corsMiddleware,
		LoggingMiddleware(logger),
		RecoveryMiddleware(logger),
	)

	return &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sources", s.handlers.Sources)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/overview", s.handlers.Overview)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/overview/all", s.handlers.OverviewAll)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/daily", s.handlers.Daily)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/models", s.handlers.Models)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/tools", s.handlers.Tools)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/projects", s.handlers.Projects)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sessions", s.handlers.Sessions)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sessions/{id}", s.handlers.SessionByID)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/projects/{id}", s.handlers.ProjectDetail)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/messages", s.handlers.Messages)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/messages/{id}", s.handlers.MessageByID)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/config", s.handlers.Config)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/cache", s.handlers.CacheStatus)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/cache/sync", s.handlers.CacheSync)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/quotas", s.handlers.Quotas)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/version", s.handlers.Version)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/assistant/status", s.handlers.AssistantStatus)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/chat", s.handlers.AssistantChat)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/chat/stream", s.handlers.AssistantChatStream)
	s.mux.HandleFunc("GET /health", s.healthHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy"}`)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Only local origins (the Vite dev server, the embedded SPA) may read this
		// API. It is unauthenticated and serves project paths, session titles and
		// config, so a wildcard would let any site the user happens to visit read
		// their local usage data — and POST /cache/sync is a CORS-simple request.
		// A non-local origin gets no Access-Control-Allow-Origin header at all,
		// which makes the browser withhold the response from the caller.
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func isLocalOrigin(origin string) bool {
	u, err := url.Parse(origin)
	if err != nil {
		return false
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

func GracefulShutdown(ctx context.Context, srv *http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	return nil
}
