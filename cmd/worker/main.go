// Command worker polls the sample job, or runs one pass with -once.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/kukalajet/go-backend-template/internal/config"
	"github.com/kukalajet/go-backend-template/internal/postgres"
	"github.com/kukalajet/go-backend-template/internal/worker"
	"github.com/kukalajet/go-backend-template/internal/worker/jobs"
)

func main() {
	once := flag.Bool("once", false, "run one pass and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var level slog.LevelVar
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &level}))
	if err := run(ctx, log, &level, *once); err != nil {
		if *once && ctx.Err() != nil {
			log.Error("Worker pass interrupted")
		} else {
			worker.LogFailure(log, err)
		}
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, level *slog.LevelVar, once bool) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level.Set(cfg.LogLevel)

	applicationName := "worker"
	if once {
		applicationName = "worker-once"
	}
	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL.Value(), applicationName)
	if err != nil {
		return err
	}
	defer pool.Close()

	job := jobs.SampleUsers(postgres.NewUserRepository(pool), log)
	if once {
		return worker.RunOnce(ctx, job, cfg.WorkerPassTimeout)
	}

	return worker.Run(ctx, job, cfg.WorkerPollInterval, cfg.WorkerPassTimeout, log)
}
