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

func projectUser(t *testing.T, pool *pgxpool.Pool, email string) domain.User {
	t.Helper()
	u, err := NewUserRepository(pool, time.Second).Create(context.Background(), domain.User{Email: email, PasswordHash: "private-test-hash", DisplayName: "User"})
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func TestMapProjectError(t *testing.T) {
	for _, tt := range []struct{ input, want error }{
		{pgx.ErrNoRows, domain.ErrNotFound},
		{&pgconn.PgError{Code: "23503"}, domain.ErrNotFound},
		{&pgconn.PgError{Code: "23505"}, domain.ErrConflict},
	} {
		if !errors.Is(mapProjectError(tt.input), tt.want) {
			t.Fatal("incorrect error mapping")
		}
	}
	var validation *domain.ValidationError
	if !errors.As(mapProjectError(&pgconn.PgError{Code: "23514"}), &validation) {
		t.Fatal("constraint error not mapped")
	}
	internal := errors.New("private database failure")
	if mapProjectError(internal) != internal {
		t.Fatal("unexpected error replaced")
	}
}

func TestProjectRepositoryIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	other := projectUser(t, pool, "other@example.com")
	repo := NewProjectRepository(pool, time.Second)
	members := NewProjectMemberRepository(pool, time.Second)
	p, err := repo.Create(ctx, domain.Project{Name: "Portfolio", Description: "First project", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if p.ID <= 0 || p.CreatedAt.IsZero() || p.CreatedAt.Location() != time.UTC || p.OwnerID != owner.ID {
		t.Fatal("incorrect project")
	}
	m, err := members.ByUser(ctx, p.ID, owner.ID)
	if err != nil || m.Role != domain.RoleOwner || m.JoinedAt.Location() != time.UTC {
		t.Fatalf("owner membership: %v", err)
	}
	if got, err := repo.ByID(ctx, p.ID); err != nil || got != p {
		t.Fatalf("read project: %v", err)
	}
	if _, err := repo.ByID(ctx, p.ID+100); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing project: %v", err)
	}
	if _, err := members.ByUser(ctx, p.ID, other.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing membership: %v", err)
	}
	if _, err := repo.Create(ctx, domain.Project{Name: "Missing owner", OwnerID: other.ID + 100}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("foreign key: %v", err)
	}
	list, total, err := repo.ListByUser(ctx, owner.ID, 1, 20)
	if err != nil || len(list) != 1 || total != 1 {
		t.Fatalf("list: %d %d %v", len(list), total, err)
	}
	list, total, err = repo.ListByUser(ctx, other.ID, 1, 20)
	if err != nil || list == nil || len(list) != 0 || total != 0 {
		t.Fatal("unrelated user saw projects")
	}
	list, total, err = repo.ListByUser(ctx, owner.ID, 2, 20)
	if err != nil || len(list) != 0 || total != 1 {
		t.Fatal("out-of-range page lost total")
	}
	p.Name = "Updated"
	if _, err := repo.Update(ctx, p.ID, other.ID, domain.ProjectChanges{Name: &p.Name}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("unauthorized update: %v", err)
	}
	if _, err := repo.Update(ctx, p.ID, owner.ID, domain.ProjectChanges{Name: &p.Name}); err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, p.ID, other.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatalf("unauthorized deletion: %v", err)
	}
	if err := repo.Delete(ctx, p.ID, owner.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := members.ByUser(ctx, p.ID, owner.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("membership not cascaded")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.Create(cancelled, domain.Project{Name: "Cancelled", OwnerID: owner.ID}); !errors.Is(err, context.Canceled) {
		t.Fatalf("context lost: %v", err)
	}
	testutil.ApplyMigration(t, pool, "down")
	testutil.ApplyMigration(t, pool, "up")
	var count int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM projects`).Scan(&count); err != nil || count != 0 {
		t.Fatalf("migration round trip: %v", err)
	}
}

func TestProjectCreationRollbackIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	owner := projectUser(t, pool, "owner@example.com")
	ctx := context.Background()
	// Inject failure on the second insert, only inside this disposable database.
	_, err := pool.Exec(ctx, `CREATE FUNCTION reject_owner() RETURNS trigger LANGUAGE plpgsql AS $$
		BEGIN RAISE EXCEPTION 'injected membership failure'; END; $$;
		CREATE TRIGGER reject_owner BEFORE INSERT ON project_members FOR EACH ROW EXECUTE FUNCTION reject_owner();`)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NewProjectRepository(pool, time.Second).Create(ctx, domain.Project{Name: "Must roll back", OwnerID: owner.ID}); err == nil {
		t.Fatal("membership failure ignored")
	}
	var projects, members int
	if err := pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM projects), (SELECT count(*) FROM project_members)`).Scan(&projects, &members); err != nil || projects != 0 || members != 0 {
		t.Fatalf("partial transaction persisted: projects=%d members=%d error=%v", projects, members, err)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("transaction leaked a connection")
	}
}

func TestProjectConstraintsIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	other := projectUser(t, pool, "other@example.com")
	p, err := NewProjectRepository(pool, time.Second).Create(ctx, domain.Project{Name: "Constraints", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name, sql string
		args      []any
		code      string
	}{
		{"duplicate", `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'member')`, []any{p.ID, owner.ID}, "23505"},
		{"invalid role", `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'superuser')`, []any{p.ID, other.ID}, "23514"},
		{"second owner", `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'owner')`, []any{p.ID, other.ID}, "23505"},
		{"missing project", `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'member')`, []any{p.ID + 100, other.ID}, "23503"},
		{"missing user", `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,'member')`, []any{p.ID, other.ID + 100}, "23503"},
		{"blank name", `UPDATE projects SET name='' WHERE id=$1`, []any{p.ID}, "23514"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, err := pool.Exec(ctx, tt.sql, tt.args...)
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != tt.code {
				t.Fatalf("constraint error: %v", err)
			}
		})
	}
}
