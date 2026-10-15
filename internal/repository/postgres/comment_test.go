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

func TestCommentWriteRechecksRoleIntegration(t *testing.T) {
	pool, task, owner, admin := taskFixture(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	repo := NewCommentRepository(pool, 8*time.Second)
	c, err := repo.Create(ctx, task.ProjectID, domain.Comment{TaskID: task.ID, AuthorID: owner.ID, Body: "Original"})
	if err != nil {
		t.Fatal(err)
	}
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blocker)
	if err := lockProject(ctx, blocker, task.ProjectID); err != nil {
		t.Fatal(err)
	}
	if _, err := blocker.Exec(ctx, `UPDATE project_members SET role='member' WHERE project_id=$1 AND user_id=$2`, task.ProjectID, admin.ID); err != nil {
		t.Fatal(err)
	}
	result := make(chan error, 1)
	go func() {
		_, err := repo.Update(ctx, task.ProjectID, task.ID, c.ID, admin.ID, "Must not persist")
		result <- err
	}()
	waitTaskWriter(t, pool)
	if err := blocker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("stale comment permission accepted: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("comment write did not finish")
	}
	got, err := repo.ByID(ctx, task.ProjectID, task.ID, c.ID)
	if err != nil || got.Body != "Original" || pool.Stat().AcquiredConns() != 0 {
		t.Fatal("failed comment write changed data or leaked connection")
	}
}
