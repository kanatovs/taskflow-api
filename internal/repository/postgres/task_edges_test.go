package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func taskFixture(t *testing.T) (*pgxpool.Pool, domain.Task, domain.User, domain.User) {
	t.Helper()
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	member := projectUser(t, pool, "member@example.com")
	project, err := NewProjectRepository(pool, time.Second).Create(ctx, domain.Project{Name: "Task fixture", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectMemberRepository(pool, time.Second).Add(ctx, project.ID, owner.ID, member.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	task, err := NewTaskRepository(pool, time.Second).Create(ctx, domain.Task{ProjectID: project.ID, CreatorID: owner.ID, Title: "Original", Status: domain.TaskTodo, Priority: domain.PriorityMedium})
	if err != nil {
		t.Fatal(err)
	}
	return pool, task, owner, member
}

func waitTaskWriter(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var blocked bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity
            WHERE datname=current_database() AND pid<>pg_backend_pid()
            AND wait_event_type='Lock' AND query LIKE 'SELECT id FROM projects%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("task writer did not block on the project lock")
		case <-ticker.C:
		}
	}
}

func TestTaskWriteRechecksAfterLockIntegration(t *testing.T) {
	for _, name := range []string{"membership", "assignment", "role", "status"} {
		t.Run(name, func(t *testing.T) {
			pool, task, owner, member := taskFixture(t)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			blocker, err := pool.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer rollback(blocker)
			if err := lockProject(ctx, blocker, task.ProjectID); err != nil {
				t.Fatal(err)
			}
			title := "Must not persist"
			changes := domain.TaskChanges{Title: &title}
			actor := member.ID
			switch name {
			case "membership", "assignment":
				if _, err := blocker.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, task.ProjectID, member.ID); err != nil {
					t.Fatal(err)
				}
				if name == "assignment" {
					actor = owner.ID
					changes.AssigneeID = domain.Nullable[int64]{Set: true, Value: &member.ID}
				}
			case "role":
				if _, err := blocker.Exec(ctx, `UPDATE project_members SET role='member' WHERE project_id=$1 AND user_id=$2`, task.ProjectID, member.ID); err != nil {
					t.Fatal(err)
				}
			case "status":
				actor = owner.ID
				status := domain.TaskTodo
				changes.Status = &status
				if _, err := blocker.Exec(ctx, `UPDATE tasks SET status='done' WHERE id=$1`, task.ID); err != nil {
					t.Fatal(err)
				}
			}
			result := make(chan error, 1)
			go func() {
				_, err := NewTaskRepository(pool, 8*time.Second).Update(ctx, task.ProjectID, task.ID, actor, changes)
				result <- err
			}()
			waitTaskWriter(t, pool)
			if err := blocker.Commit(ctx); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-result:
				if name == "assignment" {
					var validation *domain.ValidationError
					if !errors.As(err, &validation) {
						t.Fatalf("assignment recheck: %v", err)
					}
				} else {
					want := domain.ErrForbidden
					if name == "status" {
						want = domain.ErrConflict
					}
					if !errors.Is(err, want) {
						t.Fatalf("stale %s check accepted: %v", name, err)
					}
				}
			case <-ctx.Done():
				t.Fatal("task writer did not finish")
			}
			got, err := NewTaskRepository(pool, time.Second).ByID(ctx, task.ProjectID, task.ID)
			if err != nil || got.Title != "Original" {
				t.Fatal("failed transaction changed task")
			}
			if pool.Stat().AcquiredConns() != 0 {
				t.Fatal("connection leaked after rejected write")
			}
		})
	}
}

func TestTaskWriteDeadlineAndConstraintsIntegration(t *testing.T) {
	pool, task, owner, member := taskFixture(t)
	ctx := context.Background()
	blocker, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer rollback(blocker)
	if err := lockProject(ctx, blocker, task.ProjectID); err != nil {
		t.Fatal(err)
	}
	deadline, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	title := "Timeout"
	if _, err := NewTaskRepository(pool, time.Second).Update(deadline, task.ProjectID, task.ID, owner.ID, domain.TaskChanges{Title: &title}); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("deadline lost: %v", err)
	}
	rollback(blocker)
	for _, tc := range []struct {
		sql  string
		args []any
		code string
	}{
		{`UPDATE tasks SET status='closed' WHERE id=$1`, []any{task.ID}, "23514"},
		{`UPDATE tasks SET priority='urgent' WHERE id=$1`, []any{task.ID}, "23514"},
		{`UPDATE tasks SET assignee_id=$2 WHERE id=$1`, []any{task.ID, member.ID + 999}, "23503"},
		{`UPDATE tasks SET creator_id=$2 WHERE id=$1`, []any{task.ID, owner.ID + 999}, "23503"},
		{`UPDATE tasks SET project_id=$2 WHERE id=$1`, []any{task.ID, task.ProjectID + 999}, "23503"},
	} {
		_, err := pool.Exec(ctx, tc.sql, tc.args...)
		var pgErr *pgconn.PgError
		if !errors.As(err, &pgErr) || pgErr.Code != tc.code {
			t.Fatalf("constraint %s: %v", tc.code, err)
		}
	}
	// A failed write releases the connection, so subsequent valid writes work.
	if _, err := NewTaskRepository(pool, time.Second).Update(ctx, task.ProjectID, task.ID, owner.ID, domain.TaskChanges{Title: &title}); err != nil {
		t.Fatal(err)
	}
}

func TestTaskSortRejectsSQLFragments(t *testing.T) {
	for _, pair := range [][2]string{{"id; DROP TABLE tasks", "asc"}, {"id", "asc; SELECT 1"}, {"unknown", "desc"}} {
		if _, err := taskOrder(pair[0], pair[1]); err == nil {
			t.Fatal("untrusted sort accepted")
		}
	}
	if !errors.Is(mapTaskError(pgx.ErrNoRows), domain.ErrNotFound) {
		t.Fatal("not found mapping lost")
	}
}
