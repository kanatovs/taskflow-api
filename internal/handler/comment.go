package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type CommentService interface {
	Create(context.Context, int64, int64, int64, service.CommentInput) (domain.Comment, error)
	List(context.Context, int64, int64, int64, int, int) ([]domain.Comment, int, error)
	Update(context.Context, int64, int64, int64, int64, service.CommentInput) (domain.Comment, error)
	Delete(context.Context, int64, int64, int64, int64) error
}

type CommentHandler struct{ service CommentService }

func NewCommentHandler(service CommentService) *CommentHandler {
	return &CommentHandler{service: service}
}

func (h *CommentHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor, project, task, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, task)
	var input service.CommentInput
	if !decodeRequest(w, r, &input) {
		return
	}
	comment, err := h.service.Create(r.Context(), actor, project, task, input)
	if err != nil {
		writeServiceError(w, r, "create comment", err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/projects/%d/tasks/%d/comments/%d", project, task, comment.ID))
	writeData(w, r, http.StatusCreated, comment)
}

func (h *CommentHandler) List(w http.ResponseWriter, r *http.Request) {
	actor, project, task, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, task)
	page, size, ok := parsePage(w, r)
	if !ok {
		return
	}
	comments, total, err := h.service.List(r.Context(), actor, project, task, page, size)
	if err != nil {
		writeServiceError(w, r, "list comments", err)
		return
	}
	writeCollection(w, r, comments, page, size, total)
}

func (h *CommentHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor, project, task, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "commentID")
	if !ok {
		return
	}
	r = withCommentLogger(r, project, task, id)
	var input service.CommentInput
	if !decodeRequest(w, r, &input) {
		return
	}
	comment, err := h.service.Update(r.Context(), actor, project, task, id, input)
	if err != nil {
		writeServiceError(w, r, "update comment", err)
		return
	}
	writeData(w, r, http.StatusOK, comment)
}

func (h *CommentHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, project, task, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	id, ok := parseID(w, r, "commentID")
	if !ok {
		return
	}
	r = withCommentLogger(r, project, task, id)
	if err := h.service.Delete(r.Context(), actor, project, task, id); err != nil {
		writeServiceError(w, r, "delete comment", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func withCommentLogger(r *http.Request, projectID, taskID, commentID int64) *http.Request {
	r = withTaskLogger(r, projectID, taskID)
	return r.WithContext(context.WithValue(r.Context(), loggerContextKey{}, requestLogger(r).With("comment_id", commentID)))
}
