package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestProjectMemberRepositoryIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	ctx := context.Background()
	owner := projectUser(t, pool, "owner@example.com")
	admin := projectUser(t, pool, "admin@example.com")
	member := projectUser(t, pool, "member@example.com")
	projects := NewProjectRepository(pool, time.Second)
	repo := NewProjectMemberRepository(pool, time.Second)
	p, err := projects.Create(ctx, domain.Project{Name: "Members", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Add(ctx, p.ID, owner.ID, admin.ID, domain.RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Add(ctx, p.ID, admin.ID, member.ID, domain.RoleMember); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Add(ctx, p.ID, owner.ID, member.ID, domain.RoleMember); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := repo.Add(ctx, p.ID, owner.ID, 999999, domain.RoleMember); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing target user: %v", err)
	}
	if _, err := repo.Update(ctx, p.ID, admin.ID, member.ID, domain.RoleAdmin); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("admin promoted member")
	}
	if _, err := repo.Update(ctx, p.ID, owner.ID, owner.ID, domain.RoleMember); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("owner demoted")
	}
	if err := repo.Delete(ctx, p.ID, owner.ID, owner.ID); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("owner removed")
	}
	listed, total, err := repo.List(ctx, p.ID, member.ID, 1, 1)
	if err != nil || len(listed) != 1 || total != 3 {
		t.Fatalf("member list: %v", err)
	}
	other, err := projects.Create(ctx, domain.Project{Name: "Other", OwnerID: owner.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := repo.Delete(ctx, other.ID, owner.ID, member.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("cross-project deletion accepted")
	}
	if err := repo.Delete(ctx, p.ID, admin.ID, member.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := repo.List(ctx, p.ID, member.ID, 1, 20); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("removed member can list")
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.Add(cancelled, p.ID, owner.ID, member.ID, domain.RoleMember); !errors.Is(err, context.Canceled) {
		t.Fatalf("context lost: %v", err)
	}
}
