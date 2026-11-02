package worker

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

const (
	testInterval = time.Millisecond
	testTimeout  = time.Second
)

var discard = slog.New(slog.DiscardHandler)

func expected() error {
	return apperr.Infrastructure(apperr.KindInternal, "DATABASE_ERROR", "safe", "users.list",
		apperr.Diagnostics{Reason: "QueryError"}, errors.New("driver secret"))
}

func TestRunContinuesAfterExpectedError(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var passes atomic.Int32
	job := func(context.Context) error {
		if passes.Add(1) == 3 {
			cancel()

			return nil
		}

		return expected()
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	if err := Run(ctx, job, testInterval, testTimeout, log); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if passes.Load() != 3 {
		t.Fatalf("passes = %d", passes.Load())
	}
	if !strings.Contains(logs.String(), "Worker pass failed") {
		t.Fatalf("logs = %s", logs.String())
	}
	if strings.Contains(logs.String(), "driver secret") {
		t.Fatalf("logs leaked the cause: %s", logs.String())
	}
}

func TestRunStopsOnDefect(t *testing.T) {
	defect := errors.New("defect")
	var passes atomic.Int32
	job := func(context.Context) error {
		passes.Add(1)

		return defect
	}

	if err := Run(context.Background(), job, testInterval, testTimeout, discard); !errors.Is(err, defect) {
		t.Fatalf("Run = %v", err)
	}
	if passes.Load() != 1 {
		t.Fatalf("passes = %d", passes.Load())
	}
}

func TestRunContinuesAfterPassTimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var passes atomic.Int32
	job := func(passCtx context.Context) error {
		if passes.Add(1) == 2 {
			cancel()

			return nil
		}
		<-passCtx.Done()

		return passCtx.Err() // a bare context error, not an *apperr.Error
	}

	if err := Run(ctx, job, testInterval, time.Millisecond, discard); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if passes.Load() != 2 {
		t.Fatalf("passes = %d", passes.Load())
	}
}

func TestRunExitsQuietlyWhenCanceledMidPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	job := func(passCtx context.Context) error {
		cancel()
		<-passCtx.Done()

		return passCtx.Err()
	}

	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))
	if err := Run(ctx, job, testInterval, testTimeout, log); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if logs.Len() != 0 {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestRunWaitsForIntervalAfterEachPass(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var passes atomic.Int32
	job := func(context.Context) error {
		passes.Add(1)

		return expected()
	}
	done := make(chan error, 1)
	go func() { done <- Run(ctx, job, time.Hour, testTimeout, discard) }()

	time.Sleep(20 * time.Millisecond)
	cancel()
	if err := <-done; err != nil {
		t.Fatalf("Run = %v", err)
	}
	if passes.Load() != 1 {
		t.Fatalf("passes = %d, want 1", passes.Load())
	}
}

func TestRunOnce(t *testing.T) {
	defect := errors.New("defect")

	t.Run("success", func(t *testing.T) {
		if err := RunOnce(context.Background(), func(context.Context) error { return nil }, testTimeout); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("bare deadline becomes JOB_TIMEOUT", func(t *testing.T) {
		err := RunOnce(context.Background(), func(ctx context.Context) error {
			<-ctx.Done()

			return ctx.Err()
		}, time.Millisecond)

		appErr, ok := apperr.As(err)
		if !ok || appErr.Code != "JOB_TIMEOUT" || appErr.Op != "worker.pass" || appErr.Diagnostics.Reason != "Timeout" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("specific error is kept after the deadline", func(t *testing.T) {
		err := RunOnce(context.Background(), func(ctx context.Context) error {
			<-ctx.Done()

			return expected()
		}, time.Millisecond)

		appErr, ok := apperr.As(err)
		if !ok || appErr.Code != "DATABASE_ERROR" {
			t.Fatalf("got %#v", err)
		}
	})

	t.Run("defect before the deadline stays a defect", func(t *testing.T) {
		err := RunOnce(context.Background(), func(context.Context) error { return defect }, testTimeout)
		if !errors.Is(err, defect) {
			t.Fatalf("got %v", err)
		}
	})

	t.Run("parent cancellation is not a timeout", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		err := RunOnce(ctx, func(ctx context.Context) error { return ctx.Err() }, testTimeout)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	})
}

func TestLogFailure(t *testing.T) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, nil))

	LogFailure(log, expected())
	if !strings.Contains(logs.String(), "Worker pass failed") || strings.Contains(logs.String(), "driver secret") {
		t.Fatalf("logs = %s", logs.String())
	}

	logs.Reset()
	LogFailure(log, errors.New("defect detail"))
	if !strings.Contains(logs.String(), "Worker failed") || !strings.Contains(logs.String(), "defect detail") {
		t.Fatalf("logs = %s", logs.String())
	}
}
