package repository

import (
	"context"

	"github.com/P-kaizoku/small-light/internal/model"
	"github.com/jackc/pgx/v5/pgxpool"
)

type UserRepository struct {
	pool *pgxpool.Pool
}

func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{
		pool: pool,
	}
}

func (r *UserRepository) Create(ctx context.Context, email, passwordHash string) (*model.User, error) {
	var u model.User

	query := `INSERT INTO users (email, password_hash) VALUES($1, $2) RETURNING id, email, created_at, updated_at`
	err := r.pool.QueryRow(ctx, query, email, passwordHash).Scan(&u.ID, &u.Email, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		return nil, translateError(err)
	}
	return &u, nil

}

func (r *UserRepository) GetByEmail(ctx context.Context, email string) (*model.User, error) {
	var u model.User

	query := `SELECT id, email, password_hash, created_at, updated_at FROM users WHERE email = $1`
	err := r.pool.QueryRow(ctx, query, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.CreatedAt, &u.UpdatedAt)

	if err != nil {
		return nil, translateError(err)
	}

	return &u, nil
}
