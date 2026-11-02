#!/usr/bin/env bash
# End-to-end check in an isolated Compose project. It uses docker-compose.yml
# only, so it can't collide with a running dev stack, and it removes only its own
# containers and volume.
set -euo pipefail

cd "$(dirname "$0")/.."

export API_PORT="${SMOKE_API_PORT:-18000}"
export WORKER_POLL_INTERVAL=1s
project="smoke-$$"
base_url="http://127.0.0.1:$API_PORT"
email="smoke-$$@example.com"

compose() {
	docker compose -p "$project" -f docker-compose.yml --env-file .env.example --profile app --profile tools "$@"
}

cleanup() {
	compose down --volumes --remove-orphans --timeout 5 >/dev/null 2>&1 || true
}
trap cleanup EXIT

fail() {
	echo "smoke: $*" >&2
	compose logs --no-color --tail 50 >&2 || true
	exit 1
}

status_of() {
	curl -s -o /dev/null -w '%{http_code}' "$base_url$1"
}

expect_status() {
	local got
	got="$(status_of "$1")"
	[ "$got" = "$2" ] || fail "GET $1 returned $got, want $2"
}

echo "smoke: 1. stack starts, API behaves"
compose build
docker compose -p "$project" -f docker-compose.yml --env-file .env.example --profile app up -d --wait
expect_status /health/live 200
expect_status /health/ready 200
go run ./scripts/smoke-api -base-url "$base_url" -email "$email"

echo "smoke: 2. one-shot worker and repeat migration"
compose run --rm worker-once
compose run --rm migrate

echo "smoke: 3. database down"
compose stop postgresql
expect_status /health/live 200
expect_status /health/ready 503
expect_status /users 503

failed=""
for _ in $(seq 1 30); do
	if compose logs --no-color worker | grep -q "Worker pass failed"; then
		failed=yes
		break
	fi
	sleep 1
done
[ -n "$failed" ] || fail "the worker never logged a failed pass"
worker_id="$(compose ps -q worker)"
[ "$(docker inspect -f '{{.RestartCount}}' "$worker_id")" = "0" ] || fail "the worker restarted"
[ "$(docker inspect -f '{{.State.Running}}' "$worker_id")" = "true" ] || fail "the worker stopped"

echo "smoke: 4. API starts without the database"
docker compose -p "$project" -f docker-compose.yml --env-file .env.example --profile app up -d --no-deps --force-recreate --wait api
expect_status /health/live 200
expect_status /health/ready 503

echo "smoke: 5. data survives container recreation"
compose rm -f postgresql
docker compose -p "$project" -f docker-compose.yml --env-file .env.example up -d --wait postgresql
docker compose -p "$project" -f docker-compose.yml --env-file .env.example --profile app up -d --no-deps --force-recreate --wait api
expect_status /health/ready 200
curl -s "$base_url/users" | grep -q "$email" || fail "the smoke user is gone"

echo "smoke: passed"
