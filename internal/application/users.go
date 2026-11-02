package application

import (
	"context"

	"github.com/kukalajet/go-backend-template/internal/domain"
)

// UserRepository stores users.
type UserRepository interface {
	// Insert errors: USER_ALREADY_EXISTS, DATABASE_UNAVAILABLE, DATABASE_ERROR.
	Insert(ctx context.Context, in domain.CreateUser) (domain.User, error)
	// FindByID errors: USER_NOT_FOUND, DATABASE_UNAVAILABLE, DATABASE_ERROR.
	FindByID(ctx context.Context, id domain.UserID) (domain.User, error)
	// List returns newest first; limit must be positive.
	// Errors: DATABASE_UNAVAILABLE, DATABASE_ERROR.
	List(ctx context.Context, limit int) ([]domain.User, error)
}
