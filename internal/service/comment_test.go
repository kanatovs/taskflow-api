package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type commentStoreFake struct {
	comment domain.Comment
	err     error
	readErr error
	ctx     context.Context
	calls   int
	writes  int
}

func (f *commentStoreFake) Create(ctx context.Context, _ int64, c domain.Comment) (domain.Comment, error) {
	f.ctx = ctx
	f.calls++
	f.writes++
	return c, f.err
}
func (f *commentStoreFake) ByID(ctx context.Context, _, _, _ int64) (domain.Comment, error) {
	f.ctx = ctx
	f.calls++
	return f.comment, f.readErr
}
func (f *commentStoreFake) List(ctx context.Context, _, _ int64, _, _ int) ([]domain.Comment, int, error) {
	f.ctx = ctx
	f.calls++
	return []domain.Comment{}, 0, f.err
}
func (f *commentStoreFake) Update(ctx context.Context, _, _, _, _ int64, body string) (domain.Comment, error) {
	f.ctx = ctx
	f.calls++
	f.writes++
	return domain.Comment{Body: body}, f.err
}
func (f *commentStoreFake) Delete(ctx context.Context, _, _, _, _ int64) error {
	f.ctx = ctx
	f.calls++
	f.writes++
	return f.err
}

func TestCommentServiceFailuresAndContext(t *testing.T) {
	type requestKey struct{}
	ctx := context.WithValue(context.Background(), requestKey{}, "request")
	failure := errors.New("comment repository unavailable")
	for _, name := range []string{"create", "list", "update", "delete"} {
		t.Run(name, func(t *testing.T) {
			repo := &commentStoreFake{comment: domain.Comment{AuthorID: 1}, err: failure}
			tasks := &taskStoreFake{task: domain.Task{ID: 1, CreatorID: 1}}
			s := NewCommentService(taskTestProjects(domain.RoleOwner), tasks, repo)
			operation := func(ctx context.Context) error {
				switch name {
				case "create":
					_, err := s.Create(ctx, 1, 1, 1, CommentInput{Body: "Comment"})
					return err
				case "list":
					_, _, err := s.List(ctx, 1, 1, 1, 1, 20)
					return err
				case "update":
					_, err := s.Update(ctx, 1, 1, 1, 1, CommentInput{Body: "Edited"})
					return err
				default:
					return s.Delete(ctx, 1, 1, 1, 1)
				}
			}
			if err := operation(ctx); !errors.Is(err, failure) || repo.ctx != ctx || tasks.ctx != ctx {
				t.Fatalf("failure/context lost: %v", err)
			}
			repo.calls, tasks.calls = 0, 0
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err := operation(cancelled); !errors.Is(err, context.Canceled) || repo.calls != 0 || tasks.calls != 0 {
				t.Fatal("cancelled comment operation reached persistence")
			}
		})
	}
}

func TestCommentServiceValidationAndPermissions(t *testing.T) {
	for _, body := range []string{"", " \n\t ", strings.Repeat("x", 2001), "\x00", string([]byte{0xff})} {
		var validation *domain.ValidationError
		if _, err := validateCommentBody(body); !errors.As(err, &validation) {
			t.Fatal("invalid comment accepted")
		}
	}
	if body, err := validateCommentBody(" \nComment\nline\t "); err != nil || body != "Comment\nline" {
		t.Fatal("comment normalization failed")
	}
	repo := &commentStoreFake{comment: domain.Comment{AuthorID: 2}}
	tasks := &taskStoreFake{task: domain.Task{ID: 1}}
	s := NewCommentService(taskTestProjects(domain.RoleMember), tasks, repo)
	ctx := context.Background()
	if _, err := s.Update(ctx, 1, 1, 1, 1, CommentInput{Body: "Edit"}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("non-author edit accepted")
	}
	if err := s.Delete(ctx, 1, 1, 1, 1); !errors.Is(err, domain.ErrForbidden) || repo.writes != 0 {
		t.Fatal("forbidden comment persisted")
	}
	repo.readErr = domain.ErrNotFound
	if err := s.Delete(ctx, 1, 1, 1, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing comment lost")
	}
	tasks.readErr = domain.ErrNotFound
	if _, err := s.Create(ctx, 1, 1, 1, CommentInput{Body: "Comment"}); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("missing task lost")
	}
}
