package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func waitForProjectLock(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		var waiting bool
		err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FOR UPDATE%')`).Scan(&waiting)
		if err != nil {
			t.Fatal(err)
		}
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatal("mutation did not wait on the project lock")
		case <-ticker.C:
		}
	}
}

func TestProjectPermissionRaceIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	admin := projectUser(t, pool, "admin@example.com")
	member := projectUser(t, pool, "member@example.com")
	projects := NewProjectRepository(pool, 3*time.Second)
	members := NewProjectMemberRepository(pool, 3*time.Second)
	p, err := projects.Create(ctx, domain.Project{Name: "Before", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := members.Add(ctx, p.ID, owner.ID, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := members.Add(ctx, p.ID, owner.ID, member.ID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}

	t.Run("actor demotion blocks previously authorized update", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if err := lockProject(ctx, tx, p.ID); err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		name := "Unauthorized edit"
		go func() {
			_, err := projects.Update(ctx, p.ID, admin.ID, domain.ProjectChanges{Name: &name})
			finished <- err
		}()
		waitForProjectLock(t, pool)
		if _, err := tx.Exec(ctx, `UPDATE project_members SET role='member' WHERE project_id=$1 AND user_id=$2`, p.ID, admin.ID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-finished; !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("stale actor permission: %v", err)
		}
		got, err := projects.ByID(ctx, p.ID)
		if err != nil || got.Name != "Before" {
			t.Fatal("unauthorized edit persisted")
		}
	})
	if _, err := members.Update(ctx, p.ID, owner.ID, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	t.Run("target promotion blocks admin deletion", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if err := lockProject(ctx, tx, p.ID); err != nil {
			t.Fatal(err)
		}
		finished := make(chan error, 1)
		go func() { finished <- members.Delete(ctx, p.ID, admin.ID, member.ID) }()
		waitForProjectLock(t, pool)
		if _, err := tx.Exec(ctx, `UPDATE project_members SET role='admin' WHERE project_id=$1 AND user_id=$2`, p.ID, member.ID); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(ctx); err != nil {
			t.Fatal(err)
		}
		if err := <-finished; !errors.Is(err, domain.ErrForbidden) {
			t.Fatalf("stale target permission: %v", err)
		}
		if got, err := members.ByUser(ctx, p.ID, member.ID); err != nil || got.Role != domain.RoleAdmin {
			t.Fatal("promoted member deleted")
		}
	})
	t.Run("lock wait honors deadline", func(t *testing.T) {
		tx, err := pool.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer rollback(tx)
		if err := lockProject(ctx, tx, p.ID); err != nil {
			t.Fatal(err)
		}
		name := "Timeout"
		_, err = NewProjectRepository(pool, 50*time.Millisecond).Update(ctx, p.ID, owner.ID, domain.ProjectChanges{Name: &name})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("deadline ignored: %v", err)
		}
	})
	if _, err := projects.ByID(ctx, p.ID); err != nil {
		t.Fatalf("pool did not recover after timeout: %v", err)
	}
	// pgx destroys cancelled connections asynchronously before releasing the slot.
	deadline := time.Now().Add(time.Second)
	for pool.Stat().AcquiredConns() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("connection leaked after denied/timed out transactions")
	}
}

func TestProjectRepositoryCancellationIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	projects := NewProjectRepository(pool, time.Second)
	members := NewProjectMemberRepository(pool, time.Second)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, operation := range []func() error{
		func() error { _, err := projects.ByID(ctx, 1); return err },
		func() error { _, _, err := projects.ListByUser(ctx, 1, 1, 20); return err },
		func() error { _, err := projects.Update(ctx, 1, 1, domain.ProjectChanges{}); return err },
		func() error { return projects.Delete(ctx, 1, 1) },
		func() error { _, err := members.ByUser(ctx, 1, 1); return err },
		func() error { _, _, err := members.List(ctx, 1, 1, 1, 20); return err },
		func() error { _, err := members.Add(ctx, 1, 1, 2, domain.RoleMember); return err },
		func() error { _, err := members.Update(ctx, 1, 1, 2, domain.RoleMember); return err },
		func() error { return members.Delete(ctx, 1, 1, 2) },
	} {
		if err := operation(); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancellation lost: %v", err)
		}
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("cancelled operation leaked connection")
	}
}
