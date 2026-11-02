set dotenv-load

golangci_lint_version := "2.15.0"

# template-only:begin
init:
    go run ./scripts/init

test-init:
    bash scripts/test-init.sh

# template-only:end
setup:
    #!/usr/bin/env bash
    set -euo pipefail
    go mod download
    version="{{golangci_lint_version}}"
    if [ -x bin/golangci-lint ] && bin/golangci-lint version --short 2>/dev/null | grep -qx "$version"; then
        exit 0
    fi
    platform="$(uname -s | tr '[:upper:]' '[:lower:]')-$(uname -m)"
    case "$platform" in
        darwin-x86_64) arch=darwin-amd64; sum=767b899a28285e88ded2823d5dcc4bc5ef681173eb2d80f029a7b17ee4ece3b8 ;;
        darwin-arm64) arch=darwin-arm64; sum=d1e409481a8ca846c4bc97e781eb5cb1bd5c23353a0b3977a1cbda1b1b6447d5 ;;
        linux-x86_64) arch=linux-amd64; sum=9b7fe6a6bf6334b98decf8d86b11415ea7889afdabdd1959fdce23af2a9ebcea ;;
        linux-aarch64) arch=linux-arm64; sum=b4d8f17068b35ee6060cc2f69a16fb48740d706fbba81661c318649c114a6998 ;;
        *) echo "unsupported platform: $platform" >&2; exit 1 ;;
    esac
    name="golangci-lint-$version-$arch"
    tmp="$(mktemp -d)"
    trap 'rm -rf "$tmp"' EXIT
    curl -fsSL -o "$tmp/$name.tar.gz" "https://github.com/golangci/golangci-lint/releases/download/v$version/$name.tar.gz"
    echo "$sum  $tmp/$name.tar.gz" | shasum -a 256 -c - >/dev/null
    tar -xzf "$tmp/$name.tar.gz" -C "$tmp"
    mkdir -p bin
    mv "$tmp/$name/golangci-lint" bin/golangci-lint

dev-api:
    go run ./cmd/api

dev-worker:
    go run ./cmd/worker

sample-worker:
    go run ./cmd/worker -once

db-new name:
    go run ./cmd/migrate create {{name}}

db-migrate:
    go run ./cmd/migrate up

db-status:
    go run ./cmd/migrate status

generate:
    go tool -modfile=tools.mod sqlc generate

check: check-go-version
    ./bin/golangci-lint fmt --diff
    ./bin/golangci-lint run
    go vet -tags integration ./...
    go mod tidy -diff
    go tool -modfile=tools.mod sqlc diff
    go test -race ./...

[private]
check-go-version:
    #!/usr/bin/env bash
    set -euo pipefail
    version="$(awk '$1 == "go" { print $2 }' go.mod)"
    if ! grep -q "^FROM golang:$version-alpine@" Dockerfile; then
        echo "the Dockerfile's golang image must match go $version in go.mod" >&2
        exit 1
    fi

test-integration:
    docker compose up -d --wait postgresql
    go test -race -tags integration ./internal/postgres/...

smoke:
    bash scripts/smoke-docker.sh
