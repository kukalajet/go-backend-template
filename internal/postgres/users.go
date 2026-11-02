package postgres

import (
	"context"
	"errors"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kukalajet/go-backend-template/internal/application"
	"github.com/kukalajet/go-backend-template/internal/domain"
	"github.com/kukalajet/go-backend-template/internal/postgres/sqlcgen"
)

const usersEmailUniqueConstraint = "users_email_unique"

var _ application.UserRepository = (*UserRepository)(nil)

// UserRepository implements application.UserRepository.
type UserRepository struct {
	queries *sqlcgen.Queries
}

// NewUserRepository returns a repository that queries through pool.
func NewUserRepository(pool *pgxpool.Pool) *UserRepository {
	return &UserRepository{queries: sqlcgen.New(pool)}
}

// Insert stores a new user.
func (r *UserRepository) Insert(ctx context.Context, in domain.CreateUser) (domain.User, error) {
	const op = "users.insert"
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	row, err := r.queries.InsertUser(ctx, sqlcgen.InsertUserParams{
		Email:       in.Email.String(),
		DisplayName: in.DisplayName.String(),
	})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation && pgErr.ConstraintName == usersEmailUniqueConstraint {
			return domain.User{}, domain.UserAlreadyExists()
		}

		return domain.User{}, repositoryError(op, err)
	}

	return restoreUser(op, row)
}

// FindByID returns the user with the given ID.
func (r *UserRepository) FindByID(ctx context.Context, id domain.UserID) (domain.User, error) {
	const op = "users.find"
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	row, err := r.queries.FindUserByID(ctx, id.UUID())
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.User{}, domain.UserNotFound()
		}

		return domain.User{}, repositoryError(op, err)
	}

	return restoreUser(op, row)
}

// List returns up to limit users, newest first.
func (r *UserRepository) List(ctx context.Context, limit int) ([]domain.User, error) {
	const op = "users.list"
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	rows, err := r.queries.ListUsers(ctx, int32(limit)) //nolint:gosec // callers pass small, validated limits
	if err != nil {
		return nil, repositoryError(op, err)
	}

	users := make([]domain.User, 0, len(rows))
	for _, row := range rows {
		user, err := restoreUser(op, row)
		if err != nil {
			return nil, err
		}
		users = append(users, user)
	}

	return users, nil
}

func restoreUser(op string, row sqlcgen.User) (domain.User, error) {
	user, err := domain.RestoreUser(domain.UserIDFromUUID(row.ID), row.Email, row.DisplayName, row.CreatedAt)
	if err != nil {
		return domain.User{}, repositoryError(op, err)
	}

	return user, nil
}
