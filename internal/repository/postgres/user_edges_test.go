package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestRepositoryTimeoutAndConstraintsIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	repo := NewUserRepository(pool, 50*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, user := range []domain.User{
		{Email: "a@example.com", DisplayName: "User"},
		{Email: "b@example.com", PasswordHash: "test-hash", DisplayName: ""},
	} {
		if _, err := repo.Create(ctx, user); err == nil {
			t.Fatal("database constraint not enforced")
		}
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(ctx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ByID(ctx, 1); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("repository deadline missing: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.ByEmail(ctx, "missing@example.com"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("repository did not recover: %v", err)
	}
	// pgx destroys a cancelled connection asynchronously before releasing its slot.
	deadline := time.Now().Add(time.Second)
	for pool.Stat().AcquiredConns() != 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if pool.Stat().AcquiredConns() != 0 {
		t.Fatal("repository leaked an acquired connection")
	}
	pool.Close()
	for _, call := range []func() error{
		func() error {
			_, err := repo.Create(ctx, domain.User{Email: "a@example.com", PasswordHash: "test-hash", DisplayName: "User"})
			return err
		},
		func() error { _, err := repo.ByEmail(ctx, "a@example.com"); return err },
		func() error { _, err := repo.ByID(ctx, 1); return err },
	} {
		if err := call(); err == nil {
			t.Fatal("closed pool operation succeeded")
		}
	}
}

func TestOtherUniqueViolationIsNotEmailConflict(t *testing.T) {
	input := &pgconn.PgError{Code: "23505", ConstraintName: "another_constraint"}
	if errors.Is(mapUserError(input), domain.ErrEmailExists) {
		t.Fatal("unrelated constraint mapped to email conflict")
	}
}
