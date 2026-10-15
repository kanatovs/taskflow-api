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
