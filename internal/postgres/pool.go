// Package postgres implements the application ports on PostgreSQL.
package postgres

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	defaultMaxConns       = 10
	defaultConnectTimeout = 5 * time.Second
	queryTimeout          = 5 * time.Second
)

// NewPool builds a pool without opening a connection, so a binary can start
// while the database is down. pool_max_conns and connect_timeout in the URL
// take precedence over the defaults.
func NewPool(ctx context.Context, databaseURL, applicationName string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse database url: %w", err)
	}
	if !strings.Contains(databaseURL, "pool_max_conns=") {
		cfg.MaxConns = defaultMaxConns
	}
	if cfg.ConnConfig.ConnectTimeout == 0 {
		cfg.ConnConfig.ConnectTimeout = defaultConnectTimeout
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = applicationName

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	return pool, nil
}
