// Package httpapi serves the HTTP API.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"runtime/debug"
	"strconv"
	"strings"
	"time"

	"github.com/kukalajet/go-backend-template/internal/apperr"
	"github.com/kukalajet/go-backend-template/internal/application"
)

const (
	maxBodyBytes      = 1 << 20
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// Server holds the API's dependencies.
type Server struct {
	health application.HealthChecker
	users  application.UserRepository
	log    *slog.Logger
}

// New returns a Server.
func New(health application.HealthChecker, users application.UserRepository, log *slog.Logger) *Server {
	return &Server{health: health, users: users, log: log}
}

// Routes returns the API handler with its middleware applied.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.handle(s.live))
	mux.HandleFunc("GET /health/ready", s.handle(s.ready))
	mux.HandleFunc("GET /users", s.handle(s.listUsers))
	mux.HandleFunc("GET /users/{id}", s.handle(s.getUser))
	mux.HandleFunc("POST /users", s.handle(s.createUser))

	return s.withRequestID(s.withAccessLog(s.withRecovery(mux)))
}

// ListenAndServe serves until ctx is canceled, then gives open requests time to
// finish. Request contexts don't derive from ctx, so a signal doesn't cancel
// requests in progress.
func (s *Server) ListenAndServe(ctx context.Context, host string, port int) error {
	srv := &http.Server{
		Addr:              net.JoinHostPort(host, strconv.Itoa(port)),
		Handler:           s.Routes(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	s.log.InfoContext(ctx, "API listening", "address", srv.Addr)

	select {
	case err := <-serveErr:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		_ = srv.Close()

		return fmt.Errorf("shutdown: %w", err)
	}

	return nil
}

type loggerKey struct{}

// logger returns the request's logger, which carries the request ID.
func (s *Server) logger(r *http.Request) *slog.Logger {
	if log, ok := r.Context().Value(loggerKey{}).(*slog.Logger); ok {
		return log
	}

	return s.log
}

func (s *Server) withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := newRequestID()
		w.Header().Set("X-Request-ID", id)
		log := s.log.With("requestId", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), loggerKey{}, log)))
	})
}

func newRequestID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b) // crypto/rand.Read never returns an error

	return hex.EncodeToString(b)
}

// withAccessLog reads r.Pattern from the same request it passes on, because the
// mux sets it on that value.
func (s *Server) withAccessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		defer s.logAccess(r, rec, start)
		next.ServeHTTP(rec, r)
	})
}

func (s *Server) logAccess(r *http.Request, rec *statusRecorder, start time.Time) {
	level := slog.LevelInfo
	if strings.HasPrefix(r.URL.Path, "/health/") {
		level = slog.LevelDebug
	}
	s.logger(r).Log(r.Context(), level, "HTTP request",
		"method", r.Method,
		"route", r.Pattern,
		"status", rec.finalStatus(),
		"durationMs", time.Since(start).Milliseconds())
}

func (s *Server) withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			if recovered == http.ErrAbortHandler { //nolint:errorlint // a panic value, compared the way net/http does
				panic(recovered)
			}
			s.logger(r).Error("Panic recovered", "panic", recovered, "stack", string(debug.Stack()))
			if rec, ok := w.(*statusRecorder); ok && rec.status != 0 {
				return // the response has started and can't be replaced
			}
			writeErrorBody(w, http.StatusInternalServerError, internalError())
		}()
		next.ServeHTTP(w, r)
	})
}

// statusRecorder captures the status for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}

	return r.ResponseWriter.Write(b)
}

// Unwrap lets http.ResponseController reach the underlying writer.
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

func (r *statusRecorder) finalStatus() int {
	if r.status == 0 {
		return http.StatusOK
	}

	return r.status
}

func writeJSON(w http.ResponseWriter, status int, body any) error {
	encoded, err := json.Marshal(body)
	if err != nil {
		return fmt.Errorf("encode response: %w", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(encoded) // a failed write means the client has gone

	return nil
}

// decodeJSON reads one JSON value from a size-limited body. Unknown fields are
// ignored, and anything after the value is rejected.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	if err := decoder.Decode(dst); err != nil {
		return decodeError(err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return decodeError(err)
	}

	return nil
}

func decodeError(err error) error {
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return apperr.New(apperr.KindTooLarge, "REQUEST_TOO_LARGE", "The request body is too large")
	}

	return apperr.InvalidRequest()
}
