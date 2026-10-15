package domain

import "time"

type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	OwnerID     int64     `json:"owner_id"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

type ProjectRole string

type ProjectChanges struct {
	Name        *string `json:"name"`
	Description *string `json:"description"`
}

const (
	RoleOwner  ProjectRole = "owner"
	RoleAdmin  ProjectRole = "admin"
	RoleMember ProjectRole = "member"
)

type ProjectMember struct {
	ProjectID int64       `json:"project_id"`
	UserID    int64       `json:"user_id"`
	Role      ProjectRole `json:"role"`
	JoinedAt  time.Time   `json:"joined_at"`
}

// Empty current/requested roles represent adding/removing a membership.
func CanManageProjectMember(actor, current, requested ProjectRole) bool {
	if current == RoleOwner || requested == RoleOwner {
		return false
	}
	if requested != "" && requested != RoleAdmin && requested != RoleMember {
		return false
	}
	if actor == RoleOwner {
		return true
	}
	return actor == RoleAdmin && (current == "" || current == RoleMember) && (requested == "" || requested == RoleMember)
}
