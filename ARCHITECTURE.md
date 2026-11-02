# Architecture

## Layers

Dependencies point inward, and `depguard` in `.golangci.yml` enforces them.

| Package | Responsibility | May import |
|---|---|---|
| `internal/apperr` | Typed errors that cross boundaries | standard library only |
| `internal/domain` | Business types and their invariants | `apperr`, `uuid` |
| `internal/application` | Ports that adapters implement | `domain`, `apperr` |
| `internal/postgres` | Port implementations on PostgreSQL | `application`, `domain`, `apperr`, `sqlcgen`, pgx, `pgerrcode`, `uuid` |
| `internal/config` | Environment settings | `env` |
| `internal/httpapi`, `internal/worker` | Delivery adapters | `application`, `domain`, `apperr` |
| `cmd/*` | Composition roots that pick concrete implementations | anything |

Handlers call ports directly while a use case is a single port call. Add a service in `application/<feature>.go` once a use case combines several ports or applies business rules.

## Errors

Expected failures are `*apperr.Error`, and any other error that reaches a boundary is treated as a defect.

- **`Error` has no `Unwrap`.** Otherwise `errors.As(err, &pgErr)` in `httpapi` would compile and quietly tie the HTTP layer to Postgres.
- **Handled-error logs contain only `Op`, `Code` and `Diagnostics`,** never driver messages, SQL parameters or stored values. Defects are logged in full.
- **Functions that can return a nil error declare their result as `error`,** because a nil `*apperr.Error` stored in an `error` isn't `nil`.
- **Feature-specific database errors are mapped in the feature's adapter file.** Everything else goes through `repositoryError` in `postgres/diagnostics.go`.

## Domain values

Domain types have unexported fields, so valid values come only from constructors. Field paths in validation errors match API field names, because the HTTP layer returns them to clients. Wire formats stay in `httpapi`.

## Transactions

An atomic operation is one port method, so callers never see a transaction. If several features must commit together, add a single port method whose adapter runs all the writes in one transaction. No example feature needs one, so the template ships no helper. The pattern needs the pool on the repository:

```go
func (r *UserRepository) inTx(ctx context.Context, fn func(*sqlcgen.Queries) error) error {
    return pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
        return fn(r.queries.WithTx(tx))
    })
}
```

## Migrations

Migrations never run at application startup. They run through the `migrate` binary, which holds a session lock so concurrent runs can't race. They must keep working while old and new application versions run side by side.

Numbering is sequential. Two branches adding the same number collide visibly instead of applying out of order.

## Worker

Passes run one at a time, with the full interval after each one. Expected failures are logged and the next poll runs normally, which is not a retry policy for jobs with side effects. Unexpected errors and panics end the process.

Every call inside a job must take `ctx`. A job that ignores it delays shutdown until the container is killed, and it can overrun its pass timeout. Run more jobs under `errgroup` with the shared context, so one defect cancels the rest.

## Guardrails

- Add an interface only at a real boundary. Don't write a service that just forwards to the repository.
- Keep handlers unexported in their feature file until another package genuinely needs them.
- Prefer a helper inside the feature over a shared `util` package. Move it only once it's actually shared.
- Never expose pgx, sqlc or SQL types to `httpapi` or `worker`.
- Add folders as responsibilities appear. The layout isn't a quota to fill.
