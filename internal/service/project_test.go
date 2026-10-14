package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type projectStoreFake struct {
	project domain.Project
	err     error
	writes  int
	changes domain.ProjectChanges
}

func (f *projectStoreFake) Create(ctx context.Context, p domain.Project) (domain.Project, error) {
	f.writes++
	f.project = p
	return p, f.err
}
func (f *projectStoreFake) ByID(context.Context, int64) (domain.Project, error) {
	return f.project, f.err
}
func (f *projectStoreFake) ListByUser(context.Context, int64, int, int) ([]domain.Project, int, error) {
	return []domain.Project{f.project}, 1, f.err
}
func (f *projectStoreFake) Update(_ context.Context, _, _ int64, changes domain.ProjectChanges) (domain.Project, error) {
	f.writes++
	f.changes = changes
	return f.project, f.err
}
func (f *projectStoreFake) Delete(context.Context, int64, int64) error { f.writes++; return f.err }

type projectMembersFake struct {
	role domain.ProjectRole
	err  error
}

func (f projectMembersFake) ByUser(context.Context, int64, int64) (domain.ProjectMember, error) {
	return domain.ProjectMember{Role: f.role}, f.err
}

type projectUsersFake struct{ err error }

func (f projectUsersFake) ByID(context.Context, int64) (domain.User, error) {
	return domain.User{ID: 1}, f.err
}

func TestProjectServicePermissions(t *testing.T) {
	for _, role := range []domain.ProjectRole{domain.RoleOwner, domain.RoleAdmin, domain.RoleMember} {
		t.Run(string(role), func(t *testing.T) {
			repo := &projectStoreFake{project: domain.Project{ID: 1, OwnerID: 1}}
			s := NewProjectService(repo, projectMembersFake{role: role}, projectUsersFake{})
			ctx := context.Background()
			if _, err := s.Get(ctx, 1, 1); err != nil {
				t.Fatal(err)
			}
			name := " Updated "
			_, err := s.Update(ctx, 1, 1, domain.ProjectChanges{Name: &name})
			if role == domain.RoleMember {
				if !errors.Is(err, domain.ErrForbidden) || repo.writes != 0 {
					t.Fatal("member updated project")
				}
			} else if err != nil || *repo.changes.Name != "Updated" || repo.changes.Description != nil {
				t.Fatal("incorrect partial update")
			}
			repo.writes = 0
			err = s.Delete(ctx, 1, 1)
			if role == domain.RoleOwner {
				if err != nil || repo.writes != 1 {
					t.Fatal("owner cannot delete")
				}
			} else if !errors.Is(err, domain.ErrForbidden) || repo.writes != 0 {
				t.Fatal("non-owner deleted project")
			}
		})
	}
}

func TestProjectServiceValidationAndErrors(t *testing.T) {
	ctx := context.Background()
	repo := &projectStoreFake{project: domain.Project{ID: 1}}
	s := NewProjectService(repo, projectMembersFake{role: domain.RoleOwner}, projectUsersFake{})
	for _, input := range []CreateProjectInput{
		{Name: " "}, {Name: strings.Repeat("a", 101)}, {Name: "bad\x00name"},
		{Name: "Valid", Description: strings.Repeat("x", 2001)}, {Name: "Valid", Description: "bad\x00description"},
	} {
		var validation *domain.ValidationError
		if _, err := s.Create(ctx, 1, input); !errors.As(err, &validation) {
			t.Fatalf("invalid input accepted: %v", err)
		}
	}
	if repo.writes != 0 {
		t.Fatal("invalid request reached persistence")
	}
	p, err := s.Create(ctx, 1, CreateProjectInput{Name: " Valid "})
	if err != nil || p.Name != "Valid" || p.OwnerID != 1 {
		t.Fatal("incorrect creation")
	}
	if _, err := s.Update(ctx, 1, 1, domain.ProjectChanges{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	for _, pair := range [][2]int{{0, 20}, {1, 0}, {1, 101}, {2147483648, 20}} {
		if _, _, err := s.List(ctx, 1, pair[0], pair[1]); err == nil {
			t.Fatal("invalid pagination")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := s.Create(cancelled, 1, CreateProjectInput{Name: "Valid"}); !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled context lost")
	}
	if _, err := s.Get(ctx, 0, 1); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("anonymous actor accepted")
	}
	if _, err := NewProjectService(repo, projectMembersFake{err: domain.ErrNotFound}, projectUsersFake{}).Get(ctx, 1, 1); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("nonmember accepted")
	}
	if _, err := NewProjectService(repo, projectMembersFake{}, projectUsersFake{err: domain.ErrNotFound}).Get(ctx, 1, 1); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("deleted user accepted")
	}
	failure := errors.New("repository unavailable")
	repo.err = failure
	if _, err := s.Get(ctx, 1, 1); !errors.Is(err, failure) {
		t.Fatal("repository error lost")
	}
}
