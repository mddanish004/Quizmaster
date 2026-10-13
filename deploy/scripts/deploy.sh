#!/usr/bin/env bash
# Production deploy driver (runs on the EC2 host via SSM Run Command).
# Usage: deploy.sh <api-digest> <web-digest> <migrate-digest>
# Env required: ECR_REGISTRY, MIGRATIONS_DATABASE_URL (direct Neon URL, from the
#   root-owned 0600 .env), DEPLOY_DIR. AWS_REGION defaults to ap-southeast-1.
set -euo pipefail

API_DIGEST="${1:?api digest required}"
WEB_DIGEST="${2:?web digest required}"
MIGRATE_DIGEST="${3:?migrate digest required}"
DEPLOY_DIR="${DEPLOY_DIR:-/opt/quizforge}"
ENV_FILE="$DEPLOY_DIR/.env"
DEPLOY_ENV="$DEPLOY_DIR/deploy.env"

echo "[env] materializing root-owned .env from SSM (names logged, values never)"
SSM_PATH="${SSM_PATH:-/quizforge/staging}"
REGION="${AWS_REGION:-ap-southeast-1}"
getp() {
  local name="$1" v rc
  if v=$(aws ssm get-parameter --region "$REGION" --name "$SSM_PATH/$name" \
    --with-decryption --query 'Parameter.Value' --output text 2>/tmp/getp.err); then
    :
  else
    rc=$?
    echo "[env] FATAL: cannot read $SSM_PATH/$name (aws exit $rc): $(head -c 200 /tmp/getp.err)" >&2
    exit 1
  fi
  if [ -z "$v" ] || [ "$v" = "None" ]; then
    echo "[env] FATAL: $SSM_PATH/$name returned empty" >&2
    exit 1
  fi
  # getp is called through command substitution. Keep diagnostics on stderr so
  # stdout contains only the parameter value assigned to the caller.
  echo "[env] got $name (${#v} chars)" >&2
  printf '%s' "$v"
}
APP_DOMAIN="$(getp APP_DOMAIN)"
case "$APP_DOMAIN" in
  ""|"UNSET"*) echo "[env] FATAL: $SSM_PATH/APP_DOMAIN is unset" >&2; exit 1;;
esac
DATABASE_URL="$(getp DATABASE_URL)"
MIGRATIONS_DATABASE_URL="$(getp MIGRATIONS_DATABASE_URL)"
CSRF_SECRET="$(getp CSRF_SECRET)"
PASSWORD_PEPPER="$(getp PASSWORD_PEPPER)"
GOOGLE_CLIENT_ID="$(getp GOOGLE_CLIENT_ID)"
GOOGLE_CLIENT_SECRET="$(getp GOOGLE_CLIENT_SECRET)"
HF_TOKEN="$(getp HF_TOKEN)"
umask 077
# Single-quote every value (escaping embedded quotes) so arbitrary secret
# characters can never break shell sourcing of this file (S5-17).
sq() { printf "'%s'" "$(printf '%s' "$1" | sed "s/'/'\\\\''/g")"; }
{
  printf 'APP_ENV=production\n'
  printf 'APP_DOMAIN=%s\n' "$(sq "$APP_DOMAIN")"
  printf 'APP_ORIGIN=%s\n' "$(sq "https://$APP_DOMAIN")"
  printf 'HTTP_ADDR=:8080\n'
  printf 'TRUSTED_PROXY_CIDRS=172.16.0.0/12\n'
  printf 'DB_MAX_CONNS=10\n'
  printf 'GOOGLE_REDIRECT_URL=%s\n' "$(sq "https://$APP_DOMAIN/api/v1/auth/google/callback")"
  printf 'WS_ALLOWED_ORIGINS=%s\n' "$(sq "https://$APP_DOMAIN")"
  printf 'MAIL_PROVIDER=mock\nMOCK_AI=false\nMOCK_OIDC=false\nMOCK_MAIL=false\n'
  printf 'DATABASE_URL=%s\n' "$(sq "$DATABASE_URL")"
  printf 'MIGRATIONS_DATABASE_URL=%s\n' "$(sq "$MIGRATIONS_DATABASE_URL")"
  printf 'CSRF_SECRET=%s\n' "$(sq "$CSRF_SECRET")"
  printf 'PASSWORD_PEPPER=%s\n' "$(sq "$PASSWORD_PEPPER")"
  printf 'GOOGLE_CLIENT_ID=%s\n' "$(sq "$GOOGLE_CLIENT_ID")"
  printf 'GOOGLE_CLIENT_SECRET=%s\n' "$(sq "$GOOGLE_CLIENT_SECRET")"
  printf 'HF_TOKEN=%s\n' "$(sq "$HF_TOKEN")"
} > "$ENV_FILE"
chmod 600 "$ENV_FILE"
echo "[env] wrote $ENV_FILE (0600)"

echo "[compose] fetching pinned compose file"
COMPOSE_REPO="${COMPOSE_REPO:-mddanish004/Quizmaster}"
COMPOSE_REF="${COMPOSE_REF:?COMPOSE_REF (commit SHA) required}"
curl -fsSL --max-time 60 \
  "https://raw.githubusercontent.com/${COMPOSE_REPO}/${COMPOSE_REF}/deploy/compose.prod.yaml" \
  -o "$DEPLOY_DIR/compose.prod.yaml"

echo "[preflight] checking env file permissions"
test -f "$ENV_FILE"
test "$(stat -c %a "$ENV_FILE")" = "600"

# Retain current release for rollback BEFORE switching (see ROLLBACK.md).
if [ -f "$DEPLOY_ENV" ]; then
  # shellcheck disable=SC1090
  set -a; . "$DEPLOY_ENV"; set +a
  if [ -n "${IMAGE_API_DIGEST:-}" ]; then
    printf 'IMAGE_API_DIGEST=%s\nIMAGE_WEB_DIGEST=%s\n' \
      "$IMAGE_API_DIGEST" "${IMAGE_WEB_DIGEST:-unknown}" > "$DEPLOY_DIR/previous.env"
    echo "[rollback] previous digests saved to previous.env"
  fi
fi

echo "[ecr] authenticating"
aws ecr get-login-password --region "${AWS_REGION:-ap-southeast-1}" \
  | docker login --username AWS --password-stdin "${ECR_REGISTRY:?ECR_REGISTRY required}"

export ECR_REGISTRY IMAGE_API_DIGEST="$API_DIGEST" IMAGE_WEB_DIGEST="$WEB_DIGEST"
export AWS_REGION="${AWS_REGION:-ap-southeast-1}"
export CW_LOG_GROUP_API="${CW_LOG_GROUP_API:-/quizforge/staging/api}"
export CW_LOG_GROUP_WEB="${CW_LOG_GROUP_WEB:-/quizforge/staging/web}"

# Non-secret release pointer (0644): consumed by compose + janitor timer.
# NEVER write .env values here.
cat > "$DEPLOY_ENV" <<EOF
ECR_REGISTRY=$ECR_REGISTRY
IMAGE_API_DIGEST=$IMAGE_API_DIGEST
IMAGE_WEB_DIGEST=$IMAGE_WEB_DIGEST
AWS_REGION=$AWS_REGION
CW_LOG_GROUP_API=$CW_LOG_GROUP_API
CW_LOG_GROUP_WEB=$CW_LOG_GROUP_WEB
EOF
chmod 0644 "$DEPLOY_ENV"

echo "[migrate] running backward-compatible migrations"
# shellcheck disable=SC1090
set -a; . "$ENV_FILE"; set +a # host needs MIGRATIONS_DATABASE_URL (0600, root-only, never echoed)
# golang-migrate registers the driver as "postgres"; Neon issues
# "postgresql://" URLs, which it rejects with "unknown driver" (S5-18).
MIGRATE_URL="${MIGRATIONS_DATABASE_URL/#postgresql:/postgres:}"
docker run --rm \
  "${ECR_REGISTRY}/quizforge-migrate@${MIGRATE_DIGEST}" \
  -path /migrations -database "${MIGRATE_URL:?}" up

echo "[deploy] pulling + recreating services"
docker compose -f "$DEPLOY_DIR/compose.prod.yaml" pull
docker compose -f "$DEPLOY_DIR/compose.prod.yaml" up -d --remove-orphans

echo "[janitor] installing daily retention timer"
SYSTEMD_DIR="${SYSTEMD_DIR:-/etc/systemd/system}"
cat > "$SYSTEMD_DIR/quizforge-janitor.service" <<EOF
[Unit]
Description=QuizForge retention janitor
After=docker.service
[Service]
Type=oneshot
EnvironmentFile=$DEPLOY_ENV
ExecStart=/usr/bin/docker run --rm --env-file $ENV_FILE \${ECR_REGISTRY}/quizforge-api@\${IMAGE_API_DIGEST} /quizforge janitor
EOF
cat > "$SYSTEMD_DIR/quizforge-janitor.timer" <<EOF
[Unit]
Description=QuizForge retention janitor (daily)
[Timer]
OnCalendar=daily
Persistent=true
[Install]
WantedBy=timers.target
EOF
systemctl daemon-reload
systemctl enable --now quizforge-janitor.timer

echo "[verify] waiting for readiness"
for i in $(seq 1 24); do
  if curl -fsS http://127.0.0.1:8080/health/ready >/dev/null 2>&1; then
    echo "[verify] ready"
    break
  fi
  sleep 5
  if [ "$i" = "24" ]; then echo "[verify] FAILED"; exit 1; fi
done

echo "[verify] build identity"
curl -fsS http://127.0.0.1:8080/version
echo
echo "$API_DIGEST $WEB_DIGEST $(date -u +%FT%TZ)" >> "$DEPLOY_DIR/deploys.log"
echo "[deploy] complete — previous digests retained in $DEPLOY_DIR/previous.env for rollback"
