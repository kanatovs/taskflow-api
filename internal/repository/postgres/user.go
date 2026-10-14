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

type UserRepository struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewUserRepository(pool *pgxpool.Pool, timeout time.Duration) *UserRepository {
	return &UserRepository{pool: pool, timeout: timeout}
}

func (r *UserRepository) Create(ctx context.Context, user domain.User) (domain.User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, display_name) VALUES ($1, $2, $3)
		RETURNING id, email, password_hash, display_name, created_at, updated_at`, user.Email, user.PasswordHash, user.DisplayName)
	created, err := scanUser(row)
	if err != nil {
		return domain.User{}, fmt.Errorf("create user: %w", mapUserError(err))
	}
	return created, nil
}

func (r *UserRepository) ByEmail(ctx context.Context, email string) (domain.User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	user, err := scanUser(r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, display_name, created_at, updated_at FROM users WHERE email = $1`, email))
	if err != nil {
		return domain.User{}, fmt.Errorf("find user by email: %w", mapUserError(err))
	}
	return user, nil
}

func (r *UserRepository) ByID(ctx context.Context, id int64) (domain.User, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout)
	defer cancel()
	user, err := scanUser(r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, display_name, created_at, updated_at FROM users WHERE id = $1`, id))
	if err != nil {
		return domain.User{}, fmt.Errorf("find user by ID: %w", mapUserError(err))
	}
	return user, nil
}

func scanUser(row pgx.Row) (domain.User, error) {
	var user domain.User
	err := row.Scan(&user.ID, &user.Email, &user.PasswordHash, &user.DisplayName, &user.CreatedAt, &user.UpdatedAt)
	if err != nil {
		return domain.User{}, err
	}
	user.CreatedAt = user.CreatedAt.UTC()
	user.UpdatedAt = user.UpdatedAt.UTC()
	return user, nil
}

func mapUserError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "users_email_key" {
		return domain.ErrEmailExists
	}
	return err
}
