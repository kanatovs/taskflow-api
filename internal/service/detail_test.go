package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type detailProbe struct{ projectCalls, taskCalls int }
type projectDetailProbe struct{ probe *detailProbe }

func (p projectDetailProbe) ByID(context.Context, int64) (domain.Project, error) {
	p.probe.projectCalls++
	return domain.Project{ID: 1}, nil
}

type taskDetailProbe struct{ probe *detailProbe }

func (p taskDetailProbe) ByID(context.Context, int64, int64) (domain.Task, error) {
	p.probe.taskCalls++
	return domain.Task{ID: 2, ProjectID: 1}, nil
}

func TestDetailAuthorizationBeforeCache(t *testing.T) {
	for _, allowed := range []bool{true, false} {
		probe := &detailProbe{}
		members := projectMembersFake{role: domain.RoleMember}
		if !allowed {
			members.err = domain.ErrNotFound
		}
		projects := NewProjectService(&projectStoreFake{project: domain.Project{ID: 1}}, members, projectUsersFake{}, projectDetailProbe{probe})
		tasks := NewTaskService(projects, &taskStoreFake{}, taskDetailProbe{probe})
		_, projectErr := projects.Get(context.Background(), 1, 1)
		_, taskErr := tasks.Get(context.Background(), 1, 1, 2)
		if allowed {
			if projectErr != nil || taskErr != nil || probe.projectCalls != 1 || probe.taskCalls != 1 {
				t.Fatal("authorized detail read failed")
			}
		} else if !errors.Is(projectErr, domain.ErrForbidden) || !errors.Is(taskErr, domain.ErrForbidden) || probe.projectCalls != 0 || probe.taskCalls != 0 {
			t.Fatal("cache accessed before membership authorization")
		}
	}
}
