#!/usr/bin/env bash
# Manual logical backup to a local file. Neon already keeps point-in-time
# history; use this before risky operations (e.g. migrate down) or to keep
# an off-site copy: run it anywhere with psql/pg_dump installed and upload
# the artifact wherever you keep backups.
set -euo pipefail

: "${MIGRATIONS_DATABASE_URL:?direct Neon URL required}"

STAMP="$(date -u +%Y%m%dT%H%M%SZ)"
OUT="${BACKUP_FILE:-quizforge-$STAMP.dump}"
pg_dump --format=custom --no-owner --file="$OUT" "$MIGRATIONS_DATABASE_URL"
echo "backup ok: $OUT"
echo "restore with: pg_restore --no-owner -d \"\$MIGRATIONS_DATABASE_URL\" $OUT"
