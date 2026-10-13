#!/usr/bin/env bash
# Regression test for deploy.sh's SSM materialization and migration invocation.
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)
TEST_DIR=$(mktemp -d)
cleanup() {
  rc=$?
  if [ "$rc" -ne 0 ]; then
    echo "deploy regression test failed (artifacts: $TEST_DIR)" >&2
    for output in stdout stderr; do
      if [ -f "$TEST_DIR/$output" ]; then
        echo "--- $output ---" >&2
        sed -n '1,240p' "$TEST_DIR/$output" >&2
      fi
    done
  fi
  rm -rf "$TEST_DIR"
  exit "$rc"
}
trap cleanup EXIT
mkdir -p "$TEST_DIR/bin" "$TEST_DIR/deploy" "$TEST_DIR/systemd"

cat > "$TEST_DIR/bin/aws" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ "${1:-}" = "ssm" ] && [ "${2:-}" = "get-parameter" ]; then
  name=""
  while [ "$#" -gt 0 ]; do
    if [ "$1" = "--name" ]; then name="$2"; break; fi
    shift
  done
  case "${name##*/}" in
    APP_DOMAIN) printf '%s' 'quiz.example.com' ;;
    DATABASE_URL) printf '%s' 'postgresql://runtime:secret@db.example.com/app?sslmode=require' ;;
    MIGRATIONS_DATABASE_URL) printf '%s' 'postgresql://migrator:p@ss(word)@db.example.com/app?sslmode=require' ;;
    CSRF_SECRET) printf '%s' 'csrf-secret-value-0123456789' ;;
    PASSWORD_PEPPER) printf '%s' "pepper'with-quote" ;;
    GOOGLE_CLIENT_ID) printf '%s' 'google-client-id' ;;
    GOOGLE_CLIENT_SECRET) printf '%s' 'google-client-secret' ;;
    HF_TOKEN) printf '%s' 'hf_test-token' ;;
    *) exit 2 ;;
  esac
elif [ "${1:-}" = "ecr" ] && [ "${2:-}" = "get-login-password" ]; then
  printf '%s' 'login-password'
else
  exit 2
fi
EOF

cat > "$TEST_DIR/bin/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
output=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-o" ]; then output="$2"; shift 2; continue; fi
  shift
done
if [ -n "$output" ]; then cp "$COMPOSE_FIXTURE" "$output"; fi
EOF

cat > "$TEST_DIR/bin/docker" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf '%s\0' "$@" >> "$DOCKER_ARGS_LOG"
if [ "${1:-}" = "login" ]; then
  cat >/dev/null
fi
EOF

cat > "$TEST_DIR/bin/systemctl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
chmod +x "$TEST_DIR/bin/"*

DOCKER_ARGS_LOG="$TEST_DIR/docker.args" \
COMPOSE_FIXTURE="$ROOT_DIR/deploy/compose.prod.yaml" \
PATH="$TEST_DIR/bin:$PATH" \
DEPLOY_DIR="$TEST_DIR/deploy" \
ECR_REGISTRY="123456789012.dkr.ecr.ap-southeast-1.amazonaws.com" \
COMPOSE_REF="0123456789abcdef" \
SSM_PATH="/quizforge/test" \
AWS_REGION="ap-southeast-1" \
CW_LOG_GROUP_API="/quizforge/test/api" \
CW_LOG_GROUP_WEB="/quizforge/test/web" \
SYSTEMD_DIR="$TEST_DIR/systemd" \
bash "$ROOT_DIR/deploy/scripts/deploy.sh" \
  'sha256:api-1' 'sha256:web-1' 'sha256:migrate-1' \
  > "$TEST_DIR/stdout" 2> "$TEST_DIR/stderr"

# shellcheck disable=SC1090
set -a; . "$TEST_DIR/deploy/.env"; set +a
test "$APP_DOMAIN" = 'quiz.example.com'
test "$MIGRATIONS_DATABASE_URL" = 'postgresql://migrator:p@ss(word)@db.example.com/app?sslmode=require'
test "$PASSWORD_PEPPER" = "pepper'with-quote"
test "$(stat -c %a "$TEST_DIR/deploy/.env")" = '600'

python3 - "$TEST_DIR/docker.args" <<'PY'
import pathlib
import sys

args = pathlib.Path(sys.argv[1]).read_bytes().split(b"\0")
expected = b"postgres://migrator:p@ss(word)@db.example.com/app?sslmode=require"
assert expected in args, args
assert not any(arg.startswith(b"[env] got") for arg in args), args
PY

grep -F '[env] got MIGRATIONS_DATABASE_URL (' "$TEST_DIR/stderr" >/dev/null
if grep -F '[env] got' "$TEST_DIR/deploy/.env" >/dev/null; then
  echo 'SSM diagnostic leaked into .env' >&2
  exit 1
fi
echo 'deploy regression test: PASS'
