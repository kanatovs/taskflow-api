package domain

import (
	"encoding/json"
	"time"
)

type TaskStatus string
type TaskPriority string

const (
	TaskTodo       TaskStatus   = "todo"
	TaskInProgress TaskStatus   = "in_progress"
	TaskDone       TaskStatus   = "done"
	PriorityLow    TaskPriority = "low"
	PriorityMedium TaskPriority = "medium"
	PriorityHigh   TaskPriority = "high"
)

type Task struct {
	ID          int64        `json:"id"`
	ProjectID   int64        `json:"project_id"`
	Title       string       `json:"title"`
	Description string       `json:"description"`
	Status      TaskStatus   `json:"status"`
	Priority    TaskPriority `json:"priority"`
	AssigneeID  *int64       `json:"assignee_id"`
	CreatorID   int64        `json:"creator_id"`
	DueDate     *time.Time   `json:"due_date"`
	CreatedAt   time.Time    `json:"created_at"`
	UpdatedAt   time.Time    `json:"updated_at"`
}

// Nullable distinguishes an omitted PATCH field from an explicit JSON null.
type Nullable[T any] struct {
	Set   bool
	Value *T
}

func (n *Nullable[T]) UnmarshalJSON(data []byte) error {
	n.Set = true
	return json.Unmarshal(data, &n.Value)
}

type TaskChanges struct {
	Title       *string             `json:"title"`
	Description *string             `json:"description"`
	Status      *TaskStatus         `json:"status"`
	Priority    *TaskPriority       `json:"priority"`
	AssigneeID  Nullable[int64]     `json:"assignee_id"`
	DueDate     Nullable[time.Time] `json:"due_date"`
}

type TaskQuery struct {
	Page       int
	PageSize   int
	Status     TaskStatus
	Priority   TaskPriority
	AssigneeID *int64
	CreatorID  *int64
	DueFrom    *time.Time
	DueTo      *time.Time
	Search     string
	Sort       string
	Order      string
}

func CanManageTask(role ProjectRole, actorID int64, task Task) bool {
	return role == RoleOwner || role == RoleAdmin || task.CreatorID == actorID
}

// ValidateTaskUpdate is shared by the service and the locked repository write.
func ValidateTaskUpdate(role ProjectRole, actorID int64, task Task, changes TaskChanges) error {
	if !CanManageTask(role, actorID, task) {
		statusOnly := changes.Status != nil && changes.Title == nil && changes.Description == nil && changes.Priority == nil && !changes.AssigneeID.Set && !changes.DueDate.Set
		if task.AssigneeID == nil || *task.AssigneeID != actorID || !statusOnly {
			return ErrForbidden
		}
	}
	if changes.Status != nil && !CanTransitionTask(task.Status, *changes.Status) {
		return ErrConflict
	}
	return nil
}

func CanTransitionTask(from, to TaskStatus) bool {
	switch from {
	case TaskTodo:
		return to == TaskTodo || to == TaskInProgress
	case TaskInProgress:
		return to == TaskInProgress || to == TaskTodo || to == TaskDone
	case TaskDone:
		return to == TaskDone || to == TaskInProgress
	default:
		return false
	}
}
