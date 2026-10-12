package handler

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestAllowedMethods(t *testing.T) {
	router := chi.NewRouter()
	stub := func(http.ResponseWriter, *http.Request) {}
	router.Get("/health", stub)
	router.Get("/files/{name}", stub)
	router.Get("/items/{itemID}", stub)
	router.Post("/items/{itemID}", stub)
	router.Delete("/other", stub)
	router.Route("/projects/{projectID}", func(r chi.Router) {
		r.Get("/tasks/{taskID}", stub)
		r.Patch("/tasks/{taskID}", stub)
	})

	tests := []struct {
		name string
		path string
		want []string
	}{
		{
			name: "only methods registered for this path",
			path: "/health",
			want: []string{http.MethodGet},
		},
		{
			name: "multiple methods on a parameterized path",
			path: "/items/123",
			want: []string{http.MethodGet, http.MethodPost},
		},
		{
			name: "nested routes use independent lookup contexts",
			path: "/projects/123/tasks/456",
			want: []string{http.MethodGet, http.MethodPatch},
		},
		{
			name: "escaped slash stays inside the route parameter",
			path: "/files/a%2Fb",
			want: []string{http.MethodGet},
		},
		{
			name: "query string does not affect routing",
			path: "/health?check=1",
			want: []string{http.MethodGet},
		},
		{
			name: "unknown path has no allowed methods",
			path: "/unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPut, tt.path, nil)
			if got := allowedMethods(router, request); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("allowed methods = %v, want %v", got, tt.want)
			}
		})
	}
}
