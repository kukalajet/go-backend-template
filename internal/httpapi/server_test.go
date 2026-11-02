package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kukalajet/go-backend-template/internal/apperr"
	"github.com/kukalajet/go-backend-template/internal/domain"
)

const userPath = "/users/0192f3a4-7b1c-7def-8a12-3456789abcde"

type fakeHealth struct{ err error }

func (f fakeHealth) Health(context.Context) error { return f.err }

type fakeUsers struct {
	insert    func(domain.CreateUser) (domain.User, error)
	find      func(domain.UserID) (domain.User, error)
	list      func(int) ([]domain.User, error)
	lastLimit int
}

func (f *fakeUsers) Insert(_ context.Context, in domain.CreateUser) (domain.User, error) {
	return f.insert(in)
}

func (f *fakeUsers) FindByID(_ context.Context, id domain.UserID) (domain.User, error) {
	return f.find(id)
}

func (f *fakeUsers) List(_ context.Context, limit int) ([]domain.User, error) {
	f.lastLimit = limit

	return f.list(limit)
}

func sampleUser(t *testing.T) domain.User {
	t.Helper()
	id, err := domain.ParseUserID(strings.TrimPrefix(userPath, "/users/"))
	if err != nil {
		t.Fatal(err)
	}
	user, err := domain.RestoreUser(id, "a@x.com", "Ada",
		time.Date(2026, 11, 1, 18, 48, 0, 123456000, time.FixedZone("CET", 3600)),
	)
	if err != nil {
		t.Fatal(err)
	}

	return user
}

func unavailable() error {
	return apperr.Infrastructure(apperr.KindUnavailable, "DATABASE_UNAVAILABLE", "The database is temporarily unavailable",
		"users.list", apperr.Diagnostics{Reason: "ConnectionError"}, errors.New("driver secret"))
}

func databaseError() error {
	return apperr.Infrastructure(apperr.KindInternal, "DATABASE_ERROR", "The database operation could not be completed",
		"users.list", apperr.Diagnostics{Reason: "QueryError", Code: "42P01"}, errors.New("driver secret"))
}

func newServer(health error, users *fakeUsers) (*Server, *bytes.Buffer) {
	var logs bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))

	return New(fakeHealth{err: health}, users, log), &logs
}

func serve(s *Server, method, target, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(context.Background(), method, target, strings.NewReader(body))
	rec := httptest.NewRecorder()
	s.Routes().ServeHTTP(rec, req)

	return rec
}

func parseError(t *testing.T, rec *httptest.ResponseRecorder) errorDetail {
	t.Helper()
	var body errorResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}

	return body.Error
}

func TestLiveness(t *testing.T) {
	s, _ := newServer(errors.New("database down"), &fakeUsers{})

	rec := serve(s, http.MethodGet, "/health/live", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestReadiness(t *testing.T) {
	s, _ := newServer(nil, &fakeUsers{})
	rec := serve(s, http.MethodGet, "/health/ready", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"status":"ok","database":"ok"}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}

	s, logs := newServer(databaseError(), &fakeUsers{})
	rec = serve(s, http.MethodGet, "/health/ready", "")
	if rec.Code != http.StatusServiceUnavailable || parseError(t, rec).Code != "DATABASE_UNAVAILABLE" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if !strings.Contains(logs.String(), "Database health check failed") || strings.Contains(logs.String(), "driver secret") {
		t.Fatalf("logs = %s", logs)
	}
}

func TestRemovedHealthAlias(t *testing.T) {
	s, _ := newServer(nil, &fakeUsers{})
	if rec := serve(s, http.MethodGet, "/health", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestCreateUser(t *testing.T) {
	user := sampleUser(t)
	s, _ := newServer(nil, &fakeUsers{insert: func(in domain.CreateUser) (domain.User, error) {
		if in.Email.String() != "a@x.com" || in.DisplayName.String() != "Ada" {
			t.Errorf("in = %+v", in)
		}

		return user, nil
	}})

	rec := serve(s, http.MethodPost, "/users", `{"email":" A@X.com ","displayName":"Ada","ignored":true}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if got := rec.Header().Get("Location"); got != userPath {
		t.Fatalf("Location = %q", got)
	}
	want := `{"id":"0192f3a4-7b1c-7def-8a12-3456789abcde","email":"a@x.com","displayName":"Ada","createdAt":"2026-11-01T17:48:00.123Z"}`
	if strings.TrimSpace(rec.Body.String()) != want {
		t.Fatalf("body = %s", rec.Body)
	}
}

func TestErrorResponses(t *testing.T) {
	notCalled := &fakeUsers{}
	failing := func(err error) *fakeUsers {
		return &fakeUsers{
			insert: func(domain.CreateUser) (domain.User, error) { return domain.User{}, err },
			find:   func(domain.UserID) (domain.User, error) { return domain.User{}, err },
			list:   func(int) ([]domain.User, error) { return nil, err },
		}
	}
	validUser := `{"email":"a@x.com","displayName":"Ada"}`

	tests := []struct {
		name   string
		users  *fakeUsers
		method string
		target string
		body   string
		status int
		code   string
		fields []string
	}{
		{"bad limit", notCalled, http.MethodGet, "/users?limit=abc", "", 400, "INVALID_REQUEST", []string{"limit"}},
		{"limit zero", notCalled, http.MethodGet, "/users?limit=0", "", 400, "INVALID_REQUEST", []string{"limit"}},
		{"limit too high", notCalled, http.MethodGet, "/users?limit=201", "", 400, "INVALID_REQUEST", []string{"limit"}},
		{"bad uuid", notCalled, http.MethodGet, "/users/abc", "", 400, "INVALID_REQUEST", []string{"id"}},
		{"malformed json", notCalled, http.MethodPost, "/users", `{`, 400, "INVALID_REQUEST", nil},
		{"wrong json type", notCalled, http.MethodPost, "/users", `{"email":1}`, 400, "INVALID_REQUEST", nil},
		{"trailing data", notCalled, http.MethodPost, "/users", validUser + ` {}`, 400, "INVALID_REQUEST", nil},
		{"every invalid field", notCalled, http.MethodPost, "/users", `{"email":"bad","displayName":" "}`, 400, "INVALID_REQUEST", []string{"email", "displayName"}},
		{"too large", notCalled, http.MethodPost, "/users", `{"email":"` + strings.Repeat("a", maxBodyBytes) + `"}`, 413, "REQUEST_TOO_LARGE", nil},
		{"not found", failing(domain.UserNotFound()), http.MethodGet, userPath, "", 404, "USER_NOT_FOUND", nil},
		{"conflict", failing(domain.UserAlreadyExists()), http.MethodPost, "/users", validUser, 409, "USER_ALREADY_EXISTS", nil},
		{"database error", failing(databaseError()), http.MethodGet, "/users", "", 500, "DATABASE_ERROR", nil},
		{"unavailable list", failing(unavailable()), http.MethodGet, "/users", "", 503, "DATABASE_UNAVAILABLE", nil},
		{"unavailable find", failing(unavailable()), http.MethodGet, userPath, "", 503, "DATABASE_UNAVAILABLE", nil},
		{"unavailable insert", failing(unavailable()), http.MethodPost, "/users", validUser, 503, "DATABASE_UNAVAILABLE", nil},
		{"defect", failing(errors.New("driver secret")), http.MethodGet, "/users", "", 500, "INTERNAL_ERROR", nil},
		{"stored row paths stay private", failing(apperr.Infrastructure(apperr.KindInternal, "DATABASE_ERROR", "safe", "users.find", apperr.Diagnostics{Reason: "ValidationError", ValidationPaths: []string{"email"}}, nil)), http.MethodGet, userPath, "", 500, "DATABASE_ERROR", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, _ := newServer(nil, tt.users)

			rec := serve(s, tt.method, tt.target, tt.body)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
			}
			got := parseError(t, rec)
			if got.Code != tt.code || !slices.Equal(got.Fields, tt.fields) {
				t.Fatalf("got %+v", got)
			}
			if strings.Contains(rec.Body.String(), "driver secret") {
				t.Fatalf("response leaked the cause: %s", rec.Body)
			}
		})
	}
}

func TestErrorLogging(t *testing.T) {
	failing := func(err error) *fakeUsers {
		return &fakeUsers{list: func(int) ([]domain.User, error) { return nil, err }}
	}

	tests := []struct {
		name    string
		err     error
		message string
		safe    bool
	}{
		{"internal", databaseError(), "Operation failed", true},
		{"unavailable", unavailable(), "Operation failed", true},
		{"defect", errors.New("full defect detail"), "Unexpected error", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, logs := newServer(nil, failing(tt.err))

			serve(s, http.MethodGet, "/users", "")
			if !strings.Contains(logs.String(), tt.message) {
				t.Fatalf("logs = %s", logs)
			}
			if tt.safe && strings.Contains(logs.String(), "driver secret") {
				t.Fatalf("logs leaked the cause: %s", logs)
			}
			if !tt.safe && !strings.Contains(logs.String(), "full defect detail") {
				t.Fatalf("logs = %s", logs)
			}
		})
	}

	t.Run("client errors aren't logged as failures", func(t *testing.T) {
		s, logs := newServer(nil, &fakeUsers{})
		serve(s, http.MethodGet, "/users?limit=abc", "")
		if strings.Contains(logs.String(), `"level":"ERROR"`) {
			t.Fatalf("logs = %s", logs)
		}
	})
}

func TestCanceledRequest(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{list: func(int) ([]domain.User, error) { return nil, databaseError() }})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req := httptest.NewRequestWithContext(ctx, http.MethodGet, "/users", http.NoBody)
	rec := httptest.NewRecorder()

	s.Routes().ServeHTTP(rec, req)

	if rec.Code != statusClientClosedRequest || rec.Body.Len() != 0 {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if strings.Contains(logs.String(), `"level":"ERROR"`) || !strings.Contains(logs.String(), `"status":499`) {
		t.Fatalf("logs = %s", logs)
	}
}

func TestListUsers(t *testing.T) {
	users := &fakeUsers{list: func(int) ([]domain.User, error) { return nil, nil }}
	s, _ := newServer(nil, users)

	rec := serve(s, http.MethodGet, "/users", "")
	if rec.Code != http.StatusOK || strings.TrimSpace(rec.Body.String()) != `{"users":[]}` {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	if users.lastLimit != defaultListLimit {
		t.Fatalf("limit = %d", users.lastLimit)
	}

	serve(s, http.MethodGet, "/users?limit=200", "")
	if users.lastLimit != maxListLimit {
		t.Fatalf("limit = %d", users.lastLimit)
	}
}

func TestGetUser(t *testing.T) {
	user := sampleUser(t)
	s, _ := newServer(nil, &fakeUsers{find: func(id domain.UserID) (domain.User, error) {
		if id != user.ID {
			t.Errorf("id = %v", id)
		}

		return user, nil
	}})

	if rec := serve(s, http.MethodGet, userPath, ""); rec.Code != http.StatusOK {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
}

func TestAccessLog(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{find: func(domain.UserID) (domain.User, error) { return sampleUser(t), nil }})

	rec := serve(s, http.MethodGet, userPath, "")

	requestID := rec.Header().Get("X-Request-ID")
	if len(requestID) != 32 {
		t.Fatalf("X-Request-ID = %q", requestID)
	}

	var line struct {
		Level     string `json:"level"`
		Msg       string `json:"msg"`
		Method    string `json:"method"`
		Route     string `json:"route"`
		Status    int    `json:"status"`
		RequestID string `json:"requestId"`
	}
	if err := json.Unmarshal(logs.Bytes(), &line); err != nil {
		t.Fatalf("decode %q: %v", logs, err)
	}
	if line.Msg != "HTTP request" || line.Level != "INFO" || line.Method != http.MethodGet ||
		line.Route != "GET /users/{id}" || line.Status != http.StatusOK || line.RequestID != requestID {
		t.Fatalf("line = %+v", line)
	}
}

func TestEveryLogLineCarriesRequestID(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{list: func(int) ([]domain.User, error) { return nil, databaseError() }})

	rec := serve(s, http.MethodGet, "/users", "")

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %v", lines)
	}
	for _, line := range lines {
		if !strings.Contains(line, `"requestId":"`+rec.Header().Get("X-Request-ID")+`"`) {
			t.Fatalf("line = %s", line)
		}
	}
}

func TestHealthAccessLogIsDebug(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{})

	serve(s, http.MethodGet, "/health/live", "")

	if !strings.Contains(logs.String(), `"level":"DEBUG"`) {
		t.Fatalf("logs = %s", logs)
	}
}

func TestUnmatchedRoutesStayPlainText(t *testing.T) {
	s, _ := newServer(nil, &fakeUsers{})

	if rec := serve(s, http.MethodGet, "/nope", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("got %d", rec.Code)
	}
	if rec := serve(s, http.MethodDelete, "/users", ""); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d", rec.Code)
	}
}

func TestPanicBecomesJSON500(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{list: func(int) ([]domain.User, error) { panic("kaboom") }})

	rec := serve(s, http.MethodGet, "/users", "")

	if rec.Code != http.StatusInternalServerError || parseError(t, rec).Code != "INTERNAL_ERROR" {
		t.Fatalf("got %d %s", rec.Code, rec.Body)
	}
	for _, want := range []string{"Panic recovered", "kaboom", "stack", `"status":500`, "requestId"} {
		if !strings.Contains(logs.String(), want) {
			t.Fatalf("logs missing %q: %s", want, logs)
		}
	}
}

func TestAbortHandlerIsRepanicked(t *testing.T) {
	s, logs := newServer(nil, &fakeUsers{list: func(int) ([]domain.User, error) { panic(http.ErrAbortHandler) }})

	defer func() {
		if recovered := recover(); recovered != http.ErrAbortHandler { //nolint:errorlint // a panic value
			t.Fatalf("recovered = %v", recovered)
		}
		if strings.Contains(logs.String(), "Panic recovered") {
			t.Fatalf("logs = %s", logs)
		}
	}()
	serve(s, http.MethodGet, "/users", "")
}

func TestStatusMapping(t *testing.T) {
	tests := map[apperr.Kind]int{
		apperr.KindInternal:    http.StatusInternalServerError,
		apperr.KindInvalid:     http.StatusBadRequest,
		apperr.KindNotFound:    http.StatusNotFound,
		apperr.KindConflict:    http.StatusConflict,
		apperr.KindUnavailable: http.StatusServiceUnavailable,
		apperr.KindTooLarge:    http.StatusRequestEntityTooLarge,
	}
	for kind, want := range tests {
		if got := status(kind); got != want {
			t.Errorf("status(%v) = %d, want %d", kind, got, want)
		}
	}
}

func TestListenAndServeShutsDownOnCancel(t *testing.T) {
	s, _ := newServer(nil, &fakeUsers{})
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() { done <- s.ListenAndServe(ctx, "127.0.0.1", 18765) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("ListenAndServe = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server didn't stop")
	}
}
