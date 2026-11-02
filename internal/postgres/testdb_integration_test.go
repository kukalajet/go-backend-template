//go:build integration

package postgres_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"

	"github.com/kukalajet/go-backend-template/internal/postgres"
	"github.com/kukalajet/go-backend-template/migrations"
)

// newTestPool creates a uniquely named database with the migrations applied and
// drops it when the test ends.
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	ctx := context.Background()

	adminURL := os.Getenv("TEST_DATABASE_URL")
	if adminURL == "" {
		t.Fatal("TEST_DATABASE_URL is required; run through `just test-integration`")
	}

	suffix := make([]byte, 8)
	if _, err := rand.Read(suffix); err != nil {
		t.Fatal(err)
	}
	name := "test_" + hex.EncodeToString(suffix)

	adminExec(t, adminURL, `CREATE DATABASE "`+name+`"`)
	t.Cleanup(func() { adminExec(t, adminURL, `DROP DATABASE "`+name+`" WITH (FORCE)`) })

	parsed, err := url.Parse(adminURL)
	if err != nil {
		t.Fatal(err)
	}
	parsed.Path = "/" + name
	testURL := parsed.String()

	connConfig, err := pgx.ParseConfig(testURL)
	if err != nil {
		t.Fatal(err)
	}
	db := stdlib.OpenDB(*connConfig)
	t.Cleanup(func() { _ = db.Close() })

	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.Up(ctx); err != nil {
		t.Fatal(err)
	}

	pool, err := postgres.NewPool(ctx, testURL, "integration-test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	return pool
}

func adminExec(t *testing.T, adminURL, sql string) {
	t.Helper()
	ctx := context.Background()

	conn, err := pgx.Connect(ctx, adminURL)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close(ctx) }()

	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Fatal(err)
	}
}
