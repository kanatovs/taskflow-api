package service

import (
	"context"
	"errors"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func TestMe(t *testing.T) {
	failure := errors.New("repository failure")
	for _, tt := range []struct {
		name             string
		repoErr, wantErr error
	}{
		{"success", nil, nil},
		{"deleted user", domain.ErrNotFound, domain.ErrUnauthenticated},
		{"repository failure", failure, failure},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), struct{}{}, "request")
			repo := userRepositoryStub{byID: func(gotCtx context.Context, id int64) (domain.User, error) {
				if gotCtx != ctx || id != 42 {
					t.Fatal("context or ID lost")
				}
				return domain.User{ID: 42}, tt.repoErr
			}}
			user, err := NewAuthService(repo, nil).Me(ctx, 42)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Me error: %v", err)
			}
			if err == nil && user.ID != 42 {
				t.Fatal("wrong user")
			}
		})
	}
	if _, err := NewAuthService(userRepositoryStub{}, nil).Me(context.Background(), 0); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("invalid ID accepted")
	}
}
