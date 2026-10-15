package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestCommentRepositoryIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	other := projectUser(t, pool, "other@example.com")
	project, err := NewProjectRepository(pool, time.Second).Create(ctx, domain.Project{Name: "Comments", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectMemberRepository(pool, time.Second).Add(ctx, project.ID, owner.ID, other.ID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	task, err := NewTaskRepository(pool, time.Second).Create(ctx, domain.Task{ProjectID: project.ID, CreatorID: owner.ID, Title: "Task", Status: domain.TaskTodo, Priority: domain.PriorityMedium})
	if err != nil {
		t.Fatal(err)
	}
	repo := NewCommentRepository(pool, time.Second)
	c, err := repo.Create(ctx, project.ID, domain.Comment{TaskID: task.ID, AuthorID: other.ID, Body: "Comment"})
	if err != nil || c.CreatedAt.Location() != time.UTC {
		t.Fatalf("create: %+v %v", c, err)
	}
	if got, err := repo.ByID(ctx, project.ID, task.ID, c.ID); err != nil || got != c {
		t.Fatalf("read: %v", err)
	}
	if _, err := repo.ByID(ctx, project.ID+100, task.ID, c.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("scope: %v", err)
	}
	if _, err := repo.Create(ctx, project.ID, domain.Comment{TaskID: task.ID, AuthorID: owner.ID, Body: " "}); err == nil {
		t.Fatal("body constraint missing")
	}
	c, err = repo.Update(ctx, project.ID, task.ID, c.ID, other.ID, "Author edit")
	if err != nil || c.Body != "Author edit" {
		t.Fatalf("update: %v", err)
	}
	list, total, err := repo.List(ctx, project.ID, task.ID, 2, 20)
	if err != nil || list == nil || len(list) != 0 || total != 1 {
		t.Fatal("incorrect empty page")
	}
	if err := repo.Delete(ctx, project.ID, task.ID, c.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, project.ID, task.ID, c.ID, owner.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing comment not reported")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.ByID(cancelled, project.ID, task.ID, c.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("context: %v", err)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("connection leaked")
	}
}
