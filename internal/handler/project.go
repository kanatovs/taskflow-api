package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type ProjectService interface {
	Create(context.Context, int64, service.CreateProjectInput) (domain.Project, error)
	List(context.Context, int64, int, int) ([]domain.Project, int, error)
	Get(context.Context, int64, int64) (domain.Project, error)
	Update(context.Context, int64, int64, domain.ProjectChanges) (domain.Project, error)
	Delete(context.Context, int64, int64) error
}

type ProjectHandler struct {
	service ProjectService
	members ProjectMemberService
}

func NewProjectHandler(service ProjectService, members ProjectMemberService) *ProjectHandler {
	return &ProjectHandler{service: service, members: members}
}

func (h *ProjectHandler) Create(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	var input service.CreateProjectInput
	if !decodeRequest(w, r, &input) {
		return
	}
	p, err := h.service.Create(r.Context(), userID, input)
	if err != nil {
		writeServiceError(w, r, "create project", err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+strconv.FormatInt(p.ID, 10))
	writeData(w, r, http.StatusCreated, p)
}

func (h *ProjectHandler) List(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	page, size, ok := parsePage(w, r)
	if !ok {
		return
	}
	projects, total, err := h.service.List(r.Context(), userID, page, size)
	if err != nil {
		writeServiceError(w, r, "list projects", err)
		return
	}
	writeCollection(w, r, projects, page, size, total)
}

func (h *ProjectHandler) Get(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	projectID, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	p, err := h.service.Get(r.Context(), userID, projectID)
	if err != nil {
		writeServiceError(w, r, "get project", err)
		return
	}
	writeData(w, r, http.StatusOK, p)
}

func (h *ProjectHandler) Update(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	projectID, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	var changes domain.ProjectChanges
	if !decodeRequest(w, r, &changes) {
		return
	}
	p, err := h.service.Update(r.Context(), userID, projectID, changes)
	if err != nil {
		writeServiceError(w, r, "update project", err)
		return
	}
	writeData(w, r, http.StatusOK, p)
}

func (h *ProjectHandler) Delete(w http.ResponseWriter, r *http.Request) {
	userID, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	projectID, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	if err := h.service.Delete(r.Context(), userID, projectID); err != nil {
		writeServiceError(w, r, "delete project", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func authenticatedUser(w http.ResponseWriter, r *http.Request) (int64, bool) {
	userID, ok := r.Context().Value(userIDContextKey{}).(int64)
	if !ok || userID <= 0 {
		writeServiceError(w, r, "authenticate project request", domain.ErrUnauthenticated)
		return 0, false
	}
	return userID, true
}

func parseID(w http.ResponseWriter, r *http.Request, parameter string) (int64, bool) {
	id, err := strconv.ParseInt(chi.URLParam(r, parameter), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, r, http.StatusBadRequest, "validation_error", "Path IDs must be positive integers", nil)
		return 0, false
	}
	return id, true
}

func parsePage(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	page, size := 1, 20
	for key, values := range r.URL.Query() {
		if (key != "page" && key != "page_size") || len(values) != 1 {
			writeError(w, r, 400, "validation_error", "Unsupported or repeated query parameter", nil)
			return 0, 0, false
		}
		value, err := strconv.ParseInt(values[0], 10, 32)
		if err != nil || value < 1 || (key == "page_size" && value > 100) {
			writeError(w, r, 400, "validation_error", "Invalid pagination parameter", nil)
			return 0, 0, false
		}
		if key == "page" {
			page = int(value)
		} else {
			size = int(value)
		}
	}
	return page, size, true
}
