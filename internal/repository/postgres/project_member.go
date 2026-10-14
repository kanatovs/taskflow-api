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
