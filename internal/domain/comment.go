package domain

import "time"

type Comment struct {
	ID        int64     `json:"id"`
	TaskID    int64     `json:"task_id"`
	AuthorID  int64     `json:"author_id"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func CanManageComment(role ProjectRole, actorID int64, comment Comment) bool {
	return role == RoleOwner || role == RoleAdmin || comment.AuthorID == actorID
}
