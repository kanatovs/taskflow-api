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

const commentColumns = `id, task_id, author_id, body, created_at, updated_at`

type CommentRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewCommentRepository(pool *pgxpool.Pool, timeout time.Duration) *CommentRepository {
	return &CommentRepository{pool: pool, timeout: timeout}
}

func (r *CommentRepository) Create(ctx context.Context, projectID int64, input domain.Comment) (domain.Comment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, projectID, input.AuthorID)
	if err != nil {
		return domain.Comment{}, err
	}
	defer rollback(tx)
	if _, _, err := taskWriteAccess(ctx, tx, projectID, input.TaskID, input.AuthorID); err != nil {
		return domain.Comment{}, err
	}
	comment, err := scanComment(tx.QueryRow(ctx, `INSERT INTO comments (task_id,author_id,body) VALUES ($1,$2,$3) RETURNING `+commentColumns, input.TaskID, input.AuthorID, input.Body))
	if err != nil {
		return domain.Comment{}, fmt.Errorf("create comment: %w", mapCommentError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, fmt.Errorf("commit comment creation: %w", err)
	}
	return comment, nil
}

func (r *CommentRepository) ByID(ctx context.Context, projectID, taskID, commentID int64) (domain.Comment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	comment, err := scanComment(r.pool.QueryRow(ctx, `SELECT `+commentColumns+` FROM comments WHERE id=$3 AND task_id=$2
        AND EXISTS (SELECT 1 FROM tasks WHERE id=$2 AND project_id=$1)`, projectID, taskID, commentID))
	if err != nil {
		return domain.Comment{}, fmt.Errorf("find comment: %w", mapCommentError(err))
	}
	return comment, nil
}

func (r *CommentRepository) List(ctx context.Context, projectID, taskID int64, page, size int) ([]domain.Comment, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin comment listing: %w", err)
	}
	defer rollback(tx)
	const scope = ` FROM comments WHERE task_id=$2 AND EXISTS (SELECT 1 FROM tasks WHERE id=$2 AND project_id=$1)`
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*)`+scope, projectID, taskID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count comments: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT `+commentColumns+scope+` ORDER BY id LIMIT $3 OFFSET $4`, projectID, taskID, size, int64(page-1)*int64(size))
	if err != nil {
		return nil, 0, fmt.Errorf("list comments: %w", err)
	}
	defer rows.Close()
	comments := make([]domain.Comment, 0)
	for rows.Next() {
		comment, err := scanComment(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan comment: %w", err)
		}
		comments = append(comments, comment)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read comments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit comment listing: %w", err)
	}
	return comments, total, nil
}

func (r *CommentRepository) Update(ctx context.Context, projectID, taskID, commentID, actorID int64, body string) (domain.Comment, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, projectID, actorID)
	if err != nil {
		return domain.Comment{}, err
	}
	defer rollback(tx)
	if err := commentWriteAccess(ctx, tx, projectID, taskID, commentID, actorID); err != nil {
		return domain.Comment{}, err
	}
	comment, err := scanComment(tx.QueryRow(ctx, `UPDATE comments SET body=$3,updated_at=CURRENT_TIMESTAMP WHERE task_id=$1 AND id=$2 RETURNING `+commentColumns, taskID, commentID, body))
	if err != nil {
		return domain.Comment{}, fmt.Errorf("update comment: %w", mapCommentError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Comment{}, fmt.Errorf("commit comment update: %w", err)
	}
	return comment, nil
}

func (r *CommentRepository) Delete(ctx context.Context, projectID, taskID, commentID, actorID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := beginTaskWrite(ctx, r.pool, projectID, actorID)
	if err != nil {
		return err
	}
	defer rollback(tx)
	if err := commentWriteAccess(ctx, tx, projectID, taskID, commentID, actorID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM comments WHERE task_id=$1 AND id=$2`, taskID, commentID); err != nil {
		return fmt.Errorf("delete comment: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit comment deletion: %w", err)
	}
	return nil
}

func commentWriteAccess(ctx context.Context, tx pgx.Tx, projectID, taskID, commentID, actorID int64) error {
	_, role, err := taskWriteAccess(ctx, tx, projectID, taskID, actorID)
	if err != nil {
		return err
	}
	comment, err := scanComment(tx.QueryRow(ctx, `SELECT `+commentColumns+` FROM comments WHERE task_id=$1 AND id=$2 FOR UPDATE`, taskID, commentID))
	if err != nil {
		return fmt.Errorf("lock comment: %w", mapCommentError(err))
	}
	if !domain.CanManageComment(role, actorID, comment) {
		return domain.ErrForbidden
	}
	return nil
}

func scanComment(row pgx.Row) (domain.Comment, error) {
	var c domain.Comment
	err := row.Scan(&c.ID, &c.TaskID, &c.AuthorID, &c.Body, &c.CreatedAt, &c.UpdatedAt)
	c.CreatedAt, c.UpdatedAt = c.CreatedAt.UTC(), c.UpdatedAt.UTC()
	return c, err
}

func mapCommentError(err error) error {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23514" {
		return &domain.ValidationError{Fields: map[string]string{"body": "violates a comment constraint"}}
	}
	return mapTaskError(err)
}
