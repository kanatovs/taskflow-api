package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type ProjectRepository interface {
	Create(context.Context, domain.Project) (domain.Project, error)
	ByID(context.Context, int64) (domain.Project, error)
	ListByUser(context.Context, int64, int, int) ([]domain.Project, int, error)
	Update(context.Context, int64, int64, domain.ProjectChanges) (domain.Project, error)
	Delete(context.Context, int64, int64) error
}

type ProjectMembershipReader interface {
	ByUser(context.Context, int64, int64) (domain.ProjectMember, error)
}

type ProjectUserReader interface {
	ByID(context.Context, int64) (domain.User, error)
}

type ProjectService struct {
	projects ProjectRepository
	members  ProjectMembershipReader
	users    ProjectUserReader
	details  ProjectDetailReader
}

type ProjectDetailReader interface {
	ByID(context.Context, int64) (domain.Project, error)
}

// The optional detail reader is used only after PostgreSQL-backed authorization.
func NewProjectService(projects ProjectRepository, members ProjectMembershipReader, users ProjectUserReader, details ...ProjectDetailReader) *ProjectService {
	s := &ProjectService{projects: projects, members: members, users: users}
	if len(details) > 0 {
		s.details = details[0]
	}
	return s
}

type CreateProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func (s *ProjectService) Create(ctx context.Context, userID int64, input CreateProjectInput) (domain.Project, error) {
	if err := s.currentUser(ctx, userID); err != nil {
		return domain.Project{}, err
	}
	input.Name = strings.TrimSpace(input.Name)
	if err := validateProjectFields(&input.Name, &input.Description); err != nil {
		return domain.Project{}, err
	}
	p, err := s.projects.Create(ctx, domain.Project{Name: input.Name, Description: input.Description, OwnerID: userID})
	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", err)
	}
	return p, nil
}

func (s *ProjectService) List(ctx context.Context, userID int64, page, pageSize int) ([]domain.Project, int, error) {
	if err := s.currentUser(ctx, userID); err != nil {
		return nil, 0, err
	}
	if err := validatePage(page, pageSize); err != nil {
		return nil, 0, err
	}
	projects, total, err := s.projects.ListByUser(ctx, userID, page, pageSize)
	if err != nil {
		return nil, 0, fmt.Errorf("list projects: %w", err)
	}
	return projects, total, nil
}

func (s *ProjectService) Get(ctx context.Context, userID, projectID int64) (domain.Project, error) {
	if err := s.authorizeDetail(ctx, userID, projectID); err != nil {
		return domain.Project{}, err
	}
	reader := ProjectDetailReader(s.projects)
	if s.details != nil {
		reader = s.details
	}
	p, err := reader.ByID(ctx, projectID)
	if err != nil {
		return domain.Project{}, fmt.Errorf("find project: %w", err)
	}
	return p, nil
}

// A membership row proves project existence through its foreign key. Only a
// missing membership needs an entity read to preserve existing 403/404 behavior.
func (s *ProjectService) authorizeDetail(ctx context.Context, userID, projectID int64) error {
	if err := s.currentUser(ctx, userID); err != nil {
		return err
	}
	if projectID <= 0 {
		return &domain.ValidationError{Fields: map[string]string{"project_id": "must be a positive integer"}}
	}
	_, err := s.members.ByUser(ctx, projectID, userID)
	if errors.Is(err, domain.ErrNotFound) {
		if _, err := s.projects.ByID(ctx, projectID); err != nil {
			return fmt.Errorf("find project: %w", err)
		}
		return domain.ErrForbidden
	}
	if err != nil {
		return fmt.Errorf("find project membership: %w", err)
	}
	return nil
}

func (s *ProjectService) Update(ctx context.Context, userID, projectID int64, changes domain.ProjectChanges) (domain.Project, error) {
	_, role, err := s.access(ctx, userID, projectID)
	if err != nil {
		return domain.Project{}, err
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return domain.Project{}, domain.ErrForbidden
	}
	if changes.Name == nil && changes.Description == nil {
		return domain.Project{}, &domain.ValidationError{Fields: map[string]string{"body": "must include name or description"}}
	}
	if changes.Name != nil {
		name := strings.TrimSpace(*changes.Name)
		changes.Name = &name
	}
	if err := validateProjectFields(changes.Name, changes.Description); err != nil {
		return domain.Project{}, err
	}
	p, err := s.projects.Update(ctx, projectID, userID, changes)
	if err != nil {
		return domain.Project{}, fmt.Errorf("update project: %w", err)
	}
	return p, nil
}

func (s *ProjectService) Delete(ctx context.Context, userID, projectID int64) error {
	_, role, err := s.access(ctx, userID, projectID)
	if err != nil {
		return err
	}
	if role != domain.RoleOwner {
		return domain.ErrForbidden
	}
	if err := s.projects.Delete(ctx, projectID, userID); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	return nil
}

func (s *ProjectService) currentUser(ctx context.Context, userID int64) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if userID <= 0 {
		return domain.ErrUnauthenticated
	}
	if _, err := s.users.ByID(ctx, userID); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrUnauthenticated
		}
		return fmt.Errorf("find project actor: %w", err)
	}
	return nil
}

func (s *ProjectService) access(ctx context.Context, userID, projectID int64) (domain.Project, domain.ProjectRole, error) {
	if err := s.currentUser(ctx, userID); err != nil {
		return domain.Project{}, "", err
	}
	if projectID <= 0 {
		return domain.Project{}, "", &domain.ValidationError{Fields: map[string]string{"project_id": "must be a positive integer"}}
	}
	p, err := s.projects.ByID(ctx, projectID)
	if err != nil {
		return domain.Project{}, "", fmt.Errorf("find project: %w", err)
	}
	m, err := s.members.ByUser(ctx, projectID, userID)
	if errors.Is(err, domain.ErrNotFound) {
		return domain.Project{}, "", domain.ErrForbidden
	}
	if err != nil {
		return domain.Project{}, "", fmt.Errorf("find project membership: %w", err)
	}
	return p, m.Role, nil
}

func validateProjectFields(name, description *string) error {
	fields := make(map[string]string)
	if name != nil && (!utf8.ValidString(*name) || utf8.RuneCountInString(*name) < 1 || utf8.RuneCountInString(*name) > 100 || strings.IndexFunc(*name, unicode.IsControl) >= 0) {
		fields["name"] = "must contain 1 to 100 characters without control characters"
	}
	if description != nil && (!utf8.ValidString(*description) || utf8.RuneCountInString(*description) > 2000 || strings.IndexFunc(*description, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\r' && r != '\t' }) >= 0) {
		fields["description"] = "must contain at most 2000 characters without unsupported control characters"
	}
	if len(fields) > 0 {
		return &domain.ValidationError{Fields: fields}
	}
	return nil
}

func validatePage(page, pageSize int) error {
	if page < 1 || page > 2147483647 || pageSize < 1 || pageSize > 100 {
		return &domain.ValidationError{Fields: map[string]string{"pagination": "page must be 1 to 2147483647 and page_size 1 to 100"}}
	}
	return nil
}
