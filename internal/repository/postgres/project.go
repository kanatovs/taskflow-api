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

type ProjectRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewProjectRepository(pool *pgxpool.Pool, timeout time.Duration) *ProjectRepository {
	return &ProjectRepository{pool: pool, timeout: timeout}
}

func (r *ProjectRepository) Create(ctx context.Context, input domain.Project) (domain.Project, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Project{}, fmt.Errorf("begin project creation: %w", err)
	}
	defer rollback(tx)
	project, err := scanProject(tx.QueryRow(ctx, `
		INSERT INTO projects (name, description, owner_id) VALUES ($1, $2, $3)
		RETURNING id, name, description, owner_id, created_at, updated_at`, input.Name, input.Description, input.OwnerID))
	if err != nil {
		return domain.Project{}, fmt.Errorf("create project: %w", mapProjectError(err))
	}
	if _, err := tx.Exec(ctx, `INSERT INTO project_members (project_id, user_id, role) VALUES ($1, $2, 'owner')`, project.ID, project.OwnerID); err != nil {
		return domain.Project{}, fmt.Errorf("create owner membership: %w", mapProjectError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Project{}, fmt.Errorf("commit project creation: %w", err)
	}
	return project, nil
}

func (r *ProjectRepository) ByID(ctx context.Context, id int64) (domain.Project, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	project, err := scanProject(r.pool.QueryRow(ctx, `SELECT id, name, description, owner_id, created_at, updated_at FROM projects WHERE id = $1`, id))
	if err != nil {
		return domain.Project{}, fmt.Errorf("find project: %w", mapProjectError(err))
	}
	return project, nil
}

func (r *ProjectRepository) ListByUser(ctx context.Context, userID int64, page, pageSize int) ([]domain.Project, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	// Count and page use the same snapshot, including for an empty last page.
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin project listing: %w", err)
	}
	defer rollback(tx)
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE user_id = $1`, userID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count user projects: %w", err)
	}
	rows, err := tx.Query(ctx, `
		SELECT p.id, p.name, p.description, p.owner_id, p.created_at, p.updated_at
		FROM projects p JOIN project_members m ON m.project_id = p.id
		WHERE m.user_id = $1 ORDER BY p.id LIMIT $2 OFFSET $3`, userID, pageSize, int64(page-1)*int64(pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("list user projects: %w", err)
	}
	defer rows.Close()
	projects := make([]domain.Project, 0)
	for rows.Next() {
		project, err := scanProject(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan project: %w", err)
		}
		projects = append(projects, project)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read projects: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit project listing: %w", err)
	}
	return projects, total, nil
}

func (r *ProjectRepository) Update(ctx context.Context, id, actorID int64, changes domain.ProjectChanges) (domain.Project, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.Project{}, fmt.Errorf("begin project update: %w", err)
	}
	defer rollback(tx)
	if err := lockProject(ctx, tx, id); err != nil {
		return domain.Project{}, err
	}
	role, err := memberRole(ctx, tx, id, actorID)
	if err != nil {
		return domain.Project{}, err
	}
	if role != domain.RoleOwner && role != domain.RoleAdmin {
		return domain.Project{}, domain.ErrForbidden
	}
	project, err := scanProject(tx.QueryRow(ctx, `UPDATE projects SET name = COALESCE($2, name), description = COALESCE($3, description), updated_at = CURRENT_TIMESTAMP
		WHERE id = $1 RETURNING id, name, description, owner_id, created_at, updated_at`, id, changes.Name, changes.Description))
	if err != nil {
		return domain.Project{}, fmt.Errorf("update project: %w", mapProjectError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.Project{}, fmt.Errorf("commit project update: %w", err)
	}
	return project, nil
}

func (r *ProjectRepository) Delete(ctx context.Context, id, actorID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin project deletion: %w", err)
	}
	defer rollback(tx)
	if err := lockProject(ctx, tx, id); err != nil {
		return err
	}
	role, err := memberRole(ctx, tx, id, actorID)
	if err != nil {
		return err
	}
	if role != domain.RoleOwner {
		return domain.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `DELETE FROM projects WHERE id = $1`, id); err != nil {
		return fmt.Errorf("delete project: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit project deletion: %w", err)
	}
	return nil
}

func scanProject(row pgx.Row) (domain.Project, error) {
	var p domain.Project
	err := row.Scan(&p.ID, &p.Name, &p.Description, &p.OwnerID, &p.CreatedAt, &p.UpdatedAt)
	p.CreatedAt, p.UpdatedAt = p.CreatedAt.UTC(), p.UpdatedAt.UTC()
	return p, err
}

// Every project/member mutation takes this lock before checking current roles.
// This prevents a role change from racing an already authorized write.
func lockProject(ctx context.Context, tx pgx.Tx, id int64) error {
	var found int64
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id = $1 FOR UPDATE`, id).Scan(&found); err != nil {
		return fmt.Errorf("lock project: %w", mapProjectError(err))
	}
	return nil
}

func memberRole(ctx context.Context, tx pgx.Tx, projectID, userID int64) (domain.ProjectRole, error) {
	var role domain.ProjectRole
	if err := tx.QueryRow(ctx, `SELECT role FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID).Scan(&role); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domain.ErrForbidden
		}
		return "", fmt.Errorf("read project permission: %w", err)
	}
	return role, nil
}

func rollback(tx pgx.Tx) {
	// A cancelled request still needs bounded connection cleanup.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_ = tx.Rollback(ctx)
}

func mapProjectError(err error) error {
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
			return &domain.ValidationError{Fields: map[string]string{"project": "violates a project constraint"}}
		}
	}
	return err
}
