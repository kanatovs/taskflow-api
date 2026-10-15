package service

import (
	"context"
	"fmt"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type ProjectMemberRepository interface {
	ProjectMembershipReader
	List(context.Context, int64, int64, int, int) ([]domain.ProjectMember, int, error)
	Add(context.Context, int64, int64, int64, domain.ProjectRole) (domain.ProjectMember, error)
	Update(context.Context, int64, int64, int64, domain.ProjectRole) (domain.ProjectMember, error)
	Delete(context.Context, int64, int64, int64) error
}

type ProjectMemberService struct {
	projects *ProjectService
	members  ProjectMemberRepository
}

func NewProjectMemberService(projects *ProjectService, members ProjectMemberRepository) *ProjectMemberService {
	return &ProjectMemberService{projects: projects, members: members}
}

type AddProjectMemberInput struct {
	UserID int64              `json:"user_id"`
	Role   domain.ProjectRole `json:"role"`
}

type UpdateProjectMemberInput struct {
	Role domain.ProjectRole `json:"role"`
}

func (s *ProjectMemberService) List(ctx context.Context, actorID, projectID int64, page, pageSize int) ([]domain.ProjectMember, int, error) {
	if _, _, err := s.projects.access(ctx, actorID, projectID); err != nil {
		return nil, 0, err
	}
	if err := validatePage(page, pageSize); err != nil {
		return nil, 0, err
	}
	members, total, err := s.members.List(ctx, projectID, actorID, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list project members: %w", err)
	}
	return members, total, nil
}

func (s *ProjectMemberService) Add(ctx context.Context, actorID, projectID int64, input AddProjectMemberInput) (domain.ProjectMember, error) {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	if err := validateMembership(input.UserID, input.Role); err != nil {
		return domain.ProjectMember{}, err
	}
	if !domain.CanManageProjectMember(role, "", input.Role) {
		return domain.ProjectMember{}, domain.ErrForbidden
	}
	if _, err := s.projects.users.ByID(ctx, input.UserID); err != nil {
		return domain.ProjectMember{}, fmt.Errorf("find target user: %w", err)
	}
	member, err := s.members.Add(ctx, projectID, actorID, input.UserID, input.Role)
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("add project member: %w", err)
	}
	return member, nil
}

func (s *ProjectMemberService) Update(ctx context.Context, actorID, projectID, userID int64, input UpdateProjectMemberInput) (domain.ProjectMember, error) {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	if err := validateMembership(userID, input.Role); err != nil {
		return domain.ProjectMember{}, err
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return domain.ProjectMember{}, domain.ErrForbidden
	}
	target, err := s.members.ByUser(ctx, projectID, userID)
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("find target membership: %w", err)
	}
	if !domain.CanManageProjectMember(role, target.Role, input.Role) {
		return domain.ProjectMember{}, domain.ErrForbidden
	}
	member, err := s.members.Update(ctx, projectID, actorID, userID, input.Role)
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("update project member: %w", err)
	}
	return member, nil
}

func (s *ProjectMemberService) Delete(ctx context.Context, actorID, projectID, userID int64) error {
	_, role, err := s.projects.access(ctx, actorID, projectID)
	if err != nil {
		return err
	}
	if userID <= 0 {
		return &domain.ValidationError{Fields: map[string]string{"user_id": "must be a positive integer"}}
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return domain.ErrForbidden
	}
	target, err := s.members.ByUser(ctx, projectID, userID)
	if err != nil {
		return fmt.Errorf("find target membership: %w", err)
	}
	if !domain.CanManageProjectMember(role, target.Role, "") {
		return domain.ErrForbidden
	}
	if err := s.members.Delete(ctx, projectID, actorID, userID); err != nil {
		return fmt.Errorf("remove project member: %w", err)
	}
	return nil
}

func validateMembership(userID int64, role domain.ProjectRole) error {
	fields := make(map[string]string)
	if userID <= 0 {
		fields["user_id"] = "must be a positive integer"
	}
	if role != domain.RoleAdmin && role != domain.RoleMember {
		fields["role"] = "must be admin or member"
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}
