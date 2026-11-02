// Package jobs holds the worker's jobs.
package jobs

import (
	"context"
	"log/slog"

	"github.com/kukalajet/go-backend-template/internal/domain"
	"github.com/kukalajet/go-backend-template/internal/worker"
)

const sampleLimit = 5

type userLister interface {
	List(ctx context.Context, limit int) ([]domain.User, error)
}

// SampleUsers returns a job that lists a few users and logs how many it found.
func SampleUsers(users userLister, log *slog.Logger) worker.Job {
	return func(ctx context.Context) error {
		sample, err := users.List(ctx, sampleLimit)
		if err != nil {
			return err
		}
		log.InfoContext(ctx, "Worker user sample loaded", "userCount", len(sample))

		return nil
	}
}
