package service

import (
	"context"
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
	List(context.Context, int64, int, int) ([]domain.Task, int, error)
	Update(context.Context, int64, int64, int64, domain.TaskChanges) (domain.Task, error)
	Delete(context.Context, int64, int64, int64) error
}

type TaskService struct {
	projects *ProjectService
	tasks    TaskRepository
}

func NewTaskService(projects *ProjectService, tasks TaskRepository) *TaskService {
	return &TaskService{projects: projects, tasks: tasks}
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
	task, err := s.tasks.Create(ctx, domain.Task{ProjectID: projectID, CreatorID: actorID, Title: input.Title, Description: input.Description,
		Status: input.Status, Priority: input.Priority, AssigneeID: input.AssigneeID, DueDate: utcTime(input.DueDate)})
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task: %w", err)
	}
	return task, nil
}

func (s *TaskService) Get(ctx context.Context, actorID, projectID, taskID int64) (domain.Task, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return domain.Task{}, err
	}
	if err := validateTaskID(taskID); err != nil {
		return domain.Task{}, err
	}
	task, err := s.tasks.ByID(ctx, projectID, taskID)
	if err != nil {
		return domain.Task{}, fmt.Errorf("get task: %w", err)
	}
	return task, nil
}

func (s *TaskService) List(ctx context.Context, actorID, projectID int64, page, size int) ([]domain.Task, int, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return nil, 0, err
	}
	if err := validatePage(page, size); err != nil {
		return nil, 0, err
	}
	tasks, total, err := s.tasks.List(ctx, projectID, page, size)
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	return tasks, total, nil
}

func (s *TaskService) Update(ctx context.Context, actorID, projectID, taskID int64, changes domain.TaskChanges) (domain.Task, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
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
	changes.DueDate.Value = utcTime(changes.DueDate.Value)
	task, err := s.tasks.Update(ctx, projectID, taskID, actorID, changes)
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", err)
	}
	return task, nil
}

func (s *TaskService) Delete(ctx context.Context, actorID, projectID, taskID int64) error {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return err
	}
	if err := validateTaskID(taskID); err != nil {
		return err
	}
	if err := s.tasks.Delete(ctx, projectID, taskID, actorID); err != nil {
		return fmt.Errorf("delete task: %w", err)
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
