package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type ProjectMemberService interface {
	List(context.Context, int64, int64, int, int) ([]domain.ProjectMember, int, error)
	Add(context.Context, int64, int64, service.AddProjectMemberInput) (domain.ProjectMember, error)
	Update(context.Context, int64, int64, int64, service.UpdateProjectMemberInput) (domain.ProjectMember, error)
	Delete(context.Context, int64, int64, int64) error
}

func (h *ProjectHandler) ListMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	project, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	page, size, ok := parsePage(w, r)
	if !ok {
		return
	}
	r = withProjectLogger(r, project)
	members, total, err := h.members.List(r.Context(), actor, project, page, size)
	if err != nil {
		writeServiceError(w, r, "list project members", err)
		return
	}
	writeCollection(w, r, members, page, size, total)
}

func (h *ProjectHandler) AddMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	project, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	var input service.AddProjectMemberInput
	if !decodeRequest(w, r, &input) {
		return
	}
	r = withProjectLogger(r, project)
	member, err := h.members.Add(r.Context(), actor, project, input)
	if err != nil {
		writeServiceError(w, r, "add project member", err)
		return
	}
	w.Header().Set("Location", "/api/v1/projects/"+strconv.FormatInt(project, 10)+"/members/"+strconv.FormatInt(member.UserID, 10))
	writeData(w, r, http.StatusCreated, member)
}

func (h *ProjectHandler) UpdateMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	project, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	user, ok := parseID(w, r, "userID")
	if !ok {
		return
	}
	var input service.UpdateProjectMemberInput
	if !decodeRequest(w, r, &input) {
		return
	}
	r = withProjectLogger(r, project)
	member, err := h.members.Update(r.Context(), actor, project, user, input)
	if err != nil {
		writeServiceError(w, r, "update project member", err)
		return
	}
	writeData(w, r, http.StatusOK, member)
}

func (h *ProjectHandler) DeleteMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := authenticatedUser(w, r)
	if !ok {
		return
	}
	project, ok := parseID(w, r, "projectID")
	if !ok {
		return
	}
	user, ok := parseID(w, r, "userID")
	if !ok {
		return
	}
	r = withProjectLogger(r, project)
	if err := h.members.Delete(r.Context(), actor, project, user); err != nil {
		writeServiceError(w, r, "delete project member", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
