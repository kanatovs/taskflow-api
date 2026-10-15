package cache

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type memoryStore struct {
	values              map[string][]byte
	ttl                 time.Duration
	gets, sets, deletes int
	err                 error
}

func (s *memoryStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	s.gets++
	v, ok := s.values[key]
	return v, ok, s.err
}
func (s *memoryStore) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	s.sets++
	s.ttl = ttl
	if s.err == nil {
		s.values[key] = value
	}
	return s.err
}
func (s *memoryStore) Delete(ctx context.Context, key string) error {
	s.deletes++
	if s.err == nil {
		delete(s.values, key)
	}
	return s.err
}

type projectSource struct {
	value domain.Project
	reads int
	err   error
}

func (s *projectSource) ByID(context.Context, int64) (domain.Project, error) {
	s.reads++
	return s.value, s.err
}

type taskSource struct {
	value domain.Task
	reads int
	err   error
}

func (s *taskSource) ByID(context.Context, int64, int64) (domain.Task, error) {
	s.reads++
	return s.value, s.err
}
func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
func projectValue() domain.Project {
	return domain.Project{ID: 1, OwnerID: 1, Name: "Project", CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}
func taskValue() domain.Task {
	return domain.Task{ID: 2, ProjectID: 1, CreatorID: 1, Title: "Task", Status: domain.TaskTodo, Priority: domain.PriorityMedium, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC()}
}

func TestProjectMissAndHit(t *testing.T) {
	store := &memoryStore{values: map[string][]byte{}}
	source := &projectSource{value: projectValue()}
	r := NewProject(source, store, time.Minute, testLogger())
	for range 2 {
		p, err := r.ByID(context.Background(), 1)
		if err != nil || p.ID != 1 {
			t.Fatalf("read: %v", err)
		}
	}
	if source.reads != 1 || store.sets != 1 || store.ttl != time.Minute {
		t.Fatal("cache did not eliminate entity read or lost explicit TTL")
	}
	var cached domain.Project
	if json.Unmarshal(store.values[ProjectKey(1)], &cached) != nil || cached.Name != source.value.Name {
		t.Fatal("cached value is not a domain project")
	}
}

func TestTaskMissAndHit(t *testing.T) {
	store := &memoryStore{values: map[string][]byte{}}
	source := &taskSource{value: taskValue()}
	r := NewTask(source, store, time.Minute, testLogger())
	for range 2 {
		v, err := r.ByID(context.Background(), 1, 2)
		if err != nil || v.ID != 2 || v.ProjectID != 1 {
			t.Fatalf("read: %v", err)
		}
	}
	if source.reads != 1 || store.sets != 1 || store.ttl != time.Minute {
		t.Fatal("cache did not eliminate entity read or lost explicit TTL")
	}
	if TaskKey(1, 2) == TaskKey(3, 2) {
		t.Fatal("task keys overlap projects")
	}
}
