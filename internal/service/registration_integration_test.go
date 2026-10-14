package service_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestConcurrentNormalizedRegistrationIntegration(t *testing.T) {
	pool := testutil.Postgres(t)
	auth := service.NewAuthService(postgres.NewUserRepository(pool, time.Second), nil)
	start := make(chan struct{})
	results := make(chan error, 2)
	var workers sync.WaitGroup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for _, email := range []string{"User@example.com", " user@EXAMPLE.com "} {
		workers.Go(func() {
			<-start
			_, err := auth.Register(ctx, service.RegisterInput{Email: email, Password: "strong-password-123", DisplayName: "User"})
			results <- err
		})
	}
	close(start)
	workers.Wait()
	close(results)
	successes, conflicts := 0, 0
	for err := range results {
		switch {
		case err == nil:
			successes++
		case errors.Is(err, domain.ErrEmailExists):
			conflicts++
		default:
			t.Fatalf("unexpected registration failure: %v", err)
		}
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("successes=%d conflicts=%d", successes, conflicts)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM users WHERE email=$1", "user@example.com").Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("duplicate users=%d", count)
	}
}
