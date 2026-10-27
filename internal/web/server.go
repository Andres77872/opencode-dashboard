package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"opencode-dashboard/internal/analyticsagent"
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

// ServerOptions collects everything the web service can be handed. Only
// Registry, Addr and Logger have defaults; every service is optional and a nil
// one degrades to that feature reporting itself unavailable rather than
// stopping the server from starting.
//
// This is a struct rather than a parameter list because the service grew one
// optional dependency at a time: each used to add another constructor that
// delegated to the next with nils, and call sites ended up passing runs of
// positional nils whose meaning was not visible.
type ServerOptions struct {
	Addr     string
	Registry *source.Registry
	Logger   *slog.Logger
	// AllowedHosts lists extra DNS names the server answers to (for example a
	// reverse proxy's name). IP addresses and localhost are always allowed.
	AllowedHosts []string

	Cache              CacheManager
	Quotas             QuotaService
	Assistant          AssistantService
	AssistantProviders *analyticsagent.AssistantProviderRegistry
	ChatLog            AssistantChatStore
	PricingAliases     PricingAliasStore
	// PricingRates is the cross-source catalog index; handlers only invalidate it.
	PricingRates   PricingRateInvalidator
	SourceSettings SourceSettingsStore
}

func NewServer(opts ServerOptions) *http.Server {
	if opts.Addr == "" {
		opts.Addr = defaultAddr
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Registry == nil {
		opts.Registry = source.NewRegistry(source.SourceOpenCode)
	}

	srv := &Server{
		Addr:     opts.Addr,
		Registry: opts.Registry,
		handlers: NewHandlers(opts),
		mux:      http.NewServeMux(),
	}

	srv.registerRoutes()
	srv.RegisterStaticRoutes()

	handler := Chain(srv.mux,
		func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasPrefix(r.URL.Path, apiV1Prefix+"/") {
					w.Header().Set("Cache-Control", "no-store")
				}
				next.ServeHTTP(w, r)
			})
		},
		hostGuardMiddleware(newHostPolicy(opts.Addr, opts.AllowedHosts), opts.Logger),
		corsMiddleware,
		LoggingMiddleware(opts.Logger),
		RecoveryMiddleware(opts.Logger),
		mutationOriginMiddleware,
	)

	return &http.Server{
		Addr:              opts.Addr,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
	}
}

func (s *Server) registerRoutes() {
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sources", s.handlers.Sources)
	s.mux.HandleFunc("PUT "+apiV1Prefix+"/sources/{id}", s.handlers.SourceUpdate)
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
	s.mux.HandleFunc("GET "+apiV1Prefix+"/pricing/aliases", s.handlers.PricingAliases)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/pricing/aliases", s.handlers.PricingAliasUpsert)
	s.mux.HandleFunc("DELETE "+apiV1Prefix+"/pricing/aliases", s.handlers.PricingAliasDelete)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/quotas", s.handlers.Quotas)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/version", s.handlers.Version)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/assistant/status", s.handlers.AssistantStatus)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/assistant/providers", s.handlers.AssistantProviders)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/providers", s.handlers.AssistantProviderCreate)
	s.mux.HandleFunc("PATCH "+apiV1Prefix+"/assistant/providers/{id}", s.handlers.AssistantProviderUpdate)
	s.mux.HandleFunc("DELETE "+apiV1Prefix+"/assistant/providers/{id}", s.handlers.AssistantProviderDelete)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/providers/{id}/models/refresh", s.handlers.AssistantProviderModelsRefresh)
	s.mux.HandleFunc("PUT "+apiV1Prefix+"/assistant/providers/{id}/models", s.handlers.AssistantProviderModelPut)
	s.mux.HandleFunc("PUT "+apiV1Prefix+"/assistant/selection", s.handlers.AssistantSelectionPut)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/chat", s.handlers.AssistantChat)
	s.mux.HandleFunc("POST "+apiV1Prefix+"/assistant/chat/stream", s.handlers.AssistantChatStream)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/assistant/sessions", s.handlers.AssistantSessions)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/assistant/sessions/{id}", s.handlers.AssistantSessionByID)
	s.mux.HandleFunc("DELETE "+apiV1Prefix+"/assistant/sessions/{id}", s.handlers.AssistantSessionDelete)
	s.mux.HandleFunc("GET /health", s.healthHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy"}`)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A different localhost port is not implicitly trusted. Grant reads only
		// to the same origins that may issue mutations and use the assistant.
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && isAllowedDashboardOrigin(origin, r) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		methods := "GET, POST, DELETE, OPTIONS"
		if requested := r.Header.Get("Access-Control-Request-Method"); requested == http.MethodPut || requested == http.MethodPatch {
			methods = "GET, POST, PUT, PATCH, DELETE, OPTIONS"
		}
		w.Header().Set("Access-Control-Allow-Methods", methods)
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

func GracefulShutdown(ctx context.Context, srv *http.Server) error {
	shutdownCtx, cancel := context.WithTimeout(ctx, shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown error: %w", err)
	}

	return nil
}
