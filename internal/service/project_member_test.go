package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type memberStoreFake struct {
	target  domain.ProjectRole
	err     error
	writes  int
	readErr error
	ctx     context.Context
}

func (f *memberStoreFake) ByUser(ctx context.Context, _ int64, _ int64) (domain.ProjectMember, error) {
	f.ctx = ctx
	return domain.ProjectMember{Role: f.target}, f.readErr
}
func (f *memberStoreFake) List(ctx context.Context, _ int64, _ int64, _ int, _ int) ([]domain.ProjectMember, int, error) {
	f.ctx = ctx
	return []domain.ProjectMember{}, 0, f.err
}
func (f *memberStoreFake) Add(ctx context.Context, _ int64, _ int64, _ int64, _ domain.ProjectRole) (domain.ProjectMember, error) {
	f.ctx = ctx
	f.writes++
	return domain.ProjectMember{}, f.err
}
func (f *memberStoreFake) Update(ctx context.Context, _ int64, _ int64, _ int64, _ domain.ProjectRole) (domain.ProjectMember, error) {
	f.ctx = ctx
	f.writes++
	return domain.ProjectMember{}, f.err
}
func (f *memberStoreFake) Delete(ctx context.Context, _ int64, _ int64, _ int64) error {
	f.ctx = ctx
	f.writes++
	return f.err
}

func TestMembershipServiceFailuresAndContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	failure := errors.New("repository unavailable")
	projects := NewProjectService(&projectStoreFake{project: domain.Project{ID: 1}}, projectMembersFake{role: domain.RoleOwner}, projectUsersFake{})
	repo := &memberStoreFake{target: domain.RoleMember, err: failure}
	s := NewProjectMemberService(projects, repo)
	for _, operation := range []func(context.Context) error{
		func(ctx context.Context) error { _, _, err := s.List(ctx, 1, 1, 1, 20); return err },
		func(ctx context.Context) error {
			_, err := s.Add(ctx, 1, 1, AddProjectMemberInput{UserID: 2, Role: domain.RoleMember})
			return err
		},
		func(ctx context.Context) error {
			_, err := s.Update(ctx, 1, 1, 2, UpdateProjectMemberInput{Role: domain.RoleAdmin})
			return err
		},
		func(ctx context.Context) error { return s.Delete(ctx, 1, 1, 2) },
	} {
		if err := operation(ctx); !errors.Is(err, failure) {
			t.Fatalf("repository failure lost: %v", err)
		}
		if repo.ctx != ctx {
			t.Fatal("membership service replaced context")
		}
		cancelled, cancelRequest := context.WithCancel(ctx)
		cancelRequest()
		repo.writes = 0
		if err := operation(cancelled); !errors.Is(err, context.Canceled) || repo.writes != 0 {
			t.Fatal("cancelled membership operation reached persistence")
		}
	}
	repo.err, repo.readErr = nil, domain.ErrNotFound
	if _, err := s.Update(ctx, 1, 1, 2, UpdateProjectMemberInput{Role: domain.RoleAdmin}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing membership lost")
	}
	for _, input := range []AddProjectMemberInput{{UserID: 0, Role: domain.RoleMember}, {UserID: 2, Role: domain.RoleOwner}, {UserID: 2, Role: ""}} {
		var validation *domain.ValidationError
		if _, err := s.Add(ctx, 1, 1, input); !errors.As(err, &validation) {
			t.Fatal("invalid membership accepted")
		}
	}
}

func TestMembershipServicePermissions(t *testing.T) {
	for _, tt := range []struct {
		name, operation     string
		actor, target, next domain.ProjectRole
		allowed             bool
	}{
		{"owner adds admin", "add", domain.RoleOwner, "", domain.RoleAdmin, true},
		{"owner adds member", "add", domain.RoleOwner, "", domain.RoleMember, true},
		{"admin adds member", "add", domain.RoleAdmin, "", domain.RoleMember, true},
		{"admin adds admin", "add", domain.RoleAdmin, "", domain.RoleAdmin, false},
		{"member adds member", "add", domain.RoleMember, "", domain.RoleMember, false},
		{"owner promotes member", "update", domain.RoleOwner, domain.RoleMember, domain.RoleAdmin, true},
		{"owner demotes admin", "update", domain.RoleOwner, domain.RoleAdmin, domain.RoleMember, true},
		{"owner demotes owner", "update", domain.RoleOwner, domain.RoleOwner, domain.RoleMember, false},
		{"admin promotes member", "update", domain.RoleAdmin, domain.RoleMember, domain.RoleAdmin, false},
		{"admin changes admin", "update", domain.RoleAdmin, domain.RoleAdmin, domain.RoleMember, false},
		{"member changes member", "update", domain.RoleMember, domain.RoleMember, domain.RoleAdmin, false},
		{"owner removes admin", "delete", domain.RoleOwner, domain.RoleAdmin, "", true},
		{"owner removes owner", "delete", domain.RoleOwner, domain.RoleOwner, "", false},
		{"admin removes member", "delete", domain.RoleAdmin, domain.RoleMember, "", true},
		{"admin removes admin", "delete", domain.RoleAdmin, domain.RoleAdmin, "", false},
		{"admin removes owner", "delete", domain.RoleAdmin, domain.RoleOwner, "", false},
		{"member removes member", "delete", domain.RoleMember, domain.RoleMember, "", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			projects := NewProjectService(&projectStoreFake{project: domain.Project{ID: 1}}, projectMembersFake{role: tt.actor}, projectUsersFake{})
			repo := &memberStoreFake{target: tt.target}
			s := NewProjectMemberService(projects, repo)
			var err error
			switch tt.operation {
			case "add":
				_, err = s.Add(context.Background(), 1, 1, AddProjectMemberInput{UserID: 2, Role: tt.next})
			case "update":
				_, err = s.Update(context.Background(), 1, 1, 2, UpdateProjectMemberInput{Role: tt.next})
			case "delete":
				err = s.Delete(context.Background(), 1, 1, 2)
			}
			if tt.allowed {
				if err != nil || repo.writes != 1 {
					t.Fatalf("allowed write: %v", err)
				}
			} else if !errors.Is(err, domain.ErrForbidden) || repo.writes != 0 {
				t.Fatalf("forbidden write reached repository: %v", err)
			}
		})
	}
}
