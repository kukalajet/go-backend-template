// Command migrate applies the embedded SQL migrations.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/kukalajet/go-backend-template/migrations"
)

const usage = "usage: migrate up | down | status | create <name>"

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(ctx, log, os.Args[1:]); err != nil {
		log.Error("Migrate failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, args []string) error {
	if len(args) == 0 {
		return errors.New(usage)
	}

	switch args[0] {
	case "create":
		if len(args) != 2 {
			return errors.New(usage)
		}
		goose.SetSequential(true)

		return goose.Create(nil, "migrations", args[1], "sql")
	case "up", "down", "status":
		return runProvider(ctx, log, args[0])
	}

	return errors.New(usage)
}

func runProvider(ctx context.Context, log *slog.Logger, command string) error {
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		return errors.New("DATABASE_URL is required")
	}
	connConfig, err := pgx.ParseConfig(databaseURL)
	if err != nil {
		return fmt.Errorf("parse database url: %w", err)
	}
	connConfig.RuntimeParams["application_name"] = "migrate"

	db := stdlib.OpenDB(*connConfig)
	defer func() { _ = db.Close() }()

	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("create session locker: %w", err)
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("create provider: %w", err)
	}

	switch command {
	case "up":
		results, err := provider.Up(ctx)
		if err != nil {
			return fmt.Errorf("migrate up: %w", err)
		}
		log.Info("Migrations applied", "count", len(results))
	case "down":
		result, err := provider.Down(ctx)
		if err != nil {
			return fmt.Errorf("migrate down: %w", err)
		}
		log.Info("Migration rolled back", "version", result.Source.Version)
	case "status":
		statuses, err := provider.Status(ctx)
		if err != nil {
			return fmt.Errorf("migrate status: %w", err)
		}
		for _, status := range statuses {
			log.Info("Migration", "version", status.Source.Version, "path", status.Source.Path, "state", string(status.State))
		}
	}

	return nil
}
