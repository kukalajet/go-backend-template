package postgres

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kukalajet/go-backend-template/internal/application"
)

var _ application.HealthChecker = (*HealthChecker)(nil)

// HealthChecker implements application.HealthChecker.
type HealthChecker struct {
	pool *pgxpool.Pool
}

// NewHealthChecker returns a checker that queries through pool.
func NewHealthChecker(pool *pgxpool.Pool) *HealthChecker {
	return &HealthChecker{pool: pool}
}

// Health runs a bounded query.
func (h *HealthChecker) Health(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, queryTimeout)
	defer cancel()

	if _, err := h.pool.Exec(ctx, "SELECT 1"); err != nil {
		return repositoryError("health.check", err)
	}

	return nil
}
