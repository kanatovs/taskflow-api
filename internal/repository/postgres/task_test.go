package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestTaskRepositoryIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	outsider := projectUser(t, pool, "outsider@example.com")
	projects := NewProjectRepository(pool, time.Second)
	project, err := projects.Create(ctx, domain.Project{Name: "Tasks", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	other, err := projects.Create(ctx, domain.Project{Name: "Other", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewTaskRepository(pool, time.Second)
	input := domain.Task{ProjectID: project.ID, CreatorID: owner.ID, Title: "First", Status: domain.TaskTodo, Priority: domain.PriorityMedium}
	task, err := repo.Create(ctx, input)
	if err != nil || task.ID <= 0 || task.CreatedAt.IsZero() || task.CreatedAt.Location() != time.UTC {
		t.Fatalf("create: %+v %v", task, err)
	}
	if _, err := repo.ByID(ctx, other.ID, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-project read: %v", err)
	}
	title := "Changed"
	due := time.Now().UTC().Truncate(time.Microsecond)
	changes := domain.TaskChanges{Title: &title, DueDate: domain.Nullable[time.Time]{Set: true, Value: &due}, AssigneeID: domain.Nullable[int64]{Set: true, Value: &owner.ID}}
	updated, err := repo.Update(ctx, project.ID, task.ID, owner.ID, changes)
	if err != nil || updated.Title != title || updated.DueDate == nil || !updated.DueDate.Equal(due) || updated.AssigneeID == nil {
		t.Fatalf("update: %+v %v", updated, err)
	}
	updated, err = repo.Update(ctx, project.ID, task.ID, owner.ID, domain.TaskChanges{DueDate: domain.Nullable[time.Time]{Set: true}, AssigneeID: domain.Nullable[int64]{Set: true}})
	if err != nil || updated.DueDate != nil || updated.AssigneeID != nil || updated.Title != title {
		t.Fatalf("null patch: %+v %v", updated, err)
	}
	tasks, total, err := repo.List(ctx, project.ID, 1, 20)
	if err != nil || len(tasks) != 1 || total != 1 {
		t.Fatalf("list: %v", err)
	}
	tasks, total, err = repo.List(ctx, project.ID, 2, 20)
	if err != nil || tasks == nil || len(tasks) != 0 || total != 1 {
		t.Fatal("empty page lost total")
	}
	if err := repo.Delete(ctx, project.ID, task.ID, outsider.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("outsider: %v", err)
	}
	if err := repo.Delete(ctx, other.ID, task.ID, owner.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("cross-project delete: %v", err)
	}
	bad := input
	bad.Status = "invalid"
	if _, err := repo.Create(ctx, bad); err == nil {
		t.Fatal("status constraint missing")
	}
	bad = input
	bad.Priority = "urgent"
	if _, err := repo.Create(ctx, bad); err == nil {
		t.Fatal("priority constraint missing")
	}
	bad = input
	bad.Title = " "
	if _, err := repo.Create(ctx, bad); err == nil {
		t.Fatal("title constraint missing")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.ByID(cancelled, project.ID, task.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("context: %v", err)
	}
	if err := projects.Delete(ctx, project.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ByID(ctx, project.ID, task.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("task not cascaded")
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("connection leaked")
	}
	testutil.ApplyMigration(t, pool, "down")
	testutil.ApplyMigration(t, pool, "up")
}
