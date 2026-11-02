//go:build integration

package postgres_test

import (
	"context"
	"fmt"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/kukalajet/go-backend-template/internal/apperr"
	"github.com/kukalajet/go-backend-template/internal/domain"
	"github.com/kukalajet/go-backend-template/internal/postgres"
)

func createUser(t *testing.T, email, displayName string) domain.CreateUser {
	t.Helper()
	in, err := domain.NewCreateUser(email, displayName)
	if err != nil {
		t.Fatal(err)
	}

	return in
}

func requireAppErr(t *testing.T, err error, kind apperr.Kind, code string) *apperr.Diagnostics {
	t.Helper()
	appErr, ok := apperr.As(err)
	if !ok {
		t.Fatalf("got %v, want *apperr.Error", err)
	}
	if appErr.Kind != kind || appErr.Code != code {
		t.Fatalf("got kind %v code %q, want %v %q", appErr.Kind, appErr.Code, kind, code)
	}

	return appErr.Diagnostics
}

func TestInsertAndFind(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))
	ctx := context.Background()

	inserted, err := repo.Insert(ctx, createUser(t, "a@x.com", "Ada"))
	if err != nil {
		t.Fatal(err)
	}
	if inserted.Email.String() != "a@x.com" || inserted.DisplayName.String() != "Ada" || inserted.CreatedAt.IsZero() {
		t.Fatalf("inserted = %+v", inserted)
	}

	found, err := repo.FindByID(ctx, inserted.ID)
	if err != nil {
		t.Fatal(err)
	}
	if found.ID != inserted.ID || !found.CreatedAt.Equal(inserted.CreatedAt) {
		t.Fatalf("found = %+v, want %+v", found, inserted)
	}
}

func TestInsertDuplicateEmailIgnoringCase(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))
	ctx := context.Background()

	if _, err := repo.Insert(ctx, createUser(t, "a@x.com", "Ada")); err != nil {
		t.Fatal(err)
	}

	_, err := repo.Insert(ctx, createUser(t, "A@X.com", "Other"))
	requireAppErr(t, err, apperr.KindConflict, "USER_ALREADY_EXISTS")
}

func TestFindByIDNotFound(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))

	_, err := repo.FindByID(context.Background(), domain.UserIDFromUUID(uuid.New()))
	requireAppErr(t, err, apperr.KindNotFound, "USER_NOT_FOUND")
}

func TestListNewestFirst(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))
	ctx := context.Background()

	empty, err := repo.List(ctx, 10)
	if err != nil {
		t.Fatal(err)
	}
	if empty == nil || len(empty) != 0 {
		t.Fatalf("empty list = %#v", empty)
	}

	var want []string
	for i := range 3 {
		user, err := repo.Insert(ctx, createUser(t, fmt.Sprintf("u%d@x.com", i), "User"))
		if err != nil {
			t.Fatal(err)
		}
		want = append(want, user.ID.String())
		time.Sleep(2 * time.Millisecond) // uuidv7 orders by millisecond across connections
	}
	slices.Reverse(want)

	users, err := repo.List(ctx, 2)
	if err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(users))
	for _, user := range users {
		got = append(got, user.ID.String())
	}
	if !slices.Equal(got, want[:2]) {
		t.Fatalf("got %v, want %v", got, want[:2])
	}
}

func TestStoredRowFailsValidation(t *testing.T) {
	pool := newTestPool(t)
	repo := postgres.NewUserRepository(pool)
	ctx := context.Background()

	id := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO users (id, email, display_name) VALUES ($1, 'not-an-email', 'Ada')`, id); err != nil {
		t.Fatal(err)
	}

	_, err := repo.FindByID(ctx, domain.UserIDFromUUID(id))
	diagnostics := requireAppErr(t, err, apperr.KindInternal, "DATABASE_ERROR")
	if diagnostics.Reason != "ValidationError" || !slices.Equal(diagnostics.ValidationPaths, []string{"email"}) {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}

	_, err = repo.List(ctx, 10)
	requireAppErr(t, err, apperr.KindInternal, "DATABASE_ERROR")
}

func TestExpiredContextIsTimeout(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	_, err := repo.List(ctx, 10)
	diagnostics := requireAppErr(t, err, apperr.KindUnavailable, "DATABASE_UNAVAILABLE")
	if diagnostics.Reason != "Timeout" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestCanceledContext(t *testing.T) {
	repo := postgres.NewUserRepository(newTestPool(t))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := repo.List(ctx, 10)
	diagnostics := requireAppErr(t, err, apperr.KindInternal, "DATABASE_ERROR")
	if diagnostics.Reason != "Canceled" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestUnreachableDatabaseIsUnavailable(t *testing.T) {
	ctx := context.Background()
	pool, err := postgres.NewPool(ctx, "postgresql://u:p@127.0.0.1:1/db", "integration-test")
	if err != nil {
		t.Fatalf("NewPool must not connect: %v", err)
	}
	defer pool.Close()

	err = postgres.NewHealthChecker(pool).Health(ctx)
	diagnostics := requireAppErr(t, err, apperr.KindUnavailable, "DATABASE_UNAVAILABLE")
	if diagnostics.Reason != "ConnectionError" {
		t.Fatalf("diagnostics = %+v", diagnostics)
	}
}

func TestHealth(t *testing.T) {
	if err := postgres.NewHealthChecker(newTestPool(t)).Health(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestPoolSettings(t *testing.T) {
	ctx := context.Background()

	defaults, err := postgres.NewPool(ctx, "postgresql://u:p@127.0.0.1:1/db", "app")
	if err != nil {
		t.Fatal(err)
	}
	defer defaults.Close()
	if got := defaults.Config(); got.MaxConns != 10 || got.ConnConfig.ConnectTimeout != 5*time.Second {
		t.Fatalf("defaults = %d, %v", got.MaxConns, got.ConnConfig.ConnectTimeout)
	}
	if got := defaults.Config().ConnConfig.RuntimeParams["application_name"]; got != "app" {
		t.Fatalf("application_name = %q", got)
	}

	custom, err := postgres.NewPool(ctx, "postgresql://u:p@127.0.0.1:1/db?pool_max_conns=3&connect_timeout=2", "app")
	if err != nil {
		t.Fatal(err)
	}
	defer custom.Close()
	if got := custom.Config(); got.MaxConns != 3 || got.ConnConfig.ConnectTimeout != 2*time.Second {
		t.Fatalf("custom = %d, %v", got.MaxConns, got.ConnConfig.ConnectTimeout)
	}
}
