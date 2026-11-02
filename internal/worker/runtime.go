// Package worker runs jobs one pass at a time.
package worker

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

// Job is one pass of work.
type Job func(ctx context.Context) error

// Run polls job until ctx is canceled. Expected failures are logged and the
// next poll runs normally; a defect stops the loop.
func Run(ctx context.Context, job Job, interval, passTimeout time.Duration, log *slog.Logger) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-timer.C:
		}
		if err := RunOnce(ctx, job, passTimeout); err != nil {
			if ctx.Err() != nil {
				return nil // interrupted mid-pass: quiet exit
			}
			if _, ok := apperr.As(err); !ok {
				return err // defect: process exits 1
			}
			LogFailure(log, err)
		}
		timer.Reset(interval) // wait after each completed pass
	}
}

// RunOnce runs one pass under a timeout. Run and worker -once share it.
func RunOnce(ctx context.Context, job Job, timeout time.Duration) error {
	passCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	err := job(passCtx)
	if err == nil {
		return nil
	}
	if _, ok := apperr.As(err); ok {
		return err // keep the more specific diagnostics
	}
	if ctx.Err() == nil && errors.Is(passCtx.Err(), context.DeadlineExceeded) {
		return apperr.Infrastructure(apperr.KindInternal, "JOB_TIMEOUT",
			"The worker pass timed out", "worker.pass",
			apperr.Diagnostics{Reason: "Timeout"}, err)
	}

	return err
}

// LogFailure logs safe diagnostics for an *apperr.Error and the full error
// otherwise.
func LogFailure(log *slog.Logger, err error) {
	if appErr, ok := apperr.As(err); ok {
		log.Error("Worker pass failed",
			"op", appErr.Op, "code", appErr.Code, "diagnostics", appErr.Diagnostics)

		return
	}
	log.Error("Worker failed", "error", err)
}
