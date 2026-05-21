#!/usr/bin/env bash
# scripts/ci-local.sh — runs the PR-gate subset of CI on your local machine.
#
# Mirrors the lightweight jobs we still run on every PR (lint + frontend +
# test-sqlite + test-postgres optional) plus the security scanning that's
# now master-only in CI (govulncheck + npm audit; cheap enough that running
# it locally is worth catching dep CVEs before merge). The truly heavy jobs
# (race detection, docker build, Playwright E2E) live in scripts/ci-dev1.sh
# — run that against the Linux test box before pushing anything that risks
# dialect/race/E2E regressions.
#
# Usage:
#   scripts/ci-local.sh                      # run everything we can locally
#   scripts/ci-local.sh --no-frontend        # skip web build/tests
#   scripts/ci-local.sh --no-postgres        # skip postgres leg
#   scripts/ci-local.sh --no-security        # skip govulncheck + npm audit
#   SKIP_NPM_INSTALL=1 scripts/ci-local.sh   # reuse existing node_modules
#
# Exit codes:
#   0 — every step that ran passed
#   1 — at least one step failed (full list printed at the end)
#   2 — invalid invocation

set -uo pipefail

cd "$(dirname "$0")/.."

SKIP_FRONTEND=false
SKIP_POSTGRES=false
SKIP_SECURITY=false
for arg in "$@"; do
  case $arg in
    --no-frontend) SKIP_FRONTEND=true ;;
    --no-postgres) SKIP_POSTGRES=true ;;
    --no-security) SKIP_SECURITY=true ;;
    -h|--help)
      sed -n '2,22p' "$0"
      exit 0
      ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

failed=()
skipped=()

run() {
  local name=$1; shift
  echo ""
  echo "=== $name ==="
  if "$@"; then
    echo "[ok] $name"
  else
    echo "[fail] $name"
    failed+=("$name")
  fi
}

have() { command -v "$1" >/dev/null 2>&1; }

# --- Go checks ----------------------------------------------------------------

run "go vet" go vet ./...
run "go mod verify" go mod verify

if have golangci-lint; then
  run "golangci-lint" golangci-lint run ./...
else
  skipped+=("golangci-lint (not installed — go install github.com/golangci/golangci-lint/cmd/golangci-lint@v1.64.8)")
fi

if have swag; then
  run "swag drift check" bash -c '
    tmp=$(mktemp -d)
    trap "rm -rf $tmp" EXIT
    if ! swag init -g cmd/indelible/main.go -o "$tmp" --parseDependency >/dev/null; then
      echo "swag init failed"
      exit 1
    fi
    if ! diff -q docs/docs.go "$tmp/docs.go" >/dev/null 2>&1; then
      echo "docs/ is out of date. To regenerate:"
      echo "  swag init -g cmd/indelible/main.go -o docs/ --parseDependency"
      exit 1
    fi
  '
else
  skipped+=("swag drift check (not installed — go install github.com/swaggo/swag/cmd/swag@v1.16.6)")
fi

# --- Frontend -----------------------------------------------------------------

if [ "$SKIP_FRONTEND" = "true" ]; then
  skipped+=("frontend (--no-frontend)")
else
  if [ "${SKIP_NPM_INSTALL:-}" != "1" ]; then
    run "npm ci (web)" bash -c 'cd web && npm ci --silent'
  fi
  run "web type-check + build" bash -c 'cd web && npm run build'
  run "web unit tests" bash -c 'cd web && npm run test:unit'
fi

# --- Go tests -----------------------------------------------------------------

run "go test (sqlite)" go test -count=1 -timeout 5m ./...

if [ "$SKIP_POSTGRES" = "true" ]; then
  skipped+=("postgres tests (--no-postgres)")
elif [ -n "${INDELIBLE_TEST_DB_URL:-}" ]; then
  run "go test (postgres)" go test -count=1 -timeout 5m ./...
else
  skipped+=("postgres tests (set INDELIBLE_TEST_DB_URL=postgres://... or use scripts/ci-dev1.sh)")
fi

# --- Security scanning --------------------------------------------------------
# Mirrors the master-only `security` CI job. Cheap to run (~1-2min), and
# catches dep CVEs / known-vulnerable function calls before they land on master.

if [ "$SKIP_SECURITY" = "true" ]; then
  skipped+=("security scanning (--no-security)")
else
  if have govulncheck; then
    run "govulncheck" govulncheck ./...
  else
    skipped+=("govulncheck (not installed — go install golang.org/x/vuln/cmd/govulncheck@latest)")
  fi

  # `npm audit` only makes sense if node_modules exists; otherwise the registry
  # call still works but the diagnostic is less useful. Skip when frontend was
  # explicitly skipped (no install ran).
  if [ "$SKIP_FRONTEND" = "true" ]; then
    skipped+=("npm audit (frontend skipped)")
  else
    run "npm audit (web)" bash -c 'cd web && npm audit --audit-level=high'
  fi
fi

# --- Summary ------------------------------------------------------------------

echo ""
echo "======================="
if [ ${#skipped[@]} -gt 0 ]; then
  echo "Skipped:"
  for s in "${skipped[@]}"; do echo "  - $s"; done
fi
if [ ${#failed[@]} -eq 0 ]; then
  echo "[ok] All local checks passed."
  exit 0
fi
echo "[fail] Failures: ${failed[*]}"
exit 1
