package cache

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

func TestMalformedValuesFallBack(t *testing.T) {
	wrongTask := taskValue()
	wrongTask.ProjectID = 99
	wrongTaskJSON, _ := json.Marshal(wrongTask)
	for _, payload := range []string{"{", "null", "{}", `{"id":99}`, string(wrongTaskJSON)} {
		t.Run(payload, func(t *testing.T) {
			store := &memoryStore{values: map[string][]byte{ProjectKey(1): []byte(payload), TaskKey(1, 2): []byte(payload)}}
			p := &projectSource{value: projectValue()}
			task := &taskSource{value: taskValue()}
			if got, err := NewProject(p, store, time.Minute, testLogger()).ByID(context.Background(), 1); err != nil || got.ID != 1 {
				t.Fatal("project did not fall back")
			}
			if got, err := NewTask(task, store, time.Minute, testLogger()).ByID(context.Background(), 1, 2); err != nil || got.ProjectID != 1 {
				t.Fatal("task did not fall back")
			}
			if p.reads != 1 || task.reads != 1 || store.deletes != 2 || store.sets != 2 {
				t.Fatal("malformed entries were not replaced")
			}
		})
	}
}

func TestRedisFailureAndSafeWarnings(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	private := "redis://user:private-password@host:6379 token-payload"
	store := &memoryStore{values: map[string][]byte{}, err: errors.New(private)}
	p := &projectSource{value: projectValue()}
	task := &taskSource{value: taskValue()}
	projects := NewProject(p, store, time.Minute, logger)
	tasks := NewTask(task, store, time.Minute, logger)
	if _, err := projects.ByID(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ByID(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	projects.Invalidate(context.Background(), 1)
	tasks.Invalidate(context.Background(), 1, 2)
	if p.reads != 1 || task.reads != 1 || !strings.Contains(logs.String(), "invalidation failed") || strings.Contains(logs.String(), private) {
		t.Fatal("failure fallback or safe warning missing")
	}
}

func TestErrorsAreNotCached(t *testing.T) {
	for _, failure := range []error{domain.ErrNotFound, domain.ErrForbidden, errors.New("database failure")} {
		store := &memoryStore{values: map[string][]byte{}}
		p := NewProject(&projectSource{err: failure}, store, time.Minute, testLogger())
		task := NewTask(&taskSource{err: failure}, store, time.Minute, testLogger())
		if _, err := p.ByID(context.Background(), 1); !errors.Is(err, failure) {
			t.Fatal("project error lost")
		}
		if _, err := task.ByID(context.Background(), 1, 2); !errors.Is(err, failure) {
			t.Fatal("task error lost")
		}
		if store.sets != 0 || len(store.values) != 0 {
			t.Fatal("error cached")
		}
	}
}

func TestSuccessfulInvalidationPreventsStaleReads(t *testing.T) {
	ctx := context.Background()
	store := &memoryStore{values: map[string][]byte{}}
	p := &projectSource{value: projectValue()}
	task := &taskSource{value: taskValue()}
	projects := NewProject(p, store, time.Minute, testLogger())
	tasks := NewTask(task, store, time.Minute, testLogger())
	if _, err := projects.ByID(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if _, err := tasks.ByID(ctx, 1, 2); err != nil {
		t.Fatal(err)
	}
	p.value.Name = "Changed"
	task.value.Title = "Changed"
	projects.Invalidate(ctx, 1)
	tasks.Invalidate(ctx, 1, 2)
	if v, err := projects.ByID(ctx, 1); err != nil || v.Name != "Changed" {
		t.Fatal("stale project")
	}
	if v, err := tasks.ByID(ctx, 1, 2); err != nil || v.Title != "Changed" {
		t.Fatal("stale task")
	}
	p.err = domain.ErrNotFound
	task.err = domain.ErrNotFound
	projects.Invalidate(ctx, 1)
	tasks.Invalidate(ctx, 1, 2)
	if _, err := projects.ByID(ctx, 1); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted project returned")
	}
	if _, err := tasks.ByID(ctx, 1, 2); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("deleted task returned")
	}
}

func TestOnlyDomainEntitiesAreCached(t *testing.T) {
	store := &memoryStore{values: map[string][]byte{}}
	if _, err := NewProject(&projectSource{value: projectValue()}, store, time.Minute, testLogger()).ByID(context.Background(), 1); err != nil {
		t.Fatal(err)
	}
	if _, err := NewTask(&taskSource{value: taskValue()}, store, time.Minute, testLogger()).ByID(context.Background(), 1, 2); err != nil {
		t.Fatal(err)
	}
	for _, value := range store.values {
		for _, private := range []string{"password", "password_hash", "Authorization", "access_token", "JWT", `"data"`, `"error"`, `"role"`} {
			if strings.Contains(string(value), private) {
				t.Fatal("non-entity or private data in cache")
			}
		}
	}
}

func TestCancelledContextSkipsCacheAndSource(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	store := &memoryStore{values: map[string][]byte{}}
	p := &projectSource{value: projectValue()}
	task := &taskSource{value: taskValue()}
	if _, err := NewProject(p, store, time.Minute, testLogger()).ByID(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Fatal("project cancellation lost")
	}
	if _, err := NewTask(task, store, time.Minute, testLogger()).ByID(ctx, 1, 2); !errors.Is(err, context.Canceled) {
		t.Fatal("task cancellation lost")
	}
	if store.gets != 0 || p.reads != 0 || task.reads != 0 {
		t.Fatal("cancelled read performed work")
	}
}
