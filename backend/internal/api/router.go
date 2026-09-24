package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"energy-management/internal/store"
)

type Server struct {
	DB        *store.DB
	JWTSecret string
}

func NewRouter(s *Server) http.Handler {
	r := chi.NewRouter()
	r.Use(corsMiddleware)
	r.NotFound(notFoundJSON)

	r.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api", func(api chi.Router) {
		registerAuthRoutes(api, s)   // Task 2

		api.Group(func(protected chi.Router) {
			protected.Use(requireAuth(s))
			registerMeterRoutes(protected, s)      // Task 3
			registerAnomalyRoutes(protected, s)    // Task 6
			registerAnalysisRoutes(protected, s)   // Task 4-5
			registerDashboardRoutes(protected, s)  // Task 7
		})
	})

	return r
}

// Stub functions for later tasks
func registerAuthRoutes(r chi.Router, s *Server)     {}
func registerMeterRoutes(r chi.Router, s *Server)    {}
func registerAnomalyRoutes(r chi.Router, s *Server)  {}
func registerAnalysisRoutes(r chi.Router, s *Server) {}
func registerDashboardRoutes(r chi.Router, s *Server) {}

func requireAuth(s *Server) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler { return next }
}
