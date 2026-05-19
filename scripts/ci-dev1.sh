#!/usr/bin/env bash
# scripts/ci-dev1.sh — runs the heavy CI matrix on a Linux test box (dev1)
# before pushing, so we don't have to spend GitHub Actions minutes
# discovering regressions.
#
# The remote runs whichever subset of {postgres tests, race detection,
# docker build+smoke, Playwright E2E} you ask for, against the current
# commit on this branch. Your branch is pushed to origin first so dev1 can
# fetch it cleanly without scp/rsync.
#
# Setup expectations on dev1 (one-time, done in advance):
#   - Repo cloned at $DEV1_PATH (default ~/work/indelible) with `origin`
#     pointing at WithAutonomi/indelible.
#   - Go 1.25+, Node 22+, Docker, curl, openssl installed and on PATH.
#   - SSH access reachable via $DEV1_HOST (default "dev1"; typically a Host
#     alias in your ~/.ssh/config).
#
# Usage:
#   scripts/ci-dev1.sh                       # everything
#   scripts/ci-dev1.sh --only race           # only race detection
#   scripts/ci-dev1.sh --skip e2e            # skip Playwright
#   DEV1_HOST=mybox scripts/ci-dev1.sh       # different SSH alias
#   DEV1_PATH=~/code/indelible scripts/ci-dev1.sh
#
# Exit codes:
#   0 — every requested step passed
#   1 — at least one failed
#   2 — invalid invocation

set -uo pipefail

DEV1_HOST="${DEV1_HOST:-dev1}"
DEV1_PATH="${DEV1_PATH:-~/work/indelible}"

# Steps: postgres (test only), race (sqlite + postgres), docker (build +
# smoke), e2e (playwright). Default = all.
STEPS="postgres race docker e2e"
MODE="all"

while [ $# -gt 0 ]; do
  case $1 in
    --only)
      MODE="only"
      shift
      STEPS=""
      while [ $# -gt 0 ] && [[ "$1" != --* ]]; do
        STEPS="$STEPS $1"
        shift
      done
      ;;
    --skip)
      MODE="skip"
      shift
      SKIP_LIST=""
      while [ $# -gt 0 ] && [[ "$1" != --* ]]; do
        SKIP_LIST="$SKIP_LIST $1"
        shift
      done
      for s in $SKIP_LIST; do
        STEPS=$(echo "$STEPS" | sed "s/\\b$s\\b//")
      done
      ;;
    -h|--help)
      sed -n '2,32p' "$0"
      exit 0
      ;;
    *) echo "unknown flag: $1" >&2; exit 2 ;;
  esac
done

cd "$(dirname "$0")/.."

BRANCH="$(git rev-parse --abbrev-ref HEAD)"
SHA="$(git rev-parse HEAD)"
if [ "$BRANCH" = "HEAD" ]; then
  echo "Refusing to run on detached HEAD — checkout a branch first." >&2
  exit 2
fi

# Warn (don't block) if working tree is dirty — the remote will only see
# what's pushed to origin.
if ! git diff-index --quiet HEAD --; then
  echo "warning: working tree has uncommitted changes; only committed work will run on $DEV1_HOST"
fi

echo "→ branch:  $BRANCH"
echo "→ commit:  $SHA"
echo "→ remote:  $DEV1_HOST:$DEV1_PATH"
echo "→ steps:   $STEPS"
echo ""
echo "→ Pushing $BRANCH to origin so $DEV1_HOST can fetch..."
git push origin "$BRANCH"

echo ""
echo "→ Streaming heavy CI from $DEV1_HOST..."
echo ""

# Feed the remote script over stdin. The remote receives positional args
# from the ssh command after `bash -s --`.
ssh "$DEV1_HOST" bash -s -- "$DEV1_PATH" "$BRANCH" "$SHA" "$STEPS" <<'REMOTE'
set -uo pipefail
DEV1_PATH=$1; BRANCH=$2; SHA=$3; STEPS=$4

cd "$(eval echo "$DEV1_PATH")" || {
  echo "Cannot cd to $DEV1_PATH — does the repo exist there?" >&2
  exit 2
}

echo "→ fetching $BRANCH from origin..."
git fetch origin "$BRANCH"
git checkout -B "$BRANCH" "origin/$BRANCH"
git reset --hard "$SHA"

failed=()
have_step() { [[ " $STEPS " == *" $1 "* ]]; }

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

PG_CONTAINER=indelible-dev1-pg
SMOKE_CONTAINER=indelible-dev1-smoke

start_pg() {
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
  docker run -d --name "$PG_CONTAINER" -p 5433:5432 \
    -e POSTGRES_PASSWORD=ci postgres:16-alpine >/dev/null
  for _ in $(seq 1 30); do
    if docker exec "$PG_CONTAINER" pg_isready -U postgres >/dev/null 2>&1; then
      return 0
    fi
    sleep 1
  done
  echo "postgres did not become ready" >&2
  return 1
}

stop_pg() {
  docker rm -f "$PG_CONTAINER" >/dev/null 2>&1 || true
}

trap 'stop_pg; docker rm -f "$SMOKE_CONTAINER" >/dev/null 2>&1 || true' EXIT

# Postgres tests
if have_step postgres; then
  if start_pg; then
    INDELIBLE_TEST_DB_URL="postgres://postgres:ci@localhost:5433/postgres?sslmode=disable" \
      run "Test (postgres)" go test -count=1 -timeout 5m ./...
  else
    failed+=("Test (postgres) [pg startup failed]")
  fi
fi

# Race detection — sqlite, then postgres if requested with postgres
if have_step race; then
  run "Race detection (sqlite)" go test -race -count=1 -timeout 15m ./...
  if have_step postgres; then
    # Reuse running pg container if postgres ran first; otherwise start one.
    if ! docker ps --format '{{.Names}}' | grep -q "^$PG_CONTAINER$"; then
      start_pg || true
    fi
    if docker ps --format '{{.Names}}' | grep -q "^$PG_CONTAINER$"; then
      INDELIBLE_TEST_DB_URL="postgres://postgres:ci@localhost:5433/postgres?sslmode=disable" \
        run "Race detection (postgres)" go test -race -count=1 -timeout 15m ./...
    fi
  fi
fi

# Docker build + smoke
if have_step docker; then
  run "Docker build" docker build -t indelible:dev1-ci .
  run "Docker smoke (container starts + /health 200 + non-root)" bash -c '
    docker rm -f '"$SMOKE_CONTAINER"' >/dev/null 2>&1 || true
    JWT_SECRET=$(openssl rand -hex 32)
    WALLET_KEY=$(openssl rand -hex 32)
    docker run --rm -d --name '"$SMOKE_CONTAINER"' -p 18080:8080 \
      -e INDELIBLE_JWT_SECRET="$JWT_SECRET" \
      -e INDELIBLE_WALLET_ENCRYPTION_KEY="$WALLET_KEY" \
      indelible:dev1-ci >/dev/null
    for i in $(seq 1 30); do
      status=$(docker inspect -f "{{.State.Health.Status}}" '"$SMOKE_CONTAINER"' 2>/dev/null || echo starting)
      if [ "$status" = "healthy" ]; then break; fi
      if [ "$status" = "unhealthy" ]; then echo "unhealthy"; exit 1; fi
      sleep 2
    done
    curl -fsS http://localhost:18080/health >/dev/null
    uid=$(docker exec '"$SMOKE_CONTAINER"' id -u)
    [ "$uid" = "65532" ] || { echo "expected uid 65532, got $uid"; exit 1; }
  '
fi

# Playwright E2E (sqlite in-memory)
if have_step e2e; then
  run "Frontend build" bash -c 'cd web && npm ci --silent && npm run build'
  run "Server build" go build -o bin/indelible-test ./cmd/indelible
  run "Playwright E2E" bash -c '
    cd e2e && npm ci --silent
    npx playwright install --with-deps chromium >/dev/null
    INDELIBLE_DB_URL="sqlite://:memory:" \
    INDELIBLE_JWT_SECRET="e2e-ci-test-secret-minimum-32-characters" \
    INDELIBLE_WALLET_ENCRYPTION_KEY="aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaab" \
    INDELIBLE_DATA_DIR="/tmp/indelible-dev1-e2e" \
    npx playwright test
  '
fi

echo ""
echo "======================="
if [ ${#failed[@]} -eq 0 ]; then
  echo "[ok] All requested heavy checks passed on dev1."
  exit 0
fi
echo "[fail] Failures: ${failed[*]}"
exit 1
REMOTE
