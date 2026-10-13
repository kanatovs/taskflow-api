package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestMapUserError(t *testing.T) {
	internal := errors.New("connection failure")
	for _, tt := range []struct{ input, want error }{
		{pgx.ErrNoRows, domain.ErrNotFound},
		{&pgconn.PgError{Code: "23505", ConstraintName: "users_email_key"}, domain.ErrEmailExists},
		{internal, internal},
	} {
		if !errors.Is(mapUserError(tt.input), tt.want) {
			t.Fatal("incorrect error mapping")
		}
	}
}

func TestUserRepositoryIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	repo := NewUserRepository(pool, time.Second)
	ctx := context.Background()
	user, err := repo.Create(ctx, domain.User{Email: "user@example.com", PasswordHash: "test-hash", DisplayName: "Test User"})
	if err != nil {
		t.Fatal(err)
	}
	if user.ID <= 0 || user.CreatedAt.IsZero() || user.UpdatedAt.IsZero() || user.CreatedAt.Location() != time.UTC {
		t.Fatalf("invalid user: %+v", user)
	}
	byEmail, err := repo.ByEmail(ctx, user.Email)
	if err != nil || byEmail != user {
		t.Fatalf("ByEmail: %v", err)
	}
	byID, err := repo.ByID(ctx, user.ID)
	if err != nil || byID != user {
		t.Fatalf("ByID: %v", err)
	}
	if _, err := repo.Create(ctx, user); !errors.Is(err, domain.ErrEmailExists) {
		t.Fatalf("duplicate: %v", err)
	}
	if _, err := repo.ByID(ctx, user.ID+1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("missing ID: %v", err)
	}
	if _, err := repo.ByEmail(ctx, "' OR 1=1 --"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("unsafe lookup: %v", err)
	}
	for _, email := range []string{"User@example.com", " user@example.com "} {
		if _, err := repo.Create(ctx, domain.User{Email: email, PasswordHash: "test-hash", DisplayName: "User"}); err == nil {
			t.Fatal("unnormalized email accepted")
		}
	}
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := repo.ByID(cancelled, user.ID); !errors.Is(err, context.Canceled) {
		t.Fatalf("context propagation: %v", err)
	}
	testutil.ApplyMigration(t, pool, "down")
	testutil.ApplyMigration(t, pool, "up")
	if _, err := repo.ByID(ctx, user.ID); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("migration round trip: %v", err)
	}
	pool.Close()
	if _, err := repo.ByID(ctx, user.ID); err == nil {
		t.Fatal("closed pool succeeded")
	}
}
