package web

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"opencode-dashboard/internal/store"
)

const (
	DefaultHost       = "127.0.0.1"
	DefaultPort       = 7450
	defaultAddr       = "127.0.0.1:7450"
	shutdownTimeout   = 5 * time.Second
	readHeaderTimeout = 10 * time.Second
	readTimeout       = 30 * time.Second
	writeTimeout      = 30 * time.Second
	apiV1Prefix       = "/api/v1"
)

type Server struct {
	Addr     string
	Store    *store.Store
	handlers *Handlers
	mux      *http.ServeMux
}

func NewServer(addr string, s *store.Store, logger *slog.Logger) *http.Server {
	if addr == "" {
		addr = defaultAddr
	}
	if logger == nil {
		logger = slog.Default()
	}

	srv := &Server{
		Addr:     addr,
		Store:    s,
		handlers: NewHandlers(s),
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
	s.mux.HandleFunc("GET "+apiV1Prefix+"/overview", s.handlers.Overview)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/daily", s.handlers.Daily)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/models", s.handlers.Models)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/tools", s.handlers.Tools)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/projects", s.handlers.Projects)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sessions", s.handlers.Sessions)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/sessions/{id}", s.handlers.SessionByID)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/config", s.handlers.Config)
	s.mux.HandleFunc("GET "+apiV1Prefix+"/version", s.handlers.Version)
	s.mux.HandleFunc("GET /health", s.healthHandler)
}

func (s *Server) healthHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, `{"status":"healthy"}`)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && isLocalOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
		} else {
			w.Header().Set("Access-Control-Allow-Origin", "*")
		}
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
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
