package postgres

import (
	"context"
	"errors"
	"io"
	"net"

	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/kukalajet/go-backend-template/internal/apperr"
)

// repositoryError turns a failure that isn't a domain error into a safe error.
// pgErr.Message, Detail and Where are never copied into diagnostics.
func repositoryError(op string, err error) *apperr.Error {
	diagnostics, unavailable := classify(err)
	if unavailable {
		return apperr.Infrastructure(apperr.KindUnavailable, "DATABASE_UNAVAILABLE",
			"The database is temporarily unavailable", op, diagnostics, err)
	}

	return apperr.Infrastructure(apperr.KindInternal, "DATABASE_ERROR",
		"The database operation could not be completed", op, diagnostics, err)
}

// classify returns the safe diagnostics for err and whether the database, not
// the query, is the problem. The first matching case wins.
func classify(err error) (apperr.Diagnostics, bool) {
	var (
		connectErr *pgconn.ConnectError
		pgErr      *pgconn.PgError
		netErr     net.Error
	)

	switch {
	case errors.Is(err, context.Canceled):
		return apperr.Diagnostics{Reason: "Canceled"}, false
	case errors.Is(err, context.DeadlineExceeded), pgconn.Timeout(err):
		return apperr.Diagnostics{Reason: "Timeout"}, true
	case errors.As(err, &connectErr):
		return apperr.Diagnostics{Reason: "ConnectionError"}, true
	case errors.As(err, &pgErr):
		if isConnectionSQLState(pgErr.Code) {
			return apperr.Diagnostics{Reason: "ConnectionError", Code: pgErr.Code}, true
		}

		return apperr.Diagnostics{Reason: "QueryError", Code: pgErr.Code}, false
	case errors.As(err, &netErr), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return apperr.Diagnostics{Reason: "ConnectionError"}, true
	}

	if appErr, ok := apperr.As(err); ok && appErr.Kind == apperr.KindInvalid {
		return apperr.Diagnostics{
			Reason:          "ValidationError",
			ValidationPaths: appErr.Diagnostics.ValidationPaths,
		}, false
	}

	return apperr.Diagnostics{Reason: "UnknownError"}, false
}

func isConnectionSQLState(code string) bool {
	switch code {
	case pgerrcode.TooManyConnections, pgerrcode.AdminShutdown, pgerrcode.CrashShutdown, pgerrcode.CannotConnectNow:
		return true
	}

	return pgerrcode.IsConnectionException(code)
}
