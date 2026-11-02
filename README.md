# go-backend-template

A Go backend with a PostgreSQL-backed HTTP API and a polling worker with a one-shot mode, all shipped in one container image.

<!-- template-only:begin -->
## Using this template

You need Go, [`just`](https://github.com/casey/just) 1.59.0 and Docker.

```sh
go run golang.org/x/tools/cmd/gonew@v0.52.0 github.com/kukalajet/go-backend-template example.com/my-project
cd my-project
just init
```

`gonew` rewrites the module path in `go.mod` and Go imports. `just init` derives the project name from the module path, replaces the template's remaining names, and deletes everything that only the template needs, including this section.

The design is in [SPEC.md](SPEC.md).

<!-- template-only:end -->
## Getting started

You need Go, [`just`](https://github.com/casey/just) and Docker.

```sh
just setup
cp .env.example .env
docker compose up -d --wait postgresql
just db-migrate
just dev-api   # and `just dev-worker` in another terminal
```

To run the full stack in containers:

```sh
docker compose --profile app up -d --build --wait
```

Data persists across `docker compose down`. Adding `--volumes` deletes it.

## Tasks

| Task | What it does |
|---|---|
| `just setup` | Downloads modules and the pinned `golangci-lint` into `./bin` |
| `just dev-api` / `just dev-worker` | Runs the API or the polling worker |
| `just sample-worker` | Runs one worker pass |
| `just db-new <name>` | Creates a migration |
| `just db-migrate` / `just db-status` | Applies migrations or shows their status |
| `just generate` | Regenerates the sqlc code |
| `just check` | Formatting, lint, vet, tidy and sqlc drift checks, plus unit tests |
| `just test-integration` | Repository tests against the Compose Postgres |
| `just smoke` | End-to-end test in an isolated Compose project |

## API

| Endpoint | Success | Failures |
|---|---|---|
| `GET /health/live` | 200 | none; doesn't touch the database |
| `GET /health/ready` | 200 | 503 |
| `GET /users?limit=` | 200 | 400, 500, 503 |
| `GET /users/{id}` | 200 | 400, 404, 500, 503 |
| `POST /users` | 201, with `Location` | 400, 409, 413, 500, 503 |

`limit` defaults to 50 and must be between 1 and 200.

Errors use the body `{"error":{"code":"...","message":"..."}}`. Validation failures add `"fields":["email"]`.

## Replacing the example

The user feature and the sample job are examples.

1. Delete `domain/user.go`, `application/users.go`, `postgres/users.go`, `postgres/queries/users.sql`, `postgres/sqlcgen/users.sql.go`, `httpapi/users.go`, `worker/jobs/sample_users.go` and their tests. `sqlc generate` doesn't remove stale output.
2. Replace `00001_create_users.sql` with your own first migration.
3. Add your first feature's queries in the same change, because `sqlc generate` fails when there are no query files.
4. Remove the user routes and the repository argument from `httpapi.New`, and replace the sample job in `cmd/worker`.
5. Remove the user error codes, and update `scripts/smoke-api` and `scripts/smoke-docker.sh`.
6. Run `just generate`, `just check` and `just smoke`.

Health endpoints and the worker runtime don't depend on the user feature.

See [ARCHITECTURE.md](ARCHITECTURE.md) for how the code is organized.

## Pinned versions

Dependabot opens weekly updates for Go modules, Docker images and GitHub Actions. It doesn't read these, so bump them by hand:

| Tool | Version | Where |
|---|---|---|
| sqlc | 1.31.1 | `tools.mod`; update with `go get -modfile=tools.mod -tool github.com/sqlc-dev/sqlc/cmd/sqlc@<version>` |
| golangci-lint | 2.15.0 | `golangci_lint_version` and the checksums in the `justfile` |
| just | 1.59.0 | `.github/workflows/ci.yml` |

The Go version in `go.mod` and the `golang:` image in the `Dockerfile` must match, and `just check` fails if they don't.
