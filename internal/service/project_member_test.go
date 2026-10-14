package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type memberStoreFake struct {
	target domain.ProjectRole
	err    error
	writes int
}

func (f *memberStoreFake) ByUser(context.Context, int64, int64) (domain.ProjectMember, error) {
	return domain.ProjectMember{Role: f.target}, f.err
}
func (f *memberStoreFake) List(context.Context, int64, int64, int, int) ([]domain.ProjectMember, int, error) {
	return []domain.ProjectMember{}, 0, f.err
}
func (f *memberStoreFake) Add(context.Context, int64, int64, int64, domain.ProjectRole) (domain.ProjectMember, error) {
	f.writes++
	return domain.ProjectMember{}, f.err
}
func (f *memberStoreFake) Update(context.Context, int64, int64, int64, domain.ProjectRole) (domain.ProjectMember, error) {
	f.writes++
	return domain.ProjectMember{}, f.err
}
func (f *memberStoreFake) Delete(context.Context, int64, int64, int64) error {
	f.writes++
	return f.err
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
