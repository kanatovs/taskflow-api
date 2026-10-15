package service

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	repositorycache "github.com/kanatovs/taskflow-api/internal/repository/cache"
)

type detailProbe struct{ projectCalls, taskCalls, projectInvalidations, taskInvalidations int }
type projectDetailProbe struct{ probe *detailProbe }

func (p projectDetailProbe) Invalidate(context.Context, int64) { p.probe.projectInvalidations++ }

func (p projectDetailProbe) ByID(context.Context, int64) (domain.Project, error) {
	p.probe.projectCalls++
	return domain.Project{ID: 1}, nil
}

func TestRemovedMemberCannotReadCachedDetails(t *testing.T) {
	probe := &detailProbe{}
	members := &projectMembersFake{role: domain.RoleMember}
	projects := NewProjectService(&projectStoreFake{project: domain.Project{ID: 1}}, members, projectUsersFake{}, projectDetailProbe{probe})
	tasks := NewTaskService(projects, &taskStoreFake{}, taskDetailProbe{probe})
	ctx := context.Background()
	if _, err := projects.Get(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Get(ctx, 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	members.err = domain.ErrNotFound
	if _, err := projects.Get(ctx, 1, 1); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("removed member got project")
	}
	if _, err := tasks.Get(ctx, 1, 1, 2); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("removed member got task")
	}
	if probe.projectCalls != 1 || probe.taskCalls != 1 {
		t.Fatal("removed member accessed cache")
	}
}

func TestMutationsInvalidateOnlyAfterSuccess(t *testing.T) {
	ctx := context.Background()
	probe := &detailProbe{}
	projectRepo := &projectStoreFake{project: domain.Project{ID: 1}}
	projects := NewProjectService(projectRepo, projectMembersFake{role: domain.RoleOwner}, projectUsersFake{}, projectDetailProbe{probe})
	name := "Updated"
	if _, err := projects.Update(ctx, 1, 1, domain.ProjectChanges{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if err := projects.Delete(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if probe.projectInvalidations != 2 {
		t.Fatal("project update/delete not invalidated")
	}
	taskRepo := &taskStoreFake{task: domain.Task{ID: 2, ProjectID: 1, CreatorID: 1, Status: domain.TaskTodo}}
	tasks := NewTaskService(projects, taskRepo, taskDetailProbe{probe})
	status := domain.TaskInProgress
	priority := domain.PriorityHigh
	assignee := int64(2)
	due := time.Now().UTC()
	for _, changes := range []domain.TaskChanges{
		{Title: &name}, {Status: &status}, {Priority: &priority},
		{AssigneeID: domain.Nullable[int64]{Set: true, Value: &assignee}},
		{DueDate: domain.Nullable[time.Time]{Set: true, Value: &due}},
		{AssigneeID: domain.Nullable[int64]{Set: true}}, {DueDate: domain.Nullable[time.Time]{Set: true}},
	} {
		if _, err := tasks.Update(ctx, 1, 1, 2, changes); err != nil {
			t.Fatal(err)
		}
	}
	if err := tasks.Delete(ctx, 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	if probe.taskInvalidations != 8 {
		t.Fatal("not all task mutation types invalidated")
	}
	taskRepo.err = errors.New("write failed")
	if _, err := tasks.Update(ctx, 1, 1, 2, domain.TaskChanges{Title: &name}); err == nil {
		t.Fatal("write failure lost")
	}
	if err := tasks.Delete(ctx, 1, 1, 2); err == nil {
		t.Fatal("delete failure lost")
	}
	if probe.taskInvalidations != 8 {
		t.Fatal("failed task write invalidated")
	}
	projectRepo.err = errors.New("database failed")
	if _, err := projects.Update(ctx, 1, 1, domain.ProjectChanges{Name: &name}); err == nil {
		t.Fatal("failure lost")
	}
	if err := projects.Delete(ctx, 1, 1); err == nil {
		t.Fatal("failure lost")
	}
	if probe.projectInvalidations != 2 {
		t.Fatal("failed project operation invalidated")
	}
}

type unavailableCache struct{ deletes int }

func (s *unavailableCache) Get(context.Context, string) ([]byte, bool, error) {
	return nil, false, errors.New("unavailable")
}
func (s *unavailableCache) Set(context.Context, string, []byte, time.Duration) error {
	return errors.New("unavailable")
}
func (s *unavailableCache) Delete(context.Context, string) error {
	s.deletes++
	return errors.New("unavailable")
}

func TestInvalidationFailureDoesNotFailWrites(t *testing.T) {
	store := &unavailableCache{}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	projectRepo := &projectStoreFake{project: domain.Project{ID: 1}}
	projects := NewProjectService(projectRepo, projectMembersFake{role: domain.RoleOwner}, projectUsersFake{}, repositorycache.NewProject(projectRepo, store, time.Minute, logger))
	taskRepo := &taskStoreFake{task: domain.Task{ID: 2, ProjectID: 1, CreatorID: 1, Status: domain.TaskTodo}}
	tasks := NewTaskService(projects, taskRepo, repositorycache.NewTask(taskRepo, store, time.Minute, logger))
	name := "Updated"
	ctx := context.Background()
	if _, err := projects.Update(ctx, 1, 1, domain.ProjectChanges{Name: &name}); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.Update(ctx, 1, 1, 2, domain.TaskChanges{Title: &name}); err != nil {
		t.Fatal(err)
	}
	if err := tasks.Delete(ctx, 1, 1, 2); err != nil {
		t.Fatal(err)
	}
	if err := projects.Delete(ctx, 1, 1); err != nil {
		t.Fatal(err)
	}
	if store.deletes != 4 || projectRepo.writes != 2 || taskRepo.writes != 2 {
		t.Fatal("invalidation failure changed PostgreSQL write behavior")
	}
}

type taskDetailProbe struct{ probe *detailProbe }

func (p taskDetailProbe) Invalidate(context.Context, int64, int64) { p.probe.taskInvalidations++ }

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
