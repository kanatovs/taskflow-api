package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type taskStoreFake struct {
	task    domain.Task
	err     error
	readErr error
	ctx     context.Context
	writes  int
	calls   int
	changes domain.TaskChanges
}

func (f *taskStoreFake) Create(ctx context.Context, input domain.Task) (domain.Task, error) {
	f.ctx = ctx
	f.calls++
	f.writes++
	f.task = input
	return input, f.err
}
func (f *taskStoreFake) ByID(ctx context.Context, _, _ int64) (domain.Task, error) {
	f.ctx = ctx
	f.calls++
	return f.task, f.readErr
}
func (f *taskStoreFake) List(ctx context.Context, _ int64, _ domain.TaskQuery) ([]domain.Task, int, error) {
	f.ctx = ctx
	f.calls++
	return []domain.Task{f.task}, 1, f.err
}
func (f *taskStoreFake) Update(ctx context.Context, _, _, _ int64, c domain.TaskChanges) (domain.Task, error) {
	f.ctx = ctx
	f.calls++
	f.writes++
	f.changes = c
	return f.task, f.err
}
func (f *taskStoreFake) Delete(ctx context.Context, _, _, _ int64) error {
	f.ctx = ctx
	f.calls++
	f.writes++
	return f.err
}

func taskTestProjects(role domain.ProjectRole) *ProjectService {
	return NewProjectService(&projectStoreFake{project: domain.Project{ID: 1, OwnerID: 1}}, projectMembersFake{role: role}, projectUsersFake{})
}

func TestTaskServiceFailuresAndContext(t *testing.T) {
	type requestKey struct{}
	ctx := context.WithValue(context.Background(), requestKey{}, "request")
	failure := errors.New("repository unavailable")
	title := "Updated"
	for _, name := range []string{"create", "get", "list", "update", "delete"} {
		t.Run(name, func(t *testing.T) {
			repo := &taskStoreFake{task: domain.Task{ID: 1, CreatorID: 1, Status: domain.TaskTodo}, err: failure}
			if name == "get" {
				repo.readErr = failure
			}
			s := NewTaskService(taskTestProjects(domain.RoleOwner), repo)
			operation := func(ctx context.Context) error {
				switch name {
				case "create":
					_, err := s.Create(ctx, 1, 1, CreateTaskInput{Title: "Task"})
					return err
				case "get":
					_, err := s.Get(ctx, 1, 1, 1)
					return err
				case "list":
					_, _, err := s.List(ctx, 1, 1, domain.TaskQuery{Page: 1, PageSize: 20, Sort: "id", Order: "asc"})
					return err
				case "update":
					_, err := s.Update(ctx, 1, 1, 1, domain.TaskChanges{Title: &title})
					return err
				default:
					return s.Delete(ctx, 1, 1, 1)
				}
			}
			if err := operation(ctx); !errors.Is(err, failure) || repo.ctx != ctx {
				t.Fatalf("failure/context lost: %v", err)
			}
			repo.calls = 0
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			if err := operation(cancelled); !errors.Is(err, context.Canceled) || repo.calls != 0 {
				t.Fatalf("cancelled call reached repository: %v", err)
			}
		})
	}
}

func TestTaskServiceValidation(t *testing.T) {
	ctx := context.Background()
	repo := &taskStoreFake{task: domain.Task{ID: 1, CreatorID: 1, Status: domain.TaskTodo}}
	s := NewTaskService(taskTestProjects(domain.RoleOwner), repo)
	zero := int64(0)
	badDate := time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC)
	for _, input := range []CreateTaskInput{
		{}, {Title: " "}, {Title: strings.Repeat("a", 201)}, {Title: "a\x00"}, {Title: "a", Description: strings.Repeat("x", 2001)},
		{Title: "a", Status: "invalid"}, {Title: "a", Priority: "urgent"}, {Title: "a", AssigneeID: &zero}, {Title: "a", DueDate: &badDate},
		{Title: "a", Status: domain.TaskDone},
	} {
		var validation *domain.ValidationError
		if _, err := s.Create(ctx, 1, 1, input); !errors.As(err, &validation) {
			t.Fatalf("invalid task accepted: %+v %v", input, err)
		}
	}
	if repo.writes != 0 {
		t.Fatal("invalid input persisted")
	}
	due := time.Date(2026, 10, 20, 15, 0, 0, 0, time.FixedZone("offset", 5*3600))
	task, err := s.Create(ctx, 1, 1, CreateTaskInput{Title: " Trimmed ", DueDate: &due})
	if err != nil || task.Title != "Trimmed" || task.DueDate.Location() != time.UTC || task.DueDate.Hour() != 10 || task.CreatorID != 1 {
		t.Fatal("incorrect task normalization")
	}
	if _, err := s.Update(ctx, 1, 1, 1, domain.TaskChanges{}); err == nil {
		t.Fatal("empty patch accepted")
	}
	if _, err := s.Get(ctx, 1, 1, 0); err == nil {
		t.Fatal("invalid task ID accepted")
	}
	if _, err := s.Create(ctx, 0, 1, CreateTaskInput{Title: "Task"}); !errors.Is(err, domain.ErrUnauthenticated) {
		t.Fatal("missing actor accepted")
	}
	repo.readErr = domain.ErrNotFound
	if _, err := s.Get(ctx, 1, 1, 999); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("not found lost")
	}
}

func TestTaskServiceForbiddenDoesNotWrite(t *testing.T) {
	repo := &taskStoreFake{task: domain.Task{ID: 1, CreatorID: 2, Status: domain.TaskTodo}}
	s := NewTaskService(taskTestProjects(domain.RoleMember), repo)
	title := "Forbidden"
	if _, err := s.Update(context.Background(), 1, 1, 1, domain.TaskChanges{Title: &title}); !errors.Is(err, domain.ErrForbidden) {
		t.Fatal("unrelated member edit accepted")
	}
	if err := s.Delete(context.Background(), 1, 1, 1); !errors.Is(err, domain.ErrForbidden) || repo.writes != 0 {
		t.Fatal("forbidden task write reached persistence")
	}
}

type taskMemberReader struct{ targetErr error }

func (f taskMemberReader) ByUser(_ context.Context, _, user int64) (domain.ProjectMember, error) {
	if user == 1 {
		return domain.ProjectMember{Role: domain.RoleOwner}, nil
	}
	return domain.ProjectMember{}, f.targetErr
}

func TestTaskAssigneeLookupFailures(t *testing.T) {
	failure := errors.New("membership repository unavailable")
	assignee := int64(2)
	for _, readErr := range []error{domain.ErrNotFound, failure} {
		projects := taskTestProjects(domain.RoleOwner)
		projects.members = taskMemberReader{targetErr: readErr}
		repo := &taskStoreFake{task: domain.Task{CreatorID: 1, Status: domain.TaskTodo}}
		s := NewTaskService(projects, repo)
		_, err := s.Create(context.Background(), 1, 1, CreateTaskInput{Title: "Task", AssigneeID: &assignee})
		var validation *domain.ValidationError
		if readErr == domain.ErrNotFound && !errors.As(err, &validation) {
			t.Fatal("nonmember assignment accepted")
		}
		if readErr == failure && !errors.Is(err, failure) {
			t.Fatal("membership failure lost")
		}
		if repo.writes != 0 {
			t.Fatal("invalid assignment reached persistence")
		}
	}
}

func TestTaskQueryValidation(t *testing.T) {
	good := domain.TaskQuery{Page: 1, PageSize: 20, Sort: "id", Order: "asc"}
	if err := validateTaskQuery(good); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*domain.TaskQuery){
		func(q *domain.TaskQuery) { q.Page = 0 }, func(q *domain.TaskQuery) { q.PageSize = 101 },
		func(q *domain.TaskQuery) { q.Status = "invalid" }, func(q *domain.TaskQuery) { q.Priority = "urgent" },
		func(q *domain.TaskQuery) { q.Sort = "id;DROP TABLE tasks" }, func(q *domain.TaskQuery) { q.Order = "sideways" },
		func(q *domain.TaskQuery) { q.Search = strings.Repeat("a", 201) }, func(q *domain.TaskQuery) { q.Search = "\x00" },
	} {
		q := good
		change(&q)
		var validation *domain.ValidationError
		if err := validateTaskQuery(q); !errors.As(err, &validation) {
			t.Fatalf("invalid query accepted: %+v", q)
		}
	}
}
