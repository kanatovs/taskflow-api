package handler_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/kanatovs/taskflow-api/internal/domain"
	"github.com/kanatovs/taskflow-api/internal/handler"
	"github.com/kanatovs/taskflow-api/internal/platform/token"
	"github.com/kanatovs/taskflow-api/internal/repository/postgres"
	"github.com/kanatovs/taskflow-api/internal/service"
	"github.com/kanatovs/taskflow-api/internal/testutil"
)

func TestRequestTimeoutPostgresIntegration(t *testing.T) {
	for _, tt := range []struct {
		name           string
		requestTimeout time.Duration
		dbTimeout      time.Duration
		wantStatus     int
		wantCode       string
	}{
		{"request budget expires first", 200 * time.Millisecond, 5 * time.Second, http.StatusServiceUnavailable, "request_timeout"},
		{"database budget expires first", 5 * time.Second, 200 * time.Millisecond, http.StatusInternalServerError, "internal_error"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			pool := testutil.Postgres(t)
			users := postgres.NewUserRepository(pool, tt.dbTimeout)
			setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer setupCancel()
			setupUsers := postgres.NewUserRepository(pool, 5*time.Second)
			user, err := setupUsers.Create(setupCtx, domain.User{Email: "deadline@example.com", DisplayName: "Deadline", PasswordHash: "private-test-hash"})
			if err != nil {
				t.Fatal(err)
			}
			issuer, err := token.NewJWT("unit-test-secret-at-least-32-bytes", time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			access, err := issuer.Issue(user.ID)
			if err != nil {
				t.Fatal(err)
			}

			// Block the real repository read without changing any application data.
			lock, err := pool.Begin(setupCtx)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				if err := lock.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
					t.Error("release test table lock:", err)
				}
			})
			if _, err := lock.Exec(setupCtx, "LOCK TABLE users IN ACCESS EXCLUSIVE MODE"); err != nil {
				t.Fatal(err)
			}

			router := handler.NewRouter(tt.requestTimeout, slog.New(slog.NewJSONHandler(io.Discard, nil)), nil,
				handler.NewAuthHandler(service.NewAuthService(users, issuer), issuer), nil, nil)
			server := httptest.NewServer(router)
			t.Cleanup(server.Close)
			client := server.Client()
			client.Timeout = 3 * time.Second
			send := func(wantStatus int) []byte {
				t.Helper()
				r, err := http.NewRequest(http.MethodGet, server.URL+"/api/v1/users/me", nil)
				if err != nil {
					t.Fatal(err)
				}
				r.Header.Set("Authorization", "Bearer "+access.AccessToken)
				response, err := client.Do(r)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				body, err := io.ReadAll(response.Body)
				if err != nil {
					t.Fatal(err)
				}
				if response.StatusCode != wantStatus || response.Header.Get("Content-Type") != "application/json" || response.Header.Get("X-Request-ID") == "" {
					t.Fatalf("GET current user: status=%d headers=%v body=%s", response.StatusCode, response.Header, body)
				}
				return body
			}
			body := send(tt.wantStatus)
			var failure struct {
				Error struct {
					Code string `json:"code"`
				} `json:"error"`
			}
			if err := json.Unmarshal(body, &failure); err != nil || failure.Error.Code != tt.wantCode {
				t.Fatalf("unexpected deadline response: %s", body)
			}
			releaseCtx, releaseCancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer releaseCancel()
			if err := lock.Rollback(releaseCtx); err != nil {
				t.Fatal(err)
			}
			send(http.StatusOK)
		})
	}
}
