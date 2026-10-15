package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type TaskRepository interface {
	Create(context.Context, domain.Task) (domain.Task, error)
	ByID(context.Context, int64, int64) (domain.Task, error)
	List(context.Context, int64, domain.TaskQuery) ([]domain.Task, int, error)
	Update(context.Context, int64, int64, int64, domain.TaskChanges) (domain.Task, error)
	Delete(context.Context, int64, int64, int64) error
}

type TaskService struct {
	projects *ProjectService
	tasks    TaskRepository
	details  TaskDetailReader
}

type TaskDetailReader interface {
	ByID(context.Context, int64, int64) (domain.Task, error)
	Invalidate(context.Context, int64, int64)
}

func NewTaskService(projects *ProjectService, tasks TaskRepository, details ...TaskDetailReader) *TaskService {
	s := &TaskService{projects: projects, tasks: tasks}
	if len(details) > 0 {
		s.details = details[0]
	}
	return s
}

type CreateTaskInput struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Status      domain.TaskStatus   `json:"status"`
	Priority    domain.TaskPriority `json:"priority"`
	AssigneeID  *int64              `json:"assignee_id"`
	DueDate     *time.Time          `json:"due_date"`
}

func (s *TaskService) Create(ctx context.Context, actorID, projectID int64, input CreateTaskInput) (domain.Task, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return domain.Task{}, err
	}
	input.Title = strings.TrimSpace(input.Title)
	if input.Status == "" {
		input.Status = domain.TaskTodo
	}
	if input.Priority == "" {
		input.Priority = domain.PriorityMedium
	}
	changes := domain.TaskChanges{Title: &input.Title, Description: &input.Description, Status: &input.Status, Priority: &input.Priority,
		AssigneeID: domain.Nullable[int64]{Set: true, Value: input.AssigneeID}, DueDate: domain.Nullable[time.Time]{Set: true, Value: input.DueDate}}
	if err := validateTaskChanges(changes); err != nil {
		return domain.Task{}, err
	}
	if input.Status != domain.TaskTodo {
		return domain.Task{}, &domain.ValidationError{Fields: map[string]string{"status": "new tasks must start as todo"}}
	}
	if err := s.validateAssignee(ctx, projectID, input.AssigneeID); err != nil {
		return domain.Task{}, err
	}
	task, err := s.tasks.Create(ctx, domain.Task{ProjectID: projectID, CreatorID: actorID, Title: input.Title, Description: input.Description,
		Status: input.Status, Priority: input.Priority, AssigneeID: input.AssigneeID, DueDate: utcTime(input.DueDate)})
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (s *TaskService) Get(ctx context.Context, actorID, projectID, taskID int64) (domain.Task, error) {
	if err := s.projects.authorizeDetail(ctx, actorID, projectID); err != nil {
		return domain.Task{}, err
	}
	if err := validateTaskID(taskID); err != nil {
		return domain.Task{}, err
	}
	if s.details != nil {
		return s.details.ByID(ctx, projectID, taskID)
	}
	task, err := s.tasks.ByID(ctx, projectID, taskID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

func (s *TaskService) List(ctx context.Context, actorID, projectID int64, query domain.TaskQuery) ([]domain.Task, int, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return nil, 0, err
	}
	if err := validateTaskQuery(query); err != nil {
		return nil, 0, err
	}
	query.Search = strings.TrimSpace(query.Search)
	query.DueFrom, query.DueTo = utcTime(query.DueFrom), utcTime(query.DueTo)
	tasks, total, err := s.tasks.List(ctx, projectID, query)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, total, nil
}

func validateTaskQuery(q domain.TaskQuery) error {
	if err := validatePage(q.Page, q.PageSize); err != nil {
		return err
	}
	fields := map[string]string{}
	if q.Status != "" && q.Status != domain.TaskTodo && q.Status != domain.TaskInProgress && q.Status != domain.TaskDone {
		fields["status"] = "must be todo, in_progress, or done"
	}
	if q.Priority != "" && q.Priority != domain.PriorityLow && q.Priority != domain.PriorityMedium && q.Priority != domain.PriorityHigh {
		fields["priority"] = "must be low, medium, or high"
	}
	if q.AssigneeID != nil && *q.AssigneeID <= 0 {
		fields["assignee_id"] = "must be a positive integer"
	}
	if q.CreatorID != nil && *q.CreatorID <= 0 {
		fields["creator_id"] = "must be a positive integer"
	}
	if q.DueFrom != nil && q.DueTo != nil && q.DueFrom.After(*q.DueTo) {
		fields["due_from"] = "must not be after due_to"
	}
	if !utf8.ValidString(q.Search) || utf8.RuneCountInString(q.Search) > 200 || strings.IndexFunc(q.Search, unicode.IsControl) >= 0 {
		fields["search"] = "must contain at most 200 characters without control characters"
	}
	switch q.Sort {
	case "id", "created_at", "updated_at", "due_date", "priority", "status", "title":
	default:
		fields["sort"] = "must be id, created_at, updated_at, due_date, priority, status, or title"
	}
	if q.Order != "asc" && q.Order != "desc" {
		fields["order"] = "must be asc or desc"
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func (s *TaskService) Update(ctx context.Context, actorID, projectID, taskID int64, changes domain.TaskChanges) (domain.Task, error) {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := validateTaskID(taskID); err != nil {
		return domain.Task{}, err
	}
	if changes.Title != nil {
		title := strings.TrimSpace(*changes.Title)
		changes.Title = &title
	}
	if err := validateTaskChanges(changes); err != nil {
		return domain.Task{}, err
	}
	current, err := s.tasks.ByID(ctx, projectID, taskID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("find task for update: %w", err)
	}
	if err := domain.ValidateTaskUpdate(role, actorID, current, changes); err != nil {
		return domain.Task{}, err
	}
	if changes.AssigneeID.Set {
		if err := s.validateAssignee(ctx, projectID, changes.AssigneeID.Value); err != nil {
			return domain.Task{}, err
		}
	}
	changes.DueDate.Value = utcTime(changes.DueDate.Value)
	task, err := s.tasks.Update(ctx, projectID, taskID, actorID, changes)
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}
	if s.details != nil {
		s.details.Invalidate(ctx, projectID, taskID)
	}
	return task, nil
}

func (s *TaskService) Delete(ctx context.Context, actorID, projectID, taskID int64) error {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return err
	}
	if err := validateTaskID(taskID); err != nil {
		return err
	}
	task, err := s.tasks.ByID(ctx, projectID, taskID)
	if err != nil {
		return fmt.Errorf("find task for deletion: %w", err)
	}
	if !domain.CanManageTask(role, actorID, task) {
		return domain.ErrForbidden
	}
	if err := s.tasks.Delete(ctx, projectID, taskID, actorID); err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if s.details != nil {
		s.details.Invalidate(ctx, projectID, taskID)
	}
	return nil
}

func (s *TaskService) validateAssignee(ctx context.Context, projectID int64, userID *int64) error {
	if userID == nil {
		return nil
	}
	_, err := s.projects.members.ByUser(ctx, projectID, *userID)
	if errors.Is(err, domain.ErrNotFound) {
		return &domain.ValidationError{Fields: map[string]string{"assignee_id": "must identify a current project member"}}
	}
	if err != nil {
		return fmt.Errorf("find task assignee: %w", err)
	}
	return nil
}

func validateTaskID(id int64) error {
	if id <= 0 {
		return &domain.ValidationError{Fields: map[string]string{"task_id": "must be a positive integer"}}
	}
	return nil
}

func validateTaskChanges(c domain.TaskChanges) error {
	fields := map[string]string{}
	if c.Title == nil && c.Description == nil && c.Status == nil && c.Priority == nil && !c.AssigneeID.Set && !c.DueDate.Set {
		fields["body"] = "must include at least one task field"
	}
	if c.Title != nil && (!utf8.ValidString(*c.Title) || utf8.RuneCountInString(*c.Title) < 1 || utf8.RuneCountInString(*c.Title) > 200 || strings.IndexFunc(*c.Title, unicode.IsControl) >= 0) {
		fields["title"] = "must contain 1 to 200 characters without control characters"
	}
	if c.Description != nil && (!utf8.ValidString(*c.Description) || utf8.RuneCountInString(*c.Description) > 2000 || strings.IndexFunc(*c.Description, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0) {
		fields["description"] = "must contain at most 2000 characters without unsupported control characters"
	}
	if c.Status != nil && *c.Status != domain.TaskTodo && *c.Status != domain.TaskInProgress && *c.Status != domain.TaskDone {
		fields["status"] = "must be todo, in_progress, or done"
	}
	if c.Priority != nil && *c.Priority != domain.PriorityLow && *c.Priority != domain.PriorityMedium && *c.Priority != domain.PriorityHigh {
		fields["priority"] = "must be low, medium, or high"
	}
	if c.AssigneeID.Set && c.AssigneeID.Value != nil && *c.AssigneeID.Value <= 0 {
		fields["assignee_id"] = "must be a positive integer or null"
	}
	if c.DueDate.Set && c.DueDate.Value != nil && (c.DueDate.Value.Year() < 1 || c.DueDate.Value.Year() > 9999) {
		fields["due_date"] = "must be an RFC3339 timestamp or null"
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func utcTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	utc := value.UTC()
	return &utc
}
