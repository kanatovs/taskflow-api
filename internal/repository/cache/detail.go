// Package cache implements read-through detail repositories. Callers must check
// PostgreSQL-backed authorization before using them.
package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/kanatovs/taskflow-api/internal/domain"
)

type Store interface {
	Get(context.Context, string) ([]byte, bool, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

func ProjectKey(id int64) string { return fmt.Sprintf("taskflow:v1:project:%d", id) }
func TaskKey(projectID, taskID int64) string {
	return fmt.Sprintf("%s:task:%d", ProjectKey(projectID), taskID)
}

type ProjectReader interface {
	ByID(context.Context, int64) (domain.Project, error)
}
type TaskReader interface {
	ByID(context.Context, int64, int64) (domain.Task, error)
}

type Project struct {
	source ProjectReader
	store  Store
	ttl    time.Duration
	logger *slog.Logger
}

func NewProject(source ProjectReader, store Store, ttl time.Duration, logger *slog.Logger) *Project {
	return &Project{source: source, store: store, ttl: ttl, logger: logger}
}

func (r *Project) ByID(ctx context.Context, id int64) (domain.Project, error) {
	return readThrough(ctx, r.store, ProjectKey(id), r.ttl, r.logger,
		func(ctx context.Context) (domain.Project, error) { return r.source.ByID(ctx, id) },
		func(p domain.Project) bool {
			return p.ID == id && p.ID > 0 && p.OwnerID > 0 && p.Name != "" && !p.CreatedAt.IsZero() && !p.UpdatedAt.IsZero()
		})
}

func (r *Project) Invalidate(ctx context.Context, id int64) {
	invalidate(ctx, r.store, ProjectKey(id), r.logger)
}

type Task struct {
	source TaskReader
	store  Store
	ttl    time.Duration
	logger *slog.Logger
}

func NewTask(source TaskReader, store Store, ttl time.Duration, logger *slog.Logger) *Task {
	return &Task{source: source, store: store, ttl: ttl, logger: logger}
}

func (r *Task) ByID(ctx context.Context, projectID, taskID int64) (domain.Task, error) {
	return readThrough(ctx, r.store, TaskKey(projectID, taskID), r.ttl, r.logger,
		func(ctx context.Context) (domain.Task, error) { return r.source.ByID(ctx, projectID, taskID) },
		func(t domain.Task) bool {
			return t.ID == taskID && t.ProjectID == projectID && t.ID > 0 && t.CreatorID > 0 && t.Title != "" && !t.CreatedAt.IsZero() && !t.UpdatedAt.IsZero() &&
				(t.Status == domain.TaskTodo || t.Status == domain.TaskInProgress || t.Status == domain.TaskDone) &&
				(t.Priority == domain.PriorityLow || t.Priority == domain.PriorityMedium || t.Priority == domain.PriorityHigh)
		})
}

func (r *Task) Invalidate(ctx context.Context, projectID, taskID int64) {
	invalidate(ctx, r.store, TaskKey(projectID, taskID), r.logger)
}

func invalidate(ctx context.Context, store Store, key string, logger *slog.Logger) {
	if err := store.Delete(ctx, key); err != nil {
		logger.WarnContext(ctx, "Redis invalidation failed; stale data is bounded by TTL", "cache_key", key)
	}
}

func readThrough[T any](ctx context.Context, store Store, key string, ttl time.Duration, logger *slog.Logger, load func(context.Context) (T, error), valid func(T) bool) (T, error) {
	var result T
	if err := ctx.Err(); err != nil {
		return result, err
	}
	value, hit, err := store.Get(ctx, key)
	if err != nil {
		logger.WarnContext(ctx, "Redis read unavailable or timed out; using PostgreSQL", "cache_key", key)
	} else if hit {
		if json.Unmarshal(value, &result) == nil && valid(result) {
			if err := ctx.Err(); err != nil {
				var zero T
				return zero, err
			}
			return result, nil
		}
		logger.WarnContext(ctx, "Malformed cached value; using PostgreSQL", "cache_key", key)
		if err := store.Delete(ctx, key); err != nil {
			logger.WarnContext(ctx, "Redis malformed value cleanup failed", "cache_key", key)
		}
	}
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	result, err = load(ctx)
	if err != nil {
		return result, err
	}
	if ctx.Err() == nil {
		encoded, encodeErr := json.Marshal(result)
		if encodeErr != nil {
			logger.WarnContext(ctx, "Cache entity encoding failed", "cache_key", key)
		} else if err := store.Set(ctx, key, encoded, ttl); err != nil {
			logger.WarnContext(ctx, "Redis write unavailable or timed out", "cache_key", key)
		}
	}
	if err := ctx.Err(); err != nil {
		var zero T
		return zero, err
	}
	return result, nil
}
