package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/domain"
)

const taskColumns = `id, project_id, title, description, status, priority, assignee_id, creator_id, due_date, created_at, updated_at`

type TaskRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewTaskRepository(pool *pgxpool.Pool, timeout time.Duration) *TaskRepository {
	return &TaskRepository{pool: pool, timeout: timeout}
}

func (r *TaskRepository) Create(ctx context.Context, input domain.Task) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, input.ProjectID, input.CreatorID)
	if err != nil {
		return domain.Task{}, err
	}
	defer rollback(tx)
	if input.Status != domain.TaskTodo {
		return domain.Task{}, &domain.ValidationError{Fields: map[string]string{"status": "new tasks must start as todo"}}
	}
	if err := checkTaskAssignee(ctx, tx, input.ProjectID, input.AssigneeID); err != nil {
		return domain.Task{}, err
	}
	task, err := scanTask(tx.QueryRow(ctx, `INSERT INTO tasks
        (project_id,title,description,status,priority,assignee_id,creator_id,due_date)
        VALUES ($1,$2,$3,$4,$5,$6,$7,$8) RETURNING `+taskColumns,
		input.ProjectID, input.Title, input.Description, input.Status, input.Priority, input.AssigneeID, input.CreatorID, input.DueDate))
	if err != nil {
		return domain.Task{}, fmt.Errorf("create task: %w", mapTaskError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("commit task creation: %w", err)
	}
	return task, nil
}

func (r *TaskRepository) ByID(ctx context.Context, projectID, taskID int64) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	task, err := scanTask(r.pool.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE project_id=$1 AND id=$2`, projectID, taskID))
	if err != nil {
		return domain.Task{}, fmt.Errorf("find task: %w", mapTaskError(err))
	}
	return task, nil
}

func (r *TaskRepository) List(ctx context.Context, projectID int64, page, size int) ([]domain.Task, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin task listing: %w", err)
	}
	defer rollback(tx)
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM tasks WHERE project_id=$1`, projectID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tasks: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT `+taskColumns+` FROM tasks WHERE project_id=$1 ORDER BY id LIMIT $2 OFFSET $3`, projectID, size, int64(page-1)*int64(size))
	if err != nil {
		return nil, 0, fmt.Errorf("list tasks: %w", err)
	}
	defer rows.Close()
	tasks := make([]domain.Task, 0)
	for rows.Next() {
		task, err := scanTask(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan task: %w", err)
		}
		tasks = append(tasks, task)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read tasks: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit task listing: %w", err)
	}
	return tasks, total, nil
}

func (r *TaskRepository) Update(ctx context.Context, projectID, taskID, actorID int64, changes domain.TaskChanges) (domain.Task, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, projectID, actorID)
	if err != nil {
		return domain.Task{}, err
	}
	defer rollback(tx)
	current, role, err := taskWriteAccess(ctx, tx, projectID, taskID, actorID)
	if err != nil {
		return domain.Task{}, err
	}
	if err := domain.ValidateTaskUpdate(role, actorID, current, changes); err != nil {
		return domain.Task{}, err
	}
	if changes.AssigneeID.Set {
		if err := checkTaskAssignee(ctx, tx, projectID, changes.AssigneeID.Value); err != nil {
			return domain.Task{}, err
		}
	}
	task, err := scanTask(tx.QueryRow(ctx, `UPDATE tasks SET
        title=COALESCE($3,title), description=COALESCE($4,description), status=COALESCE($5,status), priority=COALESCE($6,priority),
        assignee_id=CASE WHEN $7 THEN $8 ELSE assignee_id END,
        due_date=CASE WHEN $9 THEN $10 ELSE due_date END, updated_at=CURRENT_TIMESTAMP
        WHERE project_id=$1 AND id=$2 RETURNING `+taskColumns,
		projectID, taskID, changes.Title, changes.Description, changes.Status, changes.Priority,
		changes.AssigneeID.Set, changes.AssigneeID.Value, changes.DueDate.Set, changes.DueDate.Value))
	if err != nil {
		return domain.Task{}, fmt.Errorf("update task: %w", mapTaskError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Task{}, fmt.Errorf("commit task update: %w", err)
	}
	return task, nil
}

func (r *TaskRepository) Delete(ctx context.Context, projectID, taskID, actorID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, projectID, actorID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	task, role, err := taskWriteAccess(ctx, tx, projectID, taskID, actorID)
	if err != nil {
		return err
	}
	if !domain.CanManageTask(role, actorID, task) {
		return domain.ErrForbidden
	}
	result, err := tx.Exec(ctx, `DELETE FROM tasks WHERE project_id=$1 AND id=$2`, projectID, taskID)
	if err != nil {
		return fmt.Errorf("delete task: %w", err)
	}
	if result.RowsAffected() == 0 {
		return domain.ErrNotFound
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit task deletion: %w", err)
	}
	return nil
}

func taskWriteAccess(ctx context.Context, tx pgx.Tx, projectID, taskID, actorID int64) (domain.Task, domain.ProjectRole, error) {
	task, err := scanTask(tx.QueryRow(ctx, `SELECT `+taskColumns+` FROM tasks WHERE project_id=$1 AND id=$2 FOR UPDATE`, projectID, taskID))
	if err != nil {
		return domain.Task{}, "", fmt.Errorf("lock task: %w", mapTaskError(err))
	}
	role, err := memberRole(ctx, tx, projectID, actorID)
	return task, role, err
}

func checkTaskAssignee(ctx context.Context, tx pgx.Tx, projectID int64, assigneeID *int64) error {
	if assigneeID == nil {
		return nil
	}
	_, err := memberRole(ctx, tx, projectID, *assigneeID)
	if errors.Is(err, domain.ErrForbidden) {
		return &domain.ValidationError{Fields: map[string]string{"assignee_id": "must identify a current project member"}}
	}
	return err
}

// Project/member writes use the same lock, so membership cannot disappear mid-write.
func beginTaskWrite(ctx context.Context, pool *pgxpool.Pool, projectID, actorID int64) (pgx.Tx, error) {
	tx, err := pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin task write: %w", err)
	}
	if err := lockProject(ctx, tx, projectID); err != nil {
		rollback(tx)
		return nil, err
	}
	if _, err := memberRole(ctx, tx, projectID, actorID); err != nil {
		rollback(tx)
		return nil, err
	}
	return tx, nil
}

func scanTask(row pgx.Row) (domain.Task, error) {
	var task domain.Task
	err := row.Scan(&task.ID, &task.ProjectID, &task.Title, &task.Description, &task.Status, &task.Priority,
		&task.AssigneeID, &task.CreatorID, &task.DueDate, &task.CreatedAt, &task.UpdatedAt)
	task.CreatedAt, task.UpdatedAt = task.CreatedAt.UTC(), task.UpdatedAt.UTC()
	if task.DueDate != nil {
		utc := task.DueDate.UTC()
		task.DueDate = &utc
	}
	return task, err
}

func mapTaskError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		switch pgErr.Code {
		case "23503":
			return domain.ErrNotFound
		case "23505":
			return domain.ErrConflict
		case "23514":
			return &domain.ValidationError{Fields: map[string]string{"task": "violates a task constraint"}}
		}
	}
	return err
}
