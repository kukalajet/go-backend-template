package jobs

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/kukalajet/go-backend-template/internal/domain"
)

type fakeLister struct {
	users []domain.User
	err   error
	limit int
}

func (f *fakeLister) List(_ context.Context, limit int) ([]domain.User, error) {
	f.limit = limit

	return f.users, f.err
}

func TestSampleUsersLogsCount(t *testing.T) {
	var logs bytes.Buffer
	lister := &fakeLister{users: make([]domain.User, 2)}

	job := SampleUsers(lister, slog.New(slog.NewJSONHandler(&logs, nil)))
	if err := job(context.Background()); err != nil {
		t.Fatal(err)
	}
	if lister.limit != 5 {
		t.Fatalf("limit = %d", lister.limit)
	}
	if !strings.Contains(logs.String(), `"userCount":2`) {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestSampleUsersReturnsErrors(t *testing.T) {
	want := errors.New("boom")

	job := SampleUsers(&fakeLister{err: want}, slog.New(slog.DiscardHandler))
	if err := job(context.Background()); !errors.Is(err, want) {
		t.Fatalf("got %v", err)
	}
}
