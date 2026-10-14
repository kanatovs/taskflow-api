package handler

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func NewRouter(logger *slog.Logger, readiness *Readiness, auth *AuthHandler, projects *ProjectHandler) http.Handler {
	router := chi.NewRouter()
	router.Use(requestMiddleware(logger))
	router.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeError(w, r, http.StatusNotFound, "not_found", "Route not found", nil)
	})
	router.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		if methods := allowedMethods(router, r); len(methods) > 0 {
			w.Header().Set("Allow", strings.Join(methods, ", "))
		}
		writeError(w, r, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed", nil)
	})
	router.Get("/health", Health)
	router.Get("/ready", readiness.ServeHTTP)
	if auth != nil {
		router.Post("/api/v1/auth/register", auth.Register)
		router.Post("/api/v1/auth/login", auth.Login)
		router.With(auth.Authenticate).Get("/api/v1/users/me", auth.Me)
		if projects != nil {
			router.Route("/api/v1/projects", func(r chi.Router) {
				r.Use(auth.Authenticate)
				r.Post("/", projects.Create)
				r.Get("/", projects.List)
				r.Get("/{projectID}", projects.Get)
				r.Patch("/{projectID}", projects.Update)
				r.Delete("/{projectID}", projects.Delete)
				if projects.members != nil {
					r.Get("/{projectID}/members", projects.ListMembers)
					r.Post("/{projectID}/members", projects.AddMember)
					r.Patch("/{projectID}/members/{userID}", projects.UpdateMember)
					r.Delete("/{projectID}/members/{userID}", projects.DeleteMember)
				}
			})
		}
	}
	return router
}

func allowedMethods(router chi.Router, r *http.Request) []string {
	path := r.URL.RawPath
	if path == "" {
		path = r.URL.Path
	}
	if path == "" {
		path = "/"
	}

	var allowed []string
	for _, method := range []string{
		http.MethodConnect,
		http.MethodDelete,
		http.MethodGet,
		http.MethodHead,
		http.MethodOptions,
		http.MethodPatch,
		http.MethodPost,
		http.MethodPut,
		http.MethodTrace,
	} {
		// Match mutates its route context, so each lookup needs a fresh one.
		if router.Match(chi.NewRouteContext(), method, path) {
			allowed = append(allowed, method)
		}
	}
	return allowed
}
