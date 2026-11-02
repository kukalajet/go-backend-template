#!/usr/bin/env bash
# Exercises `just init` on a copy of the checkout. gonew only fetches published
# versions, so this applies the same rewrite to go.mod and Go imports by hand.
set -euo pipefail

template_module="github.com/kukalajet/go-backend-template"
new_module="example.com/init-test/my-project"
root="$(cd "$(dirname "$0")/.." && pwd)"

fail() {
	echo "test-init: $*" >&2
	exit 1
}

# A directory copy would hide what the module zip omits, so check it directly.
nested="$(cd "$root" && find . -mindepth 2 -type f -iname go.mod -not -path './.git/*')"
if [ -n "$nested" ]; then
	fail "nested go.mod files never reach new projects: $nested"
fi

workdir="$(mktemp -d)"
trap 'rm -rf "$workdir"' EXIT
project="$workdir/my-project"
mkdir "$project"
tar -C "$root" --exclude=.git --exclude=bin --exclude=.env -cf - . | tar -C "$project" -xf -
cd "$project"

go mod edit -module "$new_module"
find . -type f -name '*.go' -exec perl -pi -e "s{\"\Q$template_module\E/}{\"$new_module/}g" {} +

just init

for removed in LICENSE SPEC.md scripts/init scripts/test-init.sh; do
	[ ! -e "$removed" ] || fail "$removed should be deleted"
done
if grep -rn -e "$template_module" -e go-backend-template -e go_template -e template-only . --exclude-dir=bin; then
	fail "template names remain"
fi
grep -q "module $new_module" go.mod || fail "go.mod has the wrong module path"
grep -q "POSTGRES_DB=my_project" .env.example || fail ".env.example has the wrong database name"
grep -q "^name: my-project" docker-compose.yml || fail "Compose has the wrong project name"
if just --summary | tr ' ' '\n' | grep -qx -e init -e test-init; then
	fail "template recipes remain"
fi
if grep -q 'test-init' .github/workflows/ci.yml; then
	fail "the init CI job remains"
fi

just setup
just check

# Prove depguard still matches after the module path changed.
cat >internal/httpapi/forbidden.go <<'EOF'
package httpapi

import "github.com/jackc/pgx/v5"

var _ = pgx.ErrNoRows
EOF
if ./bin/golangci-lint run ./internal/httpapi/... >"$workdir/lint.log" 2>&1; then
	fail "depguard accepted a forbidden pgx import"
fi
grep -q depguard "$workdir/lint.log" || {
	cat "$workdir/lint.log" >&2
	fail "lint failed for a reason other than depguard"
}
rm internal/httpapi/forbidden.go

echo "test-init: passed"
