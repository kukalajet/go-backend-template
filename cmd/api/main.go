// Command api serves the HTTP API, or checks a running one with -healthcheck.
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kukalajet/go-backend-template/internal/config"
	"github.com/kukalajet/go-backend-template/internal/httpapi"
	"github.com/kukalajet/go-backend-template/internal/postgres"
)

const healthcheckTimeout = 3 * time.Second

func main() {
	healthcheck := flag.Bool("healthcheck", false, "check liveness of a running API and exit")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	var level slog.LevelVar
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &level}))

	if *healthcheck {
		if err := checkLiveness(ctx); err != nil {
			log.Error("Health check failed", "error", err)
			os.Exit(1)
		}

		return
	}

	if err := run(ctx, log, &level); err != nil {
		log.Error("API failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, log *slog.Logger, level *slog.LevelVar) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	level.Set(cfg.LogLevel)

	pool, err := postgres.NewPool(ctx, cfg.DatabaseURL.Value(), "api")
	if err != nil {
		return err
	}
	defer pool.Close()

	srv := httpapi.New(postgres.NewHealthChecker(pool), postgres.NewUserRepository(pool), log)

	return srv.ListenAndServe(ctx, cfg.APIHost, cfg.APIPort)
}

func checkLiveness(ctx context.Context) error {
	port := os.Getenv("API_PORT")
	if port == "" {
		port = "8000"
	}

	ctx, cancel := context.WithTimeout(ctx, healthcheckTimeout)
	defer cancel()
	//nolint:gosec // the host is fixed to loopback; only the port comes from the environment
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://127.0.0.1:"+port+"/health/live", http.NoBody)
	if err != nil {
		return fmt.Errorf("build request: %w", err)
	}
	resp, err := http.DefaultClient.Do(req) //nolint:gosec // same fixed loopback request as above
	if err != nil {
		return fmt.Errorf("send request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	return nil
}
