package handler

import (
	"context"
	"fmt"
	"net/http"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/service"
)

type TaskService interface {
	Create(context.Context, int64, int64, service.CreateTaskInput) (domain.Task, error)
	Get(context.Context, int64, int64, int64) (domain.Task, error)
	List(context.Context, int64, int64, int, int) ([]domain.Task, int, error)
	Update(context.Context, int64, int64, int64, domain.TaskChanges) (domain.Task, error)
	Delete(context.Context, int64, int64, int64) error
}

type TaskHandler struct {
	service  TaskService
	comments *CommentHandler
}

func NewTaskHandler(service TaskService, comments *CommentHandler) *TaskHandler {
	return &TaskHandler{service: service, comments: comments}
}

func (h *TaskHandler) Create(w http.ResponseWriter, r *http.Request) {
	actor, project, _, ok := taskRequestIDs(w, r, false)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, 0)
	var input service.CreateTaskInput
	if !decodeRequest(w, r, &input) {
		return
	}
	task, err := h.service.Create(r.Context(), actor, project, input)
	if err != nil {
		writeServiceError(w, r, "create task", err)
		return
	}
	w.Header().Set("Location", fmt.Sprintf("/api/v1/projects/%d/tasks/%d", project, task.ID))
	writeData(w, r, http.StatusCreated, task)
}

func (h *TaskHandler) List(w http.ResponseWriter, r *http.Request) {
	actor, project, _, ok := taskRequestIDs(w, r, false)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, 0)
	page, size, ok := parsePage(w, r)
	if !ok {
		return
	}
	tasks, total, err := h.service.List(r.Context(), actor, project, page, size)
	if err != nil {
		writeServiceError(w, r, "list tasks", err)
		return
	}
	writeCollection(w, r, tasks, page, size, total)
}

func (h *TaskHandler) Get(w http.ResponseWriter, r *http.Request) {
	actor, project, id, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, id)
	task, err := h.service.Get(r.Context(), actor, project, id)
	if err != nil {
		writeServiceError(w, r, "get task", err)
		return
	}
	writeData(w, r, http.StatusOK, task)
}

func (h *TaskHandler) Update(w http.ResponseWriter, r *http.Request) {
	actor, project, id, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, id)
	var changes domain.TaskChanges
	if !decodeRequest(w, r, &changes) {
		return
	}
	task, err := h.service.Update(r.Context(), actor, project, id, changes)
	if err != nil {
		writeServiceError(w, r, "update task", err)
		return
	}
	writeData(w, r, http.StatusOK, task)
}

func (h *TaskHandler) Delete(w http.ResponseWriter, r *http.Request) {
	actor, project, id, ok := taskRequestIDs(w, r, true)
	if !ok {
		return
	}
	r = withTaskLogger(r, project, id)
	if err := h.service.Delete(r.Context(), actor, project, id); err != nil {
		writeServiceError(w, r, "delete task", err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func taskRequestIDs(w http.ResponseWriter, r *http.Request, withTask bool) (int64, int64, int64, bool) {
	actor, ok := authenticatedUser(w, r)
	if !ok {
		return 0, 0, 0, false
	}
	project, ok := parseID(w, r, "projectID")
	if !ok {
		return 0, 0, 0, false
	}
	if !withTask {
		return actor, project, 0, true
	}
	task, ok := parseID(w, r, "taskID")
	return actor, project, task, ok
}

func withTaskLogger(r *http.Request, projectID, taskID int64) *http.Request {
	logger := requestLogger(r).With("project_id", projectID)
	if taskID > 0 {
		logger = logger.With("task_id", taskID)
	}
	return r.WithContext(context.WithValue(r.Context(), loggerContextKey{}, logger))
}
