# Go Backend Template: Design Spec

A Go backend template with a PostgreSQL-backed HTTP API and a polling worker with a one-shot mode, all shipped in one container image. It uses the standard library plus a few small, single-purpose libraries.

## 1. Scope

**In scope:** the Go module layout, domain, application, Postgres adapter, config, HTTP API, worker, migrations, error handling, wiring, testing, linting, CI checks, Docker, Compose, smoke scripts, project initialization, dependency pinning and dependency updates.

**Out of scope:**
- Cloud infrastructure, deployment pipelines and production setup.
- Authentication, authorization, CORS, rate limiting, metrics and tracing.
- A workflow runtime. The user feature and the polling job are examples to replace.

The image ships every executable, including a separate migration binary, so a deployment pipeline can be added later without changing the code.

## 2. Principles

1. **Dependencies point inward.** `domain` imports only `apperr` from the project, `application` imports `domain`, and adapters implement `application` contracts.
2. **Each executable has a composition root.** Only `cmd/*/main.go` picks concrete implementations. There is no shared global wiring imported by every binary.
3. **Code is grouped by feature within a boundary.** No global `controllers/`, `services/` or `utils/` packages.
4. **Logs carry only safe diagnostics.** Handled errors log the failure reason, SQLSTATE code and validation field paths, never driver messages, SQL parameters or stored values. Client responses carry only stable codes, generic messages and public field names.
5. **Liveness doesn't depend on the database.** The API starts and answers `/health/live` while Postgres is down.
6. **Migrations never run at application startup.** They're a separate step: the `migrate` binary.
7. **Every dependency and tool version is pinned.**

## 3. Stack

| Concern | Choice |
|---|---|
| Language/toolchain | Latest stable Go, pinned by a single `go` directive in `go.mod` |
| HTTP | `net/http` (`ServeMux` method/path patterns) |
| Postgres driver/pool | `github.com/jackc/pgx/v5` (`pgxpool`, `pgconn`, `stdlib` for goose) |
| Queries | `sqlc` (generated, committed) |
| SQLSTATE names | `github.com/jackc/pgerrcode` |
| Migrations | `github.com/pressly/goose/v3` (Provider API, embedded SQL) |
| Config | `github.com/caarlos0/env/v11` |
| IDs | `github.com/google/uuid` |
| Logging | `log/slog` JSON handler |
| Shutdown | `context`, `os/signal` |
| Lint/format | `golangci-lint` v2 |
| Tests | `testing`, `httptest`, Compose Postgres |
| Tasks | `just` |
| Template init | `gonew` + `just init` |
| Dependency updates | Dependabot |

## 4. Layout

```text
.
├── go.mod / go.sum
├── tools.mod / tools.sum      # dev tools, kept out of the application's module graph
├── justfile
├── .golangci.yml
├── sqlc.yaml
├── .env.example
├── .gitignore                 # includes .env and bin/
├── .dockerignore              # includes .env and .git
├── .github/
│   ├── dependabot.yml
│   └── workflows/ci.yml
├── Dockerfile
├── docker-compose.yml
├── docker-compose.override.yml
├── LICENSE                    # MIT; template-only
├── README.md
├── ARCHITECTURE.md
├── SPEC.md                    # template-only
├── cmd/
│   ├── api/main.go            # composition root + -healthcheck mode
│   ├── worker/main.go         # persistent polling + -once mode
│   └── migrate/main.go        # goose runner over embedded migrations
├── internal/
│   ├── apperr/                # Error, Kind, Diagnostics (no project deps)
│   ├── domain/
│   │   └── user.go            # User, UserID, Email, DisplayName, errors
│   ├── application/
│   │   ├── health.go          # HealthChecker port
│   │   └── users.go           # UserRepository port
│   ├── postgres/
│   │   ├── pool.go            # lazy pgxpool
│   │   ├── health.go          # HealthChecker adapter
│   │   ├── diagnostics.go     # generic safe diagnostics + repositoryError
│   │   ├── queries/users.sql  # sqlc input
│   │   ├── sqlcgen/           # sqlc output (generated, committed)
│   │   └── users.go           # UserRepository adapter, row and error mapping
│   ├── config/config.go
│   ├── httpapi/
│   │   ├── server.go          # Server, Routes, middleware
│   │   ├── errors.go          # kind → status, JSON error body
│   │   ├── health.go
│   │   └── users.go           # handlers + request/response types
│   └── worker/
│       ├── runtime.go         # Run loop, RunOnce, failure reporting
│       └── jobs/sample_users.go
├── migrations/
│   ├── embed.go               # //go:embed *.sql
│   └── 00001_create_users.sql
└── scripts/
    ├── init/main.go           # template-only
    ├── smoke-docker.sh
    ├── smoke-api/main.go
    └── test-init.sh           # template-only
```

The application is one module. Development tools live in root-level `tools.mod` and `tools.sum`, not a nested `tools/go.mod`: Go's module zip omits any directory with its own `go.mod`, and `gonew` copies from that zip, so a nested module would never reach new projects.

Unit tests sit beside their code in `_test.go` files. Repository integration tests use the `integration` build tag.

## 5. Dependency rules

| Package | May import |
|---|---|
| `apperr`, `migrations`, `scripts/*` | standard library only |
| `domain` | `apperr`, `uuid` |
| `application` | `domain`, `apperr` |
| `postgres` | `application`, `domain`, `apperr`, `sqlcgen`, pgx, `pgerrcode`, `uuid`; in tests also goose and `migrations` |
| `sqlcgen` | pgx, `uuid` |
| `config` | `env` |
| `httpapi`, `worker` | `application`, `domain`, `apperr` |
| `cmd/*` | anything |

The standard library is available to every package.

`internal/` only stops other modules from importing the code, so these rules are enforced by `depguard` in `.golangci.yml`. In particular, nothing outside `postgres` and `cmd` may import `github.com/jackc/pgx`, and `httpapi` and `worker` may not import `postgres`.

## 6. Domain

```go
type UserID struct{ value uuid.UUID }   // String() returns the canonical form
type Email struct{ value string }       // trimmed, lowercased, matches ^[^\s@]+@[^\s@]+\.[^\s@]+$, ≤ 320 runes
type DisplayName struct{ value string } // trimmed, 1–100 runes

type User struct {
    ID          UserID
    Email       Email
    DisplayName DisplayName
    CreatedAt   time.Time
}

type CreateUser struct {
    Email       Email
    DisplayName DisplayName
}

func ParseUserID(s string) (UserID, error)
func UserIDFromUUID(id uuid.UUID) UserID
func (id UserID) UUID() uuid.UUID
func NewCreateUser(email, displayName string) (CreateUser, error)
func RestoreUser(id UserID, email, displayName string, createdAt time.Time) (User, error)
```

- **Valid values come only from constructors.** Unexported fields make the compiler enforce this: `domain.Email("junk")` doesn't compile. `ParseUserID`, `ParseEmail`, `ParseDisplayName`, `NewCreateUser` and `RestoreUser` return a `KindInvalid` error listing the failing field paths (`["email"]`). A zero value can still be declared, so code outside `domain` doesn't build these types with struct literals.
- **Each user has one valid ID form.** `ParseUserID` accepts only the 36-character canonical form, although `uuid.Parse` also accepts URN, braced and 32-character hex forms.
- **Every failing field is reported.** `NewCreateUser` returns all failing paths, not just the first.
- **Field paths match API field names** (`email`, `displayName`), because the HTTP layer returns them to clients. A test enforces this.
- **Domain types carry no wire format.** JSON tags and timestamp formatting live in `httpapi` (§11), so renaming a domain field can't change the API by accident.
- **Emails compare case-insensitively.** `ParseEmail` lowercases them, so `A@x.com` and `a@x.com` hit the same unique constraint.
- **The domain is the only home for these rules.** The schema has no `CHECK` constraints for email or display name, so the rules can't drift between Go and SQL. A project with other database writers can add constraints later.
- **Domain errors are constructors returning `*apperr.Error`:**

```go
func UserNotFound() *apperr.Error {
    return apperr.New(apperr.KindNotFound, "USER_NOT_FOUND", "User not found")
}

func UserAlreadyExists() *apperr.Error {
    return apperr.New(apperr.KindConflict, "USER_ALREADY_EXISTS",
        "A user with this email already exists")
}
```

## 7. Errors

### 7.1 `apperr`

```go
type Kind int

const (
    KindInternal Kind = iota // 500
    KindInvalid              // 400
    KindNotFound             // 404
    KindConflict             // 409
    KindUnavailable          // 503
    KindTooLarge             // 413
)

type Diagnostics struct {
    Reason          string   `json:"reason"`
    Code            string   `json:"code,omitempty"`            // SQLSTATE
    ValidationPaths []string `json:"validationPaths,omitempty"`
}

type Error struct {
    Kind        Kind
    Code        string       // stable, client-facing: "USER_NOT_FOUND"
    Message     string       // safe for clients
    Op          string       // "users.insert"; set for infrastructure failures
    Diagnostics *Diagnostics // safe for logs
    cause       error        // raw cause: no Unwrap, never logged on handled paths
}

func (e *Error) Error() string { return e.Message }
func (e *Error) Cause() error  { return e.cause }

func New(kind Kind, code, message string) *Error
func Invalid(code, message string, paths ...string) *Error // sets ValidationPaths
func InvalidRequest(paths ...string) *Error                // the standard INVALID_REQUEST failure
func Infrastructure(kind Kind, code, message, op string, d Diagnostics, cause error) *Error
func As(err error) (*Error, bool)
```

### 7.2 Rules

1. **Return `*apperr.Error` for expected failures.** Any other error that reaches a boundary is treated as a defect.
2. **`Error` has no `Unwrap`.** Otherwise an `errors.As(err, &pgErr)` in `httpapi` would compile and quietly tie the HTTP layer to Postgres. The raw cause is reachable only through `Cause()`, for debugging.
3. **Watch for typed nil.** Functions that can return a nil error declare their result as `error`, never as `*apperr.Error`, because a nil `*apperr.Error` stored in an `error` isn't `nil`.
4. **Handled-error logs contain only `Op`, `Code` and `Diagnostics`.** Defects are logged in full.
5. **Codes in use:** `INVALID_REQUEST`, `REQUEST_TOO_LARGE`, `USER_NOT_FOUND`, `USER_ALREADY_EXISTS`, `DATABASE_ERROR`, `DATABASE_UNAVAILABLE`, `JOB_TIMEOUT`, `INTERNAL_ERROR`.

### 7.3 Postgres mapping

Feature-specific mappings live with their feature in `postgres/users.go` and run first, so deleting the feature removes them:

| Condition | Result |
|---|---|
| `pgErr.Code == UniqueViolation && pgErr.ConstraintName == "users_email_unique"` | `domain.UserAlreadyExists()` |
| `pgx.ErrNoRows` in `FindByID` | `domain.UserNotFound()` |

Every other failure goes through `repositoryError(op, err)` in `postgres/diagnostics.go`. The first matching row wins.

| Condition | Diagnostics | Kind and code |
|---|---|---|
| `context.Canceled` | `Reason: "Canceled"` | `KindInternal`, `DATABASE_ERROR` |
| `context.DeadlineExceeded` / `pgconn.Timeout(err)` | `Reason: "Timeout"` | `KindUnavailable`, `DATABASE_UNAVAILABLE` |
| `*pgconn.ConnectError` | `Reason: "ConnectionError"` | `KindUnavailable`, `DATABASE_UNAVAILABLE` |
| `*pgconn.PgError` in SQLSTATE class `08`, or `53300`, `57P01`, `57P02`, `57P03` | `Reason: "ConnectionError"`, `Code: pgErr.Code` | `KindUnavailable`, `DATABASE_UNAVAILABLE` |
| Network or EOF error from a dropped connection | `Reason: "ConnectionError"` | `KindUnavailable`, `DATABASE_UNAVAILABLE` |
| Any other `*pgconn.PgError` | `Reason: "QueryError"`, `Code: pgErr.Code` | `KindInternal`, `DATABASE_ERROR` |
| Stored row fails domain validation | `Reason: "ValidationError"`, `ValidationPaths` | `KindInternal`, `DATABASE_ERROR` |
| Anything else | `Reason: "UnknownError"` | `KindInternal`, `DATABASE_ERROR` |

`DATABASE_ERROR` uses the message "The database operation could not be completed", and `DATABASE_UNAVAILABLE` uses "The database is temporarily unavailable". `pgErr.Message`, `Detail` and `Where` are never copied into diagnostics.

## 8. Application ports

```go
type HealthChecker interface {
    // Health runs a bounded query. Errors: DATABASE_UNAVAILABLE, DATABASE_ERROR.
    Health(ctx context.Context) error
}

type UserRepository interface {
    // Insert errors: USER_ALREADY_EXISTS, DATABASE_UNAVAILABLE, DATABASE_ERROR.
    Insert(ctx context.Context, in domain.CreateUser) (domain.User, error)
    // FindByID errors: USER_NOT_FOUND, DATABASE_UNAVAILABLE, DATABASE_ERROR.
    FindByID(ctx context.Context, id domain.UserID) (domain.User, error)
    // List returns newest first; limit must be positive.
    // Errors: DATABASE_UNAVAILABLE, DATABASE_ERROR.
    List(ctx context.Context, limit int) ([]domain.User, error)
}
```

Doc comments list each method's error codes, because Go's types can't express them. Consumers that need fewer methods declare narrower interfaces locally (§12).

`HealthChecker` is separate so that readiness keeps working when the example user feature is removed.

Handlers call ports directly while a use case is a single port call. Add a service in `application/<feature>.go` once a use case combines several ports or applies business rules.

## 9. Postgres adapter

- **The pool connects lazily.** `NewPool(ctx, databaseURL, applicationName string)` uses `pgxpool.NewWithConfig`. Building it opens no connection, so the API starts without the database. Each composition root passes its own `application_name` (`api`, `worker`, or `worker-once` in `-once` mode), so binaries are distinguishable in `pg_stat_activity`.
- **The URL controls pool settings.** `pool_max_conns` and `connect_timeout` in `DATABASE_URL` are honored. When they aren't set, the defaults are 10 connections and a 5s connect timeout.
- **Health** runs `SELECT 1` under a 5s `context.WithTimeout`.
- **Every query has a timeout.** Each adapter method applies a 5s `context.WithTimeout`, so a hung query can't hold one of the pool's connections indefinitely. A method that needs longer sets its own limit.
- **Queries** are written in `queries/users.sql` and generated by sqlc into `sqlcgen/`. `sqlc.yaml` sets `emit_empty_slices: true`, so an empty list serializes as `[]` rather than `null`. pgx and sqlc types never leave the package.
- **Rows are validated before they're returned**, including rows from `INSERT … RETURNING`. They pass through the domain constructors, and a stored row that's no longer valid is reported as a `ValidationError` repository failure.
- **Adapters are checked against their ports at compile time:** `var _ application.UserRepository = (*UserRepository)(nil)`, and the same for `HealthChecker`.

### Transactions

An atomic operation is one port method, so callers never see a transaction. If several features must commit together, add a single port method whose adapter runs all the writes in one transaction.

No example feature needs a transaction, so the template ships no helper for one, since it would be unused code. `ARCHITECTURE.md` documents the pattern:

```go
func (r *UserRepository) inTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
    return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
        return fn(r.queries.WithTx(tx))
    })
}
```

### Migrations

`migrations/00001_create_users.sql`:

```sql
-- +goose Up
CREATE TABLE users (
    id           uuid PRIMARY KEY DEFAULT uuidv7(),
    email        text NOT NULL,
    display_name text NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT users_email_unique UNIQUE (email)
);

-- +goose Down
DROP TABLE users;
```

- **The unique constraint is named explicitly** because the adapter matches on that name.
- **`List` orders by `id DESC`.** `uuidv7()` values are unique and time-ordered, so the primary key gives a stable newest-first order with no extra index, and future cursor pagination stays simple. All IDs must come from the database default, because imported IDs sort by their own timestamps.
- **`cmd/migrate`** embeds the folder and supports `up`, `down`, `status` and `create <name>`. It uses goose's Provider API over `pgx/v5/stdlib`, with `goose.WithSessionLocker` so concurrent runs can't race.
- **Numbering is sequential.** Two branches adding the same number collide visibly instead of applying out of order.
- **`create` needs no database connection,** so it must not require `DATABASE_URL`. It writes to the `migrations/` directory.
- **Migrations must keep working** while old and new application versions run side by side.

## 10. Configuration

```go
type Config struct {
    DatabaseURL        Secret        `env:"DATABASE_URL,required"`
    APIHost            string        `env:"API_HOST" envDefault:"127.0.0.1"`
    APIPort            int           `env:"API_PORT" envDefault:"8000"`
    WorkerPollInterval time.Duration `env:"WORKER_POLL_INTERVAL" envDefault:"5s"`
    WorkerPassTimeout  time.Duration `env:"WORKER_PASS_TIMEOUT" envDefault:"30s"`
    LogLevel           slog.Level    `env:"LOG_LEVEL" envDefault:"INFO"`
}

func Load() (Config, error) // parse + Validate(): port 1–65535, interval and timeout > 0
```

- **`api` and `worker` share one `Config`.** A bad `API_PORT` can stop the worker, but that failure is loud and rare, so per-binary config types aren't worth adding yet.
- **`migrate` and `api -healthcheck` don't use `Config`.** They read only `DATABASE_URL` and `API_PORT` respectively, through `os.Getenv`.
- **`Secret` never prints its value.** It implements `String()` and `slog.LogValuer` to redact itself, and exposes the value through an explicit `Value()` method.
- **Go doesn't read `.env`.** The `justfile` loads it with `set dotenv-load`, and Compose supplies the environment for containers.

`.env.example`:

```sh
POSTGRES_DB=go_template
POSTGRES_USER=go_template
POSTGRES_PASSWORD=go_template
POSTGRES_PORT=5432
# Used by native commands; Compose supplies its own internal DATABASE_URL.
DATABASE_URL=postgresql://go_template:go_template@127.0.0.1:5432/go_template
# Used by integration tests to create a throwaway database per test.
TEST_DATABASE_URL=postgresql://go_template:go_template@127.0.0.1:5432/go_template
API_HOST=127.0.0.1
API_PORT=8000
WORKER_POLL_INTERVAL=5s
WORKER_PASS_TIMEOUT=30s
LOG_LEVEL=INFO
```

## 11. HTTP API

### Endpoints

| Endpoint | Success | Failures |
|---|---|---|
| `GET /health/live` | 200 `{"status":"ok"}` | none; doesn't touch the database |
| `GET /health/ready` | 200 `{"status":"ok","database":"ok"}` | 503 `DATABASE_UNAVAILABLE` |
| `GET /users?limit=` | 200 `{"users":[...]}` | 400 bad limit, 500 `DATABASE_ERROR`, 503 `DATABASE_UNAVAILABLE` |
| `GET /users/{id}` | 200 user | 400 bad UUID, 404, 500, 503 |
| `POST /users` | 201 user, with `Location: /users/{id}` | 400 bad JSON or validation, 409, 413, 500, 503 |

`limit` defaults to 50 and must be between 1 and 200. Cursor pagination is added when a feature needs it.

Errors use the body `{"error":{"code":"...","message":"..."}}`. The exception is `ServeMux`'s own plain-text 404 and 405 for unmatched routes. Those are left as they are, because making them JSON means intercepting the mux's writes, and a catch-all route would lose its 405s.

**Validation failures** return `INVALID_REQUEST` with the message "The request is invalid" and a `fields` list:

```json
{"error":{"code":"INVALID_REQUEST","message":"The request is invalid","fields":["email","displayName"]}}
```

Bad path and query values report `["id"]` and `["limit"]`. Malformed JSON omits `fields`. Only `KindInvalid` errors expose `fields`, so `ValidationPaths` from a stored row that fails validation stay in the logs.

Response types live in `httpapi/users.go`. `userResponse` holds the JSON tags (`id`, `email`, `displayName`, `createdAt`). Its `Timestamp` type wraps `time.Time` and serializes as UTC with milliseconds (`2026-11-01T18:48:00.000Z`), the format of JavaScript's `Date.toISOString()`, so JavaScript clients get a stable format.

### Handler pattern

```go
type handler func(w http.ResponseWriter, r *http.Request) error

func (s *Server) handle(h handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        if err := h(w, r); err != nil {
            s.writeError(w, r, err)
        }
    }
}

func status(k apperr.Kind) int {
    switch k {
    case apperr.KindInvalid:
        return http.StatusBadRequest
    case apperr.KindNotFound:
        return http.StatusNotFound
    case apperr.KindConflict:
        return http.StatusConflict
    case apperr.KindUnavailable:
        return http.StatusServiceUnavailable
    case apperr.KindTooLarge:
        return http.StatusRequestEntityTooLarge
    case apperr.KindInternal:
        return http.StatusInternalServerError
    }
    return http.StatusInternalServerError
}
```

The `exhaustive` linter checks that `status` covers every `Kind`.

`writeError` follows four rules, in order:
1. **If the request's context is canceled,** the client has gone. Nothing is logged as a failure, and it sets status 499 with no body, so the access log shows the disconnect.
2. **An `*apperr.Error` of `KindInternal` or `KindUnavailable`** is logged as "Operation failed" with `op`, `code` and `diagnostics`, then gets its kind's status.
3. **Any other `*apperr.Error`** (validation, not found, conflict, too large) is returned as is, without logging.
4. **Anything else** is logged in full as "Unexpected error" and returns 500 `INTERNAL_ERROR`.

**Readiness** overrides the generic mapping: any `Health` error is logged as "Database health check failed" and returns 503 `DATABASE_UNAVAILABLE`.

### Server behavior

- **Middleware order:** request ID, then access log, then recovery, then the mux. A panic still produces a logged 500 with a request ID.
- **Panics:** the recovery middleware turns them into a JSON 500 and logs the stack. `net/http` would otherwise recover them silently. It re-panics `http.ErrAbortHandler`, which is a deliberate abort rather than a failure.
- **Request IDs:** middleware generates an ID with `crypto/rand` and returns it in `X-Request-ID`. It stores `log.With("requestId", id)` in the request context, read through a private `httpapi` helper, so every log line for the request carries the ID.
- **Access log:** each request gets one line with method, route pattern, status, duration and request ID.
  - Status comes from a response-writer wrapper.
  - The route pattern must be read from the same `*http.Request` passed to the mux, because the mux sets `r.Pattern` on that value and a `WithContext` copy leaves the outer one empty.
  - `/health/*` lines are logged at Debug, so container health checks don't flood the logs.
- **Request bodies:** limited to 1 MiB with `http.MaxBytesReader`. A larger body returns 413 `REQUEST_TOO_LARGE`. Unknown JSON fields are ignored, trailing data after the JSON object returns 400, and `Content-Type` isn't enforced.
- **Timeouts:** `http.Server` sets `ReadHeaderTimeout` to 5s, `ReadTimeout` and `WriteTimeout` to 15s, and `IdleTimeout` to 60s.
- **Shutdown:** on SIGINT or SIGTERM, `Shutdown` gets a 10-second context from `context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)` to finish open requests. Request contexts don't derive from the signal context, so a signal doesn't cancel requests in progress.
- **Container health check:** `api -healthcheck` sends `GET http://127.0.0.1:$API_PORT/health/live` and exits 0 or 1. It's used because the distroless image has no curl or shell.

## 12. Dependency injection

- **Constructors take every dependency.** Struct fields stay unexported, so a value can't be built with a field left empty.
- **"Accept interfaces, return structs."** Adapters return concrete types, and consumers accept the narrowest interface they need:

```go
// worker/jobs/sample_users.go
type userLister interface {
    List(ctx context.Context, limit int) ([]domain.User, error)
}
```

- **Composition roots follow the `run` pattern**, so deferred cleanup runs before `os.Exit`:

```go
func main() {
    ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
    defer stop()
    var level slog.LevelVar
    log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: &level}))
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
```

- **No DI frameworks** (`fx`, `wire`). The wiring for each binary stays under about 30 lines.
- **Test fakes live in `_test.go` files beside the code that uses them.** They're compiled only by `go test`, so runtime code can't import them.

## 13. Worker

### Runtime (`worker/runtime.go`)

```go
type Job func(ctx context.Context) error

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
            LogFailure(log, err) // "Worker pass failed" with safe diagnostics; shared with worker -once
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
```

### Sample job

`jobs.SampleUsers(users userLister, log *slog.Logger) Job` lists 5 users and logs "Worker user sample loaded" with `userCount`.

### Behavior

- **Passes run one at a time**, with the full interval after each one. That's why it uses `Timer`, not `Ticker`.
- **Each pass has a timeout** (`WORKER_PASS_TIMEOUT`). A pass that exceeds it fails with `JOB_TIMEOUT`, or with the job's own `*apperr.Error`, and the next poll runs normally.
- **Expected failures** (any `*apperr.Error`) are logged with safe diagnostics, and the next poll runs normally. This is not a retry policy for jobs with side effects.
- **Unexpected errors and panics end the process with a nonzero exit.**
- **SIGINT and SIGTERM cancel the context.** A pass in progress stops at its next database call, the pool closes, and the process exits 0 without logging.
- **`worker -once`** runs one pass. On failure it logs once at the outer boundary, with safe diagnostics for `*apperr.Error` and the full error otherwise, then exits 1. If a signal interrupts the pass, it logs "Worker pass interrupted" and exits 1, because the pass didn't finish.
- **Every call inside a job takes `ctx`.** A job that ignores it delays shutdown until the container is killed.
- **More jobs later run under `errgroup`** with the shared context, so one defect cancels the rest.

## 14. Testing

| Layer | How |
|---|---|
| `domain`, `apperr`, `config` | Table-driven unit tests |
| `httpapi` | `httptest` against `Server.Routes()` with fake repositories; one test per error code to cover the mapping |
| `worker` | Run loop with a fake job: expected error continues, defect stops, pass timeout continues (including a job that returns a bare `ctx.Err()`), cancellation exits quietly; `slog.DiscardHandler` |
| `postgres` | `//go:build integration` tests against the Compose Postgres; covers unique-constraint mapping, not found, row validation, and timeout and unavailable diagnostics |
| End-to-end | `scripts/smoke-docker.sh` + `scripts/smoke-api` |

- **Tests run with `-race`.**
- **No monkey-patching libraries.** They're banned through `depguard`. Dependencies are replaced only through interfaces.
- **Each integration test gets its own database.** A helper in the `postgres` test files connects through `TEST_DATABASE_URL`, creates a uniquely named database, applies the migrations and drops it on cleanup.
- **A missing `TEST_DATABASE_URL` fails the tests** rather than skipping them, so integration tests can't silently stop running.

**Docker smoke** runs in an isolated Compose project with `-f docker-compose.yml` only, `--env-file .env.example` and its own `API_PORT`, so it can't collide with a running dev stack and works without a `.env`. It removes only its own containers and volume. It checks that:
1. The stack starts, liveness and readiness pass, and the `scripts/smoke-api` run passes. That run covers creating, fetching and listing users, plus the 409, 413, 400 (bad UUID, malformed JSON, bad limit) and 404 cases.
2. `worker -once` exits 0, and `migrate up` can be run again safely.
3. With Postgres stopped, liveness still returns 200, readiness and `GET /users` return 503, and the worker logs "Worker pass failed" without restarting.
4. An API recreated with `up --no-deps` serves liveness without Postgres.
5. Data survives container recreation.

`scripts/smoke-api` is a small Go program that runs against the published API port.

**Init test:** `gonew` only fetches published versions, so `scripts/test-init.sh` copies the checkout to a temporary directory and applies the same rewrite (`go mod edit -module` plus import paths). It then runs `just init`, `just setup` and `just check`. It fails if:
- Any nested `go.mod` exists, because a directory copy would otherwise hide what the module zip omits.
- The template's module path or database names remain anywhere.
- `golangci-lint` still passes after a forbidden pgx import is added to `httpapi`, which proves the `depguard` rules still match.

**CI:** `.github/workflows/ci.yml` runs on pull requests and pushes to `main`, on a pinned `ubuntu-24.04` runner, with `permissions: contents: read` and Actions pinned by commit SHA. It has four jobs: `check` (`just check`), `integration` (copies `.env.example` to `.env`, then `just test-integration`), `smoke` (`just smoke`) and `init` (`just test-init`). `just init` removes the `init` job from generated projects.

## 15. Linting (`.golangci.yml`, v2)

- **Formatters:** `gofumpt`, plus `goimports` with the module path as the local prefix.
- **Linters:** `errcheck`, `errorlint`, `nilerr`, `exhaustive`, `depguard`, `forcetypeassert`, `bodyclose`, `contextcheck`, `noctx`, `gosec`, `revive`, `unparam`, `usestdlibvars`, and `nolintlint` with required explanations. `wrapcheck` is left out because typed errors pass through boundaries unchanged.
- **Build tags:** `run.build-tags: [integration]`, so integration tests are linted and can't stop compiling unnoticed.

## 16. Tasks (`justfile`)

| Task | Command |
|---|---|
| `init` | `go run ./scripts/init` (§18) |
| `setup` | `go mod download`, then download the pinned, checksum-verified `golangci-lint` release into `./bin` |
| `dev-api` / `dev-worker` | `go run ./cmd/api` / `./cmd/worker` |
| `sample-worker` | `go run ./cmd/worker -once` |
| `db-new <name>` | `go run ./cmd/migrate create <name>` |
| `db-migrate` | `go run ./cmd/migrate up` |
| `db-status` | `go run ./cmd/migrate status` |
| `generate` | `go tool -modfile=tools.mod sqlc generate` |
| `check` | `./bin/golangci-lint fmt --diff`, `./bin/golangci-lint run`, `go vet -tags integration ./...`, `go mod tidy -diff`, `go tool -modfile=tools.mod sqlc diff`, a check that the Dockerfile's `golang:` tag matches `go.mod`, `go test -race ./...` |
| `test-integration` | `docker compose up -d --wait postgresql`, then `go test -race -tags integration ./internal/postgres/...` |
| `smoke` | `bash scripts/smoke-docker.sh` |
| `test-init` | `bash scripts/test-init.sh` |

There's no watch mode or database GUI by default. Use `psql` or any Postgres client against the local port.

## 17. Docker and Compose

**Dockerfile** has two stages:
1. **Builder** (`golang:<pinned>`, `GOTOOLCHAIN=local`): `go mod download`, then static binaries (`CGO_ENABLED=0 -trimpath -ldflags="-s -w"`) for `api`, `worker` and `migrate`, written to `/app/`. Linting and tests run in `just check` and CI, not in the image build.
2. **Runtime:** `gcr.io/distroless/static-debian13`, `nonroot` variant. It exposes 8000, sets `STOPSIGNAL SIGTERM` and runs `/app/api` by default.

**Compose:**

| Service | Command | Profile |
|---|---|---|
| `postgresql` | `postgres:18.6-alpine`, `pg_isready -h 127.0.0.1` health check, named volume mounted at `/var/lib/postgresql` (the Postgres 18 image layout, not `.../data`) | default |
| `migrate` | `/app/migrate up`, after Postgres is healthy | `app`, `tools` |
| `api` | `/app/api`, health check `/app/api -healthcheck`, published as `127.0.0.1:${API_PORT}:8000`, after `migrate` completes | `app` |
| `worker` | `/app/worker`, after `migrate` completes | `app` |
| `worker-once` | `/app/worker -once`, after `migrate` completes | `tools` |

- **The Postgres health check uses TCP.** During first-time init, the image's temporary server listens only on a socket. A socket-based check could pass before `migrate` can connect.
- **The API's container address is fixed.** Compose sets `API_HOST=0.0.0.0` and `API_PORT=8000` inside the container, whatever the host's `API_PORT` is. The `.env.example` values only apply to native commands.
- **`api` and `worker`** use `restart: unless-stopped` and `stop_grace_period: 15s`, which leaves time for the 10-second shutdown before Docker kills the process.
- **`docker-compose.override.yml`** publishes Postgres on `127.0.0.1:${POSTGRES_PORT}` for native development and integration tests.
- **The internal `DATABASE_URL`** is built from `POSTGRES_USER`, `POSTGRES_PASSWORD` and `POSTGRES_DB`.
- **The full stack** runs with `docker compose --profile app up -d --build --wait`.
- **Data persists** across `docker compose down`. Adding `--volumes` deletes it.

## 18. Starting a new project

```sh
go run golang.org/x/tools/cmd/gonew@<pinned> github.com/kukalajet/go-backend-template example.com/my-project
cd my-project
just init
just setup
cp .env.example .env
docker compose up -d --wait postgresql
just db-migrate
just dev-api   # and `just dev-worker` in another terminal
```

`gonew` rewrites the module path in `go.mod` and Go imports only. `just init` runs `scripts/init/main.go`, a standard-library Go program, which handles the rest:

- **Derives the project name** from the module path, ignoring a major-version suffix such as `/v2`, so it takes no argument. Hyphens become underscores in database identifiers.
- **Replaces template names:** the module path in `.golangci.yml` and `tools.mod`, without which `depguard` rules silently stop matching, and the database identifiers in `.env.example` and Compose.
- **Removes blocks marked `template-only`** from the `justfile`, CI workflow and README: the `init` and `test-init` recipes, the `init` job and the template's usage instructions.
- **Deletes template-only files:** `scripts/init`, `scripts/test-init.sh`, `LICENSE` and `SPEC.md`. Each project chooses its own license.
- **Refuses to run a second time**, when the `template-only` markers are already gone.

### Replacing the example

1. Delete `domain/user.go`, `application/users.go`, `postgres/users.go`, `postgres/queries/users.sql`, `postgres/sqlcgen/users.sql.go`, `httpapi/users.go`, `worker/jobs/sample_users.go` and their tests. `sqlc generate` doesn't remove stale output.
2. Replace `00001_create_users.sql` with your own first migration.
3. Add your first feature's queries in the same change, because `sqlc generate` fails when there are no query files.
4. Remove the user routes and the repository argument from `httpapi.New`, and replace the sample job in `cmd/worker`.
5. Remove the user error codes, and update `scripts/smoke-api` and `scripts/smoke-docker.sh`.
6. Run `just generate`, `just check` and `just smoke`.

Health endpoints and the worker runtime don't depend on the user feature.

### Publishing the template

`gonew` needs a repository it can fetch and a version to resolve. The repository goes public once CI passes, with a `v0.1.0` tag as its first release.

## 19. Dependency policy

Exact versions are pinned when the template is scaffolded and recorded in the README:

- **Go:** a single `go` directive with the full patch version in `go.mod`. A `toolchain` line isn't used, because `go mod tidy` removes one that equals the `go` line. `just check` fails if the `golang:` builder image tag differs from `go.mod`, and `GOTOOLCHAIN=local` makes the image build fail when the image is older.
- **Runtime modules:** `pgx/v5`, `pgerrcode`, `caarlos0/env/v11`, `goose/v3`, `google/uuid`, plus `golang.org/x/sync` once there's more than one worker loop.
- **Test-only modules:** none.
- **Tools:**
  - `sqlc` through the `tool` directive in `tools.mod`, so its dependencies stay out of the application's `go.mod`. Update it with `go get -modfile=tools.mod -tool`.
  - `golangci-lint` is a pinned, checksum-verified binary in `./bin`, used by both local runs and CI, as its maintainers recommend.
  - CI pins `just`, and the README pins `gonew`.
- **Images:** `postgres:18.6-alpine`, the `golang:` builder and the distroless image are all pinned by tag and digest.
- **Lockfile:** `go.sum` is committed. Only stable releases are used, and dependencies are never patched locally.
- **Updates:** `.github/dependabot.yml` opens weekly grouped updates for Go modules, Docker and Actions. Dependabot doesn't read `tools.mod` or the `golangci-lint` version in the `justfile`, so the README lists those as manual bumps. A Go image bump that doesn't match `go.mod` fails `just check`.

## 20. Guardrails

- Add an interface only at a real boundary. Don't write a service that just forwards to the repository.
- Keep handlers unexported in their feature file until another package genuinely needs them.
- Prefer a helper inside the feature over a shared `util` package. Move it only once it's actually shared.
- Never expose pgx, sqlc or SQL types to `httpapi` or `worker`.
- Add folders as responsibilities appear. The layout isn't a quota to fill.
