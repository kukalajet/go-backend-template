package postgres

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

func TestRepositoryError(t *testing.T) {
	tests := []struct {
		name    string
		err     error
		reason  string
		sqlCode string
		kind    apperr.Kind
		code    string
	}{
		{"canceled", context.Canceled, "Canceled", "", apperr.KindInternal, "DATABASE_ERROR"},
		{"deadline", fmt.Errorf("wrap: %w", context.DeadlineExceeded), "Timeout", "", apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"connection exception", &pgconn.PgError{Code: pgerrcode.ConnectionFailure}, "ConnectionError", pgerrcode.ConnectionFailure, apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"too many connections", &pgconn.PgError{Code: pgerrcode.TooManyConnections}, "ConnectionError", pgerrcode.TooManyConnections, apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"admin shutdown", &pgconn.PgError{Code: pgerrcode.AdminShutdown}, "ConnectionError", pgerrcode.AdminShutdown, apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"crash shutdown", &pgconn.PgError{Code: pgerrcode.CrashShutdown}, "ConnectionError", pgerrcode.CrashShutdown, apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"cannot connect now", &pgconn.PgError{Code: pgerrcode.CannotConnectNow}, "ConnectionError", pgerrcode.CannotConnectNow, apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"dropped connection", &net.OpError{Op: "read", Err: errors.New("connection reset")}, "ConnectionError", "", apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"eof", fmt.Errorf("wrap: %w", io.ErrUnexpectedEOF), "ConnectionError", "", apperr.KindUnavailable, "DATABASE_UNAVAILABLE"},
		{"query error", &pgconn.PgError{Code: pgerrcode.UndefinedTable}, "QueryError", pgerrcode.UndefinedTable, apperr.KindInternal, "DATABASE_ERROR"},
		{"unknown", errors.New("boom"), "UnknownError", "", apperr.KindInternal, "DATABASE_ERROR"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := repositoryError("users.insert", tt.err)

			if got.Kind != tt.kind || got.Code != tt.code {
				t.Fatalf("got kind %v code %q, want %v %q", got.Kind, got.Code, tt.kind, tt.code)
			}
			if got.Op != "users.insert" {
				t.Fatalf("op = %q", got.Op)
			}
			if got.Diagnostics.Reason != tt.reason || got.Diagnostics.Code != tt.sqlCode {
				t.Fatalf("diagnostics = %+v", got.Diagnostics)
			}
		})
	}
}

func TestRepositoryErrorStoredRowValidation(t *testing.T) {
	got := repositoryError("users.find", apperr.InvalidRequest("email"))

	if got.Kind != apperr.KindInternal || got.Code != "DATABASE_ERROR" {
		t.Fatalf("got kind %v code %q", got.Kind, got.Code)
	}
	if got.Diagnostics.Reason != "ValidationError" || !slices.Equal(got.Diagnostics.ValidationPaths, []string{"email"}) {
		t.Fatalf("diagnostics = %+v", got.Diagnostics)
	}
}

func TestRepositoryErrorOmitsDriverMessages(t *testing.T) {
	got := repositoryError("users.insert", &pgconn.PgError{
		Code:    pgerrcode.UndefinedTable,
		Message: "secret message",
		Detail:  "secret detail",
		Where:   "secret where",
	})

	if got.Error() != "The database operation could not be completed" {
		t.Fatalf("message = %q", got.Error())
	}
	if got.Diagnostics.Reason != "QueryError" || len(got.Diagnostics.ValidationPaths) != 0 {
		t.Fatalf("diagnostics = %+v", got.Diagnostics)
	}
}
