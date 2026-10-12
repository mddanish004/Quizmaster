#!/usr/bin/env bash
# End-to-end runner: migrates a disposable database, starts the API + Vite dev
# server (with /api + /ws proxying), executes Playwright, then tears down.
# Usage: ./scripts/e2e.sh [--headed] [--spec auth|quiz|ai|live]
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
E2E_DB="${E2E_DATABASE_URL:-postgres://quizforge:quizforge@localhost:5432/quizforge_e2e?sslmode=disable}"
API_PORT="${API_PORT:-8080}"
WEB_PORT="${WEB_PORT:-5173}"
HEADED=()
SPEC=""

for arg in "$@"; do
  case "$arg" in
    --headed) HEADED=(--headed) ;;
    --spec) shift ;;
    --spec=*) SPEC="${arg#--spec=}" ;;
    *) SPEC="$arg" ;;
  esac
done

echo "[e2e] resetting disposable database"
sudo -u postgres psql -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='quizforge_e2e' AND pid<>pg_backend_pid();" >/dev/null 2>&1 || true
sudo -u postgres psql -c "DROP DATABASE IF EXISTS quizforge_e2e;" -c "CREATE DATABASE quizforge_e2e OWNER quizforge;" >/dev/null
echo "[e2e] migrating disposable database"
for f in "$ROOT"/database/migrations/*.up.sql; do
  PGPASSWORD=quizforge psql -h localhost -U quizforge -d quizforge_e2e -v ON_ERROR_STOP=1 -q -f "$f" >/dev/null
done

reap_port() {
  for pid in $(ss -ltnp 2>/dev/null | grep "127.0.0.1:$1" | grep -oP 'pid=\K[0-9]+' | sort -u); do
    kill "$pid" 2>/dev/null || true
  done
}

echo "[e2e] reclaiming ports $API_PORT/$WEB_PORT"
reap_port "$API_PORT"
reap_port "$WEB_PORT"
sleep 1

echo "[e2e] starting API on :$API_PORT"
cd "$ROOT/backend"
go build -o /tmp/qf_e2e_api ./cmd/api
DATABASE_URL="$E2E_DB" HTTP_ADDR="127.0.0.1:$API_PORT" APP_ENV=test \
  APP_ORIGIN="http://localhost:$WEB_PORT" CSRF_SECRET=test-csrf-secret-0123456789abcdef \
  MOCK_AI=true MOCK_OIDC=true MOCK_MAIL=true WS_ALLOWED_ORIGINS="http://localhost:$WEB_PORT" \
  RATE_LIMIT_ENABLED=false \
  nohup /tmp/qf_e2e_api > /tmp/qf_e2e_api.log 2>&1 &
API_PID=$!

echo "[e2e] starting Vite on :$WEB_PORT"
cd "$ROOT/frontend"
PORT="$WEB_PORT" nohup ./node_modules/.bin/vite --port "$WEB_PORT" --strictPort > /tmp/qf_e2e_web.log 2>&1 &
WEB_PID=$!

cleanup() {
  kill "$API_PID" "$WEB_PID" 2>/dev/null || true
  sleep 1
  # Reap any orphaned children the wrappers may have left behind.
  for port in "$API_PORT" "$WEB_PORT"; do
    for pid in $(ss -ltnp 2>/dev/null | grep "127.0.0.1:$port" | grep -oP 'pid=\K[0-9]+' | sort -u); do
      kill "$pid" 2>/dev/null || true
    done
  done
}
trap cleanup EXIT

for i in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:$API_PORT/health/ready" >/dev/null 2>&1 && break
  sleep 1
done
for i in $(seq 1 30); do
  curl -fsS "http://127.0.0.1:$WEB_PORT/" >/dev/null 2>&1 && break
  sleep 1
done
echo "[e2e] services up"

TARGET="e2e/"
if [ -n "$SPEC" ]; then TARGET="e2e/$SPEC.spec.ts"; fi
./node_modules/.bin/playwright test "$TARGET" "${HEADED[@]}"
