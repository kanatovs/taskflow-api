package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kanatovs/taskflow-api/internal/domain"
)

type ProjectMemberRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewProjectMemberRepository(pool *pgxpool.Pool, timeout time.Duration) *ProjectMemberRepository {
	return &ProjectMemberRepository{pool: pool, timeout: timeout}
}

func (r *ProjectMemberRepository) ByUser(ctx context.Context, projectID, userID int64) (domain.ProjectMember, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	member, err := scanProjectMember(r.pool.QueryRow(ctx, `SELECT project_id, user_id, role, joined_at FROM project_members WHERE project_id = $1 AND user_id = $2`, projectID, userID))
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("find project member: %w", mapProjectError(err))
	}
	return member, nil
}

func scanProjectMember(row pgx.Row) (domain.ProjectMember, error) {
	var member domain.ProjectMember
	err := row.Scan(&member.ProjectID, &member.UserID, &member.Role, &member.JoinedAt)
	member.JoinedAt = member.JoinedAt.UTC()
	return member, err
}

func (r *ProjectMemberRepository) List(ctx context.Context, projectID, actorID int64, page, pageSize int) ([]domain.ProjectMember, int, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly})
	if err != nil {
		return nil, 0, fmt.Errorf("begin member listing: %w", err)
	}
	defer rollback(tx)
	var id int64
	if err := tx.QueryRow(ctx, `SELECT id FROM projects WHERE id=$1`, projectID).Scan(&id); err != nil {
		return nil, 0, fmt.Errorf("find member project: %w", mapProjectError(err))
	}
	if _, err := memberRole(ctx, tx, projectID, actorID); err != nil {
		return nil, 0, err
	}
	var total int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM project_members WHERE project_id=$1`, projectID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count project members: %w", err)
	}
	rows, err := tx.Query(ctx, `SELECT project_id,user_id,role,joined_at FROM project_members WHERE project_id=$1 ORDER BY user_id LIMIT $2 OFFSET $3`, projectID, pageSize, int64(page-1)*int64(pageSize))
	if err != nil {
		return nil, 0, fmt.Errorf("list project members: %w", err)
	}
	defer rows.Close()
	members := make([]domain.ProjectMember, 0)
	for rows.Next() {
		member, err := scanProjectMember(rows)
		if err != nil {
			return nil, 0, fmt.Errorf("scan project member: %w", err)
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("read project members: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, 0, fmt.Errorf("commit member listing: %w", err)
	}
	return members, total, nil
}

func (r *ProjectMemberRepository) Add(ctx context.Context, projectID, actorID, userID int64, role domain.ProjectRole) (domain.ProjectMember, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("begin member addition: %w", err)
	}
	defer rollback(tx)
	if err := lockProject(ctx, tx, projectID); err != nil {
		return domain.ProjectMember{}, err
	}
	actor, err := memberRole(ctx, tx, projectID, actorID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	if !domain.CanManageProjectMember(actor, "", role) {
		return domain.ProjectMember{}, domain.ErrForbidden
	}
	member, err := scanProjectMember(tx.QueryRow(ctx, `INSERT INTO project_members (project_id,user_id,role) VALUES ($1,$2,$3) RETURNING project_id,user_id,role,joined_at`, projectID, userID, role))
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("insert project member: %w", mapProjectError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ProjectMember{}, fmt.Errorf("commit member addition: %w", err)
	}
	return member, nil
}

func (r *ProjectMemberRepository) Update(ctx context.Context, projectID, actorID, userID int64, role domain.ProjectRole) (domain.ProjectMember, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("begin member update: %w", err)
	}
	defer rollback(tx)
	if err := lockProject(ctx, tx, projectID); err != nil {
		return domain.ProjectMember{}, err
	}
	actor, err := memberRole(ctx, tx, projectID, actorID)
	if err != nil {
		return domain.ProjectMember{}, err
	}
	target, err := scanProjectMember(tx.QueryRow(ctx, `SELECT project_id,user_id,role,joined_at FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID))
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("find member for update: %w", mapProjectError(err))
	}
	if !domain.CanManageProjectMember(actor, target.Role, role) {
		return domain.ProjectMember{}, domain.ErrForbidden
	}
	member, err := scanProjectMember(tx.QueryRow(ctx, `UPDATE project_members SET role=$3 WHERE project_id=$1 AND user_id=$2 RETURNING project_id,user_id,role,joined_at`, projectID, userID, role))
	if err != nil {
		return domain.ProjectMember{}, fmt.Errorf("update project member: %w", mapProjectError(err))
	}
	if err := tx.Commit(ctx); err != nil {
		return domain.ProjectMember{}, fmt.Errorf("commit member update: %w", err)
	}
	return member, nil
}

func (r *ProjectMemberRepository) Delete(ctx context.Context, projectID, actorID, userID int64) error {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin member deletion: %w", err)
	}
	defer rollback(tx)
	if err := lockProject(ctx, tx, projectID); err != nil {
		return err
	}
	actor, err := memberRole(ctx, tx, projectID, actorID)
	if err != nil {
		return err
	}
	target, err := scanProjectMember(tx.QueryRow(ctx, `SELECT project_id,user_id,role,joined_at FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID))
	if err != nil {
		return fmt.Errorf("find member for deletion: %w", mapProjectError(err))
	}
	if !domain.CanManageProjectMember(actor, target.Role, "") {
		return domain.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `DELETE FROM project_members WHERE project_id=$1 AND user_id=$2`, projectID, userID); err != nil {
		return fmt.Errorf("delete project member: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit member deletion: %w", err)
	}
	return nil
}
