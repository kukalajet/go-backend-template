// Package application declares the ports that adapters implement.
package application

import "context"

// HealthChecker reports whether the database can serve queries.
type HealthChecker interface {
	// Health runs a bounded query. Errors: DATABASE_UNAVAILABLE, DATABASE_ERROR.
	Health(ctx context.Context) error
}
